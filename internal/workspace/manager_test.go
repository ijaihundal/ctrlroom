package workspace_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/git"
	"github.com/ijaihundal/ctrlroom/internal/testutil"
	"github.com/ijaihundal/ctrlroom/internal/types"
	"github.com/ijaihundal/ctrlroom/internal/workspace"
)

type fixture struct {
	db        *sql.DB
	repo      string
	gitc      *git.Client
	mgr       *workspace.Manager
	project   *types.Project
	workspace *types.Workspace
}

func setupFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Apply(context.Background(), database); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	repo := testutil.TempRepo(t)
	gitc, err := git.New()
	if err != nil {
		t.Fatalf("git new: %v", err)
	}

	wtRoot := t.TempDir()
	mgr := workspace.NewManager(database, gitc, wtRoot, slog.Default())

	def, err := gitc.DefaultBranch(repo)
	if err != nil {
		t.Fatalf("default branch: %v", err)
	}

	project, err := db.CreateProject(context.Background(), database, db.ProjectCreateParams{
		Name: "test", RepoPath: repo, DefaultBranch: def,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	ws, err := db.CreateWorkspace(context.Background(), database, db.WorkspaceCreateParams{
		ProjectID: project.ID, AgentType: types.AgentClaude, Status: types.WorkspaceQueued,
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	return &fixture{db: database, repo: repo, gitc: gitc, mgr: mgr, project: project, workspace: ws}
}

// runGit runs git with cmd.Dir = dir. Test-only helper for fixture setup.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func TestPrepare_HappyPath(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	ws, err := f.mgr.Prepare(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if ws.Status != types.WorkspaceIdle {
		t.Errorf("Status = %q, want idle", ws.Status)
	}
	if ws.WorktreePath == "" {
		t.Fatal("WorktreePath empty after Prepare")
	}
	info, err := os.Stat(ws.WorktreePath)
	if err != nil {
		t.Fatalf("Stat worktree: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("worktree path %q is not a directory", ws.WorktreePath)
	}
	exists, err := f.gitc.BranchExists(f.repo, ws.Branch)
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if !exists {
		t.Errorf("branch %q not in repo", ws.Branch)
	}
	if ws.TargetRef != f.project.DefaultBranch {
		t.Errorf("TargetRef = %q, want %q", ws.TargetRef, f.project.DefaultBranch)
	}
}

func TestPrepare_FromTerminalStatus(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	merged := types.WorkspaceMerged
	if _, err := db.UpdateWorkspace(ctx, f.db, f.workspace.ID, db.WorkspaceUpdatePatch{Status: &merged}); err != nil {
		t.Fatalf("set merged: %v", err)
	}
	_, err := f.mgr.Prepare(ctx, f.workspace.ID)
	if !errors.Is(err, workspace.ErrInvalidTransition) {
		t.Errorf("Prepare from merged: err = %v, want ErrInvalidTransition", err)
	}
}

func TestCleanup_Idempotent(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	ws, err := f.mgr.Prepare(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	wtPath := ws.WorktreePath
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree not created: %v", err)
	}
	if err := f.mgr.Cleanup(ctx, ws.ID); err != nil {
		t.Fatalf("Cleanup 1: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree after Cleanup 1: %v, want IsNotExist", err)
	}
	if err := f.mgr.Cleanup(ctx, ws.ID); err != nil {
		t.Errorf("Cleanup 2 (idempotent): %v", err)
	}
}

func TestCleanup_NoWorktreePath(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	if err := f.mgr.Cleanup(ctx, f.workspace.ID); err != nil {
		t.Errorf("Cleanup with no worktree_path: %v", err)
	}
}

func TestCancel_FromQueued(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	ws, err := f.mgr.Cancel(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if ws.Status != types.WorkspaceCancelled {
		t.Errorf("Status = %q, want cancelled", ws.Status)
	}
}

func TestCancel_FromIdleRemovesWorktree(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	ws, err := f.mgr.Prepare(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	wtPath := ws.WorktreePath

	ws, err = f.mgr.Cancel(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if ws.Status != types.WorkspaceCancelled {
		t.Errorf("Status = %q, want cancelled", ws.Status)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree after cancel: %v, want IsNotExist", err)
	}
}

func TestCancel_FromTerminal(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	merged := types.WorkspaceMerged
	if _, err := db.UpdateWorkspace(ctx, f.db, f.workspace.ID, db.WorkspaceUpdatePatch{Status: &merged}); err != nil {
		t.Fatalf("set merged: %v", err)
	}
	_, err := f.mgr.Cancel(ctx, f.workspace.ID)
	if !errors.Is(err, workspace.ErrInvalidTransition) {
		t.Errorf("Cancel from merged: err = %v, want ErrInvalidTransition", err)
	}
}

func TestDiff_NotPrepared(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	_, err := f.mgr.Diff(ctx, f.workspace.ID)
	if !errors.Is(err, workspace.ErrNotPrepared) {
		t.Errorf("Diff before Prepare: err = %v, want ErrNotPrepared", err)
	}
}

func TestDiff_AfterPrepareEmpty(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	if _, err := f.mgr.Prepare(ctx, f.workspace.ID); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	diff, err := f.mgr.Diff(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff != "" {
		t.Errorf("Diff with no commits = %q, want empty", diff)
	}
}

func TestDiff_AfterCommitNonEmpty(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	ws, err := f.mgr.Prepare(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	newFile := filepath.Join(ws.WorktreePath, "feature.txt")
	if err := os.WriteFile(newFile, []byte("feature\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, ws.WorktreePath, "add", "feature.txt")
	runGit(t, ws.WorktreePath, "commit", "-m", "feature")

	diff, err := f.mgr.Diff(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff == "" {
		t.Error("Diff after commit is empty, want non-empty")
	}
	if !strings.Contains(diff, "feature.txt") {
		t.Errorf("Diff does not mention feature.txt:\n%s", diff)
	}
}

func TestMerge_Clean(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	targetBefore, err := f.gitc.RevParse(f.repo, f.project.DefaultBranch)
	if err != nil {
		t.Fatalf("rev-parse before: %v", err)
	}

	ws, err := f.mgr.Prepare(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	newFile := filepath.Join(ws.WorktreePath, "feature.txt")
	if err := os.WriteFile(newFile, []byte("feature\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, ws.WorktreePath, "add", "feature.txt")
	runGit(t, ws.WorktreePath, "commit", "-m", "feature")

	completed := types.WorkspaceCompleted
	if _, err := db.UpdateWorkspace(ctx, f.db, ws.ID, db.WorkspaceUpdatePatch{Status: &completed}); err != nil {
		t.Fatalf("set completed: %v", err)
	}

	resp, err := f.mgr.Merge(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !resp.Merged {
		t.Errorf("Merged = false, want true; conflicts=%v", resp.Conflicts)
	}
	if resp.Workspace.Status != types.WorkspaceMerged {
		t.Errorf("Status = %q, want merged", resp.Workspace.Status)
	}
	if resp.Workspace.PendingMerge != nil {
		t.Errorf("PendingMerge = %v, want nil", resp.Workspace.PendingMerge)
	}

	targetAfter, err := f.gitc.RevParse(f.repo, f.project.DefaultBranch)
	if err != nil {
		t.Fatalf("rev-parse after: %v", err)
	}
	if targetAfter == targetBefore {
		t.Error("target SHA unchanged after merge")
	}
}

// setupConflictedBranch prepares the workspace, then makes conflicting commits
// on both the workspace branch (README = "# branch change") and the target
// branch (README = "# main change"). Returns the workspace after Prepare and
// marks the workspace completed.
func setupConflictedBranch(t *testing.T, f *fixture) *types.Workspace {
	t.Helper()
	ctx := context.Background()

	ws, err := f.mgr.Prepare(ctx, f.workspace.ID)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	readme := filepath.Join(ws.WorktreePath, "README.md")
	if err := os.WriteFile(readme, []byte("# branch change\n"), 0o600); err != nil {
		t.Fatalf("write branch: %v", err)
	}
	runGit(t, ws.WorktreePath, "add", "README.md")
	runGit(t, ws.WorktreePath, "commit", "-m", "branch modifies README")

	mainReadme := filepath.Join(f.repo, "README.md")
	if err := os.WriteFile(mainReadme, []byte("# main change\n"), 0o600); err != nil {
		t.Fatalf("write main: %v", err)
	}
	runGit(t, f.repo, "add", "README.md")
	runGit(t, f.repo, "commit", "-m", "main modifies README")

	completed := types.WorkspaceCompleted
	if _, err := db.UpdateWorkspace(ctx, f.db, ws.ID, db.WorkspaceUpdatePatch{Status: &completed}); err != nil {
		t.Fatalf("set completed: %v", err)
	}
	return ws
}

// assertPendingAttempt fails the test if the workspace's pending_merge does not
// have the expected attempt counter.
func assertPendingAttempt(t *testing.T, ws *types.Workspace, want int) {
	t.Helper()
	if ws.PendingMerge == nil {
		t.Fatalf("PendingMerge nil, want Attempt=%d", want)
	}
	if ws.PendingMerge.Attempt != want {
		t.Errorf("PendingMerge.Attempt = %d, want %d", ws.PendingMerge.Attempt, want)
	}
}

func TestMerge_Conflict(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()
	ws := setupConflictedBranch(t, f)

	resp, err := f.mgr.Merge(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if resp.Merged {
		t.Fatal("Merged = true, want false (expected conflict)")
	}
	found := false
	for _, p := range resp.Conflicts {
		if p == "README.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("Conflicts = %v, want to contain README.md", resp.Conflicts)
	}
	assertPendingAttempt(t, resp.Workspace, 1)
	if resp.Workspace.PendingMerge.Target != f.project.DefaultBranch {
		t.Errorf("PendingMerge.Target = %q, want %q",
			resp.Workspace.PendingMerge.Target, f.project.DefaultBranch)
	}
	if resp.Workspace.Status != types.WorkspaceCompleted {
		t.Errorf("Status = %q, want completed (unchanged after first conflict)",
			resp.Workspace.Status)
	}
}

func TestMerge_InvalidStatus(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	// Workspace is queued; Merge requires completed or resolving_conflict.
	_, err := f.mgr.Merge(ctx, f.workspace.ID)
	if !errors.Is(err, workspace.ErrInvalidTransition) {
		t.Errorf("Merge from queued: err = %v, want ErrInvalidTransition", err)
	}
}

func TestMerge_MultiCallReachesConflictStuck(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()
	ws := setupConflictedBranch(t, f)

	// Call 1: attempt = 1.
	resp, err := f.mgr.Merge(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Merge 1: %v", err)
	}
	if resp.Merged {
		t.Fatal("Merge 1: Merged = true, want false")
	}
	assertPendingAttempt(t, resp.Workspace, 1)

	// Call 2: attempt = 2.
	resp, err = f.mgr.Merge(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Merge 2: %v", err)
	}
	if resp.Merged {
		t.Fatal("Merge 2: Merged = true, want false")
	}
	assertPendingAttempt(t, resp.Workspace, 2)

	// Call 3: attempt = 3 > max → conflict_stuck, pending cleared.
	resp, err = f.mgr.Merge(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Merge 3: %v", err)
	}
	if resp.Merged {
		t.Fatal("Merge 3: Merged = true, want false")
	}
	if resp.Workspace.Status != types.WorkspaceConflictStuck {
		t.Errorf("Merge 3: Status = %q, want conflict_stuck", resp.Workspace.Status)
	}
	if resp.Workspace.PendingMerge != nil {
		t.Errorf("Merge 3: PendingMerge = %v, want nil (cleared)", resp.Workspace.PendingMerge)
	}
}

func TestMerge_MissingWorkspace(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	_, err := f.mgr.Merge(ctx, "no-such-id")
	if !errors.Is(err, workspace.ErrWorkspaceNotFound) {
		t.Errorf("Merge missing: err = %v, want ErrWorkspaceNotFound", err)
	}
}

func TestPrepare_MissingWorkspace(t *testing.T) {
	t.Parallel()
	f := setupFixture(t)
	ctx := context.Background()

	_, err := f.mgr.Prepare(ctx, "no-such-id")
	if !errors.Is(err, workspace.ErrWorkspaceNotFound) {
		t.Errorf("Prepare missing: err = %v, want ErrWorkspaceNotFound", err)
	}
}
