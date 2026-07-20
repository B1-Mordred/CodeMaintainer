package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/memory"
)

type memoryCandidateStore interface {
	PutCandidate(context.Context, memory.ProjectScope, memory.Record) (memory.Record, error)
	GetMemory(context.Context, memory.ProjectScope, string) (memory.Record, error)
}

func extractVerifiedCase(ctx context.Context, store memoryCandidateStore, job jobs.Job, artifactID string) error {
	scope, ok := jobMemoryScope(job)
	if !ok || artifactID == "" || job.ResultSHA == "" {
		return memory.ErrInvalid
	}
	content := fmt.Sprintf("Verified maintenance case for %s. Job %s produced result commit %s after %d QC repair cycle(s). Acceptance criteria hash: %s. The final controller report records exact verification and finding evidence.",
		job.Repository, job.ID, job.ResultSHA, job.ReviewCycle, job.AcceptanceCriteriaHash)
	return putWorkflowCandidate(ctx, store, scope, memory.Record{
		ID: candidateID("verified", job.ID, job.ResultSHA), Content: content, Kind: "verified_case",
		SourceURI: "artifact://" + artifactID + "/final-report", BaseCommit: job.BaseSHA,
	})
}

func extractFailedCase(ctx context.Context, store memoryCandidateStore, job jobs.Job, cause error) {
	scope, ok := jobMemoryScope(job)
	if !ok {
		return
	}
	sanitized := sanitizeMemoryCause(cause)
	content := fmt.Sprintf("Failed maintenance approach for %s. Job %s stopped in phase %s at durable version %d. Cause: %s. Consult the append-only job transition and phase evidence before retrying this approach.",
		job.Repository, job.ID, job.State, job.Version, sanitized)
	record := memory.Record{
		ID: candidateID("failed", job.ID, string(job.State), fmt.Sprint(job.Version)), Content: content,
		Kind: "failed_case", SourceURI: "job://" + job.ID + "/transitions", BaseCommit: job.BaseSHA,
	}
	if err := putWorkflowCandidate(ctx, store, scope, record); err == nil {
		return
	}
	record.Content = fmt.Sprintf("Failed maintenance approach for %s. Job %s stopped in phase %s at durable version %d. The cause remains in append-only job evidence and requires operator review before retry.",
		job.Repository, job.ID, job.State, job.Version)
	_ = putWorkflowCandidate(ctx, store, scope, record)
}

func putWorkflowCandidate(ctx context.Context, store memoryCandidateStore, scope memory.ProjectScope, candidate memory.Record) error {
	created, err := store.PutCandidate(ctx, scope, candidate)
	if err == nil {
		if created.Status != memory.StatusQuarantine || created.Verified {
			return memory.ErrInvalid
		}
		return nil
	}
	existing, getErr := store.GetMemory(ctx, scope, candidate.ID)
	if getErr == nil && existing.Content == candidate.Content && existing.Scope == scope {
		return nil
	}
	return err
}

func jobMemoryScope(job jobs.Job) (memory.ProjectScope, bool) {
	parts := strings.Split(job.Repository, "/")
	if len(parts) != 2 {
		return memory.ProjectScope{}, false
	}
	scope := memory.ProjectScope{Owner: parts[0], Repository: parts[1]}
	return scope, scope.Valid()
}

func candidateID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "memory_" + hex.EncodeToString(digest[:16])
}

func sanitizeMemoryCause(cause error) string {
	if cause == nil {
		return "unspecified phase failure"
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return "wall-time deadline exceeded"
	}
	value := strings.ToValidUTF8(cause.Error(), "?")
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 1024 {
		value = value[:1024]
	}
	if value == "" {
		return "unspecified phase failure"
	}
	return value
}
