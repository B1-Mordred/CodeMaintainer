package models

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	ManifestSchemaVersion = 1
	maxManifestBytes      = 64 << 10
	maxModelBytes         = int64(1 << 40)
)

var (
	manifestID = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	sha256Hex  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type Sampling struct {
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"top_p"`
	TopK        int     `json:"top_k"`
	MinP        float64 `json:"min_p"`
	Seed        int64   `json:"seed"`
}

type Manifest struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Role          string   `json:"role"`
	ModelFamily   string   `json:"model_family"`
	Filename      string   `json:"filename"`
	SHA256        string   `json:"sha256"`
	Bytes         int64    `json:"bytes"`
	SourceURI     string   `json:"source_uri"`
	License       string   `json:"license"`
	Quantization  string   `json:"quantization"`
	Context       int      `json:"context"`
	Threads       int      `json:"threads"`
	Batch         int      `json:"batch"`
	UBatch        int      `json:"ubatch"`
	NUMA          string   `json:"numa"`
	MinRAMBytes   int64    `json:"min_ram_bytes"`
	Sampling      Sampling `json:"sampling"`
}

type Catalog struct {
	profiles map[string]Manifest
}

func LoadCatalog(directory string) (*Catalog, error) {
	if !filepath.IsAbs(directory) {
		return nil, errors.New("model manifest directory must be absolute")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read model manifest directory: %w", err)
	}
	if len(entries) > 32 {
		return nil, errors.New("model manifest directory contains too many entries")
	}
	catalog := &Catalog{profiles: make(map[string]Manifest)}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
			return nil, fmt.Errorf("manifest %q must be a regular immutable file", entry.Name())
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		payload, readErr := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(payload) > maxManifestBytes {
			return nil, fmt.Errorf("manifest %q is unreadable or oversized", entry.Name())
		}
		manifest, err := decodeManifest(payload)
		if err != nil {
			return nil, fmt.Errorf("manifest %q: %w", entry.Name(), err)
		}
		if _, exists := catalog.profiles[manifest.ID]; exists {
			return nil, fmt.Errorf("duplicate model profile %q", manifest.ID)
		}
		catalog.profiles[manifest.ID] = manifest
	}
	if len(catalog.profiles) == 0 {
		return nil, errors.New("no valid model manifests found")
	}
	roleFamilies := make(map[string]string)
	for _, manifest := range catalog.profiles {
		if manifest.Role != "implementation" && manifest.Role != "qc" {
			continue
		}
		if _, exists := roleFamilies[manifest.Role]; exists {
			return nil, fmt.Errorf("model role %q must have exactly one default profile", manifest.Role)
		}
		roleFamilies[manifest.Role] = manifest.ModelFamily
	}
	if implementation, implementationExists := roleFamilies["implementation"]; implementationExists {
		if qc, qcExists := roleFamilies["qc"]; qcExists && implementation == qc {
			return nil, errors.New("implementation and QC defaults must use different model families")
		}
	}
	return catalog, nil
}

func decodeManifest(payload []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("manifest must contain exactly one JSON document")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	roles := map[string]bool{"implementation": true, "qc": true, "alternate": true, "deep_critic": true}
	numa := map[string]bool{"disabled": true, "distribute": true, "isolate": true, "numactl": true}
	parsedSource, err := url.Parse(m.SourceURI)
	if m.SchemaVersion != ManifestSchemaVersion || !manifestID.MatchString(m.ID) || !roles[m.Role] ||
		!manifestID.MatchString(m.ModelFamily) || m.Filename != filepath.Base(m.Filename) ||
		!strings.HasSuffix(strings.ToLower(m.Filename), ".gguf") || !sha256Hex.MatchString(m.SHA256) ||
		m.Bytes <= 0 || m.Bytes > maxModelBytes || err != nil || parsedSource.Scheme != "https" || parsedSource.Host == "" ||
		parsedSource.User != nil || m.License == "" || m.Quantization == "" || m.Context < 1024 || m.Context > 262144 ||
		m.Threads < 1 || m.Threads > 512 || m.Batch < 1 || m.Batch > 8192 || m.UBatch < 1 || m.UBatch > m.Batch ||
		!numa[m.NUMA] || m.MinRAMBytes <= 0 || m.MinRAMBytes > maxModelBytes ||
		m.Sampling.Temperature < 0 || m.Sampling.Temperature > 2 || m.Sampling.TopP <= 0 || m.Sampling.TopP > 1 ||
		m.Sampling.TopK < 0 || m.Sampling.TopK > 1000 || m.Sampling.MinP < 0 || m.Sampling.MinP > 1 {
		return errors.New("model manifest violates the version-one policy")
	}
	return nil
}

func (c *Catalog) Manifest(id string) (Manifest, bool) {
	if c == nil {
		return Manifest{}, false
	}
	manifest, ok := c.profiles[id]
	return manifest, ok
}

func (c *Catalog) Profiles() []Profile {
	result := make([]Profile, 0, len(c.profiles))
	for _, manifest := range c.profiles {
		result = append(result, Profile{
			ID: manifest.ID, Role: manifest.Role, ModelFamily: manifest.ModelFamily,
			Filename: manifest.Filename, SHA256: manifest.SHA256, Bytes: manifest.Bytes,
			SourceURI: manifest.SourceURI, License: manifest.License,
			Context: manifest.Context, Quantization: manifest.Quantization,
			Threads: manifest.Threads, Batch: manifest.Batch, UBatch: manifest.UBatch,
			NUMA: manifest.NUMA, MinRAMBytes: manifest.MinRAMBytes, Sampling: manifest.Sampling,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func VerifyInstalledModel(modelRoot string, manifest Manifest) (string, error) {
	if !filepath.IsAbs(modelRoot) {
		return "", errors.New("model root must be absolute")
	}
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Clean(modelRoot), manifest.Filename)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o222 != 0 || info.Size() != manifest.Bytes {
		return "", errors.New("model file is missing, mutable, symbolic, or has the wrong size")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	written, copyErr := io.Copy(digest, io.LimitReader(file, manifest.Bytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written != manifest.Bytes || hex.EncodeToString(digest.Sum(nil)) != manifest.SHA256 {
		return "", errors.New("model file failed SHA-256 verification")
	}
	return path, nil
}

func ImportModel(sourcePath, modelRoot string, manifest Manifest) (string, error) {
	if !filepath.IsAbs(sourcePath) || !filepath.IsAbs(modelRoot) {
		return "", errors.New("model import paths must be absolute")
	}
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil || !sourceInfo.Mode().IsRegular() || sourceInfo.Size() != manifest.Bytes {
		return "", errors.New("model import source must be a regular file with the declared size")
	}
	if err := os.MkdirAll(modelRoot, 0o700); err != nil {
		return "", err
	}
	return installVerifiedReader(sourcePath, modelRoot, manifest, func() (io.ReadCloser, error) { return os.Open(sourcePath) })
}

func DownloadModel(ctx context.Context, client *http.Client, modelRoot string, manifest Manifest) (string, error) {
	if client == nil || !filepath.IsAbs(modelRoot) {
		return "", errors.New("bounded HTTP client and absolute model root are required")
	}
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.SourceURI, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return "", fmt.Errorf("model download returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != manifest.Bytes {
		response.Body.Close()
		return "", errors.New("model download length differs from manifest")
	}
	if err := os.MkdirAll(modelRoot, 0o700); err != nil {
		response.Body.Close()
		return "", err
	}
	return installVerifiedReader(manifest.SourceURI, modelRoot, manifest, func() (io.ReadCloser, error) { return response.Body, nil })
}

func installVerifiedReader(source, modelRoot string, manifest Manifest, open func() (io.ReadCloser, error)) (string, error) {
	destination := filepath.Join(filepath.Clean(modelRoot), manifest.Filename)
	if _, err := os.Lstat(destination); err == nil || !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("model destination already exists or cannot be inspected")
	}
	input, err := open()
	if err != nil {
		return "", err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(modelRoot, ".model-import-*")
	if err != nil {
		return "", err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(input, manifest.Bytes+1))
	if copyErr != nil || written != manifest.Bytes || hex.EncodeToString(digest.Sum(nil)) != manifest.SHA256 {
		temporary.Close()
		return "", fmt.Errorf("model %s failed size or SHA-256 verification", source)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Chmod(0o444); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Link(temporaryName, destination); err != nil {
		return "", fmt.Errorf("publish model without overwrite: %w", err)
	}
	if _, err := VerifyInstalledModel(modelRoot, manifest); err != nil {
		os.Remove(destination)
		return "", err
	}
	return destination, nil
}
