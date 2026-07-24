package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/golden"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) SaveGoldenReport(ctx context.Context, report golden.Report) (golden.Report, error) {
	if err := report.Validate(); err != nil {
		return golden.Report{}, storage.ErrInvalid
	}
	if report.ID == "" {
		id, err := NewID("golden")
		if err != nil {
			return golden.Report{}, err
		}
		report.ID = id
	}
	report.CreatedAt = s.now()
	comparisons, _ := json.Marshal(report.Comparisons)
	_, err := s.db.ExecContext(ctx, `INSERT INTO golden_rehearsal_reports(
		id,job_id,schema_version,project_id,contract_sha256,risk_level,result_sha,source_context,
		comparisons_json,status,policy_summary,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		report.ID, report.JobID, report.SchemaVersion, report.ProjectID, report.ContractSHA256,
		report.RiskLevel, report.ResultSHA, report.SourceContext, string(comparisons), report.Status,
		report.PolicySummary, report.CreatedAt.Format(timestampFormat))
	if err != nil {
		return golden.Report{}, err
	}
	return report, nil
}

func (s *Store) ListGoldenReports(ctx context.Context, jobID string, limit int) ([]golden.Report, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,schema_version,project_id,contract_sha256,
		risk_level,result_sha,source_context,comparisons_json,status,policy_summary,created_at
		FROM golden_rehearsal_reports WHERE job_id=? ORDER BY created_at DESC,id DESC LIMIT ?`,
		jobID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []golden.Report{}
	for rows.Next() {
		item, err := scanGoldenReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetGoldenReport(ctx context.Context, reportID string) (golden.Report, error) {
	return scanGoldenReport(s.db.QueryRowContext(ctx, `SELECT id,job_id,schema_version,project_id,contract_sha256,
		risk_level,result_sha,source_context,comparisons_json,status,policy_summary,created_at
		FROM golden_rehearsal_reports WHERE id=?`, reportID))
}

func (s *Store) ApproveGoldenUpdate(ctx context.Context, request golden.ApprovalRequest) (golden.Approval, error) {
	if err := request.Validate(); err != nil {
		return golden.Approval{}, storage.ErrInvalid
	}
	report, err := s.GetGoldenReport(ctx, request.ReportID)
	if err != nil {
		return golden.Approval{}, err
	}
	if report.Status != "approval_required" {
		return golden.Approval{}, storage.ErrInvalid
	}
	var comparison golden.Comparison
	for _, item := range report.Comparisons {
		if item.ID == request.ComparisonID {
			comparison = item
			break
		}
	}
	if comparison.ID == "" || !comparison.ApprovalRequired {
		return golden.Approval{}, storage.ErrInvalid
	}
	id, err := NewID("goldenapproval")
	if err != nil {
		return golden.Approval{}, err
	}
	now := s.now()
	approval := golden.Approval{
		ID: id, ReportID: request.ReportID, ComparisonID: request.ComparisonID,
		ActorID: request.ActorID, ActorRole: request.ActorRole, Reason: request.Reason,
		Approved: request.Approved, Reauthenticated: request.Reauthenticated,
		ApprovedArtifactSHA256:  comparison.ApprovedArtifactSHA256,
		CandidateArtifactSHA256: comparison.CandidateArtifactSHA256, CreatedAt: now,
	}
	if err := approval.Validate(); err != nil {
		return golden.Approval{}, storage.ErrInvalid
	}
	approved := 0
	if approval.Approved {
		approved = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO golden_update_approvals(
		id,report_id,comparison_id,actor_id,actor_role,reason,approved,reauthenticated,
		approved_artifact_sha256,candidate_artifact_sha256,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		approval.ID, approval.ReportID, approval.ComparisonID, approval.ActorID, approval.ActorRole,
		approval.Reason, approved, 1, approval.ApprovedArtifactSHA256, approval.CandidateArtifactSHA256,
		approval.CreatedAt.Format(timestampFormat))
	if err != nil {
		return golden.Approval{}, err
	}
	return approval, nil
}

func (s *Store) ListGoldenApprovals(ctx context.Context, jobID string, limit int) ([]golden.Approval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.report_id,a.comparison_id,a.actor_id,a.actor_role,
		a.reason,a.approved,a.reauthenticated,a.approved_artifact_sha256,a.candidate_artifact_sha256,a.created_at
		FROM golden_update_approvals a JOIN golden_rehearsal_reports r ON r.id=a.report_id
		WHERE r.job_id=? ORDER BY a.created_at DESC,a.id DESC LIMIT ?`,
		jobID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []golden.Approval{}
	for rows.Next() {
		item, err := scanGoldenApproval(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanGoldenReport(row scanner) (golden.Report, error) {
	var item golden.Report
	var comparisons, created string
	if err := row.Scan(&item.ID, &item.JobID, &item.SchemaVersion, &item.ProjectID, &item.ContractSHA256,
		&item.RiskLevel, &item.ResultSHA, &item.SourceContext, &comparisons, &item.Status,
		&item.PolicySummary, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	if err := json.Unmarshal([]byte(comparisons), &item.Comparisons); err != nil {
		return item, err
	}
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}

func scanGoldenApproval(row scanner) (golden.Approval, error) {
	var item golden.Approval
	var approved, reauthenticated int
	var created string
	if err := row.Scan(&item.ID, &item.ReportID, &item.ComparisonID, &item.ActorID, &item.ActorRole,
		&item.Reason, &approved, &reauthenticated, &item.ApprovedArtifactSHA256,
		&item.CandidateArtifactSHA256, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	item.Approved = approved == 1
	item.Reauthenticated = reauthenticated == 1
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, fmt.Errorf("parse golden approval timestamp: %w", err)
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}
