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
	if request.TerminalState == "cancelled" {
		run.State = "cancelled"
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
	err := s.db.QueryRowContext(ctx, `SELECT id, revision, state, files, failures, bytes, parser_id, completed_at FROM code_intel_runs WHERE project_id = ? ORDER BY completed_at DESC, id DESC LIMIT 1`, projectID).Scan(&status.LatestRunID, &status.LatestRevision, &status.State, &status.Files, &status.Failures, new(int64), new(string), &completed)
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
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(bytes),0) FROM code_intel_blobs WHERE project_id=?`, projectID).Scan(&status.StorageBytes); err != nil {
		return status, err
	}
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
	cacheResult, err := tx.ExecContext(ctx, `DELETE FROM cache_entries WHERE project_id=? AND kind='source-parse'`, projectID)
	if err != nil {
		return err
	}
	cacheEntries, _ := cacheResult.RowsAffected()
	details, err := json.Marshal(map[string]any{"derived_index_removed": true, "derived_cache_entries_removed": cacheEntries, "reason": reason})
	if err != nil {
		return err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: actorID, ActorRole: "administrator", Action: "intelligence.rebuild", TargetType: "project", TargetID: projectID, Details: details}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) PruneIntelligence(ctx context.Context, projectID string, before, now time.Time) (intelligence.RetentionResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return intelligence.RetentionResult{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM code_intel_files WHERE run_id IN (
		SELECT id FROM code_intel_runs WHERE project_id=? AND completed_at<? AND id<>(SELECT id FROM code_intel_runs WHERE project_id=? ORDER BY completed_at DESC,id DESC LIMIT 1)
	)`, projectID, before.Format(timestampFormat), projectID); err != nil {
		return intelligence.RetentionResult{}, err
	}
	runs, err := tx.ExecContext(ctx, `DELETE FROM code_intel_runs WHERE project_id=? AND completed_at<? AND id<>(SELECT id FROM code_intel_runs WHERE project_id=? ORDER BY completed_at DESC,id DESC LIMIT 1)`, projectID, before.Format(timestampFormat), projectID)
	if err != nil {
		return intelligence.RetentionResult{}, err
	}
	blobs, err := tx.ExecContext(ctx, `DELETE FROM code_intel_blobs WHERE project_id=? AND NOT EXISTS (
		SELECT 1 FROM code_intel_files f JOIN code_intel_runs r ON r.id=f.run_id
		WHERE r.project_id=code_intel_blobs.project_id AND r.repository=code_intel_blobs.repository AND r.parser_id=code_intel_blobs.parser_id AND f.blob_sha256=code_intel_blobs.blob_sha256
	)`, projectID)
	if err != nil {
		return intelligence.RetentionResult{}, err
	}
	caches, err := tx.ExecContext(ctx, `DELETE FROM cache_entries WHERE project_id=? AND expires_at<?`, projectID, now.Format(timestampFormat))
	if err != nil {
		return intelligence.RetentionResult{}, err
	}
	runCount, _ := runs.RowsAffected()
	blobCount, _ := blobs.RowsAffected()
	cacheCount, _ := caches.RowsAffected()
	result := intelligence.RetentionResult{RunsRemoved: int(runCount), BlobsRemoved: int(blobCount), CachesRemoved: int(cacheCount)}
	if runCount+blobCount+cacheCount != 0 {
		details, _ := json.Marshal(result)
		if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: "workflow-controller", ActorRole: "system", Action: "intelligence.retention", TargetType: "project", TargetID: projectID, Details: details}); err != nil {
			return intelligence.RetentionResult{}, err
		}
	}
	return result, tx.Commit()
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
	if existing, found, err := s.FindBaseline(ctx, value.ProjectID, value.Revision, value.ConfigSHA256, value.ToolchainID, value.PackSetSHA256); err != nil {
		return value, err
	} else if found {
		return existing, nil
	}
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

func (s *Store) GetBaseline(ctx context.Context, projectID, baselineID string) (intelligence.Baseline, error) {
	value, err := scanBaseline(s.db.QueryRowContext(ctx, `SELECT id,project_id,revision,config_sha256,toolchain_id,pack_set_sha256,actor_id,reason,observations_json,created_at FROM verification_baselines WHERE project_id=? AND id=?`, projectID, baselineID))
	if errors.Is(err, sql.ErrNoRows) {
		return value, storage.ErrNotFound
	}
	return value, err
}

func (s *Store) FindBaseline(ctx context.Context, projectID, revision, configSHA256, toolchainID, packSetSHA256 string) (intelligence.Baseline, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,project_id,revision,config_sha256,toolchain_id,pack_set_sha256,actor_id,reason,observations_json,created_at
		FROM verification_baselines WHERE project_id=? AND revision=? AND config_sha256=? AND toolchain_id=? AND pack_set_sha256=?`, projectID, revision, configSHA256, toolchainID, packSetSHA256)
	value, err := scanBaseline(row)
	if errors.Is(err, sql.ErrNoRows) {
		return intelligence.Baseline{}, false, nil
	}
	return value, err == nil, err
}

func scanBaseline(row scanner) (intelligence.Baseline, error) {
	var value intelligence.Baseline
	var payload, created string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Revision, &value.ConfigSHA256, &value.ToolchainID, &value.PackSetSHA256, &value.ActorID, &value.Reason, &payload, &created); err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(payload), &value.Observations); err != nil {
		return value, err
	}
	value.CreatedAt, _ = time.Parse(timestampFormat, created)
	return value, nil
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
		value, err := scanBaseline(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func (s *Store) SaveBaselineSupersession(ctx context.Context, value intelligence.BaselineSupersession) (intelligence.BaselineSupersession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	value.ID, err = NewID("baselinesupersession")
	if err != nil {
		return value, err
	}
	value.CreatedAt = s.now()
	value.Replacement.ID, err = NewID("baseline")
	if err != nil {
		return value, err
	}
	value.Replacement.CreatedAt = value.CreatedAt
	payload, _ := json.Marshal(value.Replacement.Observations)
	_, err = tx.ExecContext(ctx, `INSERT INTO verification_baselines(id,project_id,revision,config_sha256,toolchain_id,pack_set_sha256,actor_id,reason,observations_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, value.Replacement.ID, value.Replacement.ProjectID, value.Replacement.Revision, value.Replacement.ConfigSHA256, value.Replacement.ToolchainID, value.Replacement.PackSetSHA256, value.Replacement.ActorID, value.Replacement.Reason, string(payload), value.CreatedAt.Format(timestampFormat))
	if err != nil {
		return value, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO baseline_supersessions(id,project_id,baseline_id,differential_id,replacement_baseline_id,actor_id,reason,created_at) VALUES(?,?,?,?,?,?,?,?)`, value.ID, value.ProjectID, value.BaselineID, value.DifferentialID, value.Replacement.ID, value.ActorID, value.Reason, value.CreatedAt.Format(timestampFormat))
	if err != nil {
		return value, err
	}
	details, _ := json.Marshal(map[string]any{"baseline_id": value.BaselineID, "differential_id": value.DifferentialID, "replacement_baseline_id": value.Replacement.ID, "reason": value.Reason})
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: value.ActorID, ActorRole: "administrator", Action: "baseline.supersede", TargetType: "project", TargetID: value.ProjectID, Details: details}); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (s *Store) ListBaselineSupersessions(ctx context.Context, projectID string, limit int) ([]intelligence.BaselineSupersession, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.project_id,s.baseline_id,s.differential_id,s.actor_id,s.reason,s.created_at,b.id,b.project_id,b.revision,b.config_sha256,b.toolchain_id,b.pack_set_sha256,b.actor_id,b.reason,b.observations_json,b.created_at FROM baseline_supersessions s JOIN verification_baselines b ON b.id=s.replacement_baseline_id WHERE s.project_id=? ORDER BY s.created_at DESC,s.id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []intelligence.BaselineSupersession{}
	for rows.Next() {
		var value intelligence.BaselineSupersession
		var supersededAt, observations, replacementAt string
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.BaselineID, &value.DifferentialID, &value.ActorID, &value.Reason, &supersededAt, &value.Replacement.ID, &value.Replacement.ProjectID, &value.Replacement.Revision, &value.Replacement.ConfigSHA256, &value.Replacement.ToolchainID, &value.Replacement.PackSetSHA256, &value.Replacement.ActorID, &value.Replacement.Reason, &observations, &replacementAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(observations), &value.Replacement.Observations); err != nil {
			return nil, err
		}
		value.CreatedAt, _ = time.Parse(timestampFormat, supersededAt)
		value.Replacement.CreatedAt, _ = time.Parse(timestampFormat, replacementAt)
		items = append(items, value)
	}
	return items, rows.Err()
}

func (s *Store) SaveDifferential(ctx context.Context, value intelligence.Differential) (intelligence.Differential, error) {
	var existing intelligence.Differential
	var payload, created string
	err := s.db.QueryRowContext(ctx, `SELECT id,baseline_id,candidate_sha,purpose,items_json,created_at FROM verification_differentials WHERE baseline_id=? AND candidate_sha=? AND purpose=?`, value.BaselineID, value.CandidateSHA, value.Purpose).Scan(&existing.ID, &existing.BaselineID, &existing.CandidateSHA, &existing.Purpose, &payload, &created)
	if err == nil {
		if err := json.Unmarshal([]byte(payload), &existing.Items); err != nil {
			return value, err
		}
		existing.CreatedAt, _ = time.Parse(timestampFormat, created)
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return value, err
	}
	id, err := NewID("differential")
	if err != nil {
		return value, err
	}
	value.ID = id
	value.CreatedAt = s.now()
	serialized, _ := json.Marshal(value.Items)
	_, err = s.db.ExecContext(ctx, `INSERT INTO verification_differentials(id,baseline_id,candidate_sha,purpose,items_json,created_at) VALUES(?,?,?,?,?,?)`, value.ID, value.BaselineID, value.CandidateSHA, value.Purpose, string(serialized), value.CreatedAt.Format(timestampFormat))
	return value, err
}

func (s *Store) GetDifferential(ctx context.Context, projectID, differentialID string) (intelligence.Differential, error) {
	var value intelligence.Differential
	var payload, created string
	err := s.db.QueryRowContext(ctx, `SELECT d.id,d.baseline_id,d.candidate_sha,d.purpose,d.items_json,d.created_at FROM verification_differentials d JOIN verification_baselines b ON b.id=d.baseline_id WHERE b.project_id=? AND d.id=?`, projectID, differentialID).Scan(&value.ID, &value.BaselineID, &value.CandidateSHA, &value.Purpose, &payload, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return value, storage.ErrNotFound
	}
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(payload), &value.Items); err != nil {
		return value, err
	}
	value.CreatedAt, _ = time.Parse(timestampFormat, created)
	return value, nil
}
func (s *Store) ListDifferentials(ctx context.Context, projectID string, limit int) ([]intelligence.Differential, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.baseline_id,d.candidate_sha,d.purpose,d.items_json,d.created_at FROM verification_differentials d JOIN verification_baselines b ON b.id=d.baseline_id WHERE b.project_id=? ORDER BY d.created_at DESC,d.id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []intelligence.Differential{}
	for rows.Next() {
		var value intelligence.Differential
		var payload, created string
		if err := rows.Scan(&value.ID, &value.BaselineID, &value.CandidateSHA, &value.Purpose, &payload, &created); err != nil {
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

func (s *Store) SaveDifferentialCorrection(ctx context.Context, value intelligence.DifferentialCorrection) (intelligence.DifferentialCorrection, error) {
	value.ID, _ = NewID("correction")
	value.CreatedAt = s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO differential_corrections(id,project_id,differential_id,observation_kind,observation_key,before_classification,after_classification,actor_id,reason,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, value.ID, value.ProjectID, value.DifferentialID, value.ObservationKind, value.ObservationKey, value.BeforeClassification, value.AfterClassification, value.ActorID, value.Reason, value.CreatedAt.Format(timestampFormat))
	if err != nil {
		return value, err
	}
	details, _ := json.Marshal(value)
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: value.ActorID, ActorRole: "administrator", Action: "differential.correct", TargetType: "differential", TargetID: value.DifferentialID, Details: details}); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (s *Store) ListDifferentialCorrections(ctx context.Context, projectID string, limit int) ([]intelligence.DifferentialCorrection, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,differential_id,observation_kind,observation_key,before_classification,after_classification,actor_id,reason,created_at FROM differential_corrections WHERE project_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []intelligence.DifferentialCorrection{}
	for rows.Next() {
		var value intelligence.DifferentialCorrection
		var created string
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.DifferentialID, &value.ObservationKind, &value.ObservationKey, &value.BeforeClassification, &value.AfterClassification, &value.ActorID, &value.Reason, &created); err != nil {
			return nil, err
		}
		value.CreatedAt, _ = time.Parse(timestampFormat, created)
		items = append(items, value)
	}
	return items, rows.Err()
}

func (s *Store) SaveTestImpact(ctx context.Context, value intelligence.TestImpact) (intelligence.TestImpact, error) {
	if existing, found, err := s.FindTestImpact(ctx, value.ProjectID, value.Revision); err != nil {
		return value, err
	} else if found {
		return existing, nil
	}
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

func (s *Store) GetTestImpact(ctx context.Context, projectID, impactID string) (intelligence.TestImpact, error) {
	var value intelligence.TestImpact
	var changed, selections, created string
	var full int
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,revision,changed_symbols_json,selections_json,full_suite_required,policy_explanation,created_at FROM test_impact_records WHERE project_id=? AND id=?`, projectID, impactID).Scan(&value.ID, &value.ProjectID, &value.Revision, &changed, &selections, &full, &value.PolicyExplanation, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return value, storage.ErrNotFound
	}
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(changed), &value.ChangedSymbols); err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(selections), &value.Selections); err != nil {
		return value, err
	}
	value.FullSuiteRequired = full != 0
	value.CreatedAt, _ = time.Parse(timestampFormat, created)
	return value, nil
}

func (s *Store) FindTestImpact(ctx context.Context, projectID, revision string) (intelligence.TestImpact, bool, error) {
	var value intelligence.TestImpact
	var changed, selections, created string
	var full int
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,revision,changed_symbols_json,selections_json,full_suite_required,policy_explanation,created_at FROM test_impact_records WHERE project_id=? AND revision=?`, projectID, revision).Scan(&value.ID, &value.ProjectID, &value.Revision, &changed, &selections, &full, &value.PolicyExplanation, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return intelligence.TestImpact{}, false, nil
	}
	if err != nil {
		return value, false, err
	}
	if err := json.Unmarshal([]byte(changed), &value.ChangedSymbols); err != nil {
		return value, false, err
	}
	if err := json.Unmarshal([]byte(selections), &value.Selections); err != nil {
		return value, false, err
	}
	value.FullSuiteRequired = full != 0
	value.CreatedAt, _ = time.Parse(timestampFormat, created)
	return value, true, nil
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

func (s *Store) SaveTestImpactOverride(ctx context.Context, value intelligence.TestImpactOverride) (intelligence.TestImpactOverride, error) {
	value.ID, _ = NewID("impactoverride")
	value.CreatedAt = s.now()
	var expires any
	if value.ExpiresAt != nil {
		expires = value.ExpiresAt.UTC().Format(timestampFormat)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO test_impact_overrides(id,project_id,impact_id,test_id,selected,actor_id,reason,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, value.ID, value.ProjectID, value.ImpactID, value.TestID, boolInt(value.Selected), value.ActorID, value.Reason, expires, value.CreatedAt.Format(timestampFormat))
	if err != nil {
		return value, err
	}
	details, _ := json.Marshal(value)
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{ActorID: value.ActorID, ActorRole: "administrator", Action: "test_impact.override", TargetType: "impact", TargetID: value.ImpactID, Details: details}); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (s *Store) ListTestImpactOverrides(ctx context.Context, projectID string, limit int) ([]intelligence.TestImpactOverride, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,impact_id,test_id,selected,actor_id,reason,expires_at,created_at FROM test_impact_overrides WHERE project_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []intelligence.TestImpactOverride{}
	for rows.Next() {
		var value intelligence.TestImpactOverride
		var selected int
		var expires sql.NullString
		var created string
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.ImpactID, &value.TestID, &selected, &value.ActorID, &value.Reason, &expires, &created); err != nil {
			return nil, err
		}
		value.Selected = selected != 0
		if expires.Valid {
			parsed, _ := time.Parse(timestampFormat, expires.String)
			value.ExpiresAt = &parsed
		}
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	var usage, existing int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(bytes),0) FROM cache_entries WHERE project_id=? AND cache_key<>?`, value.ProjectID, value.Key).Scan(&usage); err != nil {
		return value, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cache_entries WHERE project_id=? AND cache_key=?`, value.ProjectID, value.Key).Scan(&existing); err != nil {
		return value, err
	}
	if usage+value.Bytes > value.QuotaBytes {
		return intelligence.CacheEntry{}, storage.ErrBudgetExceeded
	}
	value.LastResult = "miss"
	value.ResultReason = "verified object registered"
	result, err := tx.ExecContext(ctx, `INSERT INTO cache_entries(project_id,cache_key,trust_domain,kind,input_sha256,object_sha256,bytes,verified,created_at,last_hit_at,expires_at,quota_bytes,last_result,result_reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id,cache_key) DO UPDATE SET last_hit_at=excluded.last_hit_at,expires_at=excluded.expires_at,quota_bytes=excluded.quota_bytes,last_result='hit',result_reason='complete input and object identity matched' WHERE cache_entries.object_sha256=excluded.object_sha256 AND cache_entries.input_sha256=excluded.input_sha256`, value.ProjectID, value.Key, value.TrustDomain, value.Kind, value.InputSHA256, value.ObjectSHA256, value.Bytes, boolInt(value.Verified), value.CreatedAt.Format(timestampFormat), value.LastHitAt.Format(timestampFormat), value.ExpiresAt.Format(timestampFormat), value.QuotaBytes, value.LastResult, value.ResultReason)
	if err != nil {
		return value, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return intelligence.CacheEntry{}, storage.ErrConflict
	}
	if existing != 0 {
		value.LastResult = "hit"
		value.ResultReason = "complete input and object identity matched"
	}
	if err := tx.Commit(); err != nil {
		return intelligence.CacheEntry{}, err
	}
	return value, nil
}
func (s *Store) ListCacheEntries(ctx context.Context, projectID string, limit int) ([]intelligence.CacheEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT cache_key,project_id,trust_domain,kind,input_sha256,object_sha256,bytes,verified,created_at,last_hit_at,expires_at,quota_bytes,last_result,result_reason FROM cache_entries WHERE project_id=? ORDER BY last_hit_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []intelligence.CacheEntry{}
	for rows.Next() {
		var value intelligence.CacheEntry
		var verified int
		var created, hit, expires string
		if err := rows.Scan(&value.Key, &value.ProjectID, &value.TrustDomain, &value.Kind, &value.InputSHA256, &value.ObjectSHA256, &value.Bytes, &verified, &created, &hit, &expires, &value.QuotaBytes, &value.LastResult, &value.ResultReason); err != nil {
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

func (s *Store) CacheUsage(ctx context.Context, projectID string) (int64, int, error) {
	var bytes int64
	var entries int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(bytes),0),COUNT(*) FROM cache_entries WHERE project_id=?`, projectID).Scan(&bytes, &entries)
	return bytes, entries, err
}

func (s *Store) VerifyCacheEntries(ctx context.Context, projectID, kind string) (intelligence.CacheVerificationReport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT cache_key,kind,input_sha256,object_sha256 FROM cache_entries WHERE project_id=? AND (?='' OR kind=?) ORDER BY kind,cache_key LIMIT 10000`, projectID, kind, kind)
	if err != nil {
		return intelligence.CacheVerificationReport{}, err
	}
	type expectedEntry struct{ key, kind, input, object string }
	expected := []expectedEntry{}
	for rows.Next() {
		var item expectedEntry
		if err := rows.Scan(&item.key, &item.kind, &item.input, &item.object); err != nil {
			rows.Close()
			return intelligence.CacheVerificationReport{}, err
		}
		expected = append(expected, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return intelligence.CacheVerificationReport{}, err
	}
	if err := rows.Close(); err != nil {
		return intelligence.CacheVerificationReport{}, err
	}
	actual := map[string]struct{ input, object string }{}
	blobs, err := s.db.QueryContext(ctx, `SELECT repository,blob_sha256,parser_id,analysis_json FROM code_intel_blobs WHERE project_id=? ORDER BY repository,blob_sha256,parser_id`, projectID)
	if err != nil {
		return intelligence.CacheVerificationReport{}, err
	}
	for blobs.Next() {
		var repository, blobSHA256, parserID, payload string
		if err := blobs.Scan(&repository, &blobSHA256, &parserID, &payload); err != nil {
			blobs.Close()
			return intelligence.CacheVerificationReport{}, err
		}
		var analysis intelligence.BlobAnalysis
		if json.Unmarshal([]byte(payload), &analysis) != nil {
			continue
		}
		key, input, object, _, identityErr := intelligence.ParseCacheIdentity(projectID, repository, blobSHA256, parserID, analysis)
		if identityErr == nil {
			actual[key] = struct{ input, object string }{input: input, object: object}
		}
	}
	if err := blobs.Err(); err != nil {
		blobs.Close()
		return intelligence.CacheVerificationReport{}, err
	}
	if err := blobs.Close(); err != nil {
		return intelligence.CacheVerificationReport{}, err
	}
	report := intelligence.CacheVerificationReport{ProjectID: projectID, Kind: kind, Items: []intelligence.CacheVerification{}}
	for _, entry := range expected {
		item := intelligence.CacheVerification{Key: entry.key, Kind: entry.kind, ExpectedObject: entry.object}
		if entry.kind != "source-parse" {
			item.Status, item.Reason = "unavailable", "this cache kind has no compiled object verifier"
			report.Unavailable++
		} else if observed, ok := actual[entry.key]; !ok {
			item.Status, item.Reason = "invalid", "the registered parsed-blob object is missing"
			report.Invalid++
		} else if observed.input != entry.input || observed.object != entry.object {
			item.Status, item.Reason, item.ObservedObject = "invalid", "input or serialized object integrity does not match", observed.object
			report.Invalid++
		} else {
			item.Status, item.Reason, item.ObservedObject = "verified", "complete input identity and serialized object hash match", observed.object
			report.Verified++
		}
		report.Items = append(report.Items, item)
	}
	return report, nil
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
