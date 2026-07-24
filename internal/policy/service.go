package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) ListBundles(ctx context.Context, limit int) ([]Bundle, error) {
	return s.store.ListPolicyBundles(ctx, limit)
}

func (s *Service) ListActivations(ctx context.Context, limit int) ([]Activation, error) {
	return s.store.ListPolicyActivations(ctx, limit)
}

func (s *Service) Activate(ctx context.Context, request ActivationRequest) (Activation, error) {
	if strings.TrimSpace(request.BundleID) == "" {
		request.BundleID = BuiltinBundle().ID
	}
	if request.Action == "" {
		request.Action = "activate"
	}
	if strings.TrimSpace(request.ActorID) == "" || strings.TrimSpace(request.ActorRole) == "" ||
		strings.TrimSpace(request.Reason) == "" {
		return Activation{}, errors.New("policy activation requires actor and reason")
	}
	if !request.Reauthenticated {
		return Activation{}, errors.New("policy activation requires recent reauthentication")
	}
	if _, err := s.store.GetPolicyBundle(ctx, request.BundleID); err != nil {
		return Activation{}, err
	}
	return s.store.ActivatePolicyBundle(ctx, request)
}

func (s *Service) Simulate(ctx context.Context, request SimulationRequest) (Simulation, error) {
	if !KnownDecisionPoint(request.DecisionPoint) {
		return Simulation{}, errors.New("unknown policy decision point")
	}
	input, err := DecodeSimulationInput(request.Input)
	if err != nil {
		return Simulation{}, err
	}
	bundle, err := bundleForRequest(ctx, s.store, request.BundleID)
	if err != nil {
		return Simulation{}, err
	}
	decision, err := Evaluate(EvaluationRequest{Bundle: &bundle, DecisionPoint: request.DecisionPoint, Input: input})
	simulation := Simulation{
		BundleID: bundle.ID, BundleVersion: bundle.Version, DecisionPoint: request.DecisionPoint,
		ActorID: firstNonempty(request.ActorID, "local-operator"),
	}
	if err != nil {
		simulation.Status = "failed"
		simulation.Errors = []string{err.Error()}
		failDecision, failErr := FailClosedDecision(bundle, "", request.DecisionPoint, input, err)
		if failErr != nil {
			return Simulation{}, failErr
		}
		simulation.Decision = failDecision
	} else {
		simulation.Status = "passed"
		simulation.Decision = decision
		if !decision.Allowed {
			simulation.Status = "failed"
			simulation.Errors = []string{decision.Explanation}
		}
	}
	redacted, hash, err := RedactedInputAndHash(input)
	if err != nil {
		return Simulation{}, err
	}
	simulation.RedactedInput = redacted
	simulation.InputSHA256 = hash
	return s.store.SavePolicySimulation(ctx, simulation)
}

func (s *Service) EvaluateAndRecord(ctx context.Context, request EvaluationRequest) (Decision, error) {
	bundle, err := bundleForRequest(ctx, s.store, "")
	if err != nil {
		failBundle := BuiltinBundle()
		decision, decisionErr := FailClosedDecision(failBundle, request.JobID, request.DecisionPoint, request.Input, err)
		if decisionErr != nil {
			return Decision{}, decisionErr
		}
		recorded, recordErr := s.store.RecordPolicyDecision(ctx, decision)
		if recordErr != nil {
			return Decision{}, recordErr
		}
		return recorded, err
	}
	request.Bundle = &bundle
	decision, err := Evaluate(request)
	if err != nil {
		decision, err = FailClosedDecision(bundle, request.JobID, request.DecisionPoint, request.Input, err)
		if err != nil {
			return Decision{}, err
		}
		recorded, recordErr := s.store.RecordPolicyDecision(ctx, decision)
		if recordErr != nil {
			return Decision{}, recordErr
		}
		return recorded, errors.New(recorded.Explanation)
	}
	recorded, err := s.store.RecordPolicyDecision(ctx, decision)
	if err != nil {
		return Decision{}, err
	}
	if !recorded.Allowed {
		return recorded, errors.New(recorded.Explanation)
	}
	return recorded, nil
}

func Evaluate(request EvaluationRequest) (Decision, error) {
	bundle := request.Bundle
	if bundle == nil {
		builtin := BuiltinBundle()
		bundle = &builtin
	}
	if err := bundle.Validate(); err != nil {
		return Decision{}, err
	}
	if !KnownDecisionPoint(request.DecisionPoint) {
		return Decision{}, errors.New("unknown policy decision point")
	}
	input, err := inputMap(request.Input)
	if err != nil {
		return Decision{}, err
	}
	redacted, hash, err := RedactedInputAndHash(input)
	if err != nil {
		return Decision{}, err
	}
	required := requiredStages(bundle, input)
	allowed, explanation := decide(bundle, request.DecisionPoint, input, required)
	outcome := OutcomeDeny
	if allowed {
		outcome = OutcomeAllow
	}
	decision := Decision{
		BundleID: bundle.ID, BundleVersion: bundle.Version, JobID: request.JobID,
		DecisionPoint: request.DecisionPoint, InputSHA256: hash, RedactedInput: redacted,
		Outcome: outcome, Allowed: allowed, RequiredStages: required, Explanation: explanation,
		FailClosed: false,
	}
	if err := decision.Validate(); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func FailClosedDecision(bundle Bundle, jobID, point string, input any, cause error) (Decision, error) {
	if !KnownDecisionPoint(point) {
		point = DecisionPointQCRequirement
	}
	value, _ := inputMap(input)
	redacted, hash, err := RedactedInputAndHash(value)
	if err != nil {
		return Decision{}, err
	}
	decision := Decision{
		BundleID: bundle.ID, BundleVersion: bundle.Version, JobID: jobID,
		DecisionPoint: point, InputSHA256: hash, RedactedInput: redacted,
		Outcome: OutcomeDeny, Allowed: false, RequiredStages: requiredStages(&bundle, value),
		Explanation: "policy evaluation failed closed: " + cause.Error(), FailClosed: true,
	}
	if err := decision.Validate(); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func inputMap(input any) (map[string]any, error) {
	switch typed := input.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return typed, nil
	case json.RawMessage:
		return DecodeSimulationInput(typed)
	case []byte:
		return DecodeSimulationInput(typed)
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	}
}

func requiredStages(bundle *Bundle, input map[string]any) []string {
	level := stringValue(input, "risk_level")
	if level == "" {
		level = "medium"
	}
	stages := append([]string(nil), bundle.StructuredRules.RequiredStages[level]...)
	if len(stages) == 0 {
		stages = append([]string(nil), bundle.StructuredRules.RequiredStages["medium"]...)
	}
	return stages
}

func decide(bundle *Bundle, point string, input map[string]any, required []string) (bool, string) {
	switch point {
	case DecisionPointQCRequirement:
		if stringValue(input, "result_sha") == "" {
			return false, "QC is denied until the job has an exact result commit"
		}
		if stringValue(input, "documentation_status") == "blocked" {
			return false, "QC is denied because documentation policy retained unsupported claims"
		}
		if stringValue(input, "golden_status") == "approval_required" || stringValue(input, "golden_status") == "failed" {
			return false, "QC is denied until golden/rehearsal evidence is approved or repaired"
		}
		if boolValue(input, "full_verification_passed") == false {
			return false, "QC is denied until full verification evidence is present"
		}
		return true, fmt.Sprintf("QC is allowed by bundle %s after required stages: %s", bundle.Version, strings.Join(required, ", "))
	case DecisionPointForgePublication:
		for _, requirement := range bundle.StructuredRules.PublicationRequires {
			if !boolValue(input, requirement) {
				return false, "forge publication denied; missing requirement " + requirement
			}
		}
		return true, "forge publication allowed for exact approved result SHA"
	case DecisionPointMemoryPromotion:
		for _, requirement := range bundle.StructuredRules.MemoryPromotionRequires {
			if !boolValue(input, requirement) {
				return false, "memory promotion denied; missing requirement " + requirement
			}
		}
		return true, "memory promotion allowed in the registered project namespace"
	case DecisionPointWaiverApproval:
		for _, requirement := range bundle.StructuredRules.WaiverRequires {
			if !boolValue(input, requirement) {
				return false, "waiver approval denied; missing requirement " + requirement
			}
		}
		return true, "waiver approval allowed with reauthentication, reason, and expiry"
	case DecisionPointRunnerProfile:
		profile := stringValue(input, "runner_profile")
		if contains(bundle.StructuredRules.AllowedRunnerProfiles, profile) {
			return true, "runner profile is allow-listed by policy"
		}
		return false, "runner profile is not allow-listed by policy"
	case DecisionPointNetworkEgress:
		profile := stringValue(input, "network_profile")
		if contains(bundle.StructuredRules.AllowedNetworkProfiles, profile) {
			return true, "network profile is allow-listed by policy"
		}
		return false, "network profile is not allow-listed by policy"
	case DecisionPointModelProvider:
		tier := stringValue(input, "model_trust_tier")
		if contains(bundle.StructuredRules.AllowedModelTrustTiers, tier) {
			return true, "model trust tier is allow-listed by policy"
		}
		return false, "model trust tier is not allow-listed by policy"
	case DecisionPointRiskRouting, DecisionPointBaselineRequirement, DecisionPointDocumentationRequirement, DecisionPointSigningInstaller, DecisionPointWindowsWorker:
		return true, fmt.Sprintf("decision point %s allowed with required stages: %s", point, strings.Join(required, ", "))
	default:
		return false, "unknown decision point"
	}
}

func bundleForRequest(ctx context.Context, store Store, bundleID string) (Bundle, error) {
	if strings.TrimSpace(bundleID) != "" {
		return store.GetPolicyBundle(ctx, bundleID)
	}
	return store.GetActivePolicyBundle(ctx)
}

func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func boolValue(input map[string]any, key string) bool {
	value, _ := input[key].(bool)
	return value
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
