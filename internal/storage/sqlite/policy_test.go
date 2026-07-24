package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/policy"
)

func TestPolicyLifecyclePersistsBundlesActivationsSimulationsAndDecisions(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bundles, err := store.ListPolicyBundles(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 || bundles[0].ID != policy.BuiltinBundle().ID || bundles[0].Status != "active" {
		t.Fatalf("unexpected builtin bundles: %#v", bundles)
	}
	activation, err := store.ActivatePolicyBundle(ctx, policy.ActivationRequest{
		BundleID: bundles[0].ID, Action: "activate", ActorID: "admin", ActorRole: "administrator",
		Reason: "exercise activation ledger", StagedRolloutPercent: 100, Reauthenticated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if activation.BundleID != bundles[0].ID {
		t.Fatalf("activation did not bind bundle: %#v", activation)
	}
	service := policy.NewService(store)
	simulation, err := service.Simulate(ctx, policy.SimulationRequest{
		DecisionPoint: policy.DecisionPointQCRequirement,
		Input:         []byte(`{"risk_level":"low","result_sha":"0123456789abcdef0123456789abcdef01234567","full_verification_passed":true,"documentation_status":"passed","github_token":"secret"}`),
		ActorID:       "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if simulation.Status != "passed" || strings.Contains(string(simulation.RedactedInput), "secret") {
		t.Fatalf("unexpected simulation: %#v input=%s", simulation, simulation.RedactedInput)
	}
	decision, err := service.EvaluateAndRecord(ctx, policy.EvaluationRequest{
		DecisionPoint: policy.DecisionPointForgePublication,
		Input:         map[string]any{"risk_level": "low", "full_verification_passed": true},
	})
	if err == nil || decision.Allowed || !strings.Contains(decision.Explanation, "missing requirement") {
		t.Fatalf("expected denied recorded decision, got %#v, %v", decision, err)
	}
	decisions, err := store.ListPolicyDecisions(ctx, "", 10)
	if err != nil || len(decisions) != 1 {
		t.Fatalf("retained decisions = %#v, %v", decisions, err)
	}
}
