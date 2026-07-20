package verification

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid verification request")
	ErrUnknownClass   = errors.New("verification command class is not available for the language profile")
)

type Class string

const (
	ClassFormat         Class = "format"
	ClassCompile        Class = "compile"
	ClassLint           Class = "lint"
	ClassTargetedTests  Class = "targeted_tests"
	ClassFullTests      Class = "full_tests"
	ClassPropertyTests  Class = "property_tests"
	ClassMutationTests  Class = "mutation_tests"
	ClassSanitizers     Class = "sanitizers"
	ClassFuzz           Class = "fuzz"
	ClassDependencyScan Class = "dependency_scan"
	ClassSecretScan     Class = "secret_scan"
	ClassDiffPolicy     Class = "diff_policy"
)

type Language string

const (
	LanguageBase   Language = "base"
	LanguagePython Language = "python"
	LanguageNode   Language = "node"
	LanguageC      Language = "c"
	LanguageCPP    Language = "cpp"
	LanguageRust   Language = "rust"
	LanguageGo     Language = "go"
	LanguageFull   Language = "full"
)

type Command struct {
	Class      Class
	Executable string
	Arguments  []string
}

type ExecutionResult struct {
	Class      Class         `json:"class"`
	Executable string        `json:"executable"`
	Arguments  []string      `json:"arguments"`
	ExitCode   int           `json:"exit_code"`
	Output     string        `json:"output"`
	Truncated  bool          `json:"truncated"`
	TimedOut   bool          `json:"timed_out"`
	StartedAt  time.Time     `json:"started_at"`
	Duration   time.Duration `json:"duration_ns"`
}

type Finding struct {
	Rule         string `json:"rule"`
	Severity     string `json:"severity"`
	Path         string `json:"path,omitempty"`
	Evidence     string `json:"evidence"`
	Verification string `json:"verification"`
}

type Request struct {
	JobID        string
	ProjectID    string
	WorktreePath string
	BaseSHA      string
	Language     Language
	Classes      []Class
	Timeout      time.Duration
	MaxLogBytes  int64
	Policy       Policy
}

type Report struct {
	JobID       string            `json:"job_id"`
	ProjectID   string            `json:"project_id"`
	BaseSHA     string            `json:"base_sha"`
	HeadSHA     string            `json:"head_sha"`
	PatchSHA256 string            `json:"patch_sha256"`
	Passed      bool              `json:"passed"`
	Commands    []ExecutionResult `json:"commands"`
	Findings    []Finding         `json:"findings"`
	ArtifactIDs []string          `json:"artifact_ids"`
	StartedAt   time.Time         `json:"started_at"`
	CompletedAt time.Time         `json:"completed_at"`
}

type Executor interface {
	Run(context.Context, string, Command, time.Duration, int64) ExecutionResult
}

type Policy struct {
	ProtectedPaths  []string
	MaxChangedFiles int
	MaxPatchBytes   int64
	MaxFileBytes    int64
}

func DefaultPolicy() Policy {
	return Policy{
		ProtectedPaths:  []string{".github/workflows/", "CODEOWNERS", ".gitmodules"},
		MaxChangedFiles: 200, MaxPatchBytes: 2 << 20, MaxFileBytes: 5 << 20,
	}
}
