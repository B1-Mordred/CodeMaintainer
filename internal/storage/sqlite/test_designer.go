package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

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
