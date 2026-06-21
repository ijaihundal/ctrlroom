package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/testutil"
)

func TestNewFindsGitInPath(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	if c.bin == "" {
		t.Error("New returned Client with empty bin path")
	}
	if _, err := os.Stat(c.bin); err != nil {
		t.Errorf("bin path %q not accessible: %v", c.bin, err)
	}
}

func TestNewWithExplicitPath(t *testing.T) {
	const bin = "/usr/bin/git"
	c := NewWithPath(bin)
	if c.bin != bin {
		t.Errorf("bin = %q, want %q", c.bin, bin)
	}
	repo := testutil.TempRepo(t)
	got, err := c.RevParse(repo, "HEAD")
	if err != nil {
		t.Fatalf("RevParse via NewWithPath client: %v", err)
	}
	if len(got) != 40 {
		t.Errorf("HEAD SHA length = %d, want 40", len(got))
	}
}

func TestVersion(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	v, err := c.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if !strings.HasPrefix(v, "git version ") {
		t.Errorf("Version() = %q, want prefix %q", v, "git version ")
	}
}

func TestIsRepoTrueForRealRepo(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	repo := testutil.TempRepo(t)
	if !c.IsRepo(repo) {
		t.Errorf("IsRepo(%q) = false, want true", repo)
	}
}

func TestIsRepoFalseForEmptyDir(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	dir := t.TempDir()
	if c.IsRepo(dir) {
		t.Errorf("IsRepo(%q) = true, want false", dir)
	}
}

func TestIsRepoFalseForNonexistentPath(t *testing.T) {
	c := NewWithPath("/usr/bin/git")
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if c.IsRepo(missing) {
		t.Errorf("IsRepo(%q) = true, want false", missing)
	}
}
