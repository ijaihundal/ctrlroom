package git

import "errors"

var (
	ErrGitNotInstalled = errors.New("git binary not found in PATH")
	ErrNotARepo        = errors.New("not a git repository")
	ErrBranchExists    = errors.New("branch already exists")
	ErrBranchNotFound  = errors.New("branch not found")
	ErrWorktreeExists  = errors.New("worktree path already in use")
	ErrWorktreeMissing = errors.New("worktree not found")
	ErrMergeConflict   = errors.New("merge conflict")
	ErrInvalidRef      = errors.New("invalid git ref")
)
