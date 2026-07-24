package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuiltInContractsAreVersionedAndSchemaHashBound(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatal(err)
	}
	contracts := BuiltInContracts()
	if len(contracts) < 7 {
		t.Fatalf("expected all Increment 2 structured output contracts, got %d", len(contracts))
	}
	seen := map[ContractKind]bool{}
	for _, contract := range contracts {
		seen[contract.Kind] = true
		if contract.SchemaVersion != SchemaVersion || len(contract.SchemaSHA256) != 64 || !json.Valid(contract.JSONSchema) {
			t.Fatalf("contract is not version/hash/schema bound: %#v", contract)
		}
		if contract.RetryPolicy.MaxAttempts != 3 || !contract.RetryPolicy.ValidationFeedback {
			t.Fatalf("contract lacks bounded validation retry policy: %#v", contract.RetryPolicy)
		}
	}
	for _, required := range []ContractKind{
		ContractTaskPacket, ContractImplementationResult, ContractQCReport, ContractTestProposal,
		ContractDocumentationManifest, ContractRiskAssessment, ContractCompletionSummary,
	} {
		if !seen[required] {
			t.Fatalf("registry omits %s", required)
		}
	}
}

func TestResponseFormatUsesStrictJSONSchema(t *testing.T) {
	format, err := ResponseFormat(ContractImplementationResult)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(format)
	text := string(payload)
	for _, required := range []string{`"type":"json_schema"`, `"strict":true`, `"schema"`, "implementation_result_v1"} {
		if !strings.Contains(text, required) {
			t.Fatalf("response format omits %s: %s", required, text)
		}
	}
}

func TestValidationRecordBindsPayloadAndSchema(t *testing.T) {
	record, err := NewValidationRecord("job_one", "implementation_result", ContractImplementationResult, []byte(`{"schema_version":1}`), 1, false, json.Unmarshal([]byte(`{`), &struct{}{}), "artifact_1")
	if err != nil {
		t.Fatal(err)
	}
	if record.SchemaVersion != SchemaVersion || len(record.SchemaSHA256) != 64 || len(record.PayloadSHA256) != 64 || record.Valid {
		t.Fatalf("record is not hash-bound to schema and payload: %#v", record)
	}
	if record.Error == "" {
		t.Fatalf("invalid validation record did not retain bounded error text")
	}
}

func FuzzTaskPacketUntrustedMarkdownAndPaths(f *testing.F) {
	f.Add("docs/runbook.md", "# Title\n\nRun `go test ./...`.")
	f.Add("../outside.md", "# malicious\n\n<script>alert(1)</script>")
	f.Add("docs/unsafe\u0000.md", "[link](file:///etc/passwd)")
	f.Fuzz(func(t *testing.T, path, markdown string) {
		packet := TaskPacket{
			SchemaVersion: 1, Mode: "documentation", JobID: "job_doc", ProjectID: "owner-repo",
			OriginalTask: "update documentation from untrusted markdown", BaseSHA: strings.Repeat("a", 40),
			ResultSHA: strings.Repeat("b", 40), ContractSHA256: strings.Repeat("c", 64), RiskLevel: "medium",
			AcceptanceCriteria: []Criterion{{ID: "AC-1", Statement: markdown, VerificationMethod: "documentation_review"}},
			RelevantFiles:      []FileContext{{Path: path, Content: markdown}},
		}
		payload, err := json.Marshal(packet)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeTaskPacket(payload, "documentation")
		if err == nil {
			if !safeRelativePath(decoded.RelevantFiles[0].Path) || len(decoded.RelevantFiles[0].Content) > maxTextBytes {
				t.Fatalf("unsafe markdown/path packet decoded: %#v", decoded.RelevantFiles[0])
			}
		}
	})
}
