package workspace

import (
	"context"
	"crypto/sha1" //nolint:gosec // G505: sha1 used for non-cryptographic path slug, not security.
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/git"
	"github.com/ijaihundal/ctrlroom/internal/types"
)

const (
	// MaxAutoMergeAttempts caps how many times the merge is retried after the
	// agent attempts conflict resolution. Beyond this we surface conflict_stuck.
	MaxAutoMergeAttempts = 2

	branchPrefix = "ctrlroom/"
)

type Manager struct {
	db           *sql.DB
	git          *git.Client
	worktreeRoot string
	logger       *slog.Logger
}

func NewManager(database *sql.DB, gitc *git.Client, worktreeRoot string, logger *slog.Logger) *Manager {
	return &Manager{db: database, git: gitc, worktreeRoot: worktreeRoot, logger: logger}
}

// Prepare creates the worktree for a workspace. Transition: queued → preparing → idle|failed.
// Sets worktree_path, branch, target_ref on the workspace.
// On success, the workspace row is updated to status=idle.
// On failure, status=failed and the error is returned.
func (m *Manager) Prepare(ctx context.Context, wsID string) (*types.Workspace, error) {
	ws, err := db.GetWorkspace(ctx, m.db, wsID)
	if err != nil {
		return nil, mapWorkspaceErr(err)
	}
	proj, err := db.GetProject(ctx, m.db, ws.ProjectID)
	if err != nil {
		return nil, mapProjectErr(err)
	}

	if err := MustTransition(ws.Status, types.WorkspacePreparing); err != nil {
		return nil, err
	}
	if err := m.transition(ctx, ws, types.WorkspacePreparing); err != nil {
		return nil, err
	}

	worktreePath := m.worktreePathFor(proj.RepoPath, ws.ID)
	branch := branchName(ws.ID)
	targetRef := proj.DefaultBranch

	if err := m.git.WorktreeAdd(proj.RepoPath, branch, worktreePath, targetRef); err != nil {
		// Best-effort transition to failed; the worktree-add error is what we return.
		if tErr := m.transition(ctx, ws, types.WorkspaceFailed); tErr != nil {
			m.logger.WarnContext(ctx, "prepare: failed to transition to failed",
				"workspace_id", ws.ID, "err", tErr)
		}
		return nil, fmt.Errorf("worktree add: %w", err)
	}

	// WorkspaceUpdatePatch doesn't expose Branch; persist it via a scoped UPDATE
	// before flipping the status so callers reading the row back see both.
	if _, err := m.db.ExecContext(ctx,
		"UPDATE workspaces SET branch = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;",
		branch, ws.ID,
	); err != nil {
		return nil, fmt.Errorf("persist branch: %w", err)
	}

	idle := types.WorkspaceIdle
	updated, err := db.UpdateWorkspace(ctx, m.db, ws.ID, db.WorkspaceUpdatePatch{
		Status:       &idle,
		WorktreePath: &worktreePath,
		TargetRef:    &targetRef,
	})
	if err != nil {
		return nil, fmt.Errorf("persist prepared workspace: %w", err)
	}
	return updated, nil
}

// Cleanup removes the worktree. Used on workspace/project deletion.
// Idempotent: no error if the worktree is already gone.
func (m *Manager) Cleanup(ctx context.Context, wsID string) error {
	ws, err := db.GetWorkspace(ctx, m.db, wsID)
	if err != nil {
		return mapWorkspaceErr(err)
	}
	if ws.WorktreePath == "" {
		return nil
	}
	proj, err := db.GetProject(ctx, m.db, ws.ProjectID)
	if err != nil {
		return mapProjectErr(err)
	}
	if err := m.git.WorktreeRemove(proj.RepoPath, ws.WorktreePath, true); err != nil {
		return fmt.Errorf("worktree remove: %w", err)
	}
	return nil
}

// Cancel transitions a workspace to cancelled and cleans up its worktree.
// Valid from: queued, preparing, idle, completed, resolving_conflict.
// Not valid from terminal states (merged/failed/cancelled/conflict_stuck).
func (m *Manager) Cancel(ctx context.Context, wsID string) (*types.Workspace, error) {
	ws, err := db.GetWorkspace(ctx, m.db, wsID)
	if err != nil {
		return nil, mapWorkspaceErr(err)
	}
	if err := MustTransition(ws.Status, types.WorkspaceCancelled); err != nil {
		return nil, err
	}
	if ws.WorktreePath != "" {
		if cErr := m.Cleanup(ctx, wsID); cErr != nil {
			m.logger.WarnContext(ctx, "cancel: worktree cleanup failed",
				"workspace_id", wsID, "err", cErr)
		}
	}
	cancelled := types.WorkspaceCancelled
	updated, err := db.UpdateWorkspace(ctx, m.db, ws.ID, db.WorkspaceUpdatePatch{Status: &cancelled})
	if err != nil {
		return nil, fmt.Errorf("persist cancel: %w", err)
	}
	return updated, nil
}

type MergeResponse struct {
	Workspace *types.Workspace
	Merged    bool     // true on clean merge
	Conflicts []string // populated when !Merged
	Target    string   // target branch name
	Base      string   // merge-base SHA (for diagnostics)
}

// Merge attempts to merge the workspace's branch into its target.
//
// Phase 2 scope: implements the clean-merge and conflict-detection parts of D3.
//   - If clean: merges (git.UpdateBranch), transitions ws to merged, clears pending_merge.
//   - If conflict: sets ws.pending_merge={target, attempt, max}, returns Conflicts.
//
// Status transitions:
//   - ws must be in {completed, resolving_conflict} to call Merge.
//   - On clean merge: → merged.
//   - On conflict (first time): stays in current status, sets pending_merge.
//   - On conflict (attempt > max): → conflict_stuck, clears pending_merge.
func (m *Manager) Merge(ctx context.Context, wsID string) (*MergeResponse, error) {
	ws, err := db.GetWorkspace(ctx, m.db, wsID)
	if err != nil {
		return nil, mapWorkspaceErr(err)
	}
	if ws.Status != types.WorkspaceCompleted && ws.Status != types.WorkspaceResolvingConflict {
		return nil, fmt.Errorf("%w: merge requires completed or resolving_conflict, got %s",
			ErrInvalidTransition, ws.Status)
	}
	proj, err := db.GetProject(ctx, m.db, ws.ProjectID)
	if err != nil {
		return nil, mapProjectErr(err)
	}
	target := ws.TargetRef
	if target == "" {
		target = proj.DefaultBranch
	}

	result, err := m.git.MergeTree(proj.RepoPath, target, ws.Branch)
	if err != nil {
		return nil, fmt.Errorf("merge-tree: %w", err)
	}

	if result.Clean {
		if err := m.git.UpdateBranch(proj.RepoPath, target, result.MergedTreeSHA); err != nil {
			return nil, fmt.Errorf("update branch: %w", err)
		}
		merged := types.WorkspaceMerged
		updated, err := db.UpdateWorkspace(ctx, m.db, ws.ID, db.WorkspaceUpdatePatch{
			Status:            &merged,
			ClearPendingMerge: true,
		})
		if err != nil {
			return nil, fmt.Errorf("persist merged: %w", err)
		}
		return &MergeResponse{
			Workspace: updated,
			Merged:    true,
			Target:    target,
			Base:      result.Base,
		}, nil
	}

	// Conflict path.
	attempt := 1
	if ws.PendingMerge != nil {
		attempt = ws.PendingMerge.Attempt + 1
	}

	if attempt > MaxAutoMergeAttempts {
		if err := MustTransition(ws.Status, types.WorkspaceConflictStuck); err != nil {
			return nil, err
		}
		stuck := types.WorkspaceConflictStuck
		updated, err := db.UpdateWorkspace(ctx, m.db, ws.ID, db.WorkspaceUpdatePatch{
			Status:            &stuck,
			ClearPendingMerge: true,
		})
		if err != nil {
			return nil, fmt.Errorf("persist conflict_stuck: %w", err)
		}
		return &MergeResponse{
			Workspace: updated,
			Merged:    false,
			Conflicts: result.Conflicts,
			Target:    target,
			Base:      result.Base,
		}, nil
	}

	newPending := &types.PendingMerge{
		Target:      target,
		Attempt:     attempt,
		MaxAttempts: MaxAutoMergeAttempts,
	}
	updated, err := db.UpdateWorkspace(ctx, m.db, ws.ID, db.WorkspaceUpdatePatch{PendingMerge: newPending})
	if err != nil {
		return nil, fmt.Errorf("persist pending merge: %w", err)
	}
	return &MergeResponse{
		Workspace: updated,
		Merged:    false,
		Conflicts: result.Conflicts,
		Target:    target,
		Base:      result.Base,
	}, nil
}

// Diff returns the diff between the workspace's branch and its target ref.
// Requires the workspace to be prepared (worktree exists).
func (m *Manager) Diff(ctx context.Context, wsID string) (string, error) {
	ws, err := db.GetWorkspace(ctx, m.db, wsID)
	if err != nil {
		return "", mapWorkspaceErr(err)
	}
	if ws.WorktreePath == "" {
		return "", ErrNotPrepared
	}
	proj, err := db.GetProject(ctx, m.db, ws.ProjectID)
	if err != nil {
		return "", mapProjectErr(err)
	}
	out, err := m.git.Diff(proj.RepoPath, ws.TargetRef, ws.Branch)
	if err != nil {
		return "", fmt.Errorf("diff: %w", err)
	}
	return out, nil
}

// --- helpers ---

// repoSlug returns a 12-char hex sha1 of the absolute repo path, used to
// namespace worktree directories per repo. Not used for security.
//
//nolint:gosec // G401: sha1 used as a path slug, not a cryptographic MAC.
func repoSlug(repoPath string) string {
	sum := sha1.Sum([]byte(repoPath))
	return hex.EncodeToString(sum[:])[:12]
}

// branchName derives the git branch name for a workspace ID.
// Uses first 8 chars of the workspace ID.
func branchName(wsID string) string {
	short := wsID
	if len(short) > 8 {
		short = short[:8]
	}
	return branchPrefix + short
}

// worktreePathFor returns the on-disk path for a workspace's worktree.
func (m *Manager) worktreePathFor(repoPath, wsID string) string {
	return filepath.Join(m.worktreeRoot, repoSlug(repoPath), wsID)
}

// transition validates and persists a status change.
// Returns ErrInvalidTransition if not allowed.
func (m *Manager) transition(ctx context.Context, ws *types.Workspace, to types.WorkspaceStatus) error {
	if !CanTransition(ws.Status, to) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, ws.Status, to)
	}
	if _, err := db.UpdateWorkspace(ctx, m.db, ws.ID, db.WorkspaceUpdatePatch{Status: &to}); err != nil {
		return fmt.Errorf("persist transition: %w", err)
	}
	ws.Status = to
	return nil
}

func mapWorkspaceErr(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrWorkspaceNotFound, err)
	}
	return err
}

func mapProjectErr(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	return err
}
