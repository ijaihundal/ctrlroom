package workspace

import (
	"errors"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

func TestCanTransition_Allowed(t *testing.T) {
	t.Parallel()
	allowed := []struct {
		from, to types.WorkspaceStatus
	}{
		{types.WorkspaceQueued, types.WorkspacePreparing},
		{types.WorkspaceQueued, types.WorkspaceCancelled},
		{types.WorkspacePreparing, types.WorkspaceIdle},
		{types.WorkspacePreparing, types.WorkspaceFailed},
		{types.WorkspacePreparing, types.WorkspaceCancelled},
		{types.WorkspaceIdle, types.WorkspaceCompleted},
		{types.WorkspaceIdle, types.WorkspaceCancelled},
		{types.WorkspaceIdle, types.WorkspacePreparing},
		{types.WorkspaceCompleted, types.WorkspaceMerged},
		{types.WorkspaceCompleted, types.WorkspaceCancelled},
		{types.WorkspaceCompleted, types.WorkspaceResolvingConflict},
		{types.WorkspaceCompleted, types.WorkspaceConflictStuck},
		{types.WorkspaceResolvingConflict, types.WorkspaceCompleted},
		{types.WorkspaceResolvingConflict, types.WorkspaceConflictStuck},
		{types.WorkspaceResolvingConflict, types.WorkspaceCancelled},
		{types.WorkspaceResolvingConflict, types.WorkspaceMerged},
	}
	for _, tc := range allowed {
		if !CanTransition(tc.from, tc.to) {
			t.Errorf("CanTransition(%s, %s) = false, want true", tc.from, tc.to)
		}
	}
}

func TestCanTransition_Disallowed(t *testing.T) {
	t.Parallel()
	disallowed := []struct {
		from, to types.WorkspaceStatus
	}{
		// Terminal states have no transitions out.
		{types.WorkspaceMerged, types.WorkspaceRunning},
		{types.WorkspaceMerged, types.WorkspaceIdle},
		{types.WorkspaceFailed, types.WorkspaceIdle},
		{types.WorkspaceFailed, types.WorkspacePreparing},
		{types.WorkspaceCancelled, types.WorkspaceQueued},
		{types.WorkspaceCancelled, types.WorkspaceIdle},
		{types.WorkspaceConflictStuck, types.WorkspaceCompleted},
		{types.WorkspaceConflictStuck, types.WorkspaceMerged},
		// Wrong direction.
		{types.WorkspaceIdle, types.WorkspaceQueued},
		{types.WorkspacePreparing, types.WorkspaceQueued},
		{types.WorkspaceCompleted, types.WorkspaceIdle},
		{types.WorkspaceMerged, types.WorkspaceResolvingConflict},
		// Skipped states.
		{types.WorkspaceQueued, types.WorkspaceIdle},
		{types.WorkspaceQueued, types.WorkspaceCompleted},
		{types.WorkspaceQueued, types.WorkspaceMerged},
		{types.WorkspaceIdle, types.WorkspaceMerged},
		// Phase 3 states (not yet supported in Phase 2).
		{types.WorkspaceIdle, types.WorkspaceRunning},
		{types.WorkspaceRunning, types.WorkspaceCompleted},
		{types.WorkspacePreparing, types.WorkspaceRunning},
		// Same-state is a no-op, not a transition.
		{types.WorkspaceIdle, types.WorkspaceIdle},
		{types.WorkspaceQueued, types.WorkspaceQueued},
		{types.WorkspaceMerged, types.WorkspaceMerged},
	}
	for _, tc := range disallowed {
		if CanTransition(tc.from, tc.to) {
			t.Errorf("CanTransition(%s, %s) = true, want false", tc.from, tc.to)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()
	terminal := []types.WorkspaceStatus{
		types.WorkspaceMerged,
		types.WorkspaceFailed,
		types.WorkspaceCancelled,
		types.WorkspaceConflictStuck,
	}
	for _, s := range terminal {
		if !IsTerminal(s) {
			t.Errorf("IsTerminal(%s) = false, want true", s)
		}
	}
	nonTerminal := []types.WorkspaceStatus{
		types.WorkspaceQueued,
		types.WorkspacePreparing,
		types.WorkspaceIdle,
		types.WorkspaceRunning,
		types.WorkspaceAwaitingInput,
		types.WorkspaceAwaitingApproval,
		types.WorkspaceResolvingConflict,
		types.WorkspaceCompleted,
	}
	for _, s := range nonTerminal {
		if IsTerminal(s) {
			t.Errorf("IsTerminal(%s) = true, want false", s)
		}
	}
}

func TestMustTransition(t *testing.T) {
	t.Parallel()
	if err := MustTransition(types.WorkspaceQueued, types.WorkspacePreparing); err != nil {
		t.Errorf("allowed transition returned err: %v", err)
	}
	err := MustTransition(types.WorkspaceMerged, types.WorkspaceRunning)
	if err == nil {
		t.Fatal("disallowed transition returned nil err")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("err = %v, want errors.Is ErrInvalidTransition", err)
	}
}
