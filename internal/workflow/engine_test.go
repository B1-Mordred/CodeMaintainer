package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/jobs"
	"github.com/B1-Mordred/CodeMaintainer/internal/memory"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	storesqlite "github.com/B1-Mordred/CodeMaintainer/internal/storage/sqlite"
)

func TestRestartAdvancesEveryAutomaticallyResumablePhase(t *testing.T) {
	paths := [][]jobs.State{
		{
			jobs.StateQueued, jobs.StateSyncing, jobs.StateCreatingWorktree,
			jobs.StatePreparingDependencies, jobs.StateLockingAcceptanceCriteria,
			jobs.StateAwaitingTaskApproval, jobs.StateLoadingImplementationModel, jobs.StateReproducing,
			jobs.StateImplementing, jobs.StateVerifyingTargeted, jobs.StateVerifyingFull,
			jobs.StateLoadingTestDesignerModel, jobs.StateTestDesignReview,
			jobs.StateLoadingQCModel, jobs.StateQCReview,
		},
		{
			jobs.StateQueued, jobs.StateSyncing, jobs.StateCreatingWorktree,
			jobs.StatePreparingDependencies, jobs.StateLockingAcceptanceCriteria,
			jobs.StateAwaitingTaskApproval, jobs.StateLoadingImplementationModel, jobs.StateReproducing,
			jobs.StateImplementing, jobs.StateVerifyingTargeted, jobs.StateVerifyingFull,
			jobs.StateLoadingTestDesignerModel, jobs.StateTestDesignReview,
			jobs.StateLoadingQCModel, jobs.StateQCReview, jobs.StateAwaitingRepair,
			jobs.StateRepairing, jobs.StateFinalVerification,
		},
		{
			jobs.StateQueued, jobs.StateSyncing, jobs.StateCreatingWorktree,
			jobs.StatePreparingDependencies, jobs.StateLockingAcceptanceCriteria,
			jobs.StateAwaitingTaskApproval, jobs.StateLoadingImplementationModel, jobs.StateReproducing,
			jobs.StateImplementing, jobs.StateVerifyingTargeted, jobs.StateVerifyingFull,
			jobs.StateLoadingTestDesignerModel, jobs.StateTestDesignReview,
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
	records, err := store.ListMemory(ctx, memory.ProjectScope{Owner: "o", Repository: "r"}, memory.StatusQuarantine, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("failure memory records = %#v, %v", records, err)
	}
	if records[0].Kind != "failed_case" || records[0].Verified || !strings.Contains(records[0].Content, "phase failed") {
		t.Fatalf("failure memory record = %#v", records[0])
	}
	extractFailedCase(ctx, store, failed, errors.New("fixture failure"))
	records, err = store.ListMemory(ctx, memory.ProjectScope{Owner: "o", Repository: "r"}, memory.StatusQuarantine, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("idempotent failure memory records = %#v, %v", records, err)
	}
}

func TestModelPhaseCannotExceedDurableTokenBudget(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ProjectID: "p", Repository: "o/r", Task: "task", ActorID: "test", MaxTokens: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []jobs.State{
		jobs.StateSyncing, jobs.StateCreatingWorktree, jobs.StatePreparingDependencies,
		jobs.StateLockingAcceptanceCriteria, jobs.StateAwaitingTaskApproval, jobs.StateLoadingImplementationModel,
		jobs.StateReproducing, jobs.StateImplementing,
	} {
		job, err = store.TransitionJob(ctx, job.ID, jobs.TransitionRequest{To: state, ActorID: "test", Reason: "seed", ExpectedVersion: job.Version})
		if err != nil {
			t.Fatal(err)
		}
	}
	failed, err := New(store, FakeExecutor{}).Step(ctx, job.ID)
	if !errors.Is(err, storage.ErrBudgetExceeded) || failed.State != jobs.StateFailed {
		t.Fatalf("budget result = %+v, %v", failed, err)
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

func TestOutcomeCanRouteFullVerificationThroughTestDesigner(t *testing.T) {
	next, err := nextState(jobs.StateVerifyingFull, Outcome{NextState: jobs.StateLoadingTestDesignerModel})
	if err != nil || next != jobs.StateLoadingTestDesignerModel {
		t.Fatalf("test-designer route = %s, %v", next, err)
	}
	if _, err := nextState(jobs.StateQueued, Outcome{NextState: jobs.StateLoadingTestDesignerModel}); err == nil {
		t.Fatal("invalid explicit route bypassed transition policy")
	}
}

type metadataExecutor struct{}

func (metadataExecutor) Execute(_ context.Context, _ jobs.Job) (Outcome, error) {
	base := "0123456789abcdef0123456789abcdef01234567"
	criteria := json.RawMessage(`[{"id":"AC-1","statement":"fixture passes","verification_method":"full_tests"}]`)
	digest := sha256.Sum256(criteria)
	hash := hex.EncodeToString(digest[:])
	return Outcome{Details: json.RawMessage(`{"adapter":"fixture"}`), Metadata: storage.JobMetadataPatch{
		BaseSHA: &base, AcceptanceCriteria: &criteria, AcceptanceCriteriaHash: &hash,
	}}, nil
}

func TestStepAtomicallyPersistsMetadataPhaseRecordAndTransition(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_phase", ProjectID: "project", Repository: "owner/repo", Task: "task", ActorID: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := New(store, metadataExecutor{}).Step(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.State != jobs.StateSyncing || advanced.BaseSHA == "" || advanced.AcceptanceCriteriaHash == "" {
		t.Fatalf("phase metadata was not persisted: %#v", advanced)
	}
	records, err := store.ListPhaseRecords(ctx, job.ID, 10)
	if err != nil || len(records) != 1 || records[0].PhaseState != jobs.StateQueued || records[0].PhaseVersion != 1 {
		t.Fatalf("phase records = %#v, %v", records, err)
	}
	if _, err := store.CompletePhase(ctx, storage.PhaseCompletion{
		JobID: job.ID, PhaseState: jobs.StateQueued, ExpectedVersion: 1, To: jobs.StateSyncing,
		Outcome: json.RawMessage(`{}`),
	}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("repeated phase completion returned %v", err)
	}
}
