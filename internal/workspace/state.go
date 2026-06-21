package workspace

import (
	"fmt"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// allowedTransitions is the workspace state machine (Phase 2+3 combined).
// Keys are the source status; values are the set of statuses reachable in one
// step. Terminal states (merged, failed, cancelled, conflict_stuck) have no
// outgoing entries and are enforced by IsTerminal.
//
// Phase 3 additions: idle→running, running→awaiting_input/cancelled/failed,
// awaiting_input→running/completed/cancelled.
var allowedTransitions = map[types.WorkspaceStatus]map[types.WorkspaceStatus]bool{
	types.WorkspaceQueued: {
		types.WorkspacePreparing: true,
		types.WorkspaceCancelled: true,
	},
	types.WorkspacePreparing: {
		types.WorkspaceIdle:      true,
		types.WorkspaceFailed:    true,
		types.WorkspaceCancelled: true,
	},
	types.WorkspaceIdle: {
		types.WorkspaceCompleted: true,
		types.WorkspaceCancelled: true,
		types.WorkspacePreparing: true,
		types.WorkspaceRunning:   true,
	},
	types.WorkspaceRunning: {
		types.WorkspaceAwaitingInput:     true,
		types.WorkspaceFailed:            true,
		types.WorkspaceCancelled:         true,
		types.WorkspaceResolvingConflict: true,
	},
	types.WorkspaceAwaitingInput: {
		types.WorkspaceRunning:   true,
		types.WorkspaceCompleted: true,
		types.WorkspaceCancelled: true,
	},
	types.WorkspaceCompleted: {
		types.WorkspaceMerged:            true,
		types.WorkspaceCancelled:         true,
		types.WorkspaceResolvingConflict: true,
		types.WorkspaceConflictStuck:     true,
	},
	types.WorkspaceResolvingConflict: {
		types.WorkspaceCompleted:     true,
		types.WorkspaceConflictStuck: true,
		types.WorkspaceCancelled:     true,
		types.WorkspaceMerged:        true,
	},
}

// CanTransition reports whether transitioning from `from` to `to` is allowed
// under the workspace state machine.
func CanTransition(from, to types.WorkspaceStatus) bool {
	if from == to {
		return false
	}
	targets, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	return targets[to]
}

// IsTerminal reports whether the status is a terminal state (no transitions
// out except deletion).
func IsTerminal(s types.WorkspaceStatus) bool {
	switch s {
	case types.WorkspaceMerged,
		types.WorkspaceFailed,
		types.WorkspaceCancelled,
		types.WorkspaceConflictStuck:
		return true
	}
	return false
}

// MustTransition returns ErrInvalidTransition if transitioning from `from` to
// `to` is not allowed.
func MustTransition(from, to types.WorkspaceStatus) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
	}
	return nil
}
