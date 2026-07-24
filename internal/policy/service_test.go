package policy

import (
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
