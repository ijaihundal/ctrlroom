package workspace

import "errors"

var (
	ErrInvalidTransition = errors.New("invalid workspace state transition")
	ErrNotPrepared       = errors.New("workspace worktree not prepared")
	ErrAlreadyMerged     = errors.New("workspace already merged")
	ErrConflictStuck     = errors.New("merge conflicts could not be resolved within max attempts")
	ErrNoMergePending    = errors.New("no merge pending for this workspace")
	ErrProjectNotFound   = errors.New("project not found")
	ErrWorkspaceNotFound = errors.New("workspace not found")
)
