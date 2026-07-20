package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/local-code-maintainer/appliance/internal/audit"
	"github.com/local-code-maintainer/appliance/internal/intelligence"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func (s *Store) FindBlobAnalysis(ctx context.Context, projectID, repository, blobSHA256, parserID string) (intelligence.BlobAnalysis, bool, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT analysis_json FROM code_intel_blobs
		WHERE project_id = ? AND repository = ? AND blob_sha256 = ? AND parser_id = ?`, projectID, repository, blobSHA256, parserID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return intelligence.BlobAnalysis{}, false, nil
	}
	if err != nil {
		return intelligence.BlobAnalysis{}, false, fmt.Errorf("find code intelligence blob: %w", err)
	}
	var analysis intelligence.BlobAnalysis
	if err := json.Unmarshal([]byte(payload), &analysis); err != nil {
		return intelligence.BlobAnalysis{}, false, fmt.Errorf("decode code intelligence blob: %w", err)
	}
	return analysis, true, nil
}

func (s *Store) CommitIndex(ctx context.Context, request intelligence.IndexRequest, analyses []intelligence.BlobAnalysis, files []intelligence.IndexedFile) (intelligence.IndexRun, error) {
	if len(files) != len(request.Files) {
		return intelligence.IndexRun{}, fmt.Errorf("file evidence does not match index request: %w", storage.ErrInvalid)
	}
	id, err := NewID("indexrun")
	if err != nil {
		return intelligence.IndexRun{}, err
	}
	now := s.now()
	run := intelligence.IndexRun{ID: id, ProjectID: request.ProjectID, Repository: request.Repository, Revision: request.Revision, ParserID: request.ParserID, State: "complete", Files: len(files), StartedAt: now, CompletedAt: now, FileEvidence: append([]intelligence.IndexedFile(nil), files...)}
	for _, file := range files {
		if file.Reused {
			run.Reused++
		} else {
			run.Parsed++
		}
		if file.Status == "failed" {
			run.Failures++
			run.State = "partial"
		}
	}
	for _, source := range request.Files {
		run.Bytes += int64(len(source.Content))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return intelligence.IndexRun{}, fmt.Errorf("begin code index: %w", err)
	}
	defer tx.Rollback()
	var registeredRepository string
	if err := tx.QueryRowContext(ctx, `SELECT repository FROM projects WHERE id = ? AND enabled = 1`, request.ProjectID).Scan(&registeredRepository); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return intelligence.IndexRun{}, storage.ErrNotFound
		}
		return intelligence.IndexRun{}, err
	}
	if registeredRepository != request.Repository {
		return intelligence.IndexRun{}, fmt.Errorf("index repository does not match registered project: %w", storage.ErrInvalid)
	}
	for _, analysis := range analyses {
		payload, marshalErr := json.Marshal(analysis)
		if marshalErr != nil {
			return intelligence.IndexRun{}, marshalErr
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO code_intel_blobs(project_id, repository, blob_sha256, parser_id, language, classification, bytes, analysis_json, created_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(project_id, repository, blob_sha256, parser_id) DO NOTHING`, request.ProjectID, request.Repository, analysis.BlobSHA256, analysis.ParserID, analysis.Language, analysis.Classification, analysis.Bytes, string(payload), now.Format(timestampFormat)); err != nil {
			return intelligence.IndexRun{}, fmt.Errorf("store code intelligence blob: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO code_intel_runs(id, project_id, repository, revision, parser_id, state, files, parsed, reused, failures, bytes, started_at, completed_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, run.ID, run.ProjectID, run.Repository, run.Revision, run.ParserID, run.State, run.Files, run.Parsed, run.Reused, run.Failures, run.Bytes, now.Format(timestampFormat), now.Format(timestampFormat)); err != nil {
		return intelligence.IndexRun{}, fmt.Errorf("store code intelligence run: %w", err)
	}
	for _, file := range files {
		if _, err := tx.ExecContext(ctx, `INSERT INTO code_intel_files(run_id, path, blob_sha256, language, status, failure, reused) VALUES(?, ?, ?, ?, ?, ?, ?)`, run.ID, file.Path, file.BlobSHA256, file.Language, file.Status, file.Failure, boolInt(file.Reused)); err != nil {
			return intelligence.IndexRun{}, fmt.Errorf("store indexed file: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return intelligence.IndexRun{}, fmt.Errorf("commit code index: %w", err)
	}
	return run, nil
}

func (s *Store) IntelligenceStatus(ctx context.Context, projectID string) (intelligence.Status, error) {
	status := intelligence.Status{ProjectID: projectID, State: "never_indexed", Languages: []string{}, ParserIDs: []string{}}
	var completed string
	err := s.db.QueryRowContext(ctx, `SELECT id, revision, state, files, failures, bytes, parser_id, completed_at FROM code_intel_runs WHERE project_id = ? ORDER BY completed_at DESC, id DESC LIMIT 1`, projectID).Scan(&status.LatestRunID, &status.LatestRevision, &status.State, &status.Files, &status.Failures, &status.StorageBytes, new(string), &completed)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		if queryErr := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id = ?`, projectID).Scan(&exists); queryErr != nil {
			return status, queryErr
		}
		if exists == 0 {
			return status, storage.ErrNotFound
		}
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.FreshAt, _ = time.Parse(timestampFormat, completed)
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT f.language, r.parser_id FROM code_intel_files f JOIN code_intel_runs r ON r.id=f.run_id WHERE r.project_id=? AND r.id=? ORDER BY f.language, r.parser_id`, projectID, status.LatestRunID)
	if err != nil {
		return status, err
	}
	defer rows.Close()
	languages, parsers := map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var language, parser string
		if err := rows.Scan(&language, &parser); err != nil {
			return status, err
		}
		languages[language] = true
		parsers[parser] = true
	}
	for value := range languages {
		status.Languages = append(status.Languages, value)
	}
	for value := range parsers {
		status.ParserIDs = append(status.ParserIDs, value)
	}
	sort.Strings(status.Languages)
	sort.Strings(status.ParserIDs)
	return status, rows.Err()
}

func (s *Store) QueryIntelligence(ctx context.Context, query intelligence.Query) (intelligence.QueryResult, error) {
	result := intelligence.QueryResult{ProjectID: query.ProjectID, Term: query.Term, Symbols: []intelligence.Symbol{}, Relations: []intelligence.Relation{}}
	runQuery := `SELECT id, revision, failures FROM code_intel_runs WHERE project_id=?`
	args := []any{query.ProjectID}
	if query.Revision != "" {
		runQuery += ` AND revision=?`
		args = append(args, query.Revision)
	}
	runQuery += ` ORDER BY completed_at DESC, id DESC LIMIT 1`
	var runID string
	if err := s.db.QueryRowContext(ctx, runQuery, args...).Scan(&runID, &result.Revision, &result.Failures); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, storage.ErrNotFound
		}
		return result, err
	}
	result.Partial = result.Failures > 0
	rows, err := s.db.QueryContext(ctx, `SELECT b.analysis_json FROM code_intel_files f JOIN code_intel_runs r ON r.id=f.run_id JOIN code_intel_blobs b ON b.project_id=r.project_id AND b.repository=r.repository AND b.blob_sha256=f.blob_sha256 AND b.parser_id=r.parser_id WHERE f.run_id=? ORDER BY f.path`, runID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	needle := strings.ToLower(query.Term)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return result, err
		}
		var analysis intelligence.BlobAnalysis
		if err := json.Unmarshal([]byte(payload), &analysis); err != nil {
			return result, err
		}
		for _, symbol := range analysis.Symbols {
			if strings.Contains(strings.ToLower(symbol.Name), needle) || strings.Contains(strings.ToLower(symbol.Path), needle) {
				if len(result.Symbols) < query.Limit {
					result.Symbols = append(result.Symbols, symbol)
				}
			}
		}
		for _, relation := range analysis.Relations {
			if strings.Contains(strings.ToLower(relation.From), needle) || strings.Contains(strings.ToLower(relation.To), needle) {
				if len(result.Relations) < query.Limit {
					result.Relations = append(result.Relations, relation)
				}
			}
		}
	}
	return result, rows.Err()
}

func (s *Store) RebuildIntelligence(ctx context.Context, projectID, actorID, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id=?`, projectID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return storage.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM code_intel_files WHERE run_id IN (SELECT id FROM code_intel_runs WHERE project_id=?)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM code_intel_runs WHERE project_id=?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM code_intel_blobs WHERE project_id=?`, projectID); err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{"derived_index_removed": true, "reason": reason})
	if err != nil {
		return err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: actorID, ActorRole: "administrator", Action: "intelligence.rebuild", TargetType: "project", TargetID: projectID, Details: details}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SaveContextManifest(ctx context.Context, manifest intelligence.ContextManifest) (intelligence.ContextManifest, error) {
	id, err := NewID("context")
	if err != nil {
		return manifest, err
	}
	manifest.ID = id
	manifest.CreatedAt = s.now()
	payload, err := json.Marshal(manifest.Selections)
	if err != nil {
		return manifest, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO context_manifests(id,project_id,job_id,stage,schema_version,budget_tokens,reserved_output_tokens,used_tokens,truncated,selections_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, manifest.ID, manifest.ProjectID, nullableString(manifest.JobID), manifest.Stage, manifest.SchemaVersion, manifest.BudgetTokens, manifest.ReservedOutputTokens, manifest.UsedTokens, boolInt(manifest.Truncated), string(payload), manifest.CreatedAt.Format(timestampFormat))
	return manifest, err
}

func (s *Store) GetContextManifest(ctx context.Context, projectID, id string) (intelligence.ContextManifest, error) {
	var result intelligence.ContextManifest
	var job sql.NullString
	var selections, created string
	var truncated int
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,job_id,stage,schema_version,budget_tokens,reserved_output_tokens,used_tokens,truncated,selections_json,created_at FROM context_manifests WHERE id=? AND project_id=?`, id, projectID).Scan(&result.ID, &result.ProjectID, &job, &result.Stage, &result.SchemaVersion, &result.BudgetTokens, &result.ReservedOutputTokens, &result.UsedTokens, &truncated, &selections, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return result, storage.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	result.JobID = job.String
	result.Truncated = truncated != 0
	result.CreatedAt, _ = time.Parse(timestampFormat, created)
	err = json.Unmarshal([]byte(selections), &result.Selections)
	return result, err
}

func (s *Store) ListContextManifests(ctx context.Context, projectID string, limit int) ([]intelligence.ContextManifest, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM context_manifests WHERE project_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]intelligence.ContextManifest, 0, len(ids))
	for _, id := range ids {
		item, err := s.GetContextManifest(ctx, projectID, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) SaveBaseline(ctx context.Context, value intelligence.Baseline) (intelligence.Baseline, error) {
	id, err := NewID("baseline")
	if err != nil {
		return value, err
	}
	value.ID = id
	value.CreatedAt = s.now()
	payload, _ := json.Marshal(value.Observations)
	_, err = s.db.ExecContext(ctx, `INSERT INTO verification_baselines(id,project_id,revision,config_sha256,toolchain_id,pack_set_sha256,actor_id,reason,observations_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, value.ID, value.ProjectID, value.Revision, value.ConfigSHA256, value.ToolchainID, value.PackSetSHA256, value.ActorID, value.Reason, string(payload), value.CreatedAt.Format(timestampFormat))
	return value, err
}
func (s *Store) ListBaselines(ctx context.Context, projectID string, limit int) ([]intelligence.Baseline, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,revision,config_sha256,toolchain_id,pack_set_sha256,actor_id,reason,observations_json,created_at FROM verification_baselines WHERE project_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []intelligence.Baseline{}
	for rows.Next() {
		var value intelligence.Baseline
		var payload, created string
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.Revision, &value.ConfigSHA256, &value.ToolchainID, &value.PackSetSHA256, &value.ActorID, &value.Reason, &payload, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &value.Observations); err != nil {
			return nil, err
		}
		value.CreatedAt, _ = time.Parse(timestampFormat, created)
		items = append(items, value)
	}
	return items, rows.Err()
}
func (s *Store) SaveDifferential(ctx context.Context, value intelligence.Differential) (intelligence.Differential, error) {
	id, err := NewID("differential")
	if err != nil {
		return value, err
	}
	value.ID = id
	value.CreatedAt = s.now()
	payload, _ := json.Marshal(value.Items)
	_, err = s.db.ExecContext(ctx, `INSERT INTO verification_differentials(id,baseline_id,candidate_sha,items_json,created_at) VALUES(?,?,?,?,?)`, value.ID, value.BaselineID, value.CandidateSHA, string(payload), value.CreatedAt.Format(timestampFormat))
	return value, err
}
func (s *Store) ListDifferentials(ctx context.Context, projectID string, limit int) ([]intelligence.Differential, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.baseline_id,d.candidate_sha,d.items_json,d.created_at FROM verification_differentials d JOIN verification_baselines b ON b.id=d.baseline_id WHERE b.project_id=? ORDER BY d.created_at DESC,d.id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []intelligence.Differential{}
	for rows.Next() {
		var value intelligence.Differential
		var payload, created string
		if err := rows.Scan(&value.ID, &value.BaselineID, &value.CandidateSHA, &payload, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &value.Items); err != nil {
			return nil, err
		}
		value.CreatedAt, _ = time.Parse(timestampFormat, created)
		items = append(items, value)
	}
	return items, rows.Err()
}
func (s *Store) SaveTestImpact(ctx context.Context, value intelligence.TestImpact) (intelligence.TestImpact, error) {
	id, err := NewID("impact")
	if err != nil {
		return value, err
	}
	value.ID = id
	value.CreatedAt = s.now()
	changed, _ := json.Marshal(value.ChangedSymbols)
	selections, _ := json.Marshal(value.Selections)
	_, err = s.db.ExecContext(ctx, `INSERT INTO test_impact_records(id,project_id,revision,changed_symbols_json,selections_json,full_suite_required,policy_explanation,created_at) VALUES(?,?,?,?,?,?,?,?)`, value.ID, value.ProjectID, value.Revision, string(changed), string(selections), boolInt(value.FullSuiteRequired), value.PolicyExplanation, value.CreatedAt.Format(timestampFormat))
	return value, err
}
func (s *Store) ListTestImpacts(ctx context.Context, projectID string, limit int) ([]intelligence.TestImpact, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,revision,changed_symbols_json,selections_json,full_suite_required,policy_explanation,created_at FROM test_impact_records WHERE project_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []intelligence.TestImpact{}
	for rows.Next() {
		var value intelligence.TestImpact
		var changed, selections, created string
		var full int
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.Revision, &changed, &selections, &full, &value.PolicyExplanation, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(changed), &value.ChangedSymbols); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(selections), &value.Selections); err != nil {
			return nil, err
		}
		value.FullSuiteRequired = full != 0
		value.CreatedAt, _ = time.Parse(timestampFormat, created)
		items = append(items, value)
	}
	return items, rows.Err()
}

func (s *Store) PutCacheEntry(ctx context.Context, value intelligence.CacheEntry) (intelligence.CacheEntry, error) {
	now := s.now()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	}
	value.LastHitAt = now
	result, err := s.db.ExecContext(ctx, `INSERT INTO cache_entries(project_id,cache_key,trust_domain,kind,input_sha256,object_sha256,bytes,verified,created_at,last_hit_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id,cache_key) DO UPDATE SET last_hit_at=excluded.last_hit_at WHERE cache_entries.object_sha256=excluded.object_sha256 AND cache_entries.input_sha256=excluded.input_sha256`, value.ProjectID, value.Key, value.TrustDomain, value.Kind, value.InputSHA256, value.ObjectSHA256, value.Bytes, boolInt(value.Verified), value.CreatedAt.Format(timestampFormat), value.LastHitAt.Format(timestampFormat), value.ExpiresAt.Format(timestampFormat))
	if err != nil {
		return value, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return intelligence.CacheEntry{}, storage.ErrConflict
	}
	return value, nil
}
func (s *Store) ListCacheEntries(ctx context.Context, projectID string, limit int) ([]intelligence.CacheEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT cache_key,project_id,trust_domain,kind,input_sha256,object_sha256,bytes,verified,created_at,last_hit_at,expires_at FROM cache_entries WHERE project_id=? ORDER BY last_hit_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []intelligence.CacheEntry{}
	for rows.Next() {
		var value intelligence.CacheEntry
		var verified int
		var created, hit, expires string
		if err := rows.Scan(&value.Key, &value.ProjectID, &value.TrustDomain, &value.Kind, &value.InputSHA256, &value.ObjectSHA256, &value.Bytes, &verified, &created, &hit, &expires); err != nil {
			return nil, err
		}
		value.Verified = verified != 0
		value.CreatedAt, _ = time.Parse(timestampFormat, created)
		value.LastHitAt, _ = time.Parse(timestampFormat, hit)
		value.ExpiresAt, _ = time.Parse(timestampFormat, expires)
		items = append(items, value)
	}
	return items, rows.Err()
}
func (s *Store) PurgeCacheEntries(ctx context.Context, projectID, kind, actorID, reason string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM cache_entries WHERE project_id=? AND (?='' OR kind=?)`, projectID, kind, kind)
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	details, _ := json.Marshal(map[string]any{"kind": kind, "entries": count, "reason": reason})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: actorID, ActorRole: "administrator", Action: "cache.purge", TargetType: "project", TargetID: projectID, Details: details}); err != nil {
		return 0, err
	}
	return int(count), tx.Commit()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
