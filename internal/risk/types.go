package risk

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/taskcontract"
)

const SchemaVersion = 1

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Level string

const (
	LevelLow    Level = "low"
	LevelMedium Level = "medium"
	LevelHigh   Level = "high"
)

type Signal struct {
	ID          string `json:"id"`
	Weight      int    `json:"weight"`
	Matched     bool   `json:"matched"`
	Explanation string `json:"explanation"`
}

type Routing struct {
	RequiredStages      []string `json:"required_stages"`
	ManualGates         []string `json:"manual_gates"`
	FullVerification    bool     `json:"full_verification"`
	DocumentationReview bool     `json:"documentation_review"`
	PublishAllowed      bool     `json:"publish_allowed"`
}

type Assessment struct {
	ID                  string          `json:"id"`
	JobID               string          `json:"job_id"`
	SchemaVersion       int             `json:"schema_version"`
	ContractVersion     int64           `json:"contract_version"`
	ContractSHA256      string          `json:"contract_sha256"`
	Level               Level           `json:"level"`
	Score               int             `json:"score"`
	Signals             []Signal        `json:"signals"`
	Routing             Routing         `json:"routing"`
	PolicyDecision      json.RawMessage `json:"policy_decision"`
	Explanation         string          `json:"explanation"`
	RequestedByOperator bool            `json:"requested_by_operator"`
	SupersededBy        string          `json:"superseded_by,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
}

type Waiver struct {
	ID              string    `json:"id"`
	JobID           string    `json:"job_id"`
	AssessmentID    string    `json:"assessment_id"`
	FromLevel       Level     `json:"from_level"`
	ToLevel         Level     `json:"to_level"`
	Reason          string    `json:"reason"`
	ActorID         string    `json:"actor_id"`
	ActorRole       string    `json:"actor_role"`
	Reauthenticated bool      `json:"reauthenticated"`
	ExpiresAt       time.Time `json:"expires_at"`
	CreatedAt       time.Time `json:"created_at"`
}

type WaiverRequest struct {
	JobID           string
	AssessmentID    string
	ToLevel         Level
	Reason          string
	ActorID         string
	ActorRole       string
	Reauthenticated bool
	ExpiresAt       time.Time
}

type Store interface {
	SaveRiskAssessment(context.Context, Assessment) (Assessment, error)
	GetLatestRiskAssessment(context.Context, string) (Assessment, error)
	ListRiskAssessments(context.Context, string, int) ([]Assessment, error)
	CreateRiskWaiver(context.Context, WaiverRequest) (Waiver, Assessment, error)
	ListRiskWaivers(context.Context, string, int) ([]Waiver, error)
}

func Assess(contract taskcontract.Contract, changedPaths []string, operatorMinimum Level) (Assessment, error) {
	if contract.Status != "approved" && contract.Status != "draft" {
		return Assessment{}, errors.New("risk assessment requires a valid task contract")
	}
	signals := []Signal{
		signal("protected_paths", 45, containsAny(changedPaths, ".github/", "CODEOWNERS", ".gitmodules", "migrations/", "database/migrations/"), "Protected path, workflow, ownership, submodule, or migration path is in scope."),
		signal("public_api_schema_database", 35, containsAny(changedPaths, "internal/api/", "openapi", "schema", "migrations/"), "Public API, schema, or database behavior may change."),
		signal("auth_secrets_crypto_network", 55, textContains(contract, "auth", "authorization", "secret", "crypto", "network", "egress", "credential"), "Authentication, secret, cryptography, or network exposure language appears in the contract."),
		signal("dependency_supply_chain", 40, containsAny(changedPaths, "go.mod", "go.sum", "package-lock.json", "Dockerfile", "compose"), "Dependency, image, or supply-chain identity may change."),
		signal("installer_service_hardware", 50, textContains(contract, "installer", "service", "signing", "hardware", "windows", "vpn"), "Installer, service, signing, hardware, Windows, or VPN behavior is in scope."),
		signal("numerical_statistical", 30, textContains(contract, "r ", "statistical", "numerical", "tolerance", "golden"), "Numerical, statistical, tolerance, or golden-output behavior is in scope."),
		signal("weak_or_missing_tests", 25, !hasEvidence(contract, "test"), "The contract does not yet cite concrete test evidence."),
		signal("generated_or_binary_artifacts", 30, textContains(contract, "generated", "binary", "screenshot", "artifact"), "Generated or binary artifact behavior is in scope."),
		signal("ambiguity", 35, len(contract.Questions) > 0 || len(contract.Assumptions) > 0, "Open questions or assumptions require explicit routing."),
	}
	score := 0
	matched := make([]string, 0)
	for _, item := range signals {
		if item.Matched {
			score += item.Weight
			matched = append(matched, item.ID)
		}
	}
	level := LevelLow
	switch {
	case score >= 90:
		level = LevelHigh
	case score >= 35:
		level = LevelMedium
	}
	if rank(operatorMinimum) > rank(level) {
		level = operatorMinimum
	}
	sort.Strings(matched)
	decision := map[string]any{"engine": "controller-deterministic-v1", "matched_signals": matched, "operator_minimum": operatorMinimum}
	rawDecision, _ := json.Marshal(decision)
	return Assessment{
		JobID: contract.JobID, SchemaVersion: SchemaVersion, ContractVersion: contract.Version,
		ContractSHA256: contract.ContractSHA256, Level: level, Score: score, Signals: signals,
		Routing: RoutingForLevel(level), PolicyDecision: rawDecision,
		Explanation:         strings.Join(matched, ", "),
		RequestedByOperator: operatorMinimum != "",
	}, nil
}

func (a Assessment) Validate() error {
	if !safeID.MatchString(a.JobID) || a.SchemaVersion != SchemaVersion || a.ContractVersion < 1 ||
		a.ContractSHA256 == "" || !validLevel(a.Level) || a.Score < 0 || a.Score > 1000 ||
		len(a.Signals) == 0 || len(a.Signals) > 64 || len(a.PolicyDecision) > 64<<10 || !json.Valid(a.PolicyDecision) {
		return errors.New("risk assessment violates version-one bounds")
	}
	for _, signal := range a.Signals {
		if !safeID.MatchString(signal.ID) || signal.Weight < 0 || signal.Weight > 100 || strings.TrimSpace(signal.Explanation) == "" {
			return errors.New("risk signal is invalid")
		}
	}
	return nil
}

func RoutingForLevel(level Level) Routing {
	base := Routing{
		RequiredStages:   []string{"clarifier", "implementation", "targeted_verification", "full_verification", "code_qc"},
		ManualGates:      []string{"task_contract_approval"},
		FullVerification: true, PublishAllowed: true,
	}
	if level == LevelMedium || level == LevelHigh {
		base.RequiredStages = append(base.RequiredStages, "independent_test_designer", "documentation_agent", "documentation_qc", "opa_policy")
		base.ManualGates = append(base.ManualGates, "risk_waiver_review", "golden_update_approval")
		base.DocumentationReview = true
	}
	if level == LevelHigh {
		base.ManualGates = append(base.ManualGates, "remote_egress_approval", "publication_reauthentication")
	}
	return base
}

func signal(id string, weight int, matched bool, explanation string) Signal {
	return Signal{ID: id, Weight: weight, Matched: matched, Explanation: explanation}
}

func containsAny(paths []string, needles ...string) bool {
	for _, path := range paths {
		lower := strings.ToLower(path)
		for _, needle := range needles {
			if strings.Contains(lower, strings.ToLower(needle)) {
				return true
			}
		}
	}
	return false
}

func textContains(contract taskcontract.Contract, needles ...string) bool {
	blob := strings.ToLower(contract.RequestedBehavior + "\n" + strings.Join(contract.LikelyRisks, "\n") + "\n" + strings.Join(contract.LikelyComponents, "\n"))
	for _, needle := range needles {
		if strings.Contains(blob, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func hasEvidence(contract taskcontract.Contract, needle string) bool {
	for _, item := range contract.RequiredEvidence {
		if strings.Contains(strings.ToLower(item), needle) {
			return true
		}
	}
	return false
}

func validLevel(level Level) bool {
	return level == LevelLow || level == LevelMedium || level == LevelHigh
}
func rank(level Level) int {
	switch level {
	case LevelHigh:
		return 3
	case LevelMedium:
		return 2
	case LevelLow:
		return 1
	default:
		return 0
	}
}
