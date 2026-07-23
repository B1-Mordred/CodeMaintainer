package jobs

import "testing"

func TestAllRequiredStatesAreUniqueAndValid(t *testing.T) {
	want := []State{
		StateQueued, StateSyncing, StatePreparingDependencies,
		StateCreatingWorktree, StateLockingAcceptanceCriteria,
		StateLoadingImplementationModel, StateReproducing,
		StateImplementing, StateVerifyingTargeted, StateVerifyingFull,
		StateLoadingTestDesignerModel, StateTestDesignReview,
		StateLoadingQCModel, StateQCReview, StateAwaitingRepair,
		StateRepairing, StateFinalVerification, StateAwaitingOperator,
		StatePublishingBranch, StateDraftPRCreated, StateCompleted,
		StateFailed, StateCancelled,
	}
	seen := make(map[State]bool, len(want))
	for _, state := range AllStates() {
		if seen[state] {
			t.Fatalf("duplicate state %q", state)
		}
		seen[state] = true
		if !state.Valid() {
			t.Fatalf("listed state %q is not valid", state)
		}
	}
	for _, state := range want {
		if !seen[state] {
			t.Errorf("required state %q is missing", state)
		}
	}
}

func TestHappyPathAndRepairPathTransitions(t *testing.T) {
	paths := [][]State{
		{
			StateQueued, StateSyncing, StateCreatingWorktree,
			StatePreparingDependencies, StateLockingAcceptanceCriteria,
			StateAwaitingTaskApproval, StateLoadingImplementationModel, StateReproducing,
			StateImplementing, StateVerifyingTargeted, StateVerifyingFull,
			StateLoadingTestDesignerModel, StateTestDesignReview,
			StateLoadingQCModel, StateQCReview, StateAwaitingOperator,
			StatePublishingBranch, StateDraftPRCreated, StateCompleted,
		},
		{
			StateQCReview, StateAwaitingRepair, StateRepairing,
			StateFinalVerification, StateLoadingQCModel, StateQCReview,
			StateAwaitingOperator,
		},
	}
	for _, path := range paths {
		for i := 0; i < len(path)-1; i++ {
			if err := ValidateTransition(path[i], path[i+1]); err != nil {
				t.Fatalf("valid path rejected: %v", err)
			}
		}
	}
}

func TestDangerousShortcutsAreRejected(t *testing.T) {
	for _, tc := range []struct{ from, to State }{
		{StateQueued, StateCompleted},
		{StateImplementing, StateAwaitingOperator},
		{StateLockingAcceptanceCriteria, StateLoadingImplementationModel},
		{StateQCReview, StatePublishingBranch},
		{StateAwaitingRepair, StateCompleted},
		{StateCompleted, StateQueued},
	} {
		if CanTransition(tc.from, tc.to) {
			t.Errorf("dangerous transition allowed: %s -> %s", tc.from, tc.to)
		}
	}
}
