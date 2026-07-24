package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/v1/format"
	"github.com/open-policy-agent/opa/v1/rego"
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

func (s *Service) CreateBundle(ctx context.Context, request CreateBundleRequest) (Bundle, TestRun, error) {
	if request.SourceKind == "" {
		request.SourceKind = SourceAdvancedRego
	}
	if request.SourceKind != SourceAdvancedRego {
		return Bundle{}, TestRun{}, errors.New("only advanced Rego bundle creation is currently supported")
	}
	if strings.TrimSpace(request.CreatedBy) == "" || strings.TrimSpace(request.Reason) == "" ||
		strings.TrimSpace(request.Version) == "" {
		return Bundle{}, TestRun{}, errors.New("policy bundle creation requires version, actor, and reason")
	}
	formatted, err := FormatRegoSource(request.RegoSource)
	if err != nil {
		return Bundle{}, TestRun{}, err
	}
	originalSHA := sha256.Sum256([]byte(request.RegoSource))
	formattedSHA := sha256.Sum256([]byte(formatted))
	bundle := Bundle{
		SchemaVersion: SchemaVersion, Version: request.Version, SourceKind: SourceAdvancedRego,
		SourceSHA256: hex.EncodeToString(originalSHA[:]), CompiledSHA256: hex.EncodeToString(formattedSHA[:]),
		StructuredRules: BuiltinBundle().StructuredRules, RegoSource: formatted,
		CreatedBy: request.CreatedBy, Reason: request.Reason,
	}
	if err := bundle.Validate(); err != nil {
		return Bundle{}, TestRun{}, err
	}
	created, err := s.store.CreatePolicyBundle(ctx, bundle)
	if err != nil {
		return Bundle{}, TestRun{}, err
	}
	run, err := s.RunTests(ctx, created, request.Tests, request.CreatedBy)
	if err != nil {
		return created, TestRun{}, err
	}
	run, err = s.store.SavePolicyTestRun(ctx, run)
	return created, run, err
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
	bundle, err := s.store.GetPolicyBundle(ctx, request.BundleID)
	if err != nil {
		return Activation{}, err
	}
	if bundle.SourceKind == SourceAdvancedRego {
		runs, err := s.store.ListPolicyTestRuns(ctx, bundle.ID, 1)
		if err != nil {
			return Activation{}, err
		}
		if len(runs) == 0 || runs[0].Status != "passed" || runs[0].FormattedSourceSHA256 != bundle.CompiledSHA256 {
			return Activation{}, errors.New("advanced policy activation requires a passing retained test run for the exact formatted source")
		}
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
	if bundle.SourceKind == SourceAdvancedRego {
		return evaluateRego(request, *bundle, input)
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

func (s *Service) RunTests(ctx context.Context, bundle Bundle, tests []TestCase, actorID string) (TestRun, error) {
	if len(tests) == 0 {
		tests = defaultPolicyTests()
	}
	run := TestRun{
		BundleID: bundle.ID, BundleVersion: bundle.Version, SourceSHA256: bundle.SourceSHA256,
		FormattedSourceSHA256: bundle.CompiledSHA256, Tests: tests, Results: []TestResult{},
		Coverage: map[string]any{"decision_points": map[string]bool{}}, Errors: []string{},
		ActorID: firstNonempty(actorID, "local-operator"),
	}
	allPassed := true
	covered := map[string]bool{}
	for _, test := range tests {
		result := TestResult{ID: test.ID}
		if err := test.Validate(); err != nil {
			result.Error = err.Error()
			allPassed = false
		} else {
			input, err := DecodeSimulationInput(test.Input)
			if err != nil {
				result.Error = err.Error()
				allPassed = false
			} else {
				decision, err := Evaluate(EvaluationRequest{Bundle: &bundle, DecisionPoint: test.DecisionPoint, Input: input})
				result.Decision = decision
				if err != nil {
					result.Error = err.Error()
					allPassed = false
				} else if decision.Allowed != test.WantAllowed {
					result.Error = fmt.Sprintf("allowed=%t, want %t", decision.Allowed, test.WantAllowed)
					allPassed = false
				} else if len(test.WantStages) != 0 && !sameStrings(decision.RequiredStages, test.WantStages) {
					result.Error = fmt.Sprintf("required_stages=%v, want %v", decision.RequiredStages, test.WantStages)
					allPassed = false
				} else if test.WantExplanation != "" && !strings.Contains(decision.Explanation, test.WantExplanation) {
					result.Error = "explanation did not contain required text"
					allPassed = false
				} else {
					result.Passed = true
				}
				covered[test.DecisionPoint] = true
			}
		}
		run.Results = append(run.Results, result)
		if result.Error != "" {
			run.Errors = append(run.Errors, test.ID+": "+result.Error)
		}
	}
	run.Status = "passed"
	if !allPassed {
		run.Status = "failed"
	}
	run.Coverage = map[string]any{
		"decision_points":         covered,
		"decision_points_covered": len(covered),
		"tests":                   len(tests),
		"passed":                  allPassed,
	}
	if err := run.Validate(); err != nil {
		return TestRun{}, err
	}
	return run, nil
}

func FormatRegoSource(source string) (string, error) {
	if len(source) == 0 || len(source) > 256*1024 {
		return "", errors.New("Rego source is empty or oversized")
	}
	formatted, err := format.Source("policy.rego", []byte(source))
	if err != nil {
		return "", fmt.Errorf("format Rego policy: %w", err)
	}
	return string(formatted), nil
}

func evaluateRego(request EvaluationRequest, bundle Bundle, input map[string]any) (Decision, error) {
	redacted, hash, err := RedactedInputAndHash(input)
	if err != nil {
		return Decision{}, err
	}
	resultSet, err := rego.New(
		rego.Query("data.codemaintainer.policy.decision"),
		rego.Module("policy.rego", bundle.RegoSource),
		rego.Input(map[string]any{"decision_point": request.DecisionPoint, "input": input}),
	).Eval(context.Background())
	if err != nil {
		return Decision{}, err
	}
	if len(resultSet) == 0 || len(resultSet[0].Expressions) == 0 {
		return Decision{}, errors.New("advanced Rego policy produced no decision")
	}
	object, ok := resultSet[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return Decision{}, errors.New("advanced Rego policy decision must be an object")
	}
	allowed, ok := object["allowed"].(bool)
	if !ok {
		return Decision{}, errors.New("advanced Rego policy decision must contain boolean allowed")
	}
	outcome := OutcomeDeny
	if allowed {
		outcome = OutcomeAllow
	}
	if value, ok := object["outcome"].(string); ok && (value == OutcomeAllow || value == OutcomeDeny) {
		outcome = value
	}
	if (outcome == OutcomeAllow) != allowed {
		return Decision{}, errors.New("advanced Rego policy returned inconsistent allowed/outcome")
	}
	required := requiredStages(&bundle, input)
	if stages, ok := toStringSlice(object["required_stages"]); ok {
		required = stages
	}
	explanation, _ := object["explanation"].(string)
	if strings.TrimSpace(explanation) == "" {
		return Decision{}, errors.New("advanced Rego policy decision must contain explanation")
	}
	decision := Decision{
		BundleID: bundle.ID, BundleVersion: bundle.Version, JobID: request.JobID,
		DecisionPoint: request.DecisionPoint, InputSHA256: hash, RedactedInput: redacted,
		Outcome: outcome, Allowed: allowed, RequiredStages: required, Explanation: explanation,
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

func defaultPolicyTests() []TestCase {
	return []TestCase{{
		ID: "QC-ALLOW", DecisionPoint: DecisionPointQCRequirement, WantAllowed: true,
		Input: json.RawMessage(`{"risk_level":"medium","result_sha":"0123456789abcdef0123456789abcdef01234567","full_verification_passed":true,"documentation_status":"passed","golden_status":"passed"}`),
	}, {
		ID: "QC-DENY-NO-RESULT", DecisionPoint: DecisionPointQCRequirement, WantAllowed: false,
		Input: json.RawMessage(`{"risk_level":"medium","full_verification_passed":true,"documentation_status":"passed","golden_status":"passed"}`),
	}}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := map[string]int{}
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func toStringSlice(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		if direct, ok := value.([]string); ok {
			return direct, true
		}
		return nil, false
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || !safeID.MatchString(text) {
			return nil, false
		}
		result = append(result, text)
	}
	return result, true
}
