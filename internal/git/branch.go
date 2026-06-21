package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultBranch detects the default branch of the repo at repoPath.
// Resolution order:
//  1. git symbolic-ref --short refs/remotes/origin/HEAD (strip "origin/" prefix)
//  2. git rev-parse --abbrev-ref HEAD (current branch of checked-out repo)
//  3. git config init.defaultBranch
//  4. fallback "main"
func (c *Client) DefaultBranch(repoPath string) (string, error) {
	if out, err := c.run(repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		s := strings.TrimSpace(strings.TrimPrefix(string(out), "origin/"))
		if s != "" {
			return s, nil
		}
	}
	if out, err := c.run(repoPath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		s := strings.TrimSpace(string(out))
		if s != "" && s != "HEAD" {
			return s, nil
		}
	}
	if out, err := c.run(repoPath, "config", "init.defaultBranch"); err == nil {
		s := strings.TrimSpace(string(out))
		if s != "" {
			return s, nil
		}
	}
	return "main", nil
}

// BranchExists reports whether a local branch with the given name exists.
func (c *Client) BranchExists(repoPath, branch string) (bool, error) {
	_, err := c.run(repoPath, "rev-parse", "--verify", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	return false, nil
}

// RevParse resolves a ref to a 40-char commit SHA.
func (c *Client) RevParse(repoPath, ref string) (string, error) {
	out, err := c.run(repoPath, "rev-parse", ref)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidRef, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// WorktreeAdd creates a new worktree at worktreePath on a new branch `branch`
// based on baseRef. Equivalent to:
//
//	git -C <repoPath> worktree add -b <branch> <worktreePath> <baseRef>
//
// Returns:
//
//	ErrBranchExists   if branch already exists
//	ErrWorktreeExists if worktreePath is already a worktree or non-empty dir
//	ErrInvalidRef     if baseRef doesn't resolve
func (c *Client) WorktreeAdd(repoPath, branch, worktreePath, baseRef string) error {
	exists, _ := c.BranchExists(repoPath, branch)
	if exists {
		return fmt.Errorf("%w: %s", ErrBranchExists, branch)
	}
	if info, err := os.Stat(worktreePath); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%w: %s is not a directory", ErrWorktreeExists, worktreePath)
		}
		entries, err := os.ReadDir(worktreePath)
		if err != nil {
			return fmt.Errorf("read worktree dir: %w", err)
		}
		if len(entries) > 0 {
			return fmt.Errorf("%w: %s is not empty", ErrWorktreeExists, worktreePath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("mkdir parent of worktree: %w", err)
	}

	_, err := c.run(repoPath, "worktree", "add", "-b", branch, worktreePath, baseRef)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "already exists"):
			return fmt.Errorf("%w: %s", ErrBranchExists, branch)
		case strings.Contains(msg, "already used") || strings.Contains(msg, "is not empty"):
			return fmt.Errorf("%w: %s", ErrWorktreeExists, worktreePath)
		case strings.Contains(msg, "invalid reference") || strings.Contains(msg, "unknown revision"):
			return fmt.Errorf("%w: %s", ErrInvalidRef, baseRef)
		}
		return fmt.Errorf("worktree add: %w", err)
	}
	return nil
}

// WorktreeRemove removes a worktree. If force is true, removes even with
// untracked / modified files. Idempotent: no error if the worktree is already gone.
func (c *Client) WorktreeRemove(repoPath, worktreePath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	_, err := c.run(repoPath, args...)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not a working tree") ||
			strings.Contains(msg, "not a worktree") ||
			strings.Contains(msg, "does not exist") {
			return nil
		}
		return fmt.Errorf("worktree remove: %w", err)
	}
	return nil
}

// IsRepo reports whether repoPath contains a git repository (bare or working).
func (c *Client) IsRepo(repoPath string) bool {
	_, err := c.run(repoPath, "rev-parse", "--git-dir")
	return err == nil
}
