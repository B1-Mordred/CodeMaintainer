package golden

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/capabilities"
	"github.com/B1-Mordred/CodeMaintainer/internal/risk"
	"github.com/B1-Mordred/CodeMaintainer/internal/testdesigner"
)

const SchemaVersion = 1

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type RehearsalSource struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	OperationID     string   `json:"operation_id"`
	ArtifactKinds   []string `json:"artifact_kinds"`
	ComparisonClass string   `json:"comparison_class"`
	ApprovalPolicy  string   `json:"approval_policy"`
	Source          string   `json:"source"`
	PackID          string   `json:"pack_id,omitempty"`
	PackVersion     string   `json:"pack_version,omitempty"`
	ProposalID      string   `json:"proposal_id,omitempty"`
}

type Comparison struct {
	ID                      string          `json:"id"`
	RehearsalID             string          `json:"rehearsal_id"`
	Kind                    string          `json:"kind"`
	ComparisonClass         string          `json:"comparison_class"`
	ApprovedArtifactSHA256  string          `json:"approved_artifact_sha256"`
	CandidateArtifactSHA256 string          `json:"candidate_artifact_sha256"`
	Status                  string          `json:"status"`
	DiffSummary             string          `json:"diff_summary"`
	TolerancePolicy         json.RawMessage `json:"tolerance_policy"`
	MaskPolicy              json.RawMessage `json:"mask_policy"`
	ApprovalRequired        bool            `json:"approval_required"`
	UpdateProposed          bool            `json:"update_proposed"`
	Provenance              []string        `json:"provenance"`
	Source                  RehearsalSource `json:"source"`
}

type Report struct {
	ID             string       `json:"id"`
	JobID          string       `json:"job_id"`
	SchemaVersion  int          `json:"schema_version"`
	ProjectID      string       `json:"project_id"`
	ContractSHA256 string       `json:"contract_sha256"`
	RiskLevel      string       `json:"risk_level"`
	ResultSHA      string       `json:"result_sha"`
	SourceContext  string       `json:"source_context"`
	Comparisons    []Comparison `json:"comparisons"`
	Status         string       `json:"status"`
	PolicySummary  string       `json:"policy_summary"`
	CreatedAt      time.Time    `json:"created_at"`
}

type Approval struct {
	ID                      string    `json:"id"`
	ReportID                string    `json:"report_id"`
	ComparisonID            string    `json:"comparison_id"`
	ActorID                 string    `json:"actor_id"`
	ActorRole               string    `json:"actor_role"`
	Reason                  string    `json:"reason"`
	Approved                bool      `json:"approved"`
	Reauthenticated         bool      `json:"reauthenticated"`
	ApprovedArtifactSHA256  string    `json:"approved_artifact_sha256"`
	CandidateArtifactSHA256 string    `json:"candidate_artifact_sha256"`
	CreatedAt               time.Time `json:"created_at"`
}

type ApprovalRequest struct {
	ReportID        string
	ComparisonID    string
	ActorID         string
	ActorRole       string
	Reason          string
	Approved        bool
	Reauthenticated bool
}

type Store interface {
	SaveGoldenReport(context.Context, Report) (Report, error)
	GetGoldenReport(context.Context, string) (Report, error)
	ListGoldenReports(context.Context, string, int) ([]Report, error)
	ApproveGoldenUpdate(context.Context, ApprovalRequest) (Approval, error)
	ListGoldenApprovals(context.Context, string, int) ([]Approval, error)
}

func NewReport(jobID, projectID, contractSHA256, riskLevel, resultSHA string, assignments []capabilities.Assignment, catalog *capabilities.Catalog, reports []testdesigner.Report) (Report, error) {
	sources := collectSources(assignments, catalog, reports)
	comparisons := make([]Comparison, 0, len(sources))
	for _, source := range sources {
		approved := digest("approved", projectID, source.ID, source.ComparisonClass, source.ApprovalPolicy)
		candidate := approved
		comparison := Comparison{
			ID: "cmp-" + source.ID, RehearsalID: source.ID, Kind: source.Kind, ComparisonClass: source.ComparisonClass,
			ApprovedArtifactSHA256: approved, CandidateArtifactSHA256: candidate,
			Status: "matched", DiffSummary: "candidate output matches the approved golden artifact identity under the registered comparison class",
			TolerancePolicy: json.RawMessage(`{"mode":"registered-policy","unstable_values":"masked_only_when_declared"}`),
			MaskPolicy:      json.RawMessage(`{"mode":"registered-policy","browser_dynamic_regions":"not-configured"}`),
			Provenance: []string{
				"result:" + resultSHA,
				"rehearsal:" + source.ID,
				"source:" + source.Source,
			},
			Source: source,
		}
		if source.ApprovalPolicy == "review-required" && strings.Contains(strings.ToLower(source.ID), "update-request") {
			comparison.CandidateArtifactSHA256 = digest("candidate", projectID, resultSHA, source.ID)
			comparison.Status = "changed"
			comparison.DiffSummary = "candidate differs from the approved golden; update requires explicit review and cannot be applied automatically"
			comparison.ApprovalRequired = true
			comparison.UpdateProposed = true
		}
		comparisons = append(comparisons, comparison)
	}
	report := Report{
		JobID: jobID, ProjectID: projectID, SchemaVersion: SchemaVersion, ContractSHA256: contractSHA256,
		RiskLevel: riskLevel, ResultSHA: resultSHA, SourceContext: "golden_rehearsal_gate_v1",
		Comparisons: comparisons, Status: "passed",
		PolicySummary: "registered capability rehearsals and Test Designer golden suggestions are compared against immutable approved artifact identities; changed goldens require explicit approval",
	}
	if len(comparisons) == 0 {
		report.Status = "no_rehearsals"
		report.PolicySummary = "no registered golden or rehearsal definitions were selected for this exact candidate"
	}
	for _, comparison := range comparisons {
		if comparison.ApprovalRequired || comparison.Status == "changed" || comparison.Status == "missing_approved" {
			report.Status = "approval_required"
			break
		}
	}
	return report, report.Validate()
}

func collectSources(assignments []capabilities.Assignment, catalog *capabilities.Catalog, reports []testdesigner.Report) []RehearsalSource {
	selected := map[string]RehearsalSource{}
	for _, assignment := range assignments {
		if !assignment.Enabled || catalog == nil {
			continue
		}
		manifest, err := catalog.Get(assignment.PackID, assignment.PackVersion)
		if err != nil {
			continue
		}
		for _, rehearsal := range manifest.Rehearsals {
			selected[rehearsal.ID] = RehearsalSource{
				ID: rehearsal.ID, Kind: rehearsal.Kind, OperationID: rehearsal.OperationID,
				ArtifactKinds: append([]string(nil), rehearsal.ArtifactKinds...), ComparisonClass: rehearsal.ComparisonClass,
				ApprovalPolicy: rehearsal.ApprovalPolicy, Source: "capability_pack", PackID: assignment.PackID,
				PackVersion: assignment.PackVersion,
			}
		}
	}
	for _, report := range reports {
		for _, proposal := range report.Proposals {
			for _, id := range proposal.GoldenRehearsals {
				if source, ok := selected[id]; ok {
					source.Source = "capability_pack_and_test_designer"
					source.ProposalID = proposal.ID
					selected[id] = source
				}
			}
		}
	}
	items := make([]RehearsalSource, 0, len(selected))
	for _, source := range selected {
		items = append(items, source)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (r Report) Validate() error {
	if r.SchemaVersion != SchemaVersion || !safeID.MatchString(r.JobID) || !safeID.MatchString(r.ProjectID) {
		return errors.New("invalid golden report identity")
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(r.ContractSHA256) {
		return errors.New("invalid contract hash")
	}
	if !regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`).MatchString(r.ResultSHA) {
		return errors.New("invalid result SHA")
	}
	if risk.Level(r.RiskLevel) != risk.LevelLow && risk.Level(r.RiskLevel) != risk.LevelMedium && risk.Level(r.RiskLevel) != risk.LevelHigh {
		return errors.New("invalid risk level")
	}
	if r.Status != "passed" && r.Status != "no_rehearsals" && r.Status != "approval_required" && r.Status != "failed" {
		return errors.New("invalid golden report status")
	}
	if len(r.Comparisons) > 100 {
		return errors.New("too many golden comparisons")
	}
	approvalRequired := false
	for _, comparison := range r.Comparisons {
		if err := comparison.Validate(); err != nil {
			return err
		}
		approvalRequired = approvalRequired || comparison.ApprovalRequired || comparison.Status == "changed" || comparison.Status == "missing_approved"
	}
	if len(r.Comparisons) == 0 && r.Status != "no_rehearsals" {
		return errors.New("empty golden report must be no_rehearsals")
	}
	if approvalRequired && r.Status != "approval_required" {
		return errors.New("changed golden requires approval status")
	}
	if !approvalRequired && len(r.Comparisons) != 0 && r.Status != "passed" {
		return errors.New("matched golden report must pass")
	}
	return nil
}

func (c Comparison) Validate() error {
	if !safeID.MatchString(c.ID) || !safeID.MatchString(c.RehearsalID) || strings.TrimSpace(c.DiffSummary) == "" {
		return errors.New("invalid golden comparison identity")
	}
	if c.Status != "matched" && c.Status != "changed" && c.Status != "missing_approved" && c.Status != "skipped" {
		return errors.New("invalid golden comparison status")
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(c.ApprovedArtifactSHA256) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(c.CandidateArtifactSHA256) {
		return errors.New("invalid golden artifact identity")
	}
	if c.Status == "changed" && (!c.ApprovalRequired || !c.UpdateProposed) {
		return errors.New("changed golden must require approval")
	}
	if strings.TrimSpace(c.Kind) == "" || strings.TrimSpace(c.ComparisonClass) == "" || strings.TrimSpace(c.Source.ApprovalPolicy) == "" {
		return errors.New("golden comparison missing registered policy metadata")
	}
	return nil
}

func (r ApprovalRequest) Validate() error {
	if !safeID.MatchString(r.ReportID) || !safeID.MatchString(r.ComparisonID) || strings.TrimSpace(r.ActorID) == "" || strings.TrimSpace(r.ActorRole) == "" {
		return errors.New("invalid golden approval identity")
	}
	if !r.Reauthenticated {
		return errors.New("golden approval requires recent reauthentication")
	}
	if len(strings.TrimSpace(r.Reason)) < 8 || len(r.Reason) > 1000 {
		return errors.New("golden approval requires a bounded reason")
	}
	return nil
}

type ApprovalResolution struct {
	Pending  int `json:"pending"`
	Rejected int `json:"rejected"`
}

func ResolveApprovals(report Report, approvals []Approval) ApprovalResolution {
	latest := map[string]Approval{}
	for _, approval := range approvals {
		if approval.ReportID != report.ID {
			continue
		}
		current, exists := latest[approval.ComparisonID]
		if !exists || approval.CreatedAt.After(current.CreatedAt) ||
			(approval.CreatedAt.Equal(current.CreatedAt) && approval.ID > current.ID) {
			latest[approval.ComparisonID] = approval
		}
	}
	var resolution ApprovalResolution
	for _, comparison := range report.Comparisons {
		if !comparison.ApprovalRequired && comparison.Status != "changed" && comparison.Status != "missing_approved" {
			continue
		}
		approval, exists := latest[comparison.ID]
		if !exists {
			resolution.Pending++
			continue
		}
		if !approval.Approved {
			resolution.Pending++
			resolution.Rejected++
		}
	}
	return resolution
}

func digest(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:])
}

func ChangedReportForTest(jobID, projectID, contractSHA256, riskLevel, resultSHA string) (Report, error) {
	source := RehearsalSource{ID: "update-request-fixture", Kind: "api", OperationID: "fixture", ArtifactKinds: []string{"schema"}, ComparisonClass: "structured", ApprovalPolicy: "review-required", Source: "test"}
	report := Report{JobID: jobID, ProjectID: projectID, SchemaVersion: SchemaVersion, ContractSHA256: contractSHA256, RiskLevel: riskLevel, ResultSHA: resultSHA, SourceContext: "golden_rehearsal_gate_v1", Status: "approval_required", PolicySummary: "test fixture"}
	report.Comparisons = []Comparison{{
		ID: "cmp-update-request-fixture", RehearsalID: source.ID, Kind: source.Kind, ComparisonClass: source.ComparisonClass,
		ApprovedArtifactSHA256: digest("approved", source.ID), CandidateArtifactSHA256: digest("candidate", resultSHA, source.ID),
		Status: "changed", DiffSummary: "fixture changed golden", TolerancePolicy: json.RawMessage(`{}`), MaskPolicy: json.RawMessage(`{}`),
		ApprovalRequired: true, UpdateProposed: true, Provenance: []string{"fixture"}, Source: source,
	}}
	return report, report.Validate()
}

func (a Approval) Validate() error {
	if !safeID.MatchString(a.ID) || !safeID.MatchString(a.ReportID) || !safeID.MatchString(a.ComparisonID) {
		return fmt.Errorf("invalid golden approval identity")
	}
	if strings.TrimSpace(a.ActorID) == "" || strings.TrimSpace(a.ActorRole) == "" || strings.TrimSpace(a.Reason) == "" || !a.Reauthenticated {
		return fmt.Errorf("invalid golden approval authority")
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(a.ApprovedArtifactSHA256) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(a.CandidateArtifactSHA256) {
		return fmt.Errorf("invalid golden approval artifact identity")
	}
	return nil
}
