package backup

import (
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion  = 1
	maxBundleBytes = 256 << 20
	maxFileBytes   = 32 << 20
)

type Snapshotter interface {
	Snapshot(context.Context, string) error
}

type Manager struct {
	root        string
	profile     string
	snapshotter Snapshotter
	key         []byte
	now         func() time.Time
}

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Data   string `json:"data"`
}

type Bundle struct {
	SchemaVersion  int       `json:"schema_version"`
	CreatedAt      time.Time `json:"created_at"`
	Profile        string    `json:"profile"`
	Files          []File    `json:"files"`
	Excluded       []string  `json:"excluded"`
	ManifestSHA256 string    `json:"manifest_sha256"`
}

type Record struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
	Encrypted bool      `json:"encrypted"`
}

type Report struct {
	ID             string   `json:"id"`
	DryRun         bool     `json:"dry_run"`
	Compatible     bool     `json:"compatible"`
	SchemaVersion  int      `json:"schema_version"`
	Files          int      `json:"files"`
	Bytes          int64    `json:"bytes"`
	ChecksumsValid bool     `json:"checksums_valid"`
	Excluded       []string `json:"excluded"`
}

func New(root, profile string, snapshotter Snapshotter, keyFile string) (*Manager, error) {
	if root == "" || snapshotter == nil || (profile != "mock" && profile != "production") {
		return nil, errors.New("invalid backup configuration")
	}
	manager := &Manager{root: root, profile: profile, snapshotter: snapshotter, now: func() time.Time { return time.Now().UTC() }}
	if profile == "production" {
		payload, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("read production backup key: %w", err)
		}
		decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(payload)))
		if err != nil || len(decoded) != 32 {
			return nil, errors.New("production backup key must be a private base64-encoded 32-byte key")
		}
		info, err := os.Stat(keyFile)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("production backup key must be a private regular file")
		}
		manager.key = decoded
	}
	return manager, os.MkdirAll(filepath.Join(root, "backups"), 0o700)
}

func (m *Manager) Create(ctx context.Context) (Record, error) {
	id := "backup_" + m.now().Format("20060102T150405.000000000Z")
	staging, err := os.MkdirTemp(filepath.Join(m.root, "backups"), ".staging-")
	if err != nil {
		return Record{}, err
	}
	defer os.RemoveAll(staging)
	database := filepath.Join(staging, "controller.db")
	if err := m.snapshotter.Snapshot(ctx, database); err != nil {
		return Record{}, err
	}
	bundle := Bundle{SchemaVersion: SchemaVersion, CreatedAt: m.now(), Profile: m.profile, Excluded: []string{"worktrees/ (disposable)", "models/ (reproducible from manifests)"}}
	if err := m.addFile(&bundle, database, "database/controller.db"); err != nil {
		return Record{}, err
	}
	for _, directory := range []string{"artifacts", "openviking"} {
		base := filepath.Join(m.root, directory)
		_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry == nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			relative, relErr := filepath.Rel(m.root, path)
			if relErr != nil {
				return nil
			}
			if info, statErr := entry.Info(); statErr != nil || info.Size() > maxFileBytes {
				bundle.Excluded = append(bundle.Excluded, filepath.ToSlash(relative)+" (size policy)")
				return nil
			}
			if err := m.addFile(&bundle, path, filepath.ToSlash(relative)); err != nil {
				return err
			}
			return nil
		})
	}
	manifest := struct {
		SchemaVersion int       `json:"schema_version"`
		CreatedAt     time.Time `json:"created_at"`
		Profile       string    `json:"profile"`
		Files         []File    `json:"files"`
		Excluded      []string  `json:"excluded"`
	}{bundle.SchemaVersion, bundle.CreatedAt, bundle.Profile, append([]File(nil), bundle.Files...), bundle.Excluded}
	for index := range manifest.Files {
		manifest.Files[index].Data = ""
	}
	manifestBytes, _ := json.Marshal(manifest)
	sum := sha256.Sum256(manifestBytes)
	bundle.ManifestSHA256 = hex.EncodeToString(sum[:])
	plain, err := encode(bundle)
	if err != nil {
		return Record{}, err
	}
	if len(plain) > maxBundleBytes {
		return Record{}, errors.New("backup exceeds bounded bundle size")
	}
	payload := plain
	extension := ".json.gz"
	if len(m.key) == 32 {
		payload, err = encrypt(m.key, plain)
		extension = ".json.gz.aesgcm"
		if err != nil {
			return Record{}, err
		}
	}
	target := filepath.Join(m.root, "backups", id+extension)
	temporary, err := os.CreateTemp(filepath.Dir(target), ".backup-")
	if err != nil {
		return Record{}, err
	}
	name := temporary.Name()
	defer os.Remove(name)
	_ = temporary.Chmod(0o600)
	if _, err = temporary.Write(payload); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Record{}, err
	}
	if err := os.Rename(name, target); err != nil {
		return Record{}, err
	}
	archiveSum := sha256.Sum256(payload)
	return Record{ID: id, CreatedAt: bundle.CreatedAt, Bytes: int64(len(payload)), SHA256: hex.EncodeToString(archiveSum[:]), Encrypted: len(m.key) == 32}, nil
}

func (m *Manager) Validate(_ context.Context, id string) (Report, error) {
	path, err := m.resolve(id)
	if err != nil {
		return Report{}, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	if len(payload) > maxBundleBytes {
		return Report{}, errors.New("backup exceeds validation size limit")
	}
	if strings.HasSuffix(path, ".aesgcm") {
		if len(m.key) != 32 {
			return Report{}, errors.New("backup key is unavailable")
		}
		payload, err = decrypt(m.key, payload)
		if err != nil {
			return Report{}, err
		}
	}
	bundle, err := decode(payload)
	if err != nil {
		return Report{}, err
	}
	manifest := struct {
		SchemaVersion int       `json:"schema_version"`
		CreatedAt     time.Time `json:"created_at"`
		Profile       string    `json:"profile"`
		Files         []File    `json:"files"`
		Excluded      []string  `json:"excluded"`
	}{bundle.SchemaVersion, bundle.CreatedAt, bundle.Profile, append([]File(nil), bundle.Files...), bundle.Excluded}
	for index := range manifest.Files {
		manifest.Files[index].Data = ""
	}
	manifestBytes, _ := json.Marshal(manifest)
	manifestSum := sha256.Sum256(manifestBytes)
	if hex.EncodeToString(manifestSum[:]) != bundle.ManifestSHA256 {
		return Report{}, errors.New("backup manifest checksum mismatch")
	}
	var total int64
	for _, file := range bundle.Files {
		data, decodeErr := base64.StdEncoding.DecodeString(file.Data)
		if decodeErr != nil || int64(len(data)) != file.Bytes {
			return Report{}, errors.New("backup file encoding is invalid")
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			return Report{}, errors.New("backup checksum mismatch")
		}
		total += file.Bytes
	}
	return Report{ID: id, DryRun: true, Compatible: bundle.SchemaVersion == SchemaVersion, SchemaVersion: bundle.SchemaVersion, Files: len(bundle.Files), Bytes: total, ChecksumsValid: true, Excluded: bundle.Excluded}, nil
}

func (m *Manager) List(_ context.Context) ([]Record, error) {
	entries, err := os.ReadDir(filepath.Join(m.root, "backups"))
	if err != nil {
		return nil, err
	}
	records := []Record{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "backup_") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(m.root, "backups", entry.Name()))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(payload)
		id := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".json.gz.aesgcm"), ".json.gz")
		records = append(records, Record{ID: id, CreatedAt: info.ModTime().UTC(), Bytes: info.Size(), SHA256: hex.EncodeToString(sum[:]), Encrypted: strings.HasSuffix(entry.Name(), ".aesgcm")})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt.After(records[j].CreatedAt) })
	return records, nil
}

func (m *Manager) addFile(bundle *Bundle, source, name string) error {
	payload, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	bundle.Files = append(bundle.Files, File{Path: name, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(payload)), Data: base64.StdEncoding.EncodeToString(payload)})
	return nil
}
func (m *Manager) resolve(id string) (string, error) {
	if !strings.HasPrefix(id, "backup_") || strings.ContainsAny(id, `/\\`) {
		return "", errors.New("invalid backup id")
	}
	for _, suffix := range []string{".json.gz.aesgcm", ".json.gz"} {
		path := filepath.Join(m.root, "backups", id+suffix)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}
func encode(bundle Bundle) ([]byte, error) {
	var output strings.Builder
	writer := gzip.NewWriter(&output)
	if err := json.NewEncoder(writer).Encode(bundle); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return []byte(output.String()), nil
}
func decode(payload []byte) (Bundle, error) {
	reader, err := gzip.NewReader(strings.NewReader(string(payload)))
	if err != nil {
		return Bundle{}, err
	}
	defer reader.Close()
	var bundle Bundle
	err = json.NewDecoder(io.LimitReader(reader, maxBundleBytes+1)).Decode(&bundle)
	return bundle, err
}
func encrypt(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, plain, []byte("local-code-maintainer-backup-v1"))...), nil
}
func decrypt(key, payload []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(payload) < aead.NonceSize() {
		return nil, errors.New("encrypted backup is truncated")
	}
	return aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], []byte("local-code-maintainer-backup-v1"))
}
