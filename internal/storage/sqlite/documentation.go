package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	documentation "github.com/B1-Mordred/CodeMaintainer/internal/docagent"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) SaveDocumentationManifest(ctx context.Context, manifest documentation.Manifest) (documentation.Manifest, error) {
	if err := manifest.Validate(); err != nil {
		return documentation.Manifest{}, storage.ErrInvalid
	}
	if manifest.ID == "" {
		id, err := NewID("docmanifest")
		if err != nil {
			return documentation.Manifest{}, err
		}
		manifest.ID = id
	}
	manifest.CreatedAt = s.now()
	requirements, _ := json.Marshal(manifest.Requirements)
	changes, _ := json.Marshal(manifest.Changes)
	checks, _ := json.Marshal(manifest.Checks)
	unsupportedClaims, _ := json.Marshal(manifest.UnsupportedClaims)
	edits, _ := json.Marshal(manifest.Edits)
	_, err := s.db.ExecContext(ctx, `INSERT INTO documentation_manifests(
		id,job_id,schema_version,project_id,contract_sha256,risk_level,result_sha,source_context,
		policy_version,requirements_json,changes_json,checks_json,unsupported_claims_json,edits_json,
		status,policy_summary,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		manifest.ID, manifest.JobID, manifest.SchemaVersion, manifest.ProjectID, manifest.ContractSHA256,
		manifest.RiskLevel, manifest.ResultSHA, manifest.SourceContext, manifest.PolicyVersion,
		string(requirements), string(changes), string(checks), string(unsupportedClaims), string(edits),
		manifest.Status, manifest.PolicySummary, manifest.CreatedAt.Format(timestampFormat))
	if err != nil {
		return documentation.Manifest{}, err
	}
	return manifest, nil
}

func (s *Store) ListDocumentationManifests(ctx context.Context, jobID string, limit int) ([]documentation.Manifest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,schema_version,project_id,contract_sha256,risk_level,
		result_sha,source_context,policy_version,requirements_json,changes_json,checks_json,
		unsupported_claims_json,edits_json,status,policy_summary,created_at
		FROM documentation_manifests WHERE job_id=? ORDER BY created_at DESC,id DESC LIMIT ?`,
		jobID, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []documentation.Manifest{}
	for rows.Next() {
		item, err := scanDocumentationManifest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanDocumentationManifest(row scanner) (documentation.Manifest, error) {
	var item documentation.Manifest
	var requirements, changes, checks, unsupportedClaims, edits, created string
	if err := row.Scan(&item.ID, &item.JobID, &item.SchemaVersion, &item.ProjectID, &item.ContractSHA256,
		&item.RiskLevel, &item.ResultSHA, &item.SourceContext, &item.PolicyVersion,
		&requirements, &changes, &checks, &unsupportedClaims, &edits, &item.Status,
		&item.PolicySummary, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, storage.ErrNotFound
		}
		return item, err
	}
	if err := json.Unmarshal([]byte(requirements), &item.Requirements); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(changes), &item.Changes); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(checks), &item.Checks); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(unsupportedClaims), &item.UnsupportedClaims); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(edits), &item.Edits); err != nil {
		return item, err
	}
	parsed, err := time.Parse(timestampFormat, created)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsed
	return item, item.Validate()
}
