package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

type ContractKind string

const (
	ContractTaskPacket            ContractKind = "task_packet"
	ContractImplementationResult  ContractKind = "implementation_result"
	ContractQCReport              ContractKind = "qc_report"
	ContractTestProposal          ContractKind = "test_proposal"
	ContractDocumentationManifest ContractKind = "documentation_manifest"
	ContractRiskAssessment        ContractKind = "risk_assessment"
	ContractCompletionSummary     ContractKind = "completion_summary"
)

type RetryPolicy struct {
	MaxAttempts        int    `json:"max_attempts"`
	OnExhausted        string `json:"on_exhausted"`
	ValidationFeedback bool   `json:"validation_feedback"`
}

type ContractDescriptor struct {
	Kind                  ContractKind    `json:"kind"`
	Name                  string          `json:"name"`
	SchemaVersion         int             `json:"schema_version"`
	SchemaSHA256          string          `json:"schema_sha256"`
	JSONSchema            json.RawMessage `json:"json_schema"`
	RetryPolicy           RetryPolicy     `json:"retry_policy"`
	CompatibleAgentRoles  []string        `json:"compatible_agent_roles"`
	CompatibleModelModes  []string        `json:"compatible_model_modes"`
	MigrationStatus       string          `json:"migration_status"`
	StructuredOutputHints []string        `json:"structured_output_hints"`
}

type ValidationRecord struct {
	ID            string       `json:"id"`
	JobID         string       `json:"job_id"`
	Phase         string       `json:"phase"`
	ContractKind  ContractKind `json:"contract_kind"`
	SchemaVersion int          `json:"schema_version"`
	SchemaSHA256  string       `json:"schema_sha256"`
	PayloadSHA256 string       `json:"payload_sha256"`
	Attempt       int          `json:"attempt"`
	Valid         bool         `json:"valid"`
	Error         string       `json:"error"`
	ArtifactID    string       `json:"artifact_id"`
	CreatedAt     time.Time    `json:"created_at"`
}

type Store interface {
	RecordAgentContractValidation(context.Context, ValidationRecord) (ValidationRecord, error)
	ListAgentContractValidations(context.Context, string, int) ([]ValidationRecord, error)
}

func BuiltInContracts() []ContractDescriptor {
	contracts := []ContractDescriptor{
		descriptor(ContractTaskPacket, "Agent task packet", taskPacketSchema, []string{"implementation", "repair", "qc"}, []string{"json_schema", "json_object"}),
		descriptor(ContractImplementationResult, "Implementation result", implementationResultSchema, []string{"implementation", "repair"}, []string{"json_schema", "json_object"}),
		descriptor(ContractQCReport, "QC report", qcReportSchema, []string{"qc"}, []string{"json_schema", "json_object"}),
		descriptor(ContractTestProposal, "Test Designer proposal", testProposalSchema, []string{"test_designer"}, []string{"json_schema", "json_object"}),
		descriptor(ContractDocumentationManifest, "Documentation manifest", documentationManifestSchema, []string{"documentation"}, []string{"json_schema", "json_object"}),
		descriptor(ContractRiskAssessment, "Risk assessment", riskAssessmentSchema, []string{"clarifier", "policy"}, []string{"json_schema", "json_object"}),
		descriptor(ContractCompletionSummary, "Completion summary", completionSummarySchema, []string{"implementation", "repair", "qc", "documentation"}, []string{"json_schema", "json_object"}),
	}
	sort.Slice(contracts, func(i, j int) bool { return contracts[i].Kind < contracts[j].Kind })
	return contracts
}

func DescriptorFor(kind ContractKind, version int) (ContractDescriptor, error) {
	for _, descriptor := range BuiltInContracts() {
		if descriptor.Kind == kind && descriptor.SchemaVersion == version {
			return descriptor, nil
		}
	}
	return ContractDescriptor{}, errors.New("agent contract descriptor is not registered")
}

func ResponseFormat(kind ContractKind) (map[string]any, error) {
	descriptor, err := DescriptorFor(kind, SchemaVersion)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   string(descriptor.Kind) + "_v1",
			"strict": true,
			"schema": json.RawMessage(descriptor.JSONSchema),
		},
	}, nil
}

func NewValidationRecord(jobID, phase string, kind ContractKind, payload []byte, attempt int, valid bool, validationErr error, artifactID string) (ValidationRecord, error) {
	descriptor, err := DescriptorFor(kind, SchemaVersion)
	if err != nil {
		return ValidationRecord{}, err
	}
	if jobID == "" || phase == "" || len(payload) == 0 || attempt < 1 || attempt > 20 {
		return ValidationRecord{}, errors.New("agent contract validation record is invalid")
	}
	payloadDigest := sha256.Sum256(payload)
	errorText := ""
	if validationErr != nil {
		errorText = validationErr.Error()
		if len(errorText) > 4000 {
			errorText = errorText[:4000]
		}
	}
	return ValidationRecord{
		JobID: jobID, Phase: phase, ContractKind: kind, SchemaVersion: descriptor.SchemaVersion,
		SchemaSHA256: descriptor.SchemaSHA256, PayloadSHA256: hex.EncodeToString(payloadDigest[:]),
		Attempt: attempt, Valid: valid, Error: errorText, ArtifactID: artifactID,
	}, nil
}

func descriptor(kind ContractKind, name string, schema string, roles []string, modelModes []string) ContractDescriptor {
	var raw json.RawMessage = json.RawMessage(schema)
	digest := sha256.Sum256(raw)
	return ContractDescriptor{
		Kind: kind, Name: name, SchemaVersion: SchemaVersion, SchemaSHA256: hex.EncodeToString(digest[:]),
		JSONSchema:           raw,
		RetryPolicy:          RetryPolicy{MaxAttempts: 3, OnExhausted: "pause_or_fail_by_policy", ValidationFeedback: true},
		CompatibleAgentRoles: append([]string(nil), roles...), CompatibleModelModes: append([]string(nil), modelModes...),
		MigrationStatus: "current", StructuredOutputHints: []string{"strict_json", "no_unknown_fields", "artifact_id_claims_required"},
	}
}

func ValidateRegistry() error {
	seen := map[string]bool{}
	for _, descriptor := range BuiltInContracts() {
		key := fmt.Sprintf("%s:%d", descriptor.Kind, descriptor.SchemaVersion)
		if seen[key] {
			return fmt.Errorf("duplicate agent contract descriptor %s", key)
		}
		seen[key] = true
		if descriptor.SchemaSHA256 == "" || !json.Valid(descriptor.JSONSchema) || descriptor.RetryPolicy.MaxAttempts < 1 {
			return fmt.Errorf("invalid agent contract descriptor %s", key)
		}
	}
	return nil
}

const taskPacketSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["schema_version","mode","job_id","original_task","acceptance_criteria","base_sha","review_cycle","relevant_files","verification"],"properties":{"schema_version":{"const":1},"mode":{"enum":["implementation","repair","qc","test_designer"]},"job_id":{"type":"string","pattern":"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"},"original_task":{"type":"string","minLength":1,"maxLength":131072},"acceptance_criteria":{"type":"array","minItems":1,"maxItems":64},"base_sha":{"type":"string","pattern":"^[a-f0-9]{40}([a-f0-9]{24})?$"},"result_sha":{"type":"string"},"contract_sha256":{"type":"string","pattern":"^[a-f0-9]{64}$"},"risk_level":{"enum":["low","medium","high"]},"review_cycle":{"type":"integer","minimum":0,"maximum":10},"diff":{"type":"string","maxLength":1048576},"relevant_files":{"type":"array","maxItems":64},"verification":{"type":"array","maxItems":64},"blocking_findings":{"type":"array","maxItems":100}}}`

const implementationResultSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["schema_version","summary","edits"],"properties":{"schema_version":{"const":1},"summary":{"type":"object","additionalProperties":false,"required":["reproduction","plan","changes","regression_tests","checks","evidence"],"properties":{"reproduction":{"type":"string","minLength":1},"plan":{"type":"string","minLength":1},"changes":{"type":"array","maxItems":64,"items":{"type":"string"}},"regression_tests":{"type":"array","maxItems":64,"items":{"type":"string"}},"checks":{"type":"array","maxItems":64,"items":{"type":"string"}},"evidence":{"type":"array","maxItems":64,"items":{"type":"string"}}}},"edits":{"type":"array","maxItems":32,"items":{"type":"object","additionalProperties":false,"required":["path","content"],"properties":{"path":{"type":"string"},"content":{"type":"string","maxLength":1048576},"expected_sha256":{"type":"string","pattern":"^[a-f0-9]{64}$"}}}}}}`

const qcReportSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["schema_version","job_id","base_sha","result_sha","verdict","findings","verification_requests"],"properties":{"schema_version":{"const":1},"job_id":{"type":"string"},"base_sha":{"type":"string"},"result_sha":{"type":"string"},"verdict":{"enum":["pass","blocking_findings","malformed_evidence"]},"findings":{"type":"array","maxItems":100},"verification_requests":{"type":"array","maxItems":20}}}`

const testProposalSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["id","job_id","schema_version","contract_sha256","risk_level","result_sha","source_context","proposals","dispositions_required","status"],"properties":{"id":{"type":"string"},"job_id":{"type":"string"},"schema_version":{"const":1},"contract_sha256":{"type":"string","pattern":"^[a-f0-9]{64}$"},"risk_level":{"enum":["medium","high"]},"result_sha":{"type":"string"},"source_context":{"type":"string"},"proposals":{"type":"array","maxItems":100,"items":{"type":"object","additionalProperties":false,"required":["id","category","claim","rationale","evidence_ids","suggested_tests","golden_rehearsals","disposition"],"properties":{"id":{"type":"string"},"category":{"type":"string"},"claim":{"type":"string"},"rationale":{"type":"string"},"evidence_ids":{"type":"array","items":{"type":"string"}},"suggested_tests":{"type":"array","items":{"type":"string"}},"golden_rehearsals":{"type":"array","items":{"type":"string"}},"disposition":{"enum":["pending","accepted","rejected","not_applicable"]},"disposition_reason":{"type":"string"}}}},"dispositions_required":{"type":"boolean"},"status":{"const":"proposed"},"created_at":{"type":"string","format":"date-time"}}}`

const documentationManifestSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["schema_version","job_id","contract_sha256","changes","unsupported_claims"],"properties":{"schema_version":{"const":1},"job_id":{"type":"string"},"contract_sha256":{"type":"string","pattern":"^[a-f0-9]{64}$"},"changes":{"type":"array","maxItems":200},"unsupported_claims":{"type":"array","maxItems":200}}}`

const riskAssessmentSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["schema_version","job_id","contract_sha256","level","score","signals","routing","policy_decision"],"properties":{"schema_version":{"const":1},"job_id":{"type":"string"},"contract_sha256":{"type":"string","pattern":"^[a-f0-9]{64}$"},"level":{"enum":["low","medium","high"]},"score":{"type":"integer","minimum":0},"signals":{"type":"array"},"routing":{"type":"object"},"policy_decision":{"type":"string"}}}`

const completionSummarySchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["schema_version","job_id","contract_sha256","result_sha","evidence_ids","unsupported_claims","limitations"],"properties":{"schema_version":{"const":1},"job_id":{"type":"string"},"contract_sha256":{"type":"string","pattern":"^[a-f0-9]{64}$"},"result_sha":{"type":"string"},"evidence_ids":{"type":"array","items":{"type":"string"}},"unsupported_claims":{"type":"array","items":{"type":"string"}},"limitations":{"type":"array","items":{"type":"string"}}}}`
