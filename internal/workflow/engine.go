package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

type Outcome struct {
	NeedsRepair bool            `json:"needs_repair"`
	Details     json.RawMessage `json:"details"`
}

type PhaseExecutor interface {
	Execute(context.Context, jobs.Job) (Outcome, error)
}

type Engine struct {
	store    storage.JobStore
	executor PhaseExecutor
}

func New(store storage.JobStore, executor PhaseExecutor) *Engine {
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
	outcome, err := e.executor.Execute(ctx, job)
	if err != nil {
		if jobs.CanTransition(job.State, jobs.StateFailed) {
			details, _ := json.Marshal(map[string]string{"error": err.Error()})
			failed, transitionErr := e.store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
				To: jobs.StateFailed, ActorID: "workflow-engine", Reason: "phase failed",
				ExpectedVersion: job.Version, Details: details,
			})
			if transitionErr == nil {
				return failed, err
			}
		}
		return jobs.Job{}, err
	}
	next, err := nextState(job.State, outcome)
	if err != nil {
		return job, err
	}
	return e.store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
		To: next, ActorID: "workflow-engine", Reason: "phase completed",
		ExpectedVersion: job.Version, Details: outcome.Details,
	})
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
		jobs.StateSyncing:                    jobs.StatePreparingDependencies,
		jobs.StatePreparingDependencies:      jobs.StateCreatingWorktree,
		jobs.StateCreatingWorktree:           jobs.StateLockingAcceptanceCriteria,
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
