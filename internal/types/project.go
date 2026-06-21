package types

import "time"

type ApprovalMode string

const (
	ApprovalAutonomous ApprovalMode = "autonomous"
	ApprovalPrompt     ApprovalMode = "prompt"
	ApprovalOnFailure  ApprovalMode = "on_failure"
)

func (a ApprovalMode) IsValid() bool {
	switch a {
	case ApprovalAutonomous, ApprovalPrompt, ApprovalOnFailure:
		return true
	}
	return false
}

type Project struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Description   string       `json:"description,omitempty"`
	RepoPath      string       `json:"repo_path"`
	DefaultBranch string       `json:"default_branch"`
	ApprovalMode  ApprovalMode `json:"approval_mode"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}
