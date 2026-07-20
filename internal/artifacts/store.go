package artifacts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/local-code-maintainer/appliance/internal/storage"
)

const (
	defaultMaxBytes  = int64(64 << 20)
	absoluteMaxBytes = int64(2 << 30)
	maxMetadataBytes = 64 << 10
)

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Store struct {
	root     string
	metadata storage.ArtifactStore
}

type PutRequest struct {
	JobID     string
	ProjectID string
	Kind      string
	MediaType string
	Producer  string
	Metadata  json.RawMessage
	Reader    io.Reader
	MaxBytes  int64
}

func New(root string, metadata storage.ArtifactStore) (*Store, error) {
	if !filepath.IsAbs(root) || metadata == nil {
		return nil, storage.ErrInvalid
	}
	root = filepath.Clean(root)
	for _, path := range []string{root, filepath.Join(root, "objects"), filepath.Join(root, "tmp")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, fmt.Errorf("create artifact directory: %w", err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return nil, fmt.Errorf("protect artifact directory: %w", err)
		}
	}
	return &Store{root: root, metadata: metadata}, nil
}

func (s *Store) Put(ctx context.Context, request PutRequest) (storage.ArtifactRecord, error) {
	if !safeName.MatchString(request.JobID) || !safeName.MatchString(request.ProjectID) ||
		!safeName.MatchString(request.Kind) || !safeName.MatchString(request.Producer) || request.Reader == nil ||
		!json.Valid(request.Metadata) || len(request.Metadata) > maxMetadataBytes {
		return storage.ArtifactRecord{}, storage.ErrInvalid
	}
	if _, _, err := mime.ParseMediaType(request.MediaType); err != nil {
		return storage.ArtifactRecord{}, storage.ErrInvalid
	}
	maximum := request.MaxBytes
	if maximum == 0 {
		maximum = defaultMaxBytes
	}
	if maximum < 1 || maximum > absoluteMaxBytes {
		return storage.ArtifactRecord{}, storage.ErrInvalid
	}

	temporary, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "incoming-*")
	if err != nil {
		return storage.ArtifactRecord{}, fmt.Errorf("create artifact temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(contextReader{ctx: ctx, source: request.Reader}, maximum+1))
	if copyErr != nil {
		temporary.Close()
		return storage.ArtifactRecord{}, fmt.Errorf("write artifact: %w", copyErr)
	}
	if written > maximum {
		temporary.Close()
		return storage.ArtifactRecord{}, fmt.Errorf("artifact exceeds %d bytes", maximum)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return storage.ArtifactRecord{}, fmt.Errorf("sync artifact: %w", err)
	}
	if err := temporary.Chmod(0o400); err != nil {
		temporary.Close()
		return storage.ArtifactRecord{}, fmt.Errorf("protect artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return storage.ArtifactRecord{}, fmt.Errorf("close artifact: %w", err)
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	relativePath := filepath.Join("objects", digest[:2], digest)
	objectPath := filepath.Join(s.root, relativePath)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o700); err != nil {
		return storage.ArtifactRecord{}, fmt.Errorf("create artifact shard: %w", err)
	}
	if err := os.Link(temporaryPath, objectPath); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return storage.ArtifactRecord{}, fmt.Errorf("publish artifact object: %w", err)
		}
		if err := verifyFile(objectPath, digest, written); err != nil {
			return storage.ArtifactRecord{}, fmt.Errorf("verify existing artifact object: %w", err)
		}
	}

	artifactID, err := newID()
	if err != nil {
		return storage.ArtifactRecord{}, err
	}
	record := storage.ArtifactRecord{
		ID: artifactID, JobID: request.JobID, ProjectID: request.ProjectID,
		ObjectSHA256: digest, Bytes: written, RelativePath: filepath.ToSlash(relativePath),
		Kind: request.Kind, MediaType: request.MediaType, Producer: request.Producer, Metadata: request.Metadata,
	}
	return s.metadata.IndexArtifact(ctx, record)
}

func (s *Store) Open(ctx context.Context, jobID, artifactID string) (storage.ArtifactRecord, io.ReadCloser, error) {
	record, err := s.metadata.GetArtifact(ctx, jobID, artifactID)
	if err != nil {
		return storage.ArtifactRecord{}, nil, err
	}
	path, err := s.objectPath(record.RelativePath)
	if err != nil {
		return storage.ArtifactRecord{}, nil, err
	}
	if err := verifyFile(path, record.ObjectSHA256, record.Bytes); err != nil {
		return storage.ArtifactRecord{}, nil, fmt.Errorf("artifact integrity check failed: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return storage.ArtifactRecord{}, nil, fmt.Errorf("open artifact: %w", err)
	}
	return record, file, nil
}

// StageInputs creates no-overwrite hardlinks for artifact associations already
// authorized to the job. runnerd mounts only these individual files, never the
// global object directory or a caller-derived object path.
func (s *Store) StageInputs(ctx context.Context, jobID string, artifactIDs []string) (string, error) {
	if !safeName.MatchString(jobID) || len(artifactIDs) == 0 || len(artifactIDs) > 32 {
		return "", storage.ErrInvalid
	}
	ids := append([]string(nil), artifactIDs...)
	sort.Strings(ids)
	directory := filepath.Join(s.root, "inputs", jobID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create artifact input stage: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("protect artifact input stage: %w", err)
	}
	previous := ""
	for _, artifactID := range ids {
		if !safeName.MatchString(artifactID) || artifactID == previous {
			return "", storage.ErrInvalid
		}
		previous = artifactID
		record, err := s.metadata.GetArtifact(ctx, jobID, artifactID)
		if err != nil {
			return "", err
		}
		objectPath, err := s.objectPath(record.RelativePath)
		if err != nil {
			return "", err
		}
		if err := verifyFile(objectPath, record.ObjectSHA256, record.Bytes); err != nil {
			return "", fmt.Errorf("verify staged artifact source: %w", err)
		}
		destination := filepath.Join(directory, artifactID)
		if err := os.Link(objectPath, destination); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return "", fmt.Errorf("stage artifact input: %w", err)
			}
			if err := verifyFile(destination, record.ObjectSHA256, record.Bytes); err != nil {
				return "", fmt.Errorf("verify existing staged artifact: %w", err)
			}
		}
	}
	return directory, nil
}

func (s *Store) objectPath(relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", storage.ErrInvalid
	}
	path := filepath.Join(s.root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", storage.ErrInvalid
	}
	return path, nil
}

func verifyFile(path, expectedHash string, expectedBytes int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return err
	}
	if written != expectedBytes || hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		return errors.New("size or sha256 mismatch")
	}
	return nil
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate artifact identifier: %w", err)
	}
	return "artifact_" + hex.EncodeToString(value), nil
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(destination []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(destination)
}
