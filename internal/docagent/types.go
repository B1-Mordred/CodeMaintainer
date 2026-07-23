package docagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const SchemaVersion = 1

var (
	safeID    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	sha256Hex = regexp.MustCompile(`^[a-f0-9]{64}$`)
	gitSHA    = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)
)

type Requirement struct {
	ID            string   `json:"id"`
	Document      string   `json:"document"`
	Reason        string   `json:"reason"`
	Source        string   `json:"source"`
	RenderTargets []string `json:"render_targets"`
	Required      bool     `json:"required"`
	Status        string   `json:"status"`
}

type Change struct {
	Path              string   `json:"path"`
	Action            string   `json:"action"`
	PolicyRule        string   `json:"policy_rule"`
	SourceOfTruth     string   `json:"source_of_truth"`
	LinkedEvidenceIDs []string `json:"linked_evidence_ids"`
}

type Check struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Target  string `json:"target"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type UnsupportedClaim struct {
	Claim    string `json:"claim"`
	Location string `json:"location"`
	Reason   string `json:"reason"`
}

type Edit struct {
	Path           string `json:"path"`
	Content        string `json:"content"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

type Manifest struct {
	ID                string             `json:"id"`
	JobID             string             `json:"job_id"`
	SchemaVersion     int                `json:"schema_version"`
	ProjectID         string             `json:"project_id"`
	ContractSHA256    string             `json:"contract_sha256"`
	RiskLevel         string             `json:"risk_level"`
	ResultSHA         string             `json:"result_sha"`
	SourceContext     string             `json:"source_context"`
	PolicyVersion     string             `json:"policy_version"`
	Requirements      []Requirement      `json:"requirements"`
	Changes           []Change           `json:"changes"`
	Checks            []Check            `json:"checks"`
	UnsupportedClaims []UnsupportedClaim `json:"unsupported_claims"`
	Edits             []Edit             `json:"edits"`
	Status            string             `json:"status"`
	PolicySummary     string             `json:"policy_summary"`
	CreatedAt         time.Time          `json:"created_at"`
}

type Store interface {
	SaveDocumentationManifest(context.Context, Manifest) (Manifest, error)
	ListDocumentationManifests(context.Context, string, int) ([]Manifest, error)
}

func DecodeManifest(payload []byte, jobID, projectID, contractSHA256, riskLevel, resultSHA string) (Manifest, error) {
	if len(payload) == 0 || len(payload) > 4<<20 {
		return Manifest{}, errors.New("documentation manifest is empty or oversized")
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("documentation manifest contains trailing data")
	}
	if manifest.JobID != jobID || manifest.ProjectID != projectID || manifest.ContractSHA256 != contractSHA256 ||
		manifest.RiskLevel != riskLevel || manifest.ResultSHA != resultSHA {
		return Manifest{}, errors.New("documentation manifest is not bound to the approved contract, risk, project, and result commit")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func NoDocumentationRequired(jobID, projectID, contractSHA256, riskLevel, resultSHA string) (Manifest, error) {
	manifest := Manifest{
		JobID: jobID, ProjectID: projectID, SchemaVersion: SchemaVersion,
		ContractSHA256: contractSHA256, RiskLevel: riskLevel, ResultSHA: resultSHA,
		SourceContext: "documentation_policy_v1", PolicyVersion: "documentation-policy-v1",
		Requirements: []Requirement{}, Changes: []Change{}, Checks: []Check{{
			ID: "DOC-CHECK-NONE", Kind: "policy", Target: "change-impact", Status: "passed",
			Summary: "controller policy found no required documentation update for this exact candidate",
		}},
		UnsupportedClaims: []UnsupportedClaim{}, Edits: []Edit{}, Status: "no_documentation_required",
		PolicySummary: "no documentation update was required by the registered documentation policy for this exact candidate",
	}
	return manifest, manifest.Validate()
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion || !safeID.MatchString(m.JobID) || !safeID.MatchString(m.ProjectID) ||
		!sha256Hex.MatchString(m.ContractSHA256) || !gitSHA.MatchString(m.ResultSHA) ||
		(m.RiskLevel != "low" && m.RiskLevel != "medium" && m.RiskLevel != "high") ||
		strings.TrimSpace(m.SourceContext) == "" || strings.TrimSpace(m.PolicyVersion) == "" ||
		strings.TrimSpace(m.PolicySummary) == "" || len(m.Requirements) > 200 || len(m.Changes) > 200 ||
		len(m.Checks) > 200 || len(m.UnsupportedClaims) > 200 || len(m.Edits) > 64 {
		return errors.New("documentation manifest violates version-one bounds")
	}
	if m.Status != "passed" && m.Status != "changes_applied" && m.Status != "no_documentation_required" && m.Status != "blocked" {
		return errors.New("invalid documentation manifest status")
	}
	if len(m.UnsupportedClaims) != 0 && m.Status != "blocked" {
		return errors.New("unsupported documentation claims must block")
	}
	if len(m.Requirements) == 0 && m.Status != "no_documentation_required" {
		return errors.New("empty documentation policy result must be no_documentation_required")
	}
	if len(m.Requirements) != 0 && m.Status == "no_documentation_required" {
		return errors.New("required documentation cannot be marked unnecessary")
	}
	if len(m.Changes) != 0 && m.Status != "changes_applied" && m.Status != "passed" {
		return errors.New("documentation changes require a passing or applied status")
	}
	for _, requirement := range m.Requirements {
		if err := requirement.Validate(); err != nil {
			return err
		}
	}
	for _, change := range m.Changes {
		if err := change.Validate(); err != nil {
			return err
		}
	}
	for _, check := range m.Checks {
		if err := check.Validate(); err != nil {
			return err
		}
	}
	for _, claim := range m.UnsupportedClaims {
		if strings.TrimSpace(claim.Claim) == "" || strings.TrimSpace(claim.Location) == "" ||
			strings.TrimSpace(claim.Reason) == "" || len(claim.Claim)+len(claim.Location)+len(claim.Reason) > 16000 {
			return errors.New("unsupported documentation claim lacks bounded evidence")
		}
	}
	for _, edit := range m.Edits {
		if !safeRelativePath(edit.Path) || len(edit.Content) > 1<<20 ||
			(edit.ExpectedSHA256 != "" && !sha256Hex.MatchString(edit.ExpectedSHA256)) {
			return errors.New("documentation edit is invalid")
		}
	}
	return nil
}

func (r Requirement) Validate() error {
	if !safeID.MatchString(r.ID) || !safeRelativePath(r.Document) || strings.TrimSpace(r.Reason) == "" ||
		strings.TrimSpace(r.Source) == "" || len(r.RenderTargets) > 16 ||
		(r.Status != "satisfied" && r.Status != "pending" && r.Status != "not_required" && r.Status != "blocked") {
		return errors.New("documentation requirement lacks stable policy evidence")
	}
	if r.Required && r.Status == "not_required" {
		return errors.New("required documentation cannot be not_required")
	}
	for _, target := range r.RenderTargets {
		if !safeID.MatchString(target) {
			return errors.New("documentation render target is invalid")
		}
	}
	return nil
}

func (c Change) Validate() error {
	if !safeRelativePath(c.Path) || strings.TrimSpace(c.PolicyRule) == "" ||
		strings.TrimSpace(c.SourceOfTruth) == "" || len(c.LinkedEvidenceIDs) > 32 ||
		(c.Action != "created" && c.Action != "updated" && c.Action != "not_changed") {
		return errors.New("documentation change lacks path, action, policy rule, or source of truth")
	}
	for _, evidence := range c.LinkedEvidenceIDs {
		if !safeID.MatchString(evidence) {
			return errors.New("documentation evidence reference is invalid")
		}
	}
	return nil
}

func (c Check) Validate() error {
	if !safeID.MatchString(c.ID) || strings.TrimSpace(c.Kind) == "" || strings.TrimSpace(c.Target) == "" ||
		strings.TrimSpace(c.Summary) == "" || (c.Status != "passed" && c.Status != "failed" && c.Status != "not_run") {
		return errors.New("documentation check lacks bounded status evidence")
	}
	return nil
}

func safeRelativePath(path string) bool {
	clean := filepath.Clean(path)
	return path != "" && path == clean && clean != "." && clean != ".." && !filepath.IsAbs(clean) &&
		!strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !strings.ContainsRune(clean, 0)
}
