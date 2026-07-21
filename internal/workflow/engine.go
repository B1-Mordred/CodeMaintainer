package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

type Outcome struct {
	NeedsRepair bool                     `json:"needs_repair"`
	Details     json.RawMessage          `json:"details"`
	Metadata    storage.JobMetadataPatch `json:"-"`
}

type PhaseExecutor interface {
	Execute(context.Context, jobs.Job) (Outcome, error)
}

type Engine struct {
	store    storage.WorkflowStore
	executor PhaseExecutor
}

func New(store storage.WorkflowStore, executor PhaseExecutor) *Engine {
	return &Engine{store: store, executor: executor}
}

func (e *Engine) Step(ctx context.Context, jobID string) (jobs.Job, error) {
	job, err := e.store.GetJob(ctx, jobID)
	if err != nil {
		return jobs.Job{}, err
	}
	if !job.State.Resumable() {
		return job, fmt.Errorf("state %s is not automatically resumable", job.State)
	}
	if !job.DeadlineAt.IsZero() && !time.Now().UTC().Before(job.DeadlineAt) {
		return e.fail(ctx, job, storage.ErrBudgetExceeded, "job wall-time budget expired")
	}
	if tokens := phaseTokenReservation(job.State); tokens != 0 {
		reserved, reserveErr := e.store.ReserveJobTokens(ctx, job.ID, job.Version, job.State, tokens)
		if reserveErr != nil {
			return e.fail(ctx, job, reserveErr, "job token budget exhausted")
		}
		job = reserved
	}
	outcome, err := e.executor.Execute(ctx, job)
	if err != nil {
		return e.fail(ctx, job, err, "phase failed")
	}
	next, err := nextState(job.State, outcome)
	if err != nil {
		return job, err
	}
	recorded, marshalErr := json.Marshal(outcome)
	if marshalErr != nil {
		return job, marshalErr
	}
	return e.store.CompletePhase(ctx, storage.PhaseCompletion{
		JobID: job.ID, PhaseState: job.State, ExpectedVersion: job.Version,
		To: next, ActorID: "workflow-engine", Reason: "phase completed",
		Details: outcome.Details, Outcome: recorded, Metadata: outcome.Metadata,
	})
}

func (e *Engine) fail(ctx context.Context, job jobs.Job, cause error, reason string) (jobs.Job, error) {
	if !jobs.CanTransition(job.State, jobs.StateFailed) {
		return jobs.Job{}, cause
	}
	transitionContext := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		transitionContext, cancel = context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	}
	defer cancel()
	details, _ := json.Marshal(map[string]string{"error": cause.Error()})
	failed, transitionErr := e.store.TransitionJob(transitionContext, job.ID, jobs.TransitionRequest{
		To: jobs.StateFailed, ActorID: "workflow-engine", Reason: reason,
		ExpectedVersion: job.Version, Details: details,
	})
	if transitionErr == nil {
		extractFailedCase(transitionContext, e.store, failed, cause)
		return failed, cause
	}
	return jobs.Job{}, cause
}

func phaseTokenReservation(state jobs.State) int {
	switch state {
	case jobs.StateImplementing, jobs.StateQCReview, jobs.StateRepairing:
		return 16_384
	default:
		return 0
	}
}

func (e *Engine) Resume(ctx context.Context, limit int) ([]jobs.Job, error) {
	items, err := e.store.ListJobs(ctx, limit, 0)
	if err != nil {
		return nil, err
	}
	result := make([]jobs.Job, 0)
	for _, job := range items {
		if !job.State.Resumable() {
			continue
		}
		advanced, err := e.Step(ctx, job.ID)
		if err != nil && !errors.Is(err, context.Canceled) {
			return result, fmt.Errorf("resume job %s: %w", job.ID, err)
		}
		result = append(result, advanced)
	}
	return result, nil
}

func nextState(state jobs.State, outcome Outcome) (jobs.State, error) {
	if outcome.NeedsRepair {
		if jobs.CanTransition(state, jobs.StateAwaitingRepair) {
			return jobs.StateAwaitingRepair, nil
		}
		return "", fmt.Errorf("state %s cannot request repair", state)
	}
	next := map[jobs.State]jobs.State{
		jobs.StateQueued:                     jobs.StateSyncing,
		jobs.StateSyncing:                    jobs.StateCreatingWorktree,
		jobs.StateCreatingWorktree:           jobs.StatePreparingDependencies,
		jobs.StatePreparingDependencies:      jobs.StateLockingAcceptanceCriteria,
		jobs.StateLockingAcceptanceCriteria:  jobs.StateLoadingImplementationModel,
		jobs.StateLoadingImplementationModel: jobs.StateReproducing,
		jobs.StateReproducing:                jobs.StateImplementing,
		jobs.StateImplementing:               jobs.StateVerifyingTargeted,
		jobs.StateVerifyingTargeted:          jobs.StateVerifyingFull,
		jobs.StateVerifyingFull:              jobs.StateLoadingQCModel,
		jobs.StateLoadingQCModel:             jobs.StateQCReview,
		jobs.StateQCReview:                   jobs.StateAwaitingOperator,
		jobs.StateAwaitingRepair:             jobs.StateRepairing,
		jobs.StateRepairing:                  jobs.StateFinalVerification,
		jobs.StateFinalVerification:          jobs.StateLoadingQCModel,
		jobs.StatePublishingBranch:           jobs.StateDraftPRCreated,
		jobs.StateDraftPRCreated:             jobs.StateCompleted,
	}
	value, ok := next[state]
	if !ok {
		return "", fmt.Errorf("state %s has no automatic successor", state)
	}
	return value, nil
}

// FakeExecutor records no hidden reasoning and always returns a bounded,
// deterministic structured outcome.
type FakeExecutor struct{}

func (FakeExecutor) Execute(_ context.Context, _ jobs.Job) (Outcome, error) {
	return Outcome{Details: json.RawMessage(`{"adapter":"fake"}`)}, nil
}
