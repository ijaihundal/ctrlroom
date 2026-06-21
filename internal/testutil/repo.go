package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TempRepo creates a temporary git repository with one initial commit on the
// "main" branch. Returns the absolute path. Auto-cleaned via t.Cleanup.
//
// Intended for Phase 2 tests of git operations and workspace worktrees.
func TempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-b", "main")
	mustGit(t, dir, "config", "user.email", "test@ctrlroom.local")
	mustGit(t, dir, "config", "user.name", "CtrlRoom Test")
	mustGit(t, dir, "config", "commit.gpgsign", "false")

	// Initial commit so HEAD exists and branches can be created from it.
	readme := filepath.Join(dir, "README.md")
	if err := writeFile(readme, "# test repo\n"); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	mustGit(t, dir, "add", "README.md")
	mustGit(t, dir, "commit", "-m", "initial")

	return dir
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
