package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/risk"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/taskcontract"
)

const taskContractSelect = `SELECT job_id,schema_version,version,status,source_kind,source_ref,requested_behavior,
	explicit_non_goals,affected_users,acceptance_criteria,constraints_json,likely_components,likely_risks,
	required_evidence,required_documentation,assumptions,questions,completion_checklist,contract_sha256,
	approved_by,approved_at,created_at,updated_at FROM task_contracts`

func (s *Store) EnsureTaskContract(ctx context.Context, request taskcontract.UpsertRequest) (taskcontract.Contract, error) {
	if strings.TrimSpace(request.ActorID) == "" || strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 4000 {
		return taskcontract.Contract{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return taskcontract.Contract{}, err
	}
	defer tx.Rollback()
	current, currentErr := scanTaskContract(tx.QueryRowContext(ctx, taskContractSelect+" WHERE job_id=?", request.JobID))
	if currentErr != nil && !errors.Is(currentErr, storage.ErrNotFound) {
		return taskcontract.Contract{}, currentErr
	}
	if currentErr == nil && current.Status == "approved" {
		return current, nil
	}
	if currentErr == nil && request.ExpectedVersion > 0 && current.Version != request.ExpectedVersion {
		return taskcontract.Contract{}, storage.ErrConflict
	}
	var currentPtr *taskcontract.Contract
	if currentErr == nil {
		currentPtr = &current
	}
	next, err := taskcontract.Normalize(request, currentPtr)
	if err != nil {
		return taskcontract.Contract{}, fmt.Errorf("%w: %v", storage.ErrInvalid, err)
	}
	now := s.now()
	if currentErr == nil {
		next.CreatedAt = current.CreatedAt
	} else {
		next.CreatedAt = now
	}
	next.UpdatedAt = now
	if err := upsertTaskContractTx(ctx, tx, next); err != nil {
		return taskcontract.Contract{}, err
	}
	details, _ := json.Marshal(map[string]any{"version": next.Version, "contract_sha256": next.ContractSHA256, "reason": request.Reason})
	if err := insertTaskContractEventTx(ctx, tx, s.now, next.JobID, next.Version, "upsert", request.ActorID, request.ActorRole, request.Reason, details); err != nil {
		return taskcontract.Contract{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: request.ActorID, ActorRole: required(request.ActorRole, "operator"),
		Action: "task_contract.upsert", TargetType: "job", TargetID: next.JobID, Details: details,
	}); err != nil {
		return taskcontract.Contract{}, err
	}
	if err := tx.Commit(); err != nil {
		return taskcontract.Contract{}, err
	}
	return s.GetTaskContract(ctx, next.JobID)
}

func upsertTaskContractTx(ctx context.Context, tx *sql.Tx, contract taskcontract.Contract) error {
	values := []any{
		contract.JobID, contract.SchemaVersion, contract.Version, contract.Status, contract.SourceKind, contract.SourceRef,
		contract.RequestedBehavior, mustJSONArray(contract.ExplicitNonGoals), mustJSONArray(contract.AffectedUsers),
		mustJSONArray(contract.AcceptanceCriteria), mustJSONArray(contract.Constraints), mustJSONArray(contract.LikelyComponents),
		mustJSONArray(contract.LikelyRisks), mustJSONArray(contract.RequiredEvidence), mustJSONArray(contract.RequiredDocumentation),
		mustJSONArray(contract.Assumptions), mustJSONArray(contract.Questions), mustJSONArray(contract.CompletionChecklist),
		contract.ContractSHA256, contract.ApprovedBy, formatOptionalTime(contract.ApprovedAt),
		contract.CreatedAt.Format(timestampFormat), contract.UpdatedAt.Format(timestampFormat),
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO task_contracts(
		job_id,schema_version,version,status,source_kind,source_ref,requested_behavior,
		explicit_non_goals,affected_users,acceptance_criteria,constraints_json,likely_components,likely_risks,
		required_evidence,required_documentation,assumptions,questions,completion_checklist,contract_sha256,
		approved_by,approved_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(job_id) DO UPDATE SET schema_version=excluded.schema_version,version=excluded.version,status=excluded.status,
		source_kind=excluded.source_kind,source_ref=excluded.source_ref,requested_behavior=excluded.requested_behavior,
		explicit_non_goals=excluded.explicit_non_goals,affected_users=excluded.affected_users,acceptance_criteria=excluded.acceptance_criteria,
		constraints_json=excluded.constraints_json,likely_components=excluded.likely_components,likely_risks=excluded.likely_risks,
		required_evidence=excluded.required_evidence,required_documentation=excluded.required_documentation,assumptions=excluded.assumptions,
		questions=excluded.questions,completion_checklist=excluded.completion_checklist,contract_sha256=excluded.contract_sha256,
		approved_by=excluded.approved_by,approved_at=excluded.approved_at,updated_at=excluded.updated_at`, values...)
	return err
}

func (s *Store) GetTaskContract(ctx context.Context, jobID string) (taskcontract.Contract, error) {
	return scanTaskContract(s.db.QueryRowContext(ctx, taskContractSelect+" WHERE job_id=?", jobID))
}

func scanTaskContract(row scanner) (taskcontract.Contract, error) {
	var item taskcontract.Contract
	var nonGoals, users, criteria, constraints, components, risks, evidence, docs, assumptions, questions, checklist string
	var approvedAt, created, updated string
	if err := row.Scan(&item.JobID, &item.SchemaVersion, &item.Version, &item.Status, &item.SourceKind, &item.SourceRef,
		&item.RequestedBehavior, &nonGoals, &users, &criteria, &constraints, &components, &risks, &evidence, &docs,
		&assumptions, &questions, &checklist, &item.ContractSHA256, &item.ApprovedBy, &approvedAt, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	if err := decodeContractJSON(&item, nonGoals, users, criteria, constraints, components, risks, evidence, docs, assumptions, questions, checklist); err != nil {
		return item, err
	}
	if approvedAt != "" {
		parsed, err := time.Parse(timestampFormat, approvedAt)
		if err != nil {
			return item, err
		}
		item.ApprovedAt = &parsed
	}
	var err error
	item.CreatedAt, err = time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.UpdatedAt, err = time.Parse(timestampFormat, updated)
	return item, err
}

func decodeContractJSON(item *taskcontract.Contract, values ...string) error {
	destinations := []any{&item.ExplicitNonGoals, &item.AffectedUsers, &item.AcceptanceCriteria, &item.Constraints, &item.LikelyComponents, &item.LikelyRisks, &item.RequiredEvidence, &item.RequiredDocumentation, &item.Assumptions, &item.Questions, &item.CompletionChecklist}
	for index, raw := range values {
		if err := json.Unmarshal([]byte(raw), destinations[index]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListTaskContractEvents(ctx context.Context, jobID string, limit int) ([]taskcontract.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,version,action,actor_id,actor_role,reason,details,created_at
		FROM task_contract_events WHERE job_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, jobID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []taskcontract.Event{}
	for rows.Next() {
		var item taskcontract.Event
		var details, created string
		if err := rows.Scan(&item.ID, &item.JobID, &item.Version, &item.Action, &item.ActorID, &item.ActorRole, &item.Reason, &details, &created); err != nil {
			return nil, err
		}
		item.Details = json.RawMessage(details)
		item.CreatedAt, err = time.Parse(timestampFormat, created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ApproveTaskContract(ctx context.Context, request taskcontract.ApprovalRequest) (taskcontract.Contract, error) {
	if strings.TrimSpace(request.ActorID) == "" || (request.ActorRole != "operator" && request.ActorRole != "reviewer" && request.ActorRole != "administrator") ||
		strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 4000 || request.ExpectedVersion < 1 {
		return taskcontract.Contract{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return taskcontract.Contract{}, err
	}
	defer tx.Rollback()
	contract, err := scanTaskContract(tx.QueryRowContext(ctx, taskContractSelect+" WHERE job_id=?", request.JobID))
	if err != nil {
		return taskcontract.Contract{}, err
	}
	if contract.Version != request.ExpectedVersion || contract.Status == "approved" {
		return taskcontract.Contract{}, storage.ErrConflict
	}
	job, err := scanJob(tx.QueryRowContext(ctx, jobSelect+" WHERE id=?", request.JobID))
	if err != nil {
		return taskcontract.Contract{}, err
	}
	if job.State != jobs.StateAwaitingTaskApproval {
		return taskcontract.Contract{}, storage.ErrConflict
	}
	now := s.now()
	contract.Status = "approved"
	contract.ApprovedBy = request.ActorID
	contract.ApprovedAt = &now
	contract.UpdatedAt = now
	criteriaJSON, criteriaHash, err := taskcontract.ToAcceptanceJSON(contract.AcceptanceCriteria)
	if err != nil {
		return taskcontract.Contract{}, storage.ErrInvalid
	}
	if err := upsertTaskContractTx(ctx, tx, contract); err != nil {
		return taskcontract.Contract{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET state=?,acceptance_criteria=?,acceptance_criteria_hash=?,version=version+1,updated_at=?
		WHERE id=? AND version=?`, jobs.StateLoadingImplementationModel, string(criteriaJSON), criteriaHash, now.Format(timestampFormat), job.ID, job.Version)
	if err != nil {
		return taskcontract.Contract{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return taskcontract.Contract{}, storage.ErrConflict
	}
	details, _ := json.Marshal(map[string]any{"version": contract.Version, "contract_sha256": contract.ContractSHA256, "acceptance_criteria_hash": criteriaHash, "reason": request.Reason})
	if err := insertTaskContractEventTx(ctx, tx, s.now, contract.JobID, contract.Version, "approve", request.ActorID, request.ActorRole, request.Reason, details); err != nil {
		return taskcontract.Contract{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO job_transitions(job_id,from_state,to_state,actor_id,reason,details,created_at)
		VALUES(?,?,?,?,?,?,?)`, job.ID, job.State, jobs.StateLoadingImplementationModel, request.ActorID, "approved task contract", string(details), now.Format(timestampFormat)); err != nil {
		return taskcontract.Contract{}, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: request.ActorID, ActorRole: request.ActorRole, Action: "task_contract.approve", TargetType: "job", TargetID: contract.JobID, Details: details,
	}); err != nil {
		return taskcontract.Contract{}, err
	}
	if err := tx.Commit(); err != nil {
		return taskcontract.Contract{}, err
	}
	return s.GetTaskContract(ctx, contract.JobID)
}

func insertTaskContractEventTx(ctx context.Context, tx *sql.Tx, now func() time.Time, jobID string, version int64, action, actorID, actorRole, reason string, details json.RawMessage) error {
	id, err := NewID("contractevt")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_contract_events(id,job_id,version,action,actor_id,actor_role,reason,details,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, id, jobID, version, action, required(actorID, "system"), required(actorRole, "operator"), reason, string(normalizeJSON(details)), now().Format(timestampFormat))
	return err
}

func (s *Store) SaveRiskAssessment(ctx context.Context, assessment risk.Assessment) (risk.Assessment, error) {
	if err := assessment.Validate(); err != nil {
		return risk.Assessment{}, fmt.Errorf("%w: %v", storage.ErrInvalid, err)
	}
	if assessment.ID == "" {
		id, err := NewID("risk")
		if err != nil {
			return risk.Assessment{}, err
		}
		assessment.ID = id
	}
	assessment.CreatedAt = s.now()
	signals, _ := json.Marshal(assessment.Signals)
	routing, _ := json.Marshal(assessment.Routing)
	_, err := s.db.ExecContext(ctx, `INSERT INTO risk_assessments(id,job_id,schema_version,contract_version,contract_sha256,level,score,signals,routing,policy_decision,explanation,requested_by_operator,superseded_by,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, assessment.ID, assessment.JobID, assessment.SchemaVersion, assessment.ContractVersion, assessment.ContractSHA256,
		assessment.Level, assessment.Score, string(signals), string(routing), string(assessment.PolicyDecision), assessment.Explanation, boolInt(assessment.RequestedByOperator), assessment.SupersededBy, assessment.CreatedAt.Format(timestampFormat))
	return assessment, err
}

func (s *Store) GetLatestRiskAssessment(ctx context.Context, jobID string) (risk.Assessment, error) {
	return scanRiskAssessment(s.db.QueryRowContext(ctx, riskAssessmentSelect+" WHERE job_id=? ORDER BY created_at DESC,id DESC LIMIT 1", jobID))
}

func (s *Store) ListRiskAssessments(ctx context.Context, jobID string, limit int) ([]risk.Assessment, error) {
	rows, err := s.db.QueryContext(ctx, riskAssessmentSelect+" WHERE job_id=? ORDER BY created_at DESC,id DESC LIMIT ?", jobID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []risk.Assessment{}
	for rows.Next() {
		item, err := scanRiskAssessment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const riskAssessmentSelect = `SELECT id,job_id,schema_version,contract_version,contract_sha256,level,score,signals,routing,policy_decision,explanation,requested_by_operator,superseded_by,created_at FROM risk_assessments`

func scanRiskAssessment(row scanner) (risk.Assessment, error) {
	var item risk.Assessment
	var level, signals, routing, decision, created string
	var requested int
	if err := row.Scan(&item.ID, &item.JobID, &item.SchemaVersion, &item.ContractVersion, &item.ContractSHA256, &level, &item.Score, &signals, &routing, &decision, &item.Explanation, &requested, &item.SupersededBy, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	item.Level = risk.Level(level)
	item.PolicyDecision = json.RawMessage(decision)
	item.RequestedByOperator = requested == 1
	if err := json.Unmarshal([]byte(signals), &item.Signals); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(routing), &item.Routing); err != nil {
		return item, err
	}
	var err error
	item.CreatedAt, err = time.Parse(timestampFormat, created)
	return item, err
}

func (s *Store) CreateRiskWaiver(ctx context.Context, request risk.WaiverRequest) (risk.Waiver, risk.Assessment, error) {
	if strings.TrimSpace(request.ActorID) == "" || (request.ActorRole != "reviewer" && request.ActorRole != "administrator") ||
		strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 4000 || !request.Reauthenticated ||
		!request.ExpiresAt.After(s.now()) {
		return risk.Waiver{}, risk.Assessment{}, storage.ErrInvalid
	}
	current, err := s.GetLatestRiskAssessment(ctx, request.JobID)
	if err != nil {
		return risk.Waiver{}, risk.Assessment{}, err
	}
	if current.ID != request.AssessmentID || rankRisk(request.ToLevel) >= rankRisk(current.Level) {
		return risk.Waiver{}, risk.Assessment{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return risk.Waiver{}, risk.Assessment{}, err
	}
	defer tx.Rollback()
	id, err := NewID("waiver")
	if err != nil {
		return risk.Waiver{}, risk.Assessment{}, err
	}
	now := s.now()
	waiver := risk.Waiver{
		ID: id, JobID: request.JobID, AssessmentID: current.ID, FromLevel: current.Level, ToLevel: request.ToLevel,
		Reason: strings.TrimSpace(request.Reason), ActorID: request.ActorID, ActorRole: request.ActorRole,
		Reauthenticated: true, ExpiresAt: request.ExpiresAt, CreatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_waivers(id,job_id,assessment_id,from_level,to_level,reason,actor_id,actor_role,reauthenticated,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, waiver.ID, waiver.JobID, waiver.AssessmentID, waiver.FromLevel, waiver.ToLevel, waiver.Reason, waiver.ActorID, waiver.ActorRole, 1, waiver.ExpiresAt.Format(timestampFormat), now.Format(timestampFormat)); err != nil {
		return waiver, risk.Assessment{}, err
	}
	next := current
	next.ID, err = NewID("risk")
	if err != nil {
		return waiver, risk.Assessment{}, err
	}
	next.Level = request.ToLevel
	next.Routing = risk.RoutingForLevel(request.ToLevel)
	next.PolicyDecision, _ = json.Marshal(map[string]any{"engine": "controller-deterministic-v1", "waiver_id": waiver.ID, "waived_from": current.Level, "waived_to": request.ToLevel, "expires_at": waiver.ExpiresAt.Format(time.RFC3339)})
	next.Explanation = current.Explanation + "; lowered by authorized expiring waiver " + waiver.ID
	next.CreatedAt = now
	signals, _ := json.Marshal(next.Signals)
	routing, _ := json.Marshal(next.Routing)
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_assessments(id,job_id,schema_version,contract_version,contract_sha256,level,score,signals,routing,policy_decision,explanation,requested_by_operator,superseded_by,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, next.ID, next.JobID, next.SchemaVersion, next.ContractVersion, next.ContractSHA256, next.Level, next.Score,
		string(signals), string(routing), string(next.PolicyDecision), next.Explanation, boolInt(next.RequestedByOperator), "", now.Format(timestampFormat)); err != nil {
		return waiver, risk.Assessment{}, err
	}
	details, _ := json.Marshal(map[string]any{"waiver_id": waiver.ID, "from": waiver.FromLevel, "to": waiver.ToLevel, "expires_at": waiver.ExpiresAt, "reason": waiver.Reason})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: waiver.ActorID, ActorRole: waiver.ActorRole, Action: "risk.waive", TargetType: "job", TargetID: waiver.JobID, Details: details}); err != nil {
		return waiver, risk.Assessment{}, err
	}
	if err := tx.Commit(); err != nil {
		return waiver, risk.Assessment{}, err
	}
	return waiver, next, nil
}

func (s *Store) ListRiskWaivers(ctx context.Context, jobID string, limit int) ([]risk.Waiver, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,assessment_id,from_level,to_level,reason,actor_id,actor_role,reauthenticated,expires_at,created_at
		FROM risk_waivers WHERE job_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, jobID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []risk.Waiver{}
	for rows.Next() {
		var item risk.Waiver
		var fromLevel, toLevel, expires, created string
		var reauth int
		if err := rows.Scan(&item.ID, &item.JobID, &item.AssessmentID, &fromLevel, &toLevel, &item.Reason, &item.ActorID, &item.ActorRole, &reauth, &expires, &created); err != nil {
			return nil, err
		}
		item.FromLevel = risk.Level(fromLevel)
		item.ToLevel = risk.Level(toLevel)
		item.Reauthenticated = reauth == 1
		var err error
		item.ExpiresAt, err = time.Parse(timestampFormat, expires)
		if err != nil {
			return nil, err
		}
		item.CreatedAt, err = time.Parse(timestampFormat, created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func mustJSONArray(value any) string {
	payload, _ := json.Marshal(value)
	return string(payload)
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(timestampFormat)
}

func rankRisk(level risk.Level) int {
	switch level {
	case risk.LevelHigh:
		return 3
	case risk.LevelMedium:
		return 2
	case risk.LevelLow:
		return 1
	default:
		return 0
	}
}
