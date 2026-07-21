package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

const SchemaVersion = 1

var safeIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@+-]{0,255}$`)

type SourceFile struct {
	Path       string `json:"path"`
	BlobSHA256 string `json:"blob_sha256"`
	Content    []byte `json:"-"`
}

type IndexRequest struct {
	ProjectID          string       `json:"project_id"`
	Repository         string       `json:"repository"`
	Revision           string       `json:"revision"`
	ParserID           string       `json:"parser_id"`
	CacheRetentionDays int          `json:"-"`
	CacheQuotaBytes    int64        `json:"-"`
	TerminalState      string       `json:"-"`
	Files              []SourceFile `json:"-"`
}

type Symbol struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Confidence int    `json:"confidence"`
}

type Relation struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Confidence int    `json:"confidence"`
}

type BlobAnalysis struct {
	BlobSHA256     string     `json:"blob_sha256"`
	ParserID       string     `json:"parser_id"`
	Language       string     `json:"language"`
	Classification string     `json:"classification"`
	Bytes          int64      `json:"bytes"`
	Symbols        []Symbol   `json:"symbols"`
	Relations      []Relation `json:"relations"`
	Failure        string     `json:"failure,omitempty"`
}

type IndexedFile struct {
	Path       string `json:"path"`
	BlobSHA256 string `json:"blob_sha256"`
	Language   string `json:"language"`
	Status     string `json:"status"`
	Failure    string `json:"failure,omitempty"`
	Reused     bool   `json:"reused"`
}

type IndexRun struct {
	ID           string        `json:"id"`
	ProjectID    string        `json:"project_id"`
	Repository   string        `json:"repository"`
	Revision     string        `json:"revision"`
	ParserID     string        `json:"parser_id"`
	State        string        `json:"state"`
	Files        int           `json:"files"`
	Parsed       int           `json:"parsed"`
	Reused       int           `json:"reused"`
	Failures     int           `json:"failures"`
	Bytes        int64         `json:"bytes"`
	StartedAt    time.Time     `json:"started_at"`
	CompletedAt  time.Time     `json:"completed_at"`
	FileEvidence []IndexedFile `json:"file_evidence,omitempty"`
}

type Status struct {
	ProjectID       string    `json:"project_id"`
	LatestRevision  string    `json:"latest_revision,omitempty"`
	LatestRunID     string    `json:"latest_run_id,omitempty"`
	State           string    `json:"state"`
	Files           int       `json:"files"`
	Languages       []string  `json:"languages"`
	Failures        int       `json:"failures"`
	StorageBytes    int64     `json:"storage_bytes"`
	ParserIDs       []string  `json:"parser_ids"`
	FreshAt         time.Time `json:"fresh_at,omitempty"`
	IndexingEnabled bool      `json:"indexing_enabled"`
	RetentionDays   int       `json:"retention_days"`
	CacheQuotaBytes int64     `json:"cache_quota_bytes"`
}

type Query struct {
	ProjectID string `json:"project_id"`
	Revision  string `json:"revision,omitempty"`
	Term      string `json:"term"`
	Limit     int    `json:"limit"`
}

type QueryResult struct {
	ProjectID string     `json:"project_id"`
	Revision  string     `json:"revision"`
	Term      string     `json:"term"`
	Symbols   []Symbol   `json:"symbols"`
	Relations []Relation `json:"relations"`
	Partial   bool       `json:"partial"`
	Failures  int        `json:"failures"`
}

type ContextCandidate struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Version  string `json:"version"`
	Reason   string `json:"reason"`
	Trust    string `json:"trust"`
	Content  []byte `json:"-"`
	Priority int    `json:"priority"`
	Stale    bool   `json:"stale"`
}

type ContextSelection struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	Version         string `json:"version"`
	Reason          string `json:"reason"`
	Trust           string `json:"trust"`
	SHA256          string `json:"sha256"`
	Bytes           int    `json:"bytes"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Included        bool   `json:"included"`
	Exclusion       string `json:"exclusion,omitempty"`
	Stale           bool   `json:"stale"`
}

type ContextManifest struct {
	ID                   string             `json:"id"`
	ProjectID            string             `json:"project_id"`
	JobID                string             `json:"job_id,omitempty"`
	Stage                string             `json:"stage"`
	SchemaVersion        int                `json:"schema_version"`
	BudgetTokens         int                `json:"budget_tokens"`
	ReservedOutputTokens int                `json:"reserved_output_tokens"`
	UsedTokens           int                `json:"used_tokens"`
	Truncated            bool               `json:"truncated"`
	Selections           []ContextSelection `json:"selections"`
	CreatedAt            time.Time          `json:"created_at"`
}

type ContextPacket struct {
	Manifest ContextManifest `json:"manifest"`
	Content  [][]byte        `json:"-"`
}

type ContextManifestComparison struct {
	ProjectID          string             `json:"project_id"`
	LeftID             string             `json:"left_id"`
	RightID            string             `json:"right_id"`
	Added              []ContextSelection `json:"added"`
	Removed            []ContextSelection `json:"removed"`
	Changed            []ContextSelection `json:"changed"`
	InputTokenDelta    int                `json:"input_token_delta"`
	OutputReserveDelta int                `json:"output_reserve_delta"`
	TruncationChanged  bool               `json:"truncation_changed"`
}

type Observation struct {
	Key      string          `json:"key"`
	Kind     string          `json:"kind"`
	Status   string          `json:"status"`
	Value    json.RawMessage `json:"value,omitempty"`
	Artifact string          `json:"artifact_id,omitempty"`
	Details  string          `json:"details,omitempty"`
}

type DifferentialObservation struct {
	Key            string          `json:"key"`
	Kind           string          `json:"kind"`
	Classification string          `json:"classification"`
	Baseline       json.RawMessage `json:"baseline,omitempty"`
	Candidate      json.RawMessage `json:"candidate,omitempty"`
	Explanation    string          `json:"explanation"`
}

type Baseline struct {
	ID            string        `json:"id"`
	ProjectID     string        `json:"project_id"`
	Revision      string        `json:"revision"`
	ConfigSHA256  string        `json:"config_sha256"`
	ToolchainID   string        `json:"toolchain_id"`
	PackSetSHA256 string        `json:"pack_set_sha256"`
	ActorID       string        `json:"actor_id"`
	Reason        string        `json:"reason"`
	Observations  []Observation `json:"observations"`
	CreatedAt     time.Time     `json:"created_at"`
}

type Differential struct {
	ID           string                    `json:"id"`
	BaselineID   string                    `json:"baseline_id"`
	CandidateSHA string                    `json:"candidate_sha"`
	Purpose      string                    `json:"purpose"`
	Items        []DifferentialObservation `json:"items"`
	CreatedAt    time.Time                 `json:"created_at"`
}

type BaselineSupersession struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	BaselineID     string    `json:"baseline_id"`
	DifferentialID string    `json:"differential_id"`
	Replacement    Baseline  `json:"replacement"`
	ActorID        string    `json:"actor_id"`
	Reason         string    `json:"reason"`
	CreatedAt      time.Time `json:"created_at"`
}

type DifferentialCorrection struct {
	ID                   string    `json:"id"`
	ProjectID            string    `json:"project_id"`
	DifferentialID       string    `json:"differential_id"`
	ObservationKind      string    `json:"observation_kind"`
	ObservationKey       string    `json:"observation_key"`
	BeforeClassification string    `json:"before_classification"`
	AfterClassification  string    `json:"after_classification"`
	ActorID              string    `json:"actor_id"`
	Reason               string    `json:"reason"`
	CreatedAt            time.Time `json:"created_at"`
}

type ImpactSelection struct {
	TestID           string   `json:"test_id"`
	Selected         bool     `json:"selected"`
	Reasons          []string `json:"reasons"`
	Confidence       int      `json:"confidence"`
	EstimatedSeconds int      `json:"estimated_seconds"`
}

type TestImpact struct {
	ID                string            `json:"id"`
	ProjectID         string            `json:"project_id"`
	Revision          string            `json:"revision"`
	ChangedSymbols    []string          `json:"changed_symbols"`
	Selections        []ImpactSelection `json:"selections"`
	FullSuiteRequired bool              `json:"full_suite_required"`
	PolicyExplanation string            `json:"policy_explanation"`
	CreatedAt         time.Time         `json:"created_at"`
}

type TestImpactOverride struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"project_id"`
	ImpactID  string     `json:"impact_id"`
	TestID    string     `json:"test_id"`
	Selected  bool       `json:"selected"`
	ActorID   string     `json:"actor_id"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type CacheEntry struct {
	Key          string    `json:"key"`
	ProjectID    string    `json:"project_id"`
	TrustDomain  string    `json:"trust_domain"`
	Kind         string    `json:"kind"`
	InputSHA256  string    `json:"input_sha256"`
	ObjectSHA256 string    `json:"object_sha256"`
	Bytes        int64     `json:"bytes"`
	Verified     bool      `json:"verified"`
	CreatedAt    time.Time `json:"created_at"`
	LastHitAt    time.Time `json:"last_hit_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	QuotaBytes   int64     `json:"quota_bytes"`
	LastResult   string    `json:"last_result"`
	ResultReason string    `json:"result_reason"`
}

type RetentionResult struct {
	RunsRemoved   int `json:"runs_removed"`
	BlobsRemoved  int `json:"blobs_removed"`
	CachesRemoved int `json:"caches_removed"`
}

type CacheVerification struct {
	Key            string `json:"key"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	ExpectedObject string `json:"expected_object_sha256"`
	ObservedObject string `json:"observed_object_sha256,omitempty"`
}

type CacheVerificationReport struct {
	ProjectID   string              `json:"project_id"`
	Kind        string              `json:"kind,omitempty"`
	Verified    int                 `json:"verified"`
	Invalid     int                 `json:"invalid"`
	Unavailable int                 `json:"unavailable"`
	Items       []CacheVerification `json:"items"`
}

type CacheSimulation struct {
	ProjectID      string `json:"project_id"`
	TrustDomain    string `json:"trust_domain"`
	Kind           string `json:"kind"`
	CurrentBytes   int64  `json:"current_bytes"`
	EstimatedBytes int64  `json:"estimated_bytes"`
	QuotaBytes     int64  `json:"quota_bytes"`
	WouldFit       bool   `json:"would_fit"`
	Explanation    string `json:"explanation"`
}

type Store interface {
	FindBlobAnalysis(context.Context, string, string, string, string) (BlobAnalysis, bool, error)
	CommitIndex(context.Context, IndexRequest, []BlobAnalysis, []IndexedFile) (IndexRun, error)
	IntelligenceStatus(context.Context, string) (Status, error)
	QueryIntelligence(context.Context, Query) (QueryResult, error)
	RebuildIntelligence(context.Context, string, string, string) error
	PruneIntelligence(context.Context, string, time.Time, time.Time) (RetentionResult, error)
	SaveContextManifest(context.Context, ContextManifest) (ContextManifest, error)
	GetContextManifest(context.Context, string, string) (ContextManifest, error)
	ListContextManifests(context.Context, string, int) ([]ContextManifest, error)
	SaveBaseline(context.Context, Baseline) (Baseline, error)
	GetBaseline(context.Context, string, string) (Baseline, error)
	FindBaseline(context.Context, string, string, string, string, string) (Baseline, bool, error)
	ListBaselines(context.Context, string, int) ([]Baseline, error)
	SaveBaselineSupersession(context.Context, BaselineSupersession) (BaselineSupersession, error)
	ListBaselineSupersessions(context.Context, string, int) ([]BaselineSupersession, error)
	SaveDifferential(context.Context, Differential) (Differential, error)
	GetDifferential(context.Context, string, string) (Differential, error)
	ListDifferentials(context.Context, string, int) ([]Differential, error)
	SaveDifferentialCorrection(context.Context, DifferentialCorrection) (DifferentialCorrection, error)
	ListDifferentialCorrections(context.Context, string, int) ([]DifferentialCorrection, error)
	SaveTestImpact(context.Context, TestImpact) (TestImpact, error)
	GetTestImpact(context.Context, string, string) (TestImpact, error)
	FindTestImpact(context.Context, string, string) (TestImpact, bool, error)
	ListTestImpacts(context.Context, string, int) ([]TestImpact, error)
	SaveTestImpactOverride(context.Context, TestImpactOverride) (TestImpactOverride, error)
	ListTestImpactOverrides(context.Context, string, int) ([]TestImpactOverride, error)
	PutCacheEntry(context.Context, CacheEntry) (CacheEntry, error)
	ListCacheEntries(context.Context, string, int) ([]CacheEntry, error)
	CacheUsage(context.Context, string) (int64, int, error)
	VerifyCacheEntries(context.Context, string, string) (CacheVerificationReport, error)
	PurgeCacheEntries(context.Context, string, string, string, string) (int, error)
}

func ValidateIdentity(value string) error {
	if !safeIdentity.MatchString(value) {
		return errors.New("identity contains unsupported characters or length")
	}
	return nil
}
