package db

import (
	"context"
	"errors"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

func TestCreateWorkspace_PopulatesFields(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	w, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID,
		Branch:    "ctrlroom/ws-abc",
		AgentType: types.AgentClaude,
		Model:     "claude-sonnet-4-5",
		TargetRef: "main",
		Prompt:    "do thing",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.ID == "" {
		t.Error("ID is empty")
	}
	if w.ProjectID != proj.ID {
		t.Errorf("ProjectID = %q, want %q", w.ProjectID, proj.ID)
	}
	if w.Branch != "ctrlroom/ws-abc" {
		t.Errorf("Branch = %q, want %q", w.Branch, "ctrlroom/ws-abc")
	}
	if w.AgentType != types.AgentClaude {
		t.Errorf("AgentType = %q, want %q", w.AgentType, types.AgentClaude)
	}
	if w.Model != "claude-sonnet-4-5" {
		t.Errorf("Model = %q, want %q", w.Model, "claude-sonnet-4-5")
	}
	if w.Status != types.WorkspaceQueued {
		t.Errorf("Status = %q, want %q", w.Status, types.WorkspaceQueued)
	}
	if w.Prompt != "do thing" {
		t.Errorf("Prompt = %q, want %q", w.Prompt, "do thing")
	}
	if w.TargetRef != "main" {
		t.Errorf("TargetRef = %q, want %q", w.TargetRef, "main")
	}
	if w.Orchestrator {
		t.Error("Orchestrator = true, want false")
	}
	if w.PendingMerge != nil {
		t.Errorf("PendingMerge = %v, want nil", w.PendingMerge)
	}
	if w.StartedAt != nil {
		t.Errorf("StartedAt = %v, want nil", w.StartedAt)
	}
	if w.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestCreateWorkspace_WithIssueID(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issue, err := CreateIssue(context.Background(), db, IssueCreateParams{
		ProjectID: proj.ID, Title: "t",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	issueID := issue.ID
	w, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		IssueID:   &issueID,
		ProjectID: proj.ID,
		Branch:    "b",
		AgentType: types.AgentCodex,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.IssueID == nil || *w.IssueID != issueID {
		t.Errorf("IssueID = %v, want %q", w.IssueID, issueID)
	}
}

func TestGetWorkspace_RoundTrip(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID,
		Branch:    "b",
		AgentType: types.AgentOpenCode,
		Model:     "gpt-5-codex",
		TargetRef: "develop",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := GetWorkspace(context.Background(), db, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.AgentType != types.AgentOpenCode {
		t.Errorf("AgentType = %q, want %q", got.AgentType, types.AgentOpenCode)
	}
	if got.Model != "gpt-5-codex" {
		t.Errorf("Model = %q, want %q", got.Model, "gpt-5-codex")
	}
	if got.TargetRef != "develop" {
		t.Errorf("TargetRef = %q, want %q", got.TargetRef, "develop")
	}
}

func TestGetWorkspace_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	_, err := GetWorkspace(context.Background(), db, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}

func TestListWorkspacesByProject_OrderedByCreatedAtDesc(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	w1, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID, Branch: "b1", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create w1: %v", err)
	}
	w2, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID, Branch: "b2", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create w2: %v", err)
	}
	w3, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID, Branch: "b3", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create w3: %v", err)
	}

	list, err := ListWorkspacesByProject(context.Background(), db, proj.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d, want 3", len(list))
	}
	want := []string{w3.ID, w2.ID, w1.ID}
	for i, w := range want {
		if list[i].ID != w {
			t.Errorf("list[%d].ID = %q, want %q (DESC by created_at)", i, list[i].ID, w)
		}
	}
}

func TestListWorkspacesByIssue_Filters(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issue1, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "i1"})
	if err != nil {
		t.Fatalf("create issue1: %v", err)
	}
	issue2, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "i2"})
	if err != nil {
		t.Fatalf("create issue2: %v", err)
	}
	i1 := issue1.ID
	i2 := issue2.ID
	if _, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		IssueID: &i1, ProjectID: proj.ID, Branch: "b1", AgentType: types.AgentClaude,
	}); err != nil {
		t.Fatalf("create w1: %v", err)
	}
	if _, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		IssueID: &i1, ProjectID: proj.ID, Branch: "b2", AgentType: types.AgentClaude,
	}); err != nil {
		t.Fatalf("create w2: %v", err)
	}
	if _, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		IssueID: &i2, ProjectID: proj.ID, Branch: "b3", AgentType: types.AgentClaude,
	}); err != nil {
		t.Fatalf("create w3: %v", err)
	}

	got, err := ListWorkspacesByIssue(context.Background(), db, issue1.ID)
	if err != nil {
		t.Fatalf("list by issue1: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	for _, w := range got {
		if w.IssueID == nil || *w.IssueID != issue1.ID {
			t.Errorf("workspace IssueID = %v, want %q", w.IssueID, issue1.ID)
		}
	}
}

func TestUpdateWorkspace_Status(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID, Branch: "b", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newStatus := types.WorkspaceRunning
	got, err := UpdateWorkspace(context.Background(), db, created.ID, WorkspaceUpdatePatch{Status: &newStatus})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Status != newStatus {
		t.Errorf("Status = %q, want %q", got.Status, newStatus)
	}
}

func TestUpdateWorkspace_PendingMergeRoundTrip(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID, Branch: "b", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	pm := &types.PendingMerge{Target: "main", Attempt: 1, MaxAttempts: 2}
	got, err := UpdateWorkspace(context.Background(), db, created.ID, WorkspaceUpdatePatch{PendingMerge: pm})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.PendingMerge == nil {
		t.Fatal("PendingMerge nil after update")
	}
	if got.PendingMerge.Target != "main" {
		t.Errorf("Target = %q, want %q", got.PendingMerge.Target, "main")
	}
	if got.PendingMerge.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", got.PendingMerge.Attempt)
	}
	if got.PendingMerge.MaxAttempts != 2 {
		t.Errorf("MaxAttempts = %d, want 2", got.PendingMerge.MaxAttempts)
	}

	again, err := GetWorkspace(context.Background(), db, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if again.PendingMerge == nil {
		t.Fatal("PendingMerge nil after read-back")
	}
	if again.PendingMerge.Target != "main" || again.PendingMerge.Attempt != 1 || again.PendingMerge.MaxAttempts != 2 {
		t.Errorf("read-back PendingMerge = %+v, want {main 1 2}", again.PendingMerge)
	}
}

func TestUpdateWorkspace_ClearPendingMerge(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID, Branch: "b", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pm := &types.PendingMerge{Target: "main", Attempt: 1, MaxAttempts: 2}
	if _, err := UpdateWorkspace(context.Background(), db, created.ID, WorkspaceUpdatePatch{PendingMerge: pm}); err != nil {
		t.Fatalf("set pending_merge: %v", err)
	}

	got, err := UpdateWorkspace(context.Background(), db, created.ID, WorkspaceUpdatePatch{ClearPendingMerge: true})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got.PendingMerge != nil {
		t.Errorf("PendingMerge = %v, want nil", got.PendingMerge)
	}

	var raw *string
	if err := db.QueryRowContext(context.Background(),
		"SELECT pending_merge FROM workspaces WHERE id = ?;", created.ID,
	).Scan(&raw); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if raw != nil {
		t.Errorf("raw column = %q, want NULL", *raw)
	}
}

func TestUpdateWorkspace_WorktreePath(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID, Branch: "b", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	path := "/data/worktrees/x/ws-1"
	got, err := UpdateWorkspace(context.Background(), db, created.ID, WorkspaceUpdatePatch{WorktreePath: &path})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.WorktreePath != path {
		t.Errorf("WorktreePath = %q, want %q", got.WorktreePath, path)
	}
}

func TestUpdateWorkspace_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	v := types.WorkspaceRunning
	_, err := UpdateWorkspace(context.Background(), db, "missing", WorkspaceUpdatePatch{Status: &v})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}

func TestDeleteWorkspace_RemovesRow(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID, Branch: "b", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := DeleteWorkspace(context.Background(), db, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetWorkspace(context.Background(), db, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete, err = %v, want ErrNotFound", err)
	}
}

func TestDeleteWorkspace_CascadesFromProject(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ws, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		ProjectID: proj.ID, Branch: "b", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := DeleteProject(context.Background(), db, proj.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := GetWorkspace(context.Background(), db, ws.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after project delete, workspace err = %v, want ErrNotFound", err)
	}
}

func TestDeleteWorkspace_OnIssueSetsNull(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issue, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "i"})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	issueID := issue.ID
	ws, err := CreateWorkspace(context.Background(), db, WorkspaceCreateParams{
		IssueID: &issueID, ProjectID: proj.ID, Branch: "b", AgentType: types.AgentClaude,
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := DeleteIssue(context.Background(), db, issue.ID); err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	got, err := GetWorkspace(context.Background(), db, ws.ID)
	if err != nil {
		t.Fatalf("get workspace after issue delete: %v", err)
	}
	if got.IssueID != nil {
		t.Errorf("IssueID = %v, want nil (SET NULL on cascade)", got.IssueID)
	}
}

func TestMigration003_WorkspacesTableExists(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	var n int
	err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'workspaces';",
	).Scan(&n)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if n != 1 {
		t.Errorf("workspaces table count = %d, want 1", n)
	}
}
