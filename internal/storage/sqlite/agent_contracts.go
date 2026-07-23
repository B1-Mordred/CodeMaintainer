package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/agents"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) RecordAgentContractValidation(ctx context.Context, record agents.ValidationRecord) (agents.ValidationRecord, error) {
	if record.JobID == "" || record.Phase == "" || record.ContractKind == "" ||
		record.SchemaVersion != agents.SchemaVersion || record.SchemaSHA256 == "" ||
		record.PayloadSHA256 == "" || record.Attempt < 1 || record.Attempt > 20 || len(record.Error) > 4000 {
		return agents.ValidationRecord{}, storage.ErrInvalid
	}
	if _, err := agents.DescriptorFor(record.ContractKind, record.SchemaVersion); err != nil {
		return agents.ValidationRecord{}, storage.ErrInvalid
	}
	if record.ID == "" {
		id, err := NewID("acv")
		if err != nil {
			return agents.ValidationRecord{}, err
		}
		record.ID = id
	}
	record.CreatedAt = s.now()
	valid := 0
	if record.Valid {
		valid = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_contract_validations(
		id,job_id,phase,contract_kind,schema_version,schema_sha256,payload_sha256,
		attempt,valid,error,artifact_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		record.ID, record.JobID, record.Phase, record.ContractKind, record.SchemaVersion,
		record.SchemaSHA256, record.PayloadSHA256, record.Attempt, valid, record.Error,
		record.ArtifactID, record.CreatedAt.Format(timestampFormat))
	if err != nil {
		return agents.ValidationRecord{}, err
	}
	return record, nil
}

func (s *Store) ListAgentContractValidations(ctx context.Context, jobID string, limit int) ([]agents.ValidationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,phase,contract_kind,schema_version,
		schema_sha256,payload_sha256,attempt,valid,error,artifact_id,created_at
		FROM agent_contract_validations WHERE job_id=? ORDER BY created_at DESC,id DESC LIMIT ?`,
		jobID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []agents.ValidationRecord{}
	for rows.Next() {
		record, err := scanAgentContractValidation(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func scanAgentContractValidation(row scanner) (agents.ValidationRecord, error) {
	var record agents.ValidationRecord
	var kind string
	var valid int
	var created string
	if err := row.Scan(&record.ID, &record.JobID, &record.Phase, &kind, &record.SchemaVersion,
		&record.SchemaSHA256, &record.PayloadSHA256, &record.Attempt, &valid,
		&record.Error, &record.ArtifactID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return record, storage.ErrNotFound
		}
		return record, err
	}
	record.ContractKind = agents.ContractKind(kind)
	record.Valid = valid == 1
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return record, err
	}
	record.CreatedAt = parsed
	return record, nil
}
