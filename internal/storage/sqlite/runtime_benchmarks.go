package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/B1-Mordred/CodeMaintainer/internal/runtimeopt"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) RecordRuntimeBenchmark(ctx context.Context, run runtimeopt.BenchmarkRun) (runtimeopt.BenchmarkRun, error) {
	if run.ID == "" {
		id, err := NewID("runtime_benchmark")
		if err != nil {
			return runtimeopt.BenchmarkRun{}, err
		}
		run.ID = id
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = s.now()
	}
	if err := run.Validate(); err != nil {
		return runtimeopt.BenchmarkRun{}, storage.ErrInvalid
	}
	fixtures, _ := json.Marshal(run.QualityFixtures)
	features, _ := json.Marshal(run.ExperimentalFeatures)
	_, err := s.db.ExecContext(ctx, `INSERT INTO runtime_benchmarks(
		id,profile_id,role,model_family,model_sha256,quantization,context_limit,threads,batch,ubatch,numa,
		runtime_identity_sha256,prompt_tokens_second,decode_tokens_second,duration_millis,memory_bytes,healthy,
		quality_fixtures_json,quality_status,determinism_status,cache_mode,cache_identity_sha256,experimental_features_json,
		recommendation,reason,actor_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.ProfileID, run.Role, run.ModelFamily, run.ModelSHA256, run.Quantization, run.ContextLimit, run.Threads,
		run.Batch, run.UBatch, run.NUMA, run.RuntimeIdentitySHA256, run.PromptTokensSecond, run.DecodeTokensSecond,
		run.DurationMillis, run.MemoryBytes, boolInt(run.Healthy), string(fixtures), run.QualityStatus,
		run.DeterminismStatus, run.CacheMode, run.CacheIdentitySHA256, string(features), run.Recommendation, run.Reason,
		run.ActorID, run.CreatedAt.Format(timestampFormat))
	if err != nil {
		return runtimeopt.BenchmarkRun{}, err
	}
	return run, nil
}

func (s *Store) ListRuntimeBenchmarks(ctx context.Context, profileID string, limit int) ([]runtimeopt.BenchmarkRun, error) {
	query := `SELECT id,profile_id,role,model_family,model_sha256,quantization,context_limit,threads,batch,ubatch,numa,
		runtime_identity_sha256,prompt_tokens_second,decode_tokens_second,duration_millis,memory_bytes,healthy,
		quality_fixtures_json,quality_status,determinism_status,cache_mode,cache_identity_sha256,experimental_features_json,
		recommendation,reason,actor_id,created_at FROM runtime_benchmarks`
	args := []any{}
	if profileID != "" {
		query += " WHERE profile_id=?"
		args = append(args, profileID)
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []runtimeopt.BenchmarkRun{}
	for rows.Next() {
		item, err := scanRuntimeBenchmark(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanRuntimeBenchmark(row scanner) (runtimeopt.BenchmarkRun, error) {
	var run runtimeopt.BenchmarkRun
	var fixtures, features, created string
	var healthy int
	err := row.Scan(&run.ID, &run.ProfileID, &run.Role, &run.ModelFamily, &run.ModelSHA256, &run.Quantization,
		&run.ContextLimit, &run.Threads, &run.Batch, &run.UBatch, &run.NUMA, &run.RuntimeIdentitySHA256,
		&run.PromptTokensSecond, &run.DecodeTokensSecond, &run.DurationMillis, &run.MemoryBytes, &healthy,
		&fixtures, &run.QualityStatus, &run.DeterminismStatus, &run.CacheMode, &run.CacheIdentitySHA256, &features,
		&run.Recommendation, &run.Reason, &run.ActorID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return runtimeopt.BenchmarkRun{}, storage.ErrNotFound
	}
	if err != nil {
		return runtimeopt.BenchmarkRun{}, err
	}
	run.Healthy = healthy == 1
	if err := json.Unmarshal([]byte(fixtures), &run.QualityFixtures); err != nil {
		return runtimeopt.BenchmarkRun{}, err
	}
	if err := json.Unmarshal([]byte(features), &run.ExperimentalFeatures); err != nil {
		return runtimeopt.BenchmarkRun{}, err
	}
	createdAt, err := parseTime(created)
	if err != nil {
		return runtimeopt.BenchmarkRun{}, err
	}
	run.CreatedAt = createdAt
	return run, nil
}
