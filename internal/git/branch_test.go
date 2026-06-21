package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/testutil"
)

func runGitInRepo(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, repo, err, out)
	}
}

func TestDefaultBranchMain(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	got, err := c.DefaultBranch(repo)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if got != "main" {
		t.Errorf("DefaultBranch = %q, want main", got)
	}
}

func TestBranchExistsTrueForMain(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	exists, err := c.BranchExists(repo, "main")
	if err != nil {
		t.Fatalf("BranchExists(main): %v", err)
	}
	if !exists {
		t.Error("BranchExists(main) = false, want true")
	}
}

func TestBranchExistsFalseForUnknown(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	exists, err := c.BranchExists(repo, "definitely-not-a-branch")
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if exists {
		t.Error("BranchExists(unknown) = true, want false")
	}
}

func TestRevParseHeadSHA(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	sha, err := c.RevParse(repo, "HEAD")
	if err != nil {
		t.Fatalf("RevParse(HEAD): %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("HEAD SHA length = %d, want 40 (got %q)", len(sha), sha)
	}
	for _, ch := range sha {
		isHex := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')
		if !isHex {
			t.Errorf("HEAD SHA contains non-hex char %q in %q", ch, sha)
			break
		}
	}
}

func TestRevParseInvalidRef(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	_, err := c.RevParse(repo, "not-a-real-ref")
	if err == nil {
		t.Fatal("RevParse(invalid): expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidRef) {
		t.Errorf("RevParse(invalid) err = %v, want errors.Is ErrInvalidRef", err)
	}
}

func TestWorktreeAddSuccess(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	wtPath := filepath.Join(t.TempDir(), "wt")
	const branch = "feature-x"

	if err := c.WorktreeAdd(repo, branch, wtPath, "main"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	info, err := os.Stat(wtPath)
	if err != nil {
		t.Fatalf("Stat worktree: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("worktree path %q is not a directory", wtPath)
	}
	gitMarker := filepath.Join(wtPath, ".git")
	if _, err := os.Stat(gitMarker); err != nil {
		t.Errorf(".git marker missing in worktree: %v", err)
	}
	exists, _ := c.BranchExists(repo, branch)
	if !exists {
		t.Errorf("branch %q not created by WorktreeAdd", branch)
	}
}

func TestWorktreeAddExistingBranch(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	runGitInRepo(t, repo, "branch", "already-here")

	wtPath := filepath.Join(t.TempDir(), "wt")
	err := c.WorktreeAdd(repo, "already-here", wtPath, "main")
	if err == nil {
		t.Fatal("WorktreeAdd with existing branch: expected error, got nil")
	}
	if !errors.Is(err, ErrBranchExists) {
		t.Errorf("err = %v, want errors.Is ErrBranchExists", err)
	}
}

func TestWorktreeAddNonEmptyPath(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "junk"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := c.WorktreeAdd(repo, "new-branch", wtPath, "main")
	if err == nil {
		t.Fatal("WorktreeAdd to non-empty path: expected error, got nil")
	}
	if !errors.Is(err, ErrWorktreeExists) {
		t.Errorf("err = %v, want errors.Is ErrWorktreeExists", err)
	}
}

func TestWorktreeAddInvalidBaseRef(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	wtPath := filepath.Join(t.TempDir(), "wt")

	err := c.WorktreeAdd(repo, "feature-bad", wtPath, "no-such-ref")
	if err == nil {
		t.Fatal("WorktreeAdd with invalid baseRef: expected error, got nil")
	}
}

func TestWorktreeRemoveAndIdempotent(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := c.WorktreeAdd(repo, "feature-rm", wtPath, "main"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	if err := c.WorktreeRemove(repo, wtPath, false); err != nil {
		t.Fatalf("WorktreeRemove first call: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("after remove, Stat worktree: %v, want IsNotExist", err)
	}

	if err := c.WorktreeRemove(repo, wtPath, false); err != nil {
		t.Errorf("WorktreeRemove second call (idempotent): %v", err)
	}
}

func TestWorktreeRemoveForceWithUntrackedChanges(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := c.WorktreeAdd(repo, "feature-force", wtPath, "main"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "untracked.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := c.WorktreeRemove(repo, wtPath, false); err == nil {
		t.Error("WorktreeRemove(force=false) on dirty worktree: expected error, got nil")
	}
	if err := c.WorktreeRemove(repo, wtPath, true); err != nil {
		t.Errorf("WorktreeRemove(force=true): %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("after force remove, Stat worktree: %v, want IsNotExist", err)
	}
}

func TestWorktreeRemoveNeverWorktree(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	never := filepath.Join(t.TempDir(), "never-a-worktree")
	if err := os.MkdirAll(never, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.WorktreeRemove(repo, never, false); err != nil {
		t.Errorf("WorktreeRemove on non-worktree: %v, want nil (idempotent)", err)
	}
}
