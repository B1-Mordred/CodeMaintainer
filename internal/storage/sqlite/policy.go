package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	"github.com/B1-Mordred/CodeMaintainer/internal/policy"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) ListPolicyBundles(ctx context.Context, limit int) ([]policy.Bundle, error) {
	if err := s.ensureBuiltinPolicyBundle(ctx); err != nil {
		return nil, err
	}
	active, _ := s.activePolicyBundleID(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT id,schema_version,version,source_kind,source_sha256,compiled_sha256,
		structured_rules_json,rego_source,created_by,reason,created_at
		FROM policy_bundles ORDER BY created_at DESC,id DESC LIMIT ?`, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []policy.Bundle{}
	for rows.Next() {
		item, err := scanPolicyBundle(rows)
		if err != nil {
			return nil, err
		}
		if item.ID == active || (active == "" && item.ID == policy.BuiltinBundle().ID) {
			item.Status = "active"
		} else {
			item.Status = "available"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreatePolicyBundle(ctx context.Context, bundle policy.Bundle) (policy.Bundle, error) {
	if err := s.ensureBuiltinPolicyBundle(ctx); err != nil {
		return policy.Bundle{}, err
	}
	if bundle.ID == "" {
		id, err := NewID("policybundle")
		if err != nil {
			return policy.Bundle{}, err
		}
		bundle.ID = id
	}
	bundle.CreatedAt = s.now()
	if err := bundle.Validate(); err != nil {
		return policy.Bundle{}, storage.ErrInvalid
	}
	rules, _ := json.Marshal(bundle.StructuredRules)
	_, err := s.db.ExecContext(ctx, `INSERT INTO policy_bundles(
		id,schema_version,version,source_kind,source_sha256,compiled_sha256,structured_rules_json,rego_source,created_by,reason,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, bundle.ID, bundle.SchemaVersion, bundle.Version, bundle.SourceKind,
		bundle.SourceSHA256, bundle.CompiledSHA256, string(rules), bundle.RegoSource, bundle.CreatedBy, bundle.Reason,
		bundle.CreatedAt.Format(timestampFormat))
	if err != nil {
		return policy.Bundle{}, err
	}
	bundle.Status = "available"
	return bundle, nil
}

func (s *Store) GetPolicyBundle(ctx context.Context, id string) (policy.Bundle, error) {
	if err := s.ensureBuiltinPolicyBundle(ctx); err != nil {
		return policy.Bundle{}, err
	}
	item, err := s.getPolicyBundle(ctx, id)
	if err != nil {
		return policy.Bundle{}, err
	}
	active, _ := s.activePolicyBundleID(ctx)
	if item.ID == active || (active == "" && item.ID == policy.BuiltinBundle().ID) {
		item.Status = "active"
	} else {
		item.Status = "available"
	}
	return item, nil
}

func (s *Store) GetActivePolicyBundle(ctx context.Context) (policy.Bundle, error) {
	if err := s.ensureBuiltinPolicyBundle(ctx); err != nil {
		return policy.Bundle{}, err
	}
	active, err := s.activePolicyBundleID(ctx)
	if err != nil {
		return policy.Bundle{}, err
	}
	if active == "" {
		active = policy.BuiltinBundle().ID
	}
	item, err := s.getPolicyBundle(ctx, active)
	if err != nil {
		return policy.Bundle{}, err
	}
	item.Status = "active"
	return item, nil
}

func (s *Store) ActivatePolicyBundle(ctx context.Context, request policy.ActivationRequest) (policy.Activation, error) {
	if err := s.ensureBuiltinPolicyBundle(ctx); err != nil {
		return policy.Activation{}, err
	}
	bundle, err := s.getPolicyBundle(ctx, request.BundleID)
	if err != nil {
		return policy.Activation{}, err
	}
	if request.Action == "" {
		request.Action = "activate"
	}
	previous, _ := s.activePolicyBundleID(ctx)
	id, err := NewID("policyact")
	if err != nil {
		return policy.Activation{}, err
	}
	activation := policy.Activation{
		ID: id, BundleID: bundle.ID, BundleVersion: bundle.Version, Action: request.Action,
		PreviousBundleID: previous, ActorID: request.ActorID, ActorRole: request.ActorRole,
		Reason: request.Reason, StagedRolloutPercent: request.StagedRolloutPercent,
		Reauthenticated: request.Reauthenticated, CreatedAt: s.now(),
	}
	if err := activation.Validate(); err != nil {
		return policy.Activation{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return policy.Activation{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_activations(
		id,bundle_id,bundle_version,action,previous_bundle_id,actor_id,actor_role,reason,staged_rollout_percent,reauthenticated,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, activation.ID, activation.BundleID, activation.BundleVersion, activation.Action,
		activation.PreviousBundleID, activation.ActorID, activation.ActorRole, activation.Reason, activation.StagedRolloutPercent,
		boolInt(activation.Reauthenticated), activation.CreatedAt.Format(timestampFormat)); err != nil {
		tx.Rollback()
		return policy.Activation{}, err
	}
	details, _ := json.Marshal(activation)
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: activation.ActorID, ActorRole: activation.ActorRole, Action: "policy.activation." + activation.Action,
		TargetType: "policy_bundle", TargetID: activation.BundleID, Details: details,
	}); err != nil {
		tx.Rollback()
		return policy.Activation{}, err
	}
	if err := tx.Commit(); err != nil {
		return policy.Activation{}, err
	}
	return activation, nil
}

func (s *Store) ListPolicyActivations(ctx context.Context, limit int) ([]policy.Activation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,bundle_id,bundle_version,action,previous_bundle_id,actor_id,
		actor_role,reason,staged_rollout_percent,reauthenticated,created_at
		FROM policy_activations ORDER BY created_at DESC,id DESC LIMIT ?`, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []policy.Activation{}
	for rows.Next() {
		item, err := scanPolicyActivation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SavePolicySimulation(ctx context.Context, simulation policy.Simulation) (policy.Simulation, error) {
	if simulation.ID == "" {
		id, err := NewID("policysim")
		if err != nil {
			return policy.Simulation{}, err
		}
		simulation.ID = id
	}
	simulation.CreatedAt = s.now()
	if err := simulation.Validate(); err != nil {
		return policy.Simulation{}, storage.ErrInvalid
	}
	decision, _ := json.Marshal(simulation.Decision)
	errorsJSON, _ := json.Marshal(simulation.Errors)
	_, err := s.db.ExecContext(ctx, `INSERT INTO policy_simulations(
		id,bundle_id,bundle_version,decision_point,input_sha256,redacted_input_json,decision_json,status,errors_json,actor_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, simulation.ID, simulation.BundleID, simulation.BundleVersion, simulation.DecisionPoint,
		simulation.InputSHA256, string(simulation.RedactedInput), string(decision), simulation.Status, string(errorsJSON),
		simulation.ActorID, simulation.CreatedAt.Format(timestampFormat))
	if err != nil {
		return policy.Simulation{}, err
	}
	return simulation, nil
}

func (s *Store) ListPolicySimulations(ctx context.Context, limit int) ([]policy.Simulation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,bundle_id,bundle_version,decision_point,input_sha256,
		redacted_input_json,decision_json,status,errors_json,actor_id,created_at
		FROM policy_simulations ORDER BY created_at DESC,id DESC LIMIT ?`, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []policy.Simulation{}
	for rows.Next() {
		item, err := scanPolicySimulation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SavePolicyTestRun(ctx context.Context, run policy.TestRun) (policy.TestRun, error) {
	if run.ID == "" {
		id, err := NewID("policytestrun")
		if err != nil {
			return policy.TestRun{}, err
		}
		run.ID = id
	}
	run.CreatedAt = s.now()
	if err := run.Validate(); err != nil {
		return policy.TestRun{}, storage.ErrInvalid
	}
	tests, _ := json.Marshal(run.Tests)
	results, _ := json.Marshal(run.Results)
	coverage, _ := json.Marshal(run.Coverage)
	errorsJSON, _ := json.Marshal(run.Errors)
	_, err := s.db.ExecContext(ctx, `INSERT INTO policy_test_runs(
		id,bundle_id,bundle_version,source_sha256,formatted_source_sha256,status,tests_json,results_json,coverage_json,errors_json,actor_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.BundleID, run.BundleVersion, run.SourceSHA256,
		run.FormattedSourceSHA256, run.Status, string(tests), string(results), string(coverage), string(errorsJSON),
		run.ActorID, run.CreatedAt.Format(timestampFormat))
	if err != nil {
		return policy.TestRun{}, err
	}
	return run, nil
}

func (s *Store) ListPolicyTestRuns(ctx context.Context, bundleID string, limit int) ([]policy.TestRun, error) {
	query := `SELECT id,bundle_id,bundle_version,source_sha256,formatted_source_sha256,status,tests_json,results_json,
		coverage_json,errors_json,actor_id,created_at FROM policy_test_runs`
	args := []any{}
	if bundleID != "" {
		query += ` WHERE bundle_id=?`
		args = append(args, bundleID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []policy.TestRun{}
	for rows.Next() {
		item, err := scanPolicyTestRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RecordPolicyDecision(ctx context.Context, decision policy.Decision) (policy.Decision, error) {
	if decision.ID == "" {
		id, err := NewID("policydec")
		if err != nil {
			return policy.Decision{}, err
		}
		decision.ID = id
	}
	decision.CreatedAt = s.now()
	if err := decision.Validate(); err != nil {
		return policy.Decision{}, storage.ErrInvalid
	}
	stages, _ := json.Marshal(decision.RequiredStages)
	var jobID any
	if decision.JobID != "" {
		jobID = decision.JobID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO policy_decisions(
		id,bundle_id,bundle_version,job_id,decision_point,input_sha256,redacted_input_json,outcome,allowed,
		required_stages_json,explanation,fail_closed,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, decision.ID, decision.BundleID, decision.BundleVersion, jobID,
		decision.DecisionPoint, decision.InputSHA256, string(decision.RedactedInput), decision.Outcome,
		boolInt(decision.Allowed), string(stages), decision.Explanation, boolInt(decision.FailClosed),
		decision.CreatedAt.Format(timestampFormat))
	if err != nil {
		return policy.Decision{}, err
	}
	return decision, nil
}

func (s *Store) ListPolicyDecisions(ctx context.Context, jobID string, limit int) ([]policy.Decision, error) {
	query := `SELECT id,bundle_id,bundle_version,COALESCE(job_id,''),decision_point,input_sha256,redacted_input_json,
		outcome,allowed,required_stages_json,explanation,fail_closed,created_at FROM policy_decisions`
	args := []any{}
	if jobID != "" {
		query += ` WHERE job_id=?`
		args = append(args, jobID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []policy.Decision{}
	for rows.Next() {
		item, err := scanPolicyDecision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ensureBuiltinPolicyBundle(ctx context.Context) error {
	bundle := policy.BuiltinBundle()
	if _, err := s.getPolicyBundle(ctx, bundle.ID); err == nil {
		return nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	raw, _ := json.Marshal(bundle.StructuredRules)
	_, err := s.db.ExecContext(ctx, `INSERT INTO policy_bundles(
		id,schema_version,version,source_kind,source_sha256,compiled_sha256,structured_rules_json,rego_source,created_by,reason,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, bundle.ID, bundle.SchemaVersion, bundle.Version, bundle.SourceKind,
		bundle.SourceSHA256, bundle.CompiledSHA256, string(raw), bundle.RegoSource, bundle.CreatedBy, bundle.Reason,
		s.now().Format(timestampFormat))
	return err
}

func (s *Store) getPolicyBundle(ctx context.Context, id string) (policy.Bundle, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,schema_version,version,source_kind,source_sha256,compiled_sha256,
		structured_rules_json,rego_source,created_by,reason,created_at FROM policy_bundles WHERE id=?`, id)
	return scanPolicyBundle(row)
}

func (s *Store) activePolicyBundleID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT bundle_id FROM policy_activations ORDER BY created_at DESC,id DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func scanPolicyBundle(row scanner) (policy.Bundle, error) {
	var item policy.Bundle
	var rules, created string
	if err := row.Scan(&item.ID, &item.SchemaVersion, &item.Version, &item.SourceKind, &item.SourceSHA256,
		&item.CompiledSHA256, &rules, &item.RegoSource, &item.CreatedBy, &item.Reason, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	item.StructuredRulesJSON = []byte(rules)
	if err := json.Unmarshal([]byte(rules), &item.StructuredRules); err != nil {
		return item, err
	}
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}

func scanPolicyActivation(row scanner) (policy.Activation, error) {
	var item policy.Activation
	var reauth int
	var created string
	if err := row.Scan(&item.ID, &item.BundleID, &item.BundleVersion, &item.Action, &item.PreviousBundleID,
		&item.ActorID, &item.ActorRole, &item.Reason, &item.StagedRolloutPercent, &reauth, &created); err != nil {
		return item, err
	}
	item.Reauthenticated = reauth != 0
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}

func scanPolicySimulation(row scanner) (policy.Simulation, error) {
	var item policy.Simulation
	var input, decision, errorsJSON, created string
	if err := row.Scan(&item.ID, &item.BundleID, &item.BundleVersion, &item.DecisionPoint, &item.InputSHA256,
		&input, &decision, &item.Status, &errorsJSON, &item.ActorID, &created); err != nil {
		return item, err
	}
	item.RedactedInput = []byte(input)
	if err := json.Unmarshal([]byte(decision), &item.Decision); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(errorsJSON), &item.Errors); err != nil {
		return item, err
	}
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}

func scanPolicyTestRun(row scanner) (policy.TestRun, error) {
	var item policy.TestRun
	var tests, results, coverage, errorsJSON, created string
	if err := row.Scan(&item.ID, &item.BundleID, &item.BundleVersion, &item.SourceSHA256,
		&item.FormattedSourceSHA256, &item.Status, &tests, &results, &coverage, &errorsJSON,
		&item.ActorID, &created); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(tests), &item.Tests); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(results), &item.Results); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(coverage), &item.Coverage); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(errorsJSON), &item.Errors); err != nil {
		return item, err
	}
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}

func scanPolicyDecision(row scanner) (policy.Decision, error) {
	var item policy.Decision
	var input, stages, created string
	var allowed, failClosed int
	if err := row.Scan(&item.ID, &item.BundleID, &item.BundleVersion, &item.JobID, &item.DecisionPoint,
		&item.InputSHA256, &input, &item.Outcome, &allowed, &stages, &item.Explanation, &failClosed, &created); err != nil {
		return item, err
	}
	item.RedactedInput = []byte(input)
	item.Allowed = allowed != 0
	item.FailClosed = failClosed != 0
	if err := json.Unmarshal([]byte(stages), &item.RequiredStages); err != nil {
		return item, err
	}
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}
