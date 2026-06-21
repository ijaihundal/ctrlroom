package types

import "time"

type AgentType string

const (
	AgentClaude   AgentType = "claude"
	AgentCodex    AgentType = "codex"
	AgentOpenCode AgentType = "opencode"
)

func (a AgentType) IsValid() bool {
	switch a {
	case AgentClaude, AgentCodex, AgentOpenCode:
		return true
	}
	return false
}

type WorkspaceStatus string

const (
	WorkspaceQueued            WorkspaceStatus = "queued"
	WorkspacePreparing         WorkspaceStatus = "preparing"
	WorkspaceIdle              WorkspaceStatus = "idle"
	WorkspaceRunning           WorkspaceStatus = "running"
	WorkspaceAwaitingInput     WorkspaceStatus = "awaiting_input"
	WorkspaceAwaitingApproval  WorkspaceStatus = "awaiting_approval"
	WorkspaceResolvingConflict WorkspaceStatus = "resolving_conflict"
	WorkspaceConflictStuck     WorkspaceStatus = "conflict_stuck"
	WorkspaceCompleted         WorkspaceStatus = "completed"
	WorkspaceMerged            WorkspaceStatus = "merged"
	WorkspaceFailed            WorkspaceStatus = "failed"
	WorkspaceCancelled         WorkspaceStatus = "cancelled"
)

func (s WorkspaceStatus) IsValid() bool {
	switch s {
	case WorkspaceQueued, WorkspacePreparing, WorkspaceIdle, WorkspaceRunning,
		WorkspaceAwaitingInput, WorkspaceAwaitingApproval, WorkspaceResolvingConflict,
		WorkspaceConflictStuck, WorkspaceCompleted, WorkspaceMerged,
		WorkspaceFailed, WorkspaceCancelled:
		return true
	}
	return false
}

func (s WorkspaceStatus) IsTerminal() bool {
	switch s {
	case WorkspaceMerged, WorkspaceFailed, WorkspaceCancelled:
		return true
	}
	return false
}

type PendingMerge struct {
	Target      string `json:"target"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"max_attempts"`
}

type Workspace struct {
	ID           string          `json:"id"`
	IssueID      *string         `json:"issue_id,omitempty"`
	ProjectID    string          `json:"project_id"`
	Branch       string          `json:"branch"`
	AgentType    AgentType       `json:"agent_type"`
	Model        string          `json:"model,omitempty"`
	Status       WorkspaceStatus `json:"status"`
	Prompt       string          `json:"prompt,omitempty"`
	WorktreePath string          `json:"worktree_path,omitempty"`
	TargetRef    string          `json:"target_ref,omitempty"`
	PendingMerge *PendingMerge   `json:"pending_merge,omitempty"`
	Orchestrator bool            `json:"orchestrator"`
	TokensIn     int             `json:"tokens_in"`
	TokensOut    int             `json:"tokens_out"`
	CostUSD      float64         `json:"cost_usd"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}
