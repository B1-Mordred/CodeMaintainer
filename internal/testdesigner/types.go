package testdesigner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

const SchemaVersion = 1

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var sha256Hex = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Proposal struct {
	ID                string   `json:"id"`
	Category          string   `json:"category"`
	Claim             string   `json:"claim"`
	Rationale         string   `json:"rationale"`
	EvidenceIDs       []string `json:"evidence_ids"`
	SuggestedTests    []string `json:"suggested_tests"`
	GoldenRehearsals  []string `json:"golden_rehearsals"`
	Disposition       string   `json:"disposition"`
	DispositionReason string   `json:"disposition_reason,omitempty"`
}

type Disposition struct {
	ID          string    `json:"id"`
	ReportID    string    `json:"report_id"`
	JobID       string    `json:"job_id"`
	ProposalID  string    `json:"proposal_id"`
	Disposition string    `json:"disposition"`
	Reason      string    `json:"reason"`
	ActorID     string    `json:"actor_id"`
	ActorRole   string    `json:"actor_role"`
	CreatedAt   time.Time `json:"created_at"`
}

type Report struct {
	ID                   string     `json:"id"`
	JobID                string     `json:"job_id"`
	SchemaVersion        int        `json:"schema_version"`
	ContractSHA256       string     `json:"contract_sha256"`
	RiskLevel            string     `json:"risk_level"`
	ResultSHA            string     `json:"result_sha"`
	SourceContext        string     `json:"source_context"`
	Proposals            []Proposal `json:"proposals"`
	DispositionsRequired bool       `json:"dispositions_required"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"created_at"`
}

type Store interface {
	SaveTestDesignerReport(context.Context, Report) (Report, error)
	ListTestDesignerReports(context.Context, string, int) ([]Report, error)
	SaveTestDesignerDisposition(context.Context, Disposition) (Disposition, error)
	ListTestDesignerDispositions(context.Context, string, string, int) ([]Disposition, error)
}

func NewSkipped(jobID, contractSHA256, riskLevel, resultSHA string) (Report, error) {
	report := Report{
		JobID: jobID, SchemaVersion: SchemaVersion, ContractSHA256: contractSHA256,
		RiskLevel: riskLevel, ResultSHA: resultSHA, SourceContext: "risk_router",
		Proposals: []Proposal{}, DispositionsRequired: false, Status: "skipped",
	}
	if err := report.Validate(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func DecodeReport(payload []byte, jobID, contractSHA256, riskLevel, resultSHA string) (Report, error) {
	if len(payload) == 0 || len(payload) > 2<<20 {
		return Report{}, errors.New("test designer report is empty or oversized")
	}
	var report Report
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return Report{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Report{}, errors.New("test designer report contains trailing data")
	}
	if report.JobID != jobID || report.ContractSHA256 != contractSHA256 ||
		report.RiskLevel != riskLevel || report.ResultSHA != resultSHA {
		return Report{}, errors.New("test designer report is not bound to the approved contract, risk, and result commit")
	}
	if err := report.Validate(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func (r Report) Validate() error {
	if !safeID.MatchString(r.JobID) || r.SchemaVersion != SchemaVersion ||
		!sha256Hex.MatchString(r.ContractSHA256) || strings.TrimSpace(r.ResultSHA) == "" ||
		(r.RiskLevel != "low" && r.RiskLevel != "medium" && r.RiskLevel != "high") ||
		(r.Status != "proposed" && r.Status != "skipped") ||
		strings.TrimSpace(r.SourceContext) == "" || len(r.Proposals) > 100 {
		return errors.New("test designer report violates version-one bounds")
	}
	if r.Status == "skipped" && (r.RiskLevel == "medium" || r.RiskLevel == "high" || len(r.Proposals) != 0 || r.DispositionsRequired) {
		return errors.New("only low-risk test designer reports may be skipped")
	}
	if r.Status == "proposed" && r.RiskLevel == "low" {
		return errors.New("low-risk jobs must not produce independent Test Designer proposals")
	}
	seen := map[string]bool{}
	for _, proposal := range r.Proposals {
		if !safeID.MatchString(proposal.ID) || seen[proposal.ID] ||
			strings.TrimSpace(proposal.Category) == "" ||
			strings.TrimSpace(proposal.Claim) == "" || strings.TrimSpace(proposal.Rationale) == "" ||
			(proposal.Disposition != "pending" && proposal.Disposition != "accepted" &&
				proposal.Disposition != "rejected" && proposal.Disposition != "not_applicable") ||
			len(proposal.Claim) > 8000 || len(proposal.Rationale) > 8000 ||
			len(proposal.EvidenceIDs) > 32 || len(proposal.SuggestedTests) > 32 ||
			len(proposal.GoldenRehearsals) > 32 {
			return errors.New("test designer proposal is missing a stable claim, evidence, or disposition")
		}
		seen[proposal.ID] = true
		if proposal.Disposition == "pending" && !r.DispositionsRequired {
			return errors.New("pending test designer proposals require explicit dispositions")
		}
	}
	return nil
}

func (d Disposition) Validate() error {
	if (d.ID != "" && !safeID.MatchString(d.ID)) || !safeID.MatchString(d.ReportID) ||
		!safeID.MatchString(d.JobID) || !safeID.MatchString(d.ProposalID) ||
		(d.Disposition != "accepted" && d.Disposition != "rejected" && d.Disposition != "not_applicable") ||
		strings.TrimSpace(d.Reason) == "" || len(d.Reason) > 4000 ||
		strings.TrimSpace(d.ActorID) == "" ||
		(d.ActorRole != "reviewer" && d.ActorRole != "administrator") {
		return errors.New("test designer disposition violates authorization or provenance bounds")
	}
	return nil
}

func ApplyDispositions(reports []Report, dispositions []Disposition) []Report {
	latest := latestDispositions(dispositions)
	result := make([]Report, len(reports))
	for reportIndex, report := range reports {
		copyReport := report
		copyReport.Proposals = append([]Proposal(nil), report.Proposals...)
		for proposalIndex, proposal := range copyReport.Proposals {
			if disposition, ok := latest[report.ID+"/"+proposal.ID]; ok {
				proposal.Disposition = disposition.Disposition
				proposal.DispositionReason = disposition.Reason
				copyReport.Proposals[proposalIndex] = proposal
			}
		}
		result[reportIndex] = copyReport
	}
	return result
}

func PendingDispositionCount(reports []Report, dispositions []Disposition) int {
	count := 0
	for _, report := range ApplyDispositions(reports, dispositions) {
		if !report.DispositionsRequired {
			continue
		}
		for _, proposal := range report.Proposals {
			if proposal.Disposition == "pending" {
				count++
			}
		}
	}
	return count
}

func latestDispositions(dispositions []Disposition) map[string]Disposition {
	latest := map[string]Disposition{}
	for _, disposition := range dispositions {
		key := disposition.ReportID + "/" + disposition.ProposalID
		if current, ok := latest[key]; !ok || disposition.CreatedAt.After(current.CreatedAt) ||
			(disposition.CreatedAt.Equal(current.CreatedAt) && disposition.ID > current.ID) {
			latest[key] = disposition
		}
	}
	return latest
}
