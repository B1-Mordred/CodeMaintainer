package agents

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	SchemaVersion  = 1
	MaxPacketBytes = 2 << 20
	maxTextBytes   = 128 << 10
)

var (
	identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	gitSHA     = regexp.MustCompile(`^[a-f0-9]{40}(?:[a-f0-9]{24})?$`)
	findingID  = regexp.MustCompile(`^QC-[A-Z0-9][A-Z0-9_-]{2,63}$`)
)

type Criterion struct {
	ID                 string `json:"id"`
	Statement          string `json:"statement"`
	VerificationMethod string `json:"verification_method"`
}

type FileContext struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type VerificationEvidence struct {
	Class   string `json:"class"`
	Passed  bool   `json:"passed"`
	Summary string `json:"summary"`
}

type TaskPacket struct {
	SchemaVersion      int                    `json:"schema_version"`
	Mode               string                 `json:"mode"`
	JobID              string                 `json:"job_id"`
	OriginalTask       string                 `json:"original_task"`
	AcceptanceCriteria []Criterion            `json:"acceptance_criteria"`
	BaseSHA            string                 `json:"base_sha"`
	ResultSHA          string                 `json:"result_sha,omitempty"`
	ContractSHA256     string                 `json:"contract_sha256,omitempty"`
	RiskLevel          string                 `json:"risk_level,omitempty"`
	ReviewCycle        int                    `json:"review_cycle"`
	Diff               string                 `json:"diff,omitempty"`
	RelevantFiles      []FileContext          `json:"relevant_files"`
	Verification       []VerificationEvidence `json:"verification"`
	BlockingFindings   []Finding              `json:"blocking_findings,omitempty"`
}

type Edit struct {
	Path           string `json:"path"`
	Content        string `json:"content"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

type ActionSummary struct {
	Reproduction    string   `json:"reproduction"`
	Plan            string   `json:"plan"`
	Changes         []string `json:"changes"`
	RegressionTests []string `json:"regression_tests"`
	Checks          []string `json:"checks"`
	Evidence        []string `json:"evidence"`
}

type ImplementationResult struct {
	SchemaVersion int           `json:"schema_version"`
	Summary       ActionSummary `json:"summary"`
	Edits         []Edit        `json:"edits"`
}

type Location struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

type Finding struct {
	ID                 string   `json:"id"`
	Severity           string   `json:"severity"`
	Category           string   `json:"category"`
	Claim              string   `json:"claim"`
	Location           Location `json:"location"`
	Evidence           string   `json:"evidence"`
	RequiredResolution string   `json:"required_resolution"`
	VerificationMethod string   `json:"verification_method"`
}

type VerificationRequest struct {
	CommandClass string `json:"command_class"`
	Reason       string `json:"reason"`
}

type QCReport struct {
	SchemaVersion        int                   `json:"schema_version"`
	JobID                string                `json:"job_id"`
	BaseSHA              string                `json:"base_sha"`
	ResultSHA            string                `json:"result_sha"`
	Verdict              string                `json:"verdict"`
	Findings             []Finding             `json:"findings"`
	VerificationRequests []VerificationRequest `json:"verification_requests"`
}

func DecodeTaskPacket(payload []byte, expectedMode string) (TaskPacket, error) {
	if len(payload) == 0 || len(payload) > MaxPacketBytes {
		return TaskPacket{}, errors.New("agent task packet is empty or oversized")
	}
	var packet TaskPacket
	if err := strictJSON(payload, &packet); err != nil {
		return TaskPacket{}, err
	}
	if packet.SchemaVersion != SchemaVersion || packet.Mode != expectedMode || !validTaskMode(packet.Mode) || !identifier.MatchString(packet.JobID) ||
		strings.TrimSpace(packet.OriginalTask) == "" || len(packet.OriginalTask) > maxTextBytes ||
		!gitSHA.MatchString(packet.BaseSHA) || len(packet.AcceptanceCriteria) == 0 || len(packet.AcceptanceCriteria) > 64 ||
		len(packet.RelevantFiles) > 64 || len(packet.Verification) > 64 || len(packet.BlockingFindings) > 100 || len(packet.Diff) > 1<<20 {
		return TaskPacket{}, errors.New("agent task packet violates version-one bounds")
	}
	if packet.ReviewCycle < 0 || packet.ReviewCycle > 10 {
		return TaskPacket{}, errors.New("agent review cycle is out of bounds")
	}
	if (expectedMode == "qc" || expectedMode == "test_designer") && !gitSHA.MatchString(packet.ResultSHA) {
		return TaskPacket{}, errors.New("review task requires an exact result SHA")
	}
	if expectedMode == "test_designer" && (!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(packet.ContractSHA256) ||
		(packet.RiskLevel != "medium" && packet.RiskLevel != "high")) {
		return TaskPacket{}, errors.New("test designer task requires exact contract and risk bindings")
	}
	seenCriteria := make(map[string]struct{}, len(packet.AcceptanceCriteria))
	for _, criterion := range packet.AcceptanceCriteria {
		if !identifier.MatchString(criterion.ID) || strings.TrimSpace(criterion.Statement) == "" ||
			strings.TrimSpace(criterion.VerificationMethod) == "" || len(criterion.Statement)+len(criterion.VerificationMethod) > 16000 {
			return TaskPacket{}, errors.New("acceptance criterion is invalid")
		}
		if _, exists := seenCriteria[criterion.ID]; exists {
			return TaskPacket{}, errors.New("acceptance criterion IDs must be unique")
		}
		seenCriteria[criterion.ID] = struct{}{}
	}
	for _, file := range packet.RelevantFiles {
		if !safeRelativePath(file.Path) || len(file.Content) > maxTextBytes {
			return TaskPacket{}, errors.New("relevant file context is invalid")
		}
	}
	for _, finding := range packet.BlockingFindings {
		if err := finding.Validate(); err != nil {
			return TaskPacket{}, err
		}
	}
	return packet, nil
}

func validTaskMode(mode string) bool {
	return mode == "implementation" || mode == "repair" || mode == "qc" || mode == "test_designer"
}

func DecodeImplementationResult(payload []byte) (ImplementationResult, error) {
	if len(payload) == 0 || len(payload) > 10<<20 {
		return ImplementationResult{}, errors.New("implementation response is empty or oversized")
	}
	var result ImplementationResult
	if err := strictJSON(payload, &result); err != nil {
		return ImplementationResult{}, err
	}
	if result.SchemaVersion != SchemaVersion || len(result.Edits) > 32 || len(result.Summary.Reproduction) == 0 ||
		len(result.Summary.Plan) == 0 || len(result.Summary.Changes) > 64 || len(result.Summary.RegressionTests) > 64 ||
		len(result.Summary.Checks) > 64 || len(result.Summary.Evidence) > 64 {
		return ImplementationResult{}, errors.New("implementation response violates version-one bounds")
	}
	var total int
	seen := make(map[string]struct{}, len(result.Edits))
	for _, edit := range result.Edits {
		if !safeRelativePath(edit.Path) || len(edit.Content) > 1<<20 ||
			(edit.ExpectedSHA256 != "" && !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(edit.ExpectedSHA256)) {
			return ImplementationResult{}, errors.New("implementation edit is invalid")
		}
		if _, exists := seen[edit.Path]; exists {
			return ImplementationResult{}, errors.New("implementation edits contain duplicate paths")
		}
		seen[edit.Path] = struct{}{}
		total += len(edit.Content)
		if total > 8<<20 {
			return ImplementationResult{}, errors.New("implementation edits exceed total size limit")
		}
	}
	return result, nil
}

func DecodeQCReport(payload []byte, packet TaskPacket) (QCReport, error) {
	if len(payload) == 0 || len(payload) > 2<<20 {
		return QCReport{}, errors.New("QC response is empty or oversized")
	}
	var report QCReport
	if err := strictJSON(payload, &report); err != nil {
		return QCReport{}, err
	}
	verdicts := map[string]bool{"pass": true, "blocking_findings": true, "malformed_evidence": true}
	if report.SchemaVersion != SchemaVersion || report.JobID != packet.JobID || report.BaseSHA != packet.BaseSHA ||
		report.ResultSHA != packet.ResultSHA || !verdicts[report.Verdict] || len(report.Findings) > 100 || len(report.VerificationRequests) > 20 {
		return QCReport{}, errors.New("QC report violates its trusted task binding")
	}
	seen := make(map[string]struct{}, len(report.Findings))
	blocking := false
	for _, finding := range report.Findings {
		if err := finding.Validate(); err != nil {
			return QCReport{}, err
		}
		if _, exists := seen[finding.ID]; exists {
			return QCReport{}, errors.New("QC finding IDs must be unique")
		}
		seen[finding.ID] = struct{}{}
		blocking = blocking || finding.Severity == "blocker" || finding.Severity == "must_fix"
	}
	for _, request := range report.VerificationRequests {
		if !manifestIDLike(request.CommandClass) || strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 4000 {
			return QCReport{}, errors.New("QC verification request is invalid")
		}
	}
	if (report.Verdict == "blocking_findings") != blocking || (report.Verdict == "pass" && len(report.Findings) != 0) {
		return QCReport{}, errors.New("QC verdict does not match evidence-bearing findings")
	}
	return report, nil
}

func (f Finding) Validate() error {
	severities := map[string]bool{"blocker": true, "must_fix": true, "should_fix": true, "note": true}
	if !findingID.MatchString(f.ID) || !severities[f.Severity] || strings.TrimSpace(f.Category) == "" ||
		strings.TrimSpace(f.Claim) == "" || !safeRelativePath(f.Location.Path) || f.Location.Line < 0 ||
		strings.TrimSpace(f.Evidence) == "" || strings.TrimSpace(f.RequiredResolution) == "" ||
		strings.TrimSpace(f.VerificationMethod) == "" || len(f.Claim) > 8000 || len(f.Evidence) > 16000 ||
		len(f.RequiredResolution) > 8000 || len(f.VerificationMethod) > 8000 {
		return errors.New("QC finding lacks a stable identity, location, evidence, resolution, or verification method")
	}
	return nil
}

func strictJSON(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON contract contains trailing data")
	}
	return nil
}

func safeRelativePath(path string) bool {
	clean := filepath.Clean(path)
	return path != "" && path == clean && clean != "." && clean != ".." && !filepath.IsAbs(clean) &&
		!strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !strings.ContainsRune(clean, 0)
}

func manifestIDLike(value string) bool {
	return regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`).MatchString(value)
}

func HashContent(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func ValidateDifferentFamilies(implementation, qc string) error {
	if implementation == "" || qc == "" || implementation == qc {
		return fmt.Errorf("implementation and QC require different non-empty model families")
	}
	return nil
}
