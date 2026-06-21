package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/testutil"
)

func commitFile(t *testing.T, repo, path, content, msg string) {
	t.Helper()
	fullPath := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", fullPath, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
	runGitInRepo(t, repo, "add", path)
	runGitInRepo(t, repo, "commit", "-m", msg)
}

func checkout(t *testing.T, repo, branch string) {
	t.Helper()
	runGitInRepo(t, repo, "checkout", branch)
}

func TestMergeTreeFastForward(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	commitFile(t, repo, "a.txt", "A\n", "add a")
	commitFile(t, repo, "b.txt", "B\n", "add b")
	runGitInRepo(t, repo, "checkout", "-b", "feat")
	commitFile(t, repo, "c.txt", "C\n", "add c on feat")

	res, err := c.MergeTree(repo, "main", "feat")
	if err != nil {
		t.Fatalf("MergeTree: %v", err)
	}
	if !res.Clean {
		t.Errorf("Clean = false, want true; conflicts=%v", res.Conflicts)
	}
	if res.MergedTreeSHA == "" {
		t.Error("MergedTreeSHA empty")
	}
	if len(res.Conflicts) != 0 {
		t.Errorf("Conflicts = %v, want empty", res.Conflicts)
	}

	branchTree, err := c.run(repo, "rev-parse", "feat^{tree}")
	if err != nil {
		t.Fatalf("rev-parse feat tree: %v", err)
	}
	gotTree := strings.TrimSpace(string(branchTree))
	if res.MergedTreeSHA != gotTree {
		t.Errorf("MergedTreeSHA = %q, want %q (FF should equal branch tree)", res.MergedTreeSHA, gotTree)
	}
}

func TestMergeTreeCleanThreeWay(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	commitFile(t, repo, "base.txt", "base\n", "base commit")

	runGitInRepo(t, repo, "checkout", "-b", "feat")
	commitFile(t, repo, "file_b.txt", "B\n", "add file_b on feat")
	checkout(t, repo, "main")
	commitFile(t, repo, "file_a.txt", "A\n", "add file_a on main")

	res, err := c.MergeTree(repo, "main", "feat")
	if err != nil {
		t.Fatalf("MergeTree: %v", err)
	}
	if !res.Clean {
		t.Errorf("Clean = false, want true; conflicts=%v", res.Conflicts)
	}
	if res.MergedTreeSHA == "" {
		t.Error("MergedTreeSHA empty on clean 3-way merge")
	}
	if len(res.Conflicts) != 0 {
		t.Errorf("Conflicts = %v, want empty", res.Conflicts)
	}
}

func TestMergeTreeConflict(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	commitFile(t, repo, "file.txt", "line1\n", "initial file")

	runGitInRepo(t, repo, "checkout", "-b", "feat")
	commitFile(t, repo, "file.txt", "line1 changed on feat\n", "modify on feat")
	checkout(t, repo, "main")
	commitFile(t, repo, "file.txt", "line1 changed on main\n", "modify on main")

	res, err := c.MergeTree(repo, "main", "feat")
	if err != nil {
		t.Fatalf("MergeTree: %v", err)
	}
	if res.Clean {
		t.Fatal("Clean = true, want false (expected conflict)")
	}
	found := false
	for _, p := range res.Conflicts {
		if p == "file.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("Conflicts = %v, want to contain file.txt", res.Conflicts)
	}
}

func TestMergeTreeIdenticalAddNoConflict(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	runGitInRepo(t, repo, "checkout", "-b", "feat")
	commitFile(t, repo, "same.txt", "identical\n", "add same on feat")
	checkout(t, repo, "main")
	commitFile(t, repo, "same.txt", "identical\n", "add same on main")

	res, err := c.MergeTree(repo, "main", "feat")
	if err != nil {
		t.Fatalf("MergeTree: %v", err)
	}
	if !res.Clean {
		t.Errorf("Clean = false, want true (identical add); conflicts=%v", res.Conflicts)
	}
}

func TestMergeTreeInvalidRef(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	_, err := c.MergeTree(repo, "no-such-target", "main")
	if err == nil {
		t.Fatal("MergeTree(invalid target): expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidRef) {
		t.Errorf("err = %v, want errors.Is ErrInvalidRef", err)
	}

	_, err = c.MergeTree(repo, "main", "no-such-branch")
	if err == nil {
		t.Fatal("MergeTree(invalid branch): expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidRef) {
		t.Errorf("err = %v, want errors.Is ErrInvalidRef", err)
	}
}

func TestUpdateBranchAdvancesRef(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	beforeSHA, err := c.RevParse(repo, "main")
	if err != nil {
		t.Fatalf("RevParse main before: %v", err)
	}

	commitFile(t, repo, "extra.txt", "extra\n", "extra commit")
	runGitInRepo(t, repo, "checkout", "-b", "feat")

	res, err := c.MergeTree(repo, "main", "feat")
	if err != nil {
		t.Fatalf("MergeTree: %v", err)
	}
	if !res.Clean {
		t.Fatalf("expected clean merge, got conflicts=%v", res.Conflicts)
	}

	if err := c.UpdateBranch(repo, "main", res.MergedTreeSHA); err != nil {
		t.Fatalf("UpdateBranch: %v", err)
	}

	afterSHA, err := c.RevParse(repo, "main")
	if err != nil {
		t.Fatalf("RevParse main after: %v", err)
	}
	if afterSHA == beforeSHA {
		t.Error("main SHA unchanged after UpdateBranch")
	}
}
