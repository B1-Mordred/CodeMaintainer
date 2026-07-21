package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/B1-Mordred/CodeMaintainer/internal/audit"
	"github.com/B1-Mordred/CodeMaintainer/internal/gitbridge"
	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/memory"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) ApplyGitHubPullRequestEvent(ctx context.Context, event gitbridge.PullRequestEvent) (storage.GitHubDeliveryResult, error) {
	jobID, valid := strings.CutPrefix(event.Branch, "maintainer/")
	_, hashErr := hex.DecodeString(event.PayloadSHA256)
	if !valid || jobID == "" || event.DeliveryID == "" || event.Number <= 0 || event.Action != "closed" ||
		(event.Outcome != "merged" && event.Outcome != "rejected") || event.HeadSHA == "" || !memory.ValidCommit(event.HeadSHA) ||
		(event.Outcome == "merged" && (event.MergedCommit == "" || !memory.ValidCommit(event.MergedCommit))) ||
		(event.Outcome == "rejected" && event.MergedCommit != "") || len(event.PayloadSHA256) != 64 || hashErr != nil {
		return storage.GitHubDeliveryResult{}, storage.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storage.GitHubDeliveryResult{}, err
	}
	defer tx.Rollback()
	var existing storage.GitHubDeliveryResult
	var existingHash string
	err = tx.QueryRowContext(ctx, `SELECT delivery_id, outcome, affected_records, payload_sha256
		FROM github_deliveries WHERE delivery_id = ?`, event.DeliveryID).
		Scan(&existing.DeliveryID, &existing.Outcome, &existing.AffectedMemory, &existingHash)
	if err == nil {
		if existingHash != event.PayloadSHA256 || existing.Outcome != event.Outcome {
			return storage.GitHubDeliveryResult{}, storage.ErrConflict
		}
		existing.Replay = true
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storage.GitHubDeliveryResult{}, err
	}
	job, err := scanJob(tx.QueryRowContext(ctx, jobSelect+" WHERE id = ?", jobID))
	if err != nil {
		return storage.GitHubDeliveryResult{}, err
	}
	if job.Repository != event.Repository || job.ResultSHA != event.HeadSHA ||
		(job.State != jobs.StateCompleted && job.State != jobs.StateDraftPRCreated) {
		return storage.GitHubDeliveryResult{}, storage.ErrConflict
	}
	var defaultBranch string
	if err := tx.QueryRowContext(ctx, "SELECT default_branch FROM projects WHERE id = ? AND provider = 'github' AND repository = ? AND enabled = 1", job.ProjectID, event.Repository).Scan(&defaultBranch); err != nil {
		return storage.GitHubDeliveryResult{}, storage.ErrNotFound
	}
	if defaultBranch != event.BaseBranch {
		return storage.GitHubDeliveryResult{}, storage.ErrConflict
	}
	var publicationMatches int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_artifacts WHERE job_id = ? AND kind = 'publication'
		AND json_extract(metadata, '$.provider') = 'github' AND json_extract(metadata, '$.number') = ?
		AND json_extract(metadata, '$.branch') = ? AND json_extract(metadata, '$.result_sha') = ?`,
		job.ID, event.Number, event.Branch, event.HeadSHA).Scan(&publicationMatches); err != nil {
		return storage.GitHubDeliveryResult{}, err
	}
	if publicationMatches != 1 {
		return storage.GitHubDeliveryResult{}, storage.ErrConflict
	}
	parts := strings.Split(event.Repository, "/")
	if len(parts) != 2 {
		return storage.GitHubDeliveryResult{}, storage.ErrInvalid
	}
	scope := memory.ProjectScope{Owner: parts[0], Repository: parts[1]}
	rows, err := tx.QueryContext(ctx, memorySelect+` WHERE owner = ? AND repository = ? AND kind = 'verified_case'
		AND source_uri IN (SELECT 'artifact://' || id || '/final-report' FROM job_artifacts WHERE job_id = ? AND kind = 'final_report')`,
		scope.Owner, scope.Repository, job.ID)
	if err != nil {
		return storage.GitHubDeliveryResult{}, err
	}
	candidates, err := scanMemoryRows(rows)
	rows.Close()
	if err != nil {
		return storage.GitHubDeliveryResult{}, err
	}
	affected := 0
	for _, record := range candidates {
		changed, applyErr := s.applyPullOutcome(ctx, tx, event, record)
		if applyErr != nil {
			return storage.GitHubDeliveryResult{}, applyErr
		}
		if changed {
			affected++
		}
	}
	now := s.now()
	_, err = tx.ExecContext(ctx, `INSERT INTO github_deliveries(delivery_id, event, action, outcome, repository,
		pr_number, job_id, head_sha, merged_commit, payload_sha256, affected_records, created_at)
		VALUES(?, 'pull_request', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.DeliveryID, event.Action, event.Outcome,
		event.Repository, event.Number, job.ID, event.HeadSHA, event.MergedCommit, event.PayloadSHA256, affected, formatTime(now))
	if err != nil {
		return storage.GitHubDeliveryResult{}, fmt.Errorf("record GitHub delivery: %w", err)
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: "github-webhook", ActorRole: "service", Action: "github.pull_request." + event.Outcome,
		TargetType: "job", TargetID: job.ID, Details: mustJSONValue(map[string]any{"delivery_id": event.DeliveryID, "number": event.Number, "affected_memory": affected}),
	}); err != nil {
		return storage.GitHubDeliveryResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return storage.GitHubDeliveryResult{}, err
	}
	return storage.GitHubDeliveryResult{DeliveryID: event.DeliveryID, Outcome: event.Outcome, AffectedMemory: affected}, nil
}

func (s *Store) applyPullOutcome(ctx context.Context, tx *sql.Tx, event gitbridge.PullRequestEvent, record memory.Record) (bool, error) {
	if record.Status != memory.StatusQuarantine && record.Status != memory.StatusCanonical {
		return false, nil
	}
	now := s.now()
	action, rationale, nextStatus := "rejected", "authenticated pull request rejection marked the candidate stale", memory.StatusStale
	mergedCommit := record.MergedCommit
	if event.Outcome == "merged" {
		if record.Status == memory.StatusCanonical && record.MergedCommit == event.MergedCommit {
			return false, nil
		}
		action, rationale, nextStatus, mergedCommit = "promoted", "authenticated merged pull request promoted the verified candidate", memory.StatusCanonical, event.MergedCommit
	}
	result, err := tx.ExecContext(ctx, `UPDATE memory_records SET status = ?, merged_commit = ?, version = version + 1,
		updated_at = ? WHERE id = ? AND owner = ? AND repository = ? AND version = ?`, nextStatus, mergedCommit,
		formatTime(now), record.ID, record.Scope.Owner, record.Scope.Repository, record.Version)
	if err != nil {
		return false, err
	}
	if err := requireMemoryAffected(result); err != nil {
		return false, err
	}
	wasCanonical := record.Status == memory.StatusCanonical
	record.Status, record.MergedCommit, record.Version, record.UpdatedAt = nextStatus, mergedCommit, record.Version+1, now
	if wasCanonical && nextStatus != memory.StatusCanonical {
		if err := enqueueMemoryIndexOperation(ctx, tx, record, "forget", now); err != nil {
			return false, err
		}
	} else if nextStatus == memory.StatusCanonical {
		if err := enqueueMemoryIndexOperation(ctx, tx, record, "upsert", now); err != nil {
			return false, err
		}
	}
	details, _ := json.Marshal(map[string]any{"delivery_id": event.DeliveryID, "pull_request": event.Number, "basis": "merged_pr"})
	if err := appendMemoryEvent(ctx, tx, record, action, "github-webhook", rationale, details, now); err != nil {
		return false, err
	}
	if err := appendAuditTx(ctx, tx, s.now, audit.AppendRequest{
		ActorID: "github-webhook", ActorRole: "service", Action: "memory." + action,
		TargetType: "memory", TargetID: record.ID, Details: mustJSONValue(map[string]string{"rationale": rationale}),
	}); err != nil {
		return false, err
	}
	return true, nil
}

func mustJSONValue(value any) json.RawMessage {
	payload, _ := json.Marshal(value)
	return payload
}
