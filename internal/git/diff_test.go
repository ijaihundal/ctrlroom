package git

import (
	"strings"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/testutil"
)

func TestDiffEmpty(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	got, err := c.Diff(repo, "main", "main")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if got != "" {
		t.Errorf("Diff identical refs = %q, want empty", got)
	}
}

func TestDiffShowsAddedFile(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	runGitInRepo(t, repo, "checkout", "-b", "feat")
	commitFile(t, repo, "newfile.txt", "new content\n", "add newfile")

	got, err := c.Diff(repo, "main", "feat")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(got, "newfile.txt") {
		t.Errorf("diff does not mention newfile.txt:\n%s", got)
	}
	if !strings.Contains(got, "+new content") {
		t.Errorf("diff missing added line:\n%s", got)
	}
}

func TestDiffStatSingleFile(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)

	runGitInRepo(t, repo, "checkout", "-b", "feat")
	commitFile(t, repo, "stats.txt", "a\nb\nc\nd\n", "add stats")

	stats, err := c.DiffStat(repo, "main", "feat")
	if err != nil {
		t.Fatalf("DiffStat: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("len(stats) = %d, want 1", len(stats))
	}
	s := stats[0]
	if s.Path != "stats.txt" {
		t.Errorf("Path = %q, want stats.txt", s.Path)
	}
	if s.Added != 4 {
		t.Errorf("Added = %d, want 4", s.Added)
	}
	if s.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0", s.Deleted)
	}
}
