package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/local-code-maintainer/appliance/internal/jobs"
	"github.com/local-code-maintainer/appliance/internal/storage"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
)

func TestRestartAdvancesEveryAutomaticallyResumablePhase(t *testing.T) {
	paths := [][]jobs.State{
		{
			jobs.StateQueued, jobs.StateSyncing, jobs.StatePreparingDependencies,
			jobs.StateCreatingWorktree, jobs.StateLockingAcceptanceCriteria,
			jobs.StateLoadingImplementationModel, jobs.StateReproducing,
			jobs.StateImplementing, jobs.StateVerifyingTargeted, jobs.StateVerifyingFull,
			jobs.StateLoadingQCModel, jobs.StateQCReview,
		},
		{
			jobs.StateQueued, jobs.StateSyncing, jobs.StatePreparingDependencies,
			jobs.StateCreatingWorktree, jobs.StateLockingAcceptanceCriteria,
			jobs.StateLoadingImplementationModel, jobs.StateReproducing,
			jobs.StateImplementing, jobs.StateVerifyingTargeted, jobs.StateVerifyingFull,
			jobs.StateLoadingQCModel, jobs.StateQCReview, jobs.StateAwaitingRepair,
			jobs.StateRepairing, jobs.StateFinalVerification,
		},
		{
			jobs.StateQueued, jobs.StateSyncing, jobs.StatePreparingDependencies,
			jobs.StateCreatingWorktree, jobs.StateLockingAcceptanceCriteria,
			jobs.StateLoadingImplementationModel, jobs.StateReproducing,
			jobs.StateImplementing, jobs.StateVerifyingTargeted, jobs.StateVerifyingFull,
			jobs.StateLoadingQCModel, jobs.StateQCReview, jobs.StateAwaitingOperator,
			jobs.StatePublishingBranch, jobs.StateDraftPRCreated,
		},
	}

	tested := make(map[jobs.State]bool)
	for _, path := range paths {
		for index, state := range path {
			if tested[state] || !state.Resumable() {
				continue
			}
			tested[state] = true
			t.Run(string(state), func(t *testing.T) {
				ctx := context.Background()
				database := filepath.Join(t.TempDir(), "controller.db")
				store, err := storesqlite.Open(ctx, database)
				if err != nil {
					t.Fatal(err)
				}
				job, err := store.CreateJob(ctx, storage.CreateJobParams{
					ProjectID: "project", Repository: "owner/repo", Task: "task", ActorID: "test",
				})
				if err != nil {
					t.Fatal(err)
				}
				for pathIndex := 1; pathIndex <= index; pathIndex++ {
					job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{
						To: path[pathIndex], ActorID: "test", Reason: "seed phase", ExpectedVersion: job.Version,
					})
					if err != nil {
						t.Fatalf("seed %s: %v", path[pathIndex], err)
					}
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}

				reopened, err := storesqlite.Open(ctx, database)
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				advanced, err := New(reopened, FakeExecutor{}).Step(ctx, job.ID)
				if err != nil {
					t.Fatal(err)
				}
				expected, err := nextState(state, Outcome{})
				if err != nil {
					t.Fatal(err)
				}
				if advanced.State != expected {
					t.Fatalf("restart advanced %s to %s, want %s", state, advanced.State, expected)
				}
			})
		}
	}

	for _, state := range jobs.AllStates() {
		if state.Resumable() && !tested[state] {
			t.Errorf("resumable state %s lacks restart coverage", state)
		}
	}
}

type failingExecutor struct{}

func (failingExecutor) Execute(context.Context, jobs.Job) (Outcome, error) {
	return Outcome{}, errors.New("fixture failure")
}

func TestPhaseFailurePersistsFailedState(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{ProjectID: "p", Repository: "o/r", Task: "task", ActorID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := New(store, failingExecutor{}).Step(ctx, job.ID)
	if err == nil {
		t.Fatal("expected executor failure")
	}
	if failed.State != jobs.StateFailed {
		t.Fatalf("failure state is %s", failed.State)
	}
}

func TestRepairOutcomeCannotBypassTransitionPolicy(t *testing.T) {
	if _, err := nextState(jobs.StateQueued, Outcome{NeedsRepair: true}); err == nil {
		t.Fatal("queued job was allowed to request repair")
	}
	if next, err := nextState(jobs.StateQCReview, Outcome{NeedsRepair: true}); err != nil || next != jobs.StateAwaitingRepair {
		t.Fatalf("QC repair outcome = %s, %v", next, err)
	}
}
