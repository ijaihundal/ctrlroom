package types

import "testing"

func TestApprovalMode_IsValid(t *testing.T) {
	t.Parallel()
	valid := []ApprovalMode{
		ApprovalAutonomous,
		ApprovalPrompt,
		ApprovalOnFailure,
	}
	invalid := []ApprovalMode{
		"",
		"bogus",
	}
	for _, v := range valid {
		if !v.IsValid() {
			t.Errorf("ApprovalMode(%q).IsValid() = false, want true", v)
		}
	}
	for _, v := range invalid {
		if v.IsValid() {
			t.Errorf("ApprovalMode(%q).IsValid() = true, want false", v)
		}
	}
}

func TestIssueStatus_IsValid(t *testing.T) {
	t.Parallel()
	valid := []IssueStatus{
		IssueTodo,
		IssueInProgress,
		IssueReview,
		IssueDone,
	}
	invalid := []IssueStatus{
		"",
		"bogus",
	}
	for _, v := range valid {
		if !v.IsValid() {
			t.Errorf("IssueStatus(%q).IsValid() = false, want true", v)
		}
	}
	for _, v := range invalid {
		if v.IsValid() {
			t.Errorf("IssueStatus(%q).IsValid() = true, want false", v)
		}
	}
}

func TestAgentType_IsValid(t *testing.T) {
	t.Parallel()
	valid := []AgentType{
		AgentClaude,
		AgentCodex,
		AgentOpenCode,
	}
	invalid := []AgentType{
		"",
		"bogus",
	}
	for _, v := range valid {
		if !v.IsValid() {
			t.Errorf("AgentType(%q).IsValid() = false, want true", v)
		}
	}
	for _, v := range invalid {
		if v.IsValid() {
			t.Errorf("AgentType(%q).IsValid() = true, want false", v)
		}
	}
}

func TestWorkspaceStatus_IsValid(t *testing.T) {
	t.Parallel()
	valid := []WorkspaceStatus{
		WorkspaceQueued,
		WorkspacePreparing,
		WorkspaceIdle,
		WorkspaceRunning,
		WorkspaceAwaitingInput,
		WorkspaceAwaitingApproval,
		WorkspaceResolvingConflict,
		WorkspaceConflictStuck,
		WorkspaceCompleted,
		WorkspaceMerged,
		WorkspaceFailed,
		WorkspaceCancelled,
	}
	invalid := []WorkspaceStatus{
		"",
		"bogus",
	}
	for _, v := range valid {
		if !v.IsValid() {
			t.Errorf("WorkspaceStatus(%q).IsValid() = false, want true", v)
		}
	}
	for _, v := range invalid {
		if v.IsValid() {
			t.Errorf("WorkspaceStatus(%q).IsValid() = true, want false", v)
		}
	}
}

func TestWorkspaceStatus_IsTerminal(t *testing.T) {
	t.Parallel()
	terminal := []WorkspaceStatus{
		WorkspaceMerged,
		WorkspaceFailed,
		WorkspaceCancelled,
	}
	nonTerminal := []WorkspaceStatus{
		"",
		"bogus",
		WorkspaceQueued,
		WorkspacePreparing,
		WorkspaceIdle,
		WorkspaceRunning,
		WorkspaceAwaitingInput,
		WorkspaceAwaitingApproval,
		WorkspaceResolvingConflict,
		WorkspaceConflictStuck,
		WorkspaceCompleted,
	}
	for _, v := range terminal {
		if !v.IsTerminal() {
			t.Errorf("WorkspaceStatus(%q).IsTerminal() = false, want true", v)
		}
	}
	for _, v := range nonTerminal {
		if v.IsTerminal() {
			t.Errorf("WorkspaceStatus(%q).IsTerminal() = true, want false", v)
		}
	}
}
