package policy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestEvaluateAllowsQCWithRedactedInputs(t *testing.T) {
	decision, err := Evaluate(EvaluationRequest{
		DecisionPoint: DecisionPointQCRequirement,
		JobID:         "job-1",
		Input: map[string]any{
			"risk_level": "medium", "result_sha": "0123456789abcdef0123456789abcdef01234567",
			"full_verification_passed": true, "documentation_status": "passed", "golden_status": "passed",
			"github_token": "secret-value",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.Outcome != OutcomeAllow || len(decision.RequiredStages) == 0 {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if strings.Contains(string(decision.RedactedInput), "secret-value") || !strings.Contains(string(decision.RedactedInput), "***REDACTED***") {
		t.Fatalf("policy input was not redacted: %s", decision.RedactedInput)
	}
}

func TestEvaluateDeniesProtectedPublicationWhenRequirementMissing(t *testing.T) {
	decision, err := Evaluate(EvaluationRequest{
		DecisionPoint: DecisionPointForgePublication,
		Input:         map[string]any{"risk_level": "low", "full_verification_passed": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || decision.Outcome != OutcomeDeny || !strings.Contains(decision.Explanation, "missing requirement") {
		t.Fatalf("publication was not denied with explanation: %#v", decision)
	}
}

func TestSimulationRejectsUnknownFieldsAndTrailingData(t *testing.T) {
	raw := json.RawMessage(`{"risk_level":"low"} {"trailing":true}`)
	if _, err := DecodeSimulationInput(raw); err == nil {
		t.Fatal("simulation input with trailing JSON was accepted")
	}
}

func TestAdvancedRegoPolicyEvaluatesThroughOPA(t *testing.T) {
	source := `package codemaintainer.policy

default decision := {
  "allowed": false,
  "outcome": "deny",
  "required_stages": ["baseline", "qc"],
  "explanation": "missing exact result",
}

decision := {
  "allowed": true,
  "outcome": "allow",
  "required_stages": ["baseline", "full_tests", "qc"],
  "explanation": "qc allowed by advanced rego",
} if {
  input.decision_point == "qc_requirement"
  input.input.result_sha != ""
  input.input.full_verification_passed == true
}`
	formatted, err := FormatRegoSource(source)
	if err != nil {
		t.Fatal(err)
	}
	bundle := BuiltinBundle()
	bundle.ID = "advanced-bundle"
	bundle.SourceKind = SourceAdvancedRego
	bundle.RegoSource = formatted
	bundle.Version = "test.1"
	decision, err := Evaluate(EvaluationRequest{
		Bundle: &bundle, DecisionPoint: DecisionPointQCRequirement,
		Input: map[string]any{"risk_level": "medium", "result_sha": "0123456789abcdef0123456789abcdef01234567", "full_verification_passed": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || !strings.Contains(decision.Explanation, "advanced rego") {
		t.Fatalf("unexpected advanced decision: %#v", decision)
	}
}

func TestRunTestsReportsCoverageAndFailures(t *testing.T) {
	bundle := BuiltinBundle()
	bundle.ID = "builtin-test"
	run, err := NewService(nil).RunTests(context.Background(), bundle, []TestCase{{
		ID: "deny-publication", DecisionPoint: DecisionPointForgePublication,
		Input:       json.RawMessage(`{"risk_level":"low","full_verification_passed":true}`),
		WantAllowed: false, WantExplanation: "missing requirement",
	}}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "passed" || len(run.Results) != 1 || !run.Results[0].Passed {
		t.Fatalf("unexpected test run: %#v", run)
	}
	if run.Coverage["decision_points_covered"].(int) != 1 {
		t.Fatalf("coverage not recorded: %#v", run.Coverage)
	}
}
