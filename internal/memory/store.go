package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProjectScope struct {
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
}

func (s ProjectScope) Valid() bool {
	return projectPartPattern.MatchString(s.Owner) && projectPartPattern.MatchString(s.Repository)
}

var projectPartPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func (s ProjectScope) Namespace() string {
	return "viking://resources/projects/" + s.Owner + "/" + s.Repository + "/"
}

type Status string

const (
	StatusQuarantine Status = "quarantine"
	StatusCanonical  Status = "canonical"
	StatusStale      Status = "stale"
	StatusDeleted    Status = "deleted"
)

type Record struct {
	ID               string       `json:"id"`
	Scope            ProjectScope `json:"scope"`
	Namespace        string       `json:"namespace"`
	Content          string       `json:"content"`
	ContentHash      string       `json:"content_hash"`
	SourceURI        string       `json:"source_uri"`
	BaseCommit       string       `json:"base_commit"`
	MergedCommit     string       `json:"merged_commit,omitempty"`
	AffectedPaths    []string     `json:"affected_paths"`
	Status           Status       `json:"status"`
	Verified         bool         `json:"verified"`
	SecretScanPass   bool         `json:"secret_scan_pass"`
	Kind             string       `json:"kind"`
	InvalidationRule string       `json:"invalidation_rule,omitempty"`
	ExpiresAt        *time.Time   `json:"expires_at,omitempty"`
	UpdatedAt        time.Time    `json:"updated_at"`
	DeletedAt        *time.Time   `json:"deleted_at,omitempty"`
	Version          int64        `json:"version"`
	CreatedAt        time.Time    `json:"created_at"`
}

type Store interface {
	PutCandidate(context.Context, ProjectScope, Record) (Record, error)
	Search(context.Context, ProjectScope, string, int) ([]Record, error)
	Promote(context.Context, ProjectScope, string, string) (Record, error)
	Invalidate(context.Context, ProjectScope, string, string) (Record, error)
	Delete(context.Context, ProjectScope, string, string) error
}

type Event struct {
	Sequence  int64           `json:"sequence"`
	RecordID  string          `json:"record_id"`
	Scope     ProjectScope    `json:"scope"`
	Action    string          `json:"action"`
	ActorID   string          `json:"actor_id"`
	Rationale string          `json:"rationale"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}

type RetrievalTrace struct {
	ID              string          `json:"id"`
	JobID           string          `json:"job_id,omitempty"`
	Scope           ProjectScope    `json:"scope"`
	Namespace       string          `json:"namespace"`
	Query           string          `json:"query"`
	QueryHash       string          `json:"query_hash"`
	CandidateIDs    []string        `json:"candidate_ids"`
	SelectedIDs     []string        `json:"selected_ids"`
	BudgetTokens    int             `json:"budget_tokens"`
	AllocatedTokens int             `json:"allocated_tokens"`
	Trajectory      json.RawMessage `json:"trajectory"`
	CreatedAt       time.Time       `json:"created_at"`
}

type CorrectionRequest struct {
	Content          string
	AffectedPaths    []string
	InvalidationRule string
	ActorID          string
	Rationale        string
	ExpectedVersion  int64
}

type PromotionRequest struct {
	ActorID         string
	Rationale       string
	Basis           string
	MergedCommit    string
	ExpectedVersion int64
}

type VerificationRequest struct {
	ActorID         string
	JobID           string
	ArtifactID      string
	Commit          string
	ExpectedVersion int64
}

type InvalidationRequest struct {
	ActorID         string
	Rationale       string
	ExpectedVersion int64
}

type DeletionRequest struct {
	ActorID         string
	Rationale       string
	ExpectedVersion int64
}

type IndexOperation struct {
	ID            string       `json:"id"`
	RecordID      string       `json:"record_id"`
	Scope         ProjectScope `json:"scope"`
	Action        string       `json:"action"`
	RecordVersion int64        `json:"record_version"`
	State         string       `json:"state"`
	Attempts      int          `json:"attempts"`
	LastError     string       `json:"last_error,omitempty"`
	LeaseOwner    string       `json:"-"`
	LeaseExpires  time.Time    `json:"-"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type IndexQueue interface {
	ClaimMemoryIndexOperation(context.Context, string, time.Duration) (IndexOperation, error)
	CompleteMemoryIndexOperation(context.Context, string, string) error
	FailMemoryIndexOperation(context.Context, string, string, string, time.Duration) error
	EnqueueProjectMemoryRebuild(context.Context, ProjectScope, string) (int, error)
}

type DurableStore interface {
	PutCandidate(context.Context, ProjectScope, Record) (Record, error)
	Search(context.Context, ProjectScope, string, int) ([]Record, error)
	GetMemory(context.Context, ProjectScope, string) (Record, error)
	ListMemory(context.Context, ProjectScope, Status, int) ([]Record, error)
	CorrectMemory(context.Context, ProjectScope, string, CorrectionRequest) (Record, error)
	VerifyMemory(context.Context, ProjectScope, string, VerificationRequest) (Record, error)
	PromoteMemory(context.Context, ProjectScope, string, PromotionRequest) (Record, error)
	InvalidateMemory(context.Context, ProjectScope, string, InvalidationRequest) (Record, error)
	DeleteMemory(context.Context, ProjectScope, string, DeletionRequest) error
	ListMemoryEvents(context.Context, ProjectScope, string, int) ([]Event, error)
	RecordRetrieval(context.Context, RetrievalTrace) (RetrievalTrace, error)
	ListRetrievals(context.Context, ProjectScope, int) ([]RetrievalTrace, error)
	ExportProjectMemory(context.Context, ProjectScope) (ExportBundle, error)
	RestoreProjectMemory(context.Context, ProjectScope, ExportBundle, bool, string) (RestoreReport, error)
}

const ExportSchemaVersion = 1

type ExportRecord struct {
	OriginalID       string     `json:"original_id"`
	Content          string     `json:"content"`
	SourceURI        string     `json:"source_uri,omitempty"`
	BaseCommit       string     `json:"base_commit,omitempty"`
	MergedCommit     string     `json:"merged_commit,omitempty"`
	AffectedPaths    []string   `json:"affected_paths"`
	Kind             string     `json:"kind"`
	InvalidationRule string     `json:"invalidation_rule,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	OriginalStatus   Status     `json:"original_status"`
	OriginalVerified bool       `json:"original_verified"`
	ContentHash      string     `json:"content_hash"`
}

type ExportBundle struct {
	SchemaVersion int            `json:"schema_version"`
	Scope         ProjectScope   `json:"scope"`
	ExportedAt    time.Time      `json:"exported_at"`
	Records       []ExportRecord `json:"records"`
	ManifestHash  string         `json:"manifest_hash"`
}

type RestoreReport struct {
	DryRun       bool   `json:"dry_run"`
	Validated    int    `json:"validated"`
	Imported     int    `json:"imported"`
	Skipped      int    `json:"skipped"`
	ManifestHash string `json:"manifest_hash"`
}

func SealExport(scope ProjectScope, records []ExportRecord, exportedAt time.Time) (ExportBundle, error) {
	bundle := ExportBundle{SchemaVersion: ExportSchemaVersion, Scope: scope, ExportedAt: exportedAt.UTC(), Records: records}
	if err := ValidateExport(scope, bundle); err != nil {
		return ExportBundle{}, err
	}
	payload, err := json.Marshal(bundle)
	if err != nil {
		return ExportBundle{}, err
	}
	digest := sha256.Sum256(payload)
	bundle.ManifestHash = hex.EncodeToString(digest[:])
	return bundle, nil
}

func ValidateExport(scope ProjectScope, bundle ExportBundle) error {
	if !scope.Valid() || bundle.SchemaVersion != ExportSchemaVersion || bundle.Scope != scope || bundle.ExportedAt.IsZero() || len(bundle.Records) > 100 {
		return ErrInvalid
	}
	if bundle.ManifestHash != "" {
		provided := bundle.ManifestHash
		bundle.ManifestHash = ""
		payload, err := json.Marshal(bundle)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(payload)
		expected := hex.EncodeToString(digest[:])
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			return ErrInvalid
		}
	}
	total := 0
	for _, item := range bundle.Records {
		if !ValidID(item.OriginalID) || item.OriginalStatus == StatusDeleted {
			return ErrInvalid
		}
		candidate, err := PrepareCandidate(scope, Record{
			Content: item.Content, SourceURI: item.SourceURI, BaseCommit: item.BaseCommit,
			MergedCommit: item.MergedCommit, AffectedPaths: item.AffectedPaths, Kind: item.Kind,
			InvalidationRule: item.InvalidationRule, ExpiresAt: item.ExpiresAt,
		})
		if err != nil || candidate.ContentHash != item.ContentHash {
			return ErrInvalid
		}
		total += len(item.Content)
		if total > 7<<20 {
			return ErrInvalid
		}
	}
	return nil
}

var (
	ErrScope            = errors.New("invalid or mismatched project scope")
	ErrInvalid          = errors.New("invalid memory record")
	ErrNotFound         = errors.New("memory record not found")
	ErrConflict         = errors.New("memory record version conflict")
	ErrNoIndexOperation = errors.New("no memory index operation is ready")
)

func PrepareCandidate(scope ProjectScope, record Record) (Record, error) {
	if !scope.Valid() || (record.Scope.Valid() && record.Scope != scope) || len(record.Content) == 0 || len(record.Content) > 64<<10 || strings.ContainsRune(record.Content, 0) {
		return Record{}, ErrInvalid
	}
	if record.ID == "" {
		identifier := make([]byte, 16)
		if _, err := rand.Read(identifier); err != nil {
			return Record{}, err
		}
		record.ID = "memory_" + hex.EncodeToString(identifier)
	}
	if !strings.HasPrefix(record.ID, "memory_") || len(record.ID) > 80 {
		return Record{}, ErrInvalid
	}
	if record.Kind == "" {
		record.Kind = "project_knowledge"
	}
	allowedKind := map[string]bool{"project_knowledge": true, "verified_case": true, "failed_case": true, "pattern": true, "known_issue": true, "flaky_test": true, "issue_history": true}
	if !allowedKind[record.Kind] || !ValidCommit(record.BaseCommit) || !ValidCommit(record.MergedCommit) || !validSourceURI(record.SourceURI) {
		return Record{}, ErrInvalid
	}
	for _, affectedPath := range record.AffectedPaths {
		cleaned := path.Clean(affectedPath)
		if cleaned != affectedPath || cleaned == "." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") || strings.ContainsRune(cleaned, 0) {
			return Record{}, ErrInvalid
		}
	}
	if containsLikelySecret(record.Content) {
		return Record{}, errors.New("memory candidate failed secret scanning")
	}
	hash := sha256.Sum256([]byte(record.Content))
	record.Scope = scope
	record.Namespace = scope.Namespace()
	record.ContentHash = hex.EncodeToString(hash[:])
	record.SecretScanPass = true
	record.Status = StatusQuarantine
	record.Version = 1
	record.Verified = false
	return record, nil
}

func ValidCommit(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func ValidID(value string) bool {
	if !strings.HasPrefix(value, "memory_") || len(value) != len("memory_")+32 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "memory_"))
	return err == nil
}

func validSourceURI(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 2048 || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	switch parsed.Scheme {
	case "job", "artifact", "viking":
		return parsed.Host != "" && parsed.Path != ""
	case "https":
		return (strings.EqualFold(parsed.Hostname(), "github.com") || strings.EqualFold(parsed.Hostname(), "api.github.com")) && parsed.Path != ""
	default:
		return false
	}
}

func containsLikelySecret(content string) bool {
	lower := strings.ToLower(content)
	for _, marker := range []string{"-----begin private key-----", "ghp_", "github_pat_", "sk-ant-", "aws_secret_access_key", "password="} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

type Fake struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewFake() *Fake { return &Fake{records: make(map[string]Record)} }

func (f *Fake) PutCandidate(_ context.Context, scope ProjectScope, record Record) (Record, error) {
	if !scope.Valid() || (record.Scope.Valid() && record.Scope != scope) || record.ID == "" {
		return Record{}, ErrScope
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	record.Scope = scope
	record.Namespace = scope.Namespace()
	record.Status = StatusQuarantine
	record.Verified = false
	record.CreatedAt = time.Now().UTC()
	f.records[record.ID] = record
	return record, nil
}

func (f *Fake) Search(_ context.Context, scope ProjectScope, query string, limit int) ([]Record, error) {
	if !scope.Valid() {
		return nil, ErrScope
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]Record, 0)
	query = strings.ToLower(query)
	for _, record := range f.records {
		if record.Scope == scope && record.Status == StatusCanonical && strings.Contains(strings.ToLower(record.Content), query) {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (f *Fake) Promote(_ context.Context, scope ProjectScope, id, actor string) (Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.records[id]
	if !ok {
		return Record{}, errors.New("memory record not found")
	}
	if !scope.Valid() || record.Scope != scope {
		return Record{}, ErrScope
	}
	if actor == "" || !record.Verified || !record.SecretScanPass {
		return Record{}, errors.New("memory record is not eligible for promotion")
	}
	record.Status = StatusCanonical
	f.records[id] = record
	return record, nil
}

func (f *Fake) Invalidate(_ context.Context, scope ProjectScope, id, actor string) (Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.records[id]
	if !ok {
		return Record{}, errors.New("memory record not found")
	}
	if !scope.Valid() || record.Scope != scope || actor == "" {
		return Record{}, ErrScope
	}
	record.Status = StatusStale
	f.records[id] = record
	return record, nil
}

func (f *Fake) Delete(_ context.Context, scope ProjectScope, id, actor string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.records[id]
	if !ok {
		return errors.New("memory record not found")
	}
	if !scope.Valid() || record.Scope != scope || actor == "" {
		return ErrScope
	}
	delete(f.records, id)
	return nil
}
