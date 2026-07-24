package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	"github.com/B1-Mordred/CodeMaintainer/internal/testdesigner"
)

func (s *Store) SaveTestDesignerReport(ctx context.Context, report testdesigner.Report) (testdesigner.Report, error) {
	if err := report.Validate(); err != nil {
		return testdesigner.Report{}, storage.ErrInvalid
	}
	if report.ID == "" {
		id, err := NewID("tdr")
		if err != nil {
			return testdesigner.Report{}, err
		}
		report.ID = id
	}
	report.CreatedAt = s.now()
	proposals, _ := json.Marshal(report.Proposals)
	required := 0
	if report.DispositionsRequired {
		required = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO test_designer_reports(
		id,job_id,schema_version,contract_sha256,risk_level,result_sha,source_context,
		proposals,dispositions_required,status,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		report.ID, report.JobID, report.SchemaVersion, report.ContractSHA256, report.RiskLevel,
		report.ResultSHA, report.SourceContext, string(proposals), required, report.Status,
		report.CreatedAt.Format(timestampFormat))
	if err != nil {
		return testdesigner.Report{}, err
	}
	return report, nil
}

func (s *Store) ListTestDesignerReports(ctx context.Context, jobID string, limit int) ([]testdesigner.Report, error) {
	reports, err := s.listTestDesignerReports(ctx, jobID, limit)
	if err != nil {
		return nil, err
	}
	dispositions, err := s.ListTestDesignerDispositions(ctx, jobID, "", 500)
	if err != nil {
		return nil, err
	}
	return testdesigner.ApplyDispositions(reports, dispositions), nil
}

func (s *Store) listTestDesignerReports(ctx context.Context, jobID string, limit int) ([]testdesigner.Report, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,schema_version,contract_sha256,risk_level,
		result_sha,source_context,proposals,dispositions_required,status,created_at
		FROM test_designer_reports WHERE job_id=? ORDER BY created_at DESC,id DESC LIMIT ?`,
		jobID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []testdesigner.Report{}
	for rows.Next() {
		item, err := scanTestDesignerReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveTestDesignerDisposition(ctx context.Context, disposition testdesigner.Disposition) (testdesigner.Disposition, error) {
	report, err := s.getTestDesignerReport(ctx, disposition.ReportID)
	if err != nil {
		return testdesigner.Disposition{}, err
	}
	disposition.JobID = report.JobID
	found := false
	for _, proposal := range report.Proposals {
		if proposal.ID == disposition.ProposalID {
			found = true
			break
		}
	}
	if !found {
		return testdesigner.Disposition{}, storage.ErrInvalid
	}
	if err := disposition.Validate(); err != nil {
		return testdesigner.Disposition{}, storage.ErrInvalid
	}
	if disposition.ID == "" {
		id, err := NewID("tdd")
		if err != nil {
			return testdesigner.Disposition{}, err
		}
		disposition.ID = id
	}
	disposition.CreatedAt = s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return testdesigner.Disposition{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO test_designer_dispositions(
		id,report_id,job_id,proposal_id,disposition,reason,actor_id,actor_role,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, disposition.ID, disposition.ReportID, disposition.JobID,
		disposition.ProposalID, disposition.Disposition, disposition.Reason, disposition.ActorID,
		disposition.ActorRole, disposition.CreatedAt.Format(timestampFormat))
	if err != nil {
		return testdesigner.Disposition{}, err
	}
	details, _ := json.Marshal(disposition)
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: disposition.ActorID, ActorRole: disposition.ActorRole,
		Action: "test_designer.disposition.record", TargetType: "test_designer_report",
		TargetID: disposition.ReportID, Details: details,
	}); err != nil {
		return testdesigner.Disposition{}, err
	}
	if err := tx.Commit(); err != nil {
		return testdesigner.Disposition{}, err
	}
	return disposition, nil
}

func (s *Store) ListTestDesignerDispositions(ctx context.Context, jobID, reportID string, limit int) ([]testdesigner.Disposition, error) {
	query := `SELECT id,report_id,job_id,proposal_id,disposition,reason,actor_id,actor_role,created_at
		FROM test_designer_dispositions`
	args := []any{}
	switch {
	case reportID != "" && jobID != "":
		query += ` WHERE report_id=? AND job_id=?`
		args = append(args, reportID, jobID)
	case reportID != "":
		query += ` WHERE report_id=?`
		args = append(args, reportID)
	case jobID != "":
		query += ` WHERE job_id=?`
		args = append(args, jobID)
	default:
		return nil, storage.ErrInvalid
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []testdesigner.Disposition{}
	for rows.Next() {
		item, err := scanTestDesignerDisposition(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) getTestDesignerReport(ctx context.Context, id string) (testdesigner.Report, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,job_id,schema_version,contract_sha256,risk_level,
		result_sha,source_context,proposals,dispositions_required,status,created_at
		FROM test_designer_reports WHERE id=?`, id)
	return scanTestDesignerReport(row)
}

func scanTestDesignerReport(row scanner) (testdesigner.Report, error) {
	var item testdesigner.Report
	var proposals, created string
	var required int
	if err := row.Scan(&item.ID, &item.JobID, &item.SchemaVersion, &item.ContractSHA256,
		&item.RiskLevel, &item.ResultSHA, &item.SourceContext, &proposals, &required,
		&item.Status, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	if err := json.Unmarshal([]byte(proposals), &item.Proposals); err != nil {
		return item, err
	}
	item.DispositionsRequired = required == 1
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}

func scanTestDesignerDisposition(row scanner) (testdesigner.Disposition, error) {
	var item testdesigner.Disposition
	var created string
	if err := row.Scan(&item.ID, &item.ReportID, &item.JobID, &item.ProposalID,
		&item.Disposition, &item.Reason, &item.ActorID, &item.ActorRole, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, fmt.Errorf("parse test designer disposition timestamp: %w", err)
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}
