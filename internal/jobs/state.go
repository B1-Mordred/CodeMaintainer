package jobs

import "fmt"

// State is a durable controller-owned workflow phase. String values form part
// of the API and database contract and must change only through a migration.
type State string

const (
	StateQueued                        State = "queued"
	StateSyncing                       State = "syncing"
	StatePreparingDependencies         State = "preparing_dependencies"
	StateCreatingWorktree              State = "creating_worktree"
	StateLockingAcceptanceCriteria     State = "locking_acceptance_criteria"
	StateAwaitingTaskApproval          State = "awaiting_task_approval"
	StateLoadingImplementationModel    State = "loading_implementation_model"
	StateReproducing                   State = "reproducing"
	StateImplementing                  State = "implementing"
	StateVerifyingTargeted             State = "verifying_targeted"
	StateVerifyingFull                 State = "verifying_full"
	StateLoadingTestDesignerModel      State = "loading_test_designer_model"
	StateTestDesignReview              State = "test_design_review"
	StateAwaitingTestDesignDisposition State = "awaiting_test_design_disposition"
	StateGoldenRehearsalReview         State = "golden_rehearsal_review"
	StateLoadingDocumentationModel     State = "loading_documentation_model"
	StateDocumentationReview           State = "documentation_review"
	StatePolicyReview                  State = "policy_review"
	StateLoadingQCModel                State = "loading_qc_model"
	StateQCReview                      State = "qc_review"
	StateAwaitingRepair                State = "awaiting_repair"
	StateRepairing                     State = "repairing"
	StateFinalVerification             State = "final_verification"
	StateAwaitingOperator              State = "awaiting_operator"
	StatePublishingBranch              State = "publishing_branch"
	StateDraftPRCreated                State = "draft_pr_created"
	StateCompleted                     State = "completed"
	StateFailed                        State = "failed"
	StateCancelled                     State = "cancelled"
)

var allStates = []State{
	StateQueued,
	StateSyncing,
	StatePreparingDependencies,
	StateCreatingWorktree,
	StateLockingAcceptanceCriteria,
	StateAwaitingTaskApproval,
	StateLoadingImplementationModel,
	StateReproducing,
	StateImplementing,
	StateVerifyingTargeted,
	StateVerifyingFull,
	StateLoadingTestDesignerModel,
	StateTestDesignReview,
	StateAwaitingTestDesignDisposition,
	StateGoldenRehearsalReview,
	StateLoadingDocumentationModel,
	StateDocumentationReview,
	StatePolicyReview,
	StateLoadingQCModel,
	StateQCReview,
	StateAwaitingRepair,
	StateRepairing,
	StateFinalVerification,
	StateAwaitingOperator,
	StatePublishingBranch,
	StateDraftPRCreated,
	StateCompleted,
	StateFailed,
	StateCancelled,
}

var transitions = map[State]map[State]struct{}{
	StateQueued:                        set(StateSyncing, StateCancelled, StateFailed),
	StateSyncing:                       set(StateCreatingWorktree, StateCancelled, StateFailed),
	StateCreatingWorktree:              set(StatePreparingDependencies, StateCancelled, StateFailed),
	StatePreparingDependencies:         set(StateLockingAcceptanceCriteria, StateCancelled, StateFailed),
	StateLockingAcceptanceCriteria:     set(StateAwaitingTaskApproval, StateCancelled, StateFailed),
	StateAwaitingTaskApproval:          set(StateLoadingImplementationModel, StateCancelled, StateFailed),
	StateLoadingImplementationModel:    set(StateReproducing, StateCancelled, StateFailed),
	StateReproducing:                   set(StateImplementing, StateCancelled, StateFailed),
	StateImplementing:                  set(StateVerifyingTargeted, StateCancelled, StateFailed),
	StateVerifyingTargeted:             set(StateVerifyingFull, StateAwaitingRepair, StateCancelled, StateFailed),
	StateVerifyingFull:                 set(StateLoadingTestDesignerModel, StateGoldenRehearsalReview, StateLoadingDocumentationModel, StateLoadingQCModel, StateAwaitingRepair, StateCancelled, StateFailed),
	StateLoadingTestDesignerModel:      set(StateTestDesignReview, StateCancelled, StateFailed),
	StateTestDesignReview:              set(StateAwaitingTestDesignDisposition, StateGoldenRehearsalReview, StateLoadingDocumentationModel, StateLoadingQCModel, StateCancelled, StateFailed),
	StateAwaitingTestDesignDisposition: set(StateGoldenRehearsalReview, StateLoadingDocumentationModel, StateLoadingQCModel, StateCancelled, StateFailed),
	StateGoldenRehearsalReview:         set(StateLoadingDocumentationModel, StateLoadingQCModel, StateCancelled, StateFailed),
	StateLoadingDocumentationModel:     set(StateDocumentationReview, StateCancelled, StateFailed),
	StateDocumentationReview:           set(StatePolicyReview, StateLoadingQCModel, StateCancelled, StateFailed),
	StatePolicyReview:                  set(StateLoadingQCModel, StateCancelled, StateFailed),
	StateLoadingQCModel:                set(StateQCReview, StateCancelled, StateFailed),
	StateQCReview:                      set(StateAwaitingRepair, StateAwaitingOperator, StateCancelled, StateFailed),
	StateAwaitingRepair:                set(StateRepairing, StateAwaitingOperator, StateCancelled, StateFailed),
	StateRepairing:                     set(StateFinalVerification, StateCancelled, StateFailed),
	StateFinalVerification:             set(StateLoadingDocumentationModel, StatePolicyReview, StateLoadingQCModel, StateAwaitingRepair, StateCancelled, StateFailed),
	StateAwaitingOperator:              set(StatePublishingBranch, StateCompleted, StateCancelled, StateFailed),
	StatePublishingBranch:              set(StateDraftPRCreated, StateAwaitingOperator, StateFailed),
	StateDraftPRCreated:                set(StateCompleted, StateFailed),
	StateFailed:                        set(StateQueued),
	StateCancelled:                     set(StateQueued),
}

func set(states ...State) map[State]struct{} {
	result := make(map[State]struct{}, len(states))
	for _, state := range states {
		result[state] = struct{}{}
	}
	return result
}

// AllStates returns a defensive copy in stable workflow order.
func AllStates() []State {
	result := make([]State, len(allStates))
	copy(result, allStates)
	return result
}

func (s State) Valid() bool {
	for _, candidate := range allStates {
		if s == candidate {
			return true
		}
	}
	return false
}

func (s State) Terminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}

func (s State) Resumable() bool {
	return s.Valid() && !s.Terminal() && s != StateAwaitingOperator && s != StateAwaitingTaskApproval && s != StateAwaitingTestDesignDisposition
}

func CanTransition(from, to State) bool {
	_, ok := transitions[from][to]
	return ok
}

func ValidateTransition(from, to State) error {
	if !from.Valid() {
		return fmt.Errorf("unknown source state %q", from)
	}
	if !to.Valid() {
		return fmt.Errorf("unknown destination state %q", to)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("transition %s -> %s is not allowed", from, to)
	}
	return nil
}
