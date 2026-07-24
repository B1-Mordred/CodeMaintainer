package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/B1-Mordred/CodeMaintainer/internal/evaluation"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) CreateEvaluationDataset(ctx context.Context, dataset evaluation.Dataset) (evaluation.Dataset, error) {
	if dataset.ID == "" {
		id, err := NewID("evaldataset")
		if err != nil {
			return evaluation.Dataset{}, err
		}
		dataset.ID = id
	}
	if dataset.CreatedAt.IsZero() {
		dataset.CreatedAt = s.now()
	}
	if err := dataset.Validate(); err != nil {
		return evaluation.Dataset{}, storage.ErrInvalid
	}
	exclusions, _ := json.Marshal(dataset.Exclusions)
	_, err := s.db.ExecContext(ctx, `INSERT INTO evaluation_datasets(
		id,schema_version,project_id,name,source_kind,repository,base_revision,target_revision,known_patch_sha256,
		hidden_patch_sha256,exclusions_json,scoring_profile,retention_days,reproducibility_key,metadata_json,actor_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		dataset.ID, dataset.SchemaVersion, dataset.ProjectID, dataset.Name, dataset.SourceKind, dataset.Repository,
		dataset.BaseRevision, dataset.TargetRevision, dataset.KnownPatchSHA256, dataset.HiddenPatchSHA256,
		string(exclusions), dataset.ScoringProfile, dataset.RetentionDays, dataset.ReproducibilityKey,
		string(dataset.Metadata), dataset.ActorID, dataset.CreatedAt.Format(timestampFormat))
	if err != nil {
		return evaluation.Dataset{}, err
	}
	return dataset, nil
}

func (s *Store) GetEvaluationDataset(ctx context.Context, id string) (evaluation.Dataset, error) {
	return scanEvaluationDataset(s.db.QueryRowContext(ctx, `SELECT id,schema_version,project_id,name,source_kind,repository,
		base_revision,target_revision,known_patch_sha256,hidden_patch_sha256,exclusions_json,scoring_profile,
		retention_days,reproducibility_key,metadata_json,actor_id,created_at FROM evaluation_datasets WHERE id=?`, id))
}

func (s *Store) ListEvaluationDatasets(ctx context.Context, projectID string, limit int) ([]evaluation.Dataset, error) {
	query := `SELECT id,schema_version,project_id,name,source_kind,repository,base_revision,target_revision,known_patch_sha256,
		hidden_patch_sha256,exclusions_json,scoring_profile,retention_days,reproducibility_key,metadata_json,actor_id,created_at
		FROM evaluation_datasets`
	args := []any{}
	if projectID != "" {
		query += " WHERE project_id=?"
		args = append(args, projectID)
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []evaluation.Dataset{}
	for rows.Next() {
		item, err := scanEvaluationDataset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RecordEvaluationRun(ctx context.Context, run evaluation.Run) (evaluation.Run, error) {
	if run.ID == "" {
		id, err := NewID("evalrun")
		if err != nil {
			return evaluation.Run{}, err
		}
		run.ID = id
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = s.now()
	}
	if err := run.Validate(); err != nil {
		return evaluation.Run{}, storage.ErrInvalid
	}
	profiles, _ := json.Marshal(run.ProfileMatrix)
	results, _ := json.Marshal(run.Results)
	_, err := s.db.ExecContext(ctx, `INSERT INTO evaluation_runs(
		id,schema_version,dataset_id,project_id,status,profile_matrix_json,isolated_memory_namespace,
		isolated_cache_namespace,budget_seconds,concurrency,scoring_profile,results_json,report_sha256,
		promotion_recommendation,reason,actor_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.SchemaVersion, run.DatasetID, run.ProjectID, run.Status, string(profiles),
		run.IsolatedMemoryNamespace, run.IsolatedCacheNamespace, run.BudgetSeconds, run.Concurrency,
		run.ScoringProfile, string(results), run.ReportSHA256, run.PromotionRecommendation,
		run.Reason, run.ActorID, run.CreatedAt.Format(timestampFormat))
	if err != nil {
		return evaluation.Run{}, err
	}
	return run, nil
}

func (s *Store) ListEvaluationRuns(ctx context.Context, datasetID string, limit int) ([]evaluation.Run, error) {
	query := `SELECT id,schema_version,dataset_id,project_id,status,profile_matrix_json,isolated_memory_namespace,
		isolated_cache_namespace,budget_seconds,concurrency,scoring_profile,results_json,report_sha256,
		promotion_recommendation,reason,actor_id,created_at FROM evaluation_runs`
	args := []any{}
	if datasetID != "" {
		query += " WHERE dataset_id=?"
		args = append(args, datasetID)
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []evaluation.Run{}
	for rows.Next() {
		item, err := scanEvaluationRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanEvaluationDataset(row scanner) (evaluation.Dataset, error) {
	var dataset evaluation.Dataset
	var exclusions, metadata, created string
	err := row.Scan(&dataset.ID, &dataset.SchemaVersion, &dataset.ProjectID, &dataset.Name, &dataset.SourceKind,
		&dataset.Repository, &dataset.BaseRevision, &dataset.TargetRevision, &dataset.KnownPatchSHA256,
		&dataset.HiddenPatchSHA256, &exclusions, &dataset.ScoringProfile, &dataset.RetentionDays,
		&dataset.ReproducibilityKey, &metadata, &dataset.ActorID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return evaluation.Dataset{}, storage.ErrNotFound
	}
	if err != nil {
		return evaluation.Dataset{}, err
	}
	if err := json.Unmarshal([]byte(exclusions), &dataset.Exclusions); err != nil {
		return evaluation.Dataset{}, err
	}
	dataset.Metadata = json.RawMessage(metadata)
	createdAt, err := parseTime(created)
	if err != nil {
		return evaluation.Dataset{}, err
	}
	dataset.CreatedAt = createdAt
	return dataset, nil
}

func scanEvaluationRun(row scanner) (evaluation.Run, error) {
	var run evaluation.Run
	var profiles, results, created string
	err := row.Scan(&run.ID, &run.SchemaVersion, &run.DatasetID, &run.ProjectID, &run.Status, &profiles,
		&run.IsolatedMemoryNamespace, &run.IsolatedCacheNamespace, &run.BudgetSeconds, &run.Concurrency,
		&run.ScoringProfile, &results, &run.ReportSHA256, &run.PromotionRecommendation, &run.Reason,
		&run.ActorID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return evaluation.Run{}, storage.ErrNotFound
	}
	if err != nil {
		return evaluation.Run{}, err
	}
	if err := json.Unmarshal([]byte(profiles), &run.ProfileMatrix); err != nil {
		return evaluation.Run{}, err
	}
	if err := json.Unmarshal([]byte(results), &run.Results); err != nil {
		return evaluation.Run{}, err
	}
	createdAt, err := parseTime(created)
	if err != nil {
		return evaluation.Run{}, err
	}
	run.CreatedAt = createdAt
	return run, nil
}
