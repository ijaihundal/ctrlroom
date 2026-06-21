package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

func TestCreateProject_PopulatesFields(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	p, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name:          "alpha",
		Description:   "first project",
		RepoPath:      "/repos/alpha",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == "" {
		t.Error("ID is empty")
	}
	if p.Name != "alpha" {
		t.Errorf("Name = %q, want %q", p.Name, "alpha")
	}
	if p.Description != "first project" {
		t.Errorf("Description = %q, want %q", p.Description, "first project")
	}
	if p.RepoPath != "/repos/alpha" {
		t.Errorf("RepoPath = %q, want %q", p.RepoPath, "/repos/alpha")
	}
	if p.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", p.DefaultBranch, "main")
	}
	if p.ApprovalMode != types.ApprovalAutonomous {
		t.Errorf("ApprovalMode = %q, want %q", p.ApprovalMode, types.ApprovalAutonomous)
	}
	if p.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if p.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}
}

func TestCreateProject_DefaultsApprovalMode(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	p, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name:          "no-mode",
		RepoPath:      "/repos/no-mode",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ApprovalMode != types.ApprovalAutonomous {
		t.Errorf("ApprovalMode = %q, want default %q", p.ApprovalMode, types.ApprovalAutonomous)
	}
}

func TestGetProject_RoundTrip(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	created, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name:          "beta",
		Description:   "rt",
		RepoPath:      "/repos/beta",
		DefaultBranch: "develop",
		ApprovalMode:  types.ApprovalPrompt,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := GetProject(context.Background(), db, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.Name != "beta" {
		t.Errorf("Name = %q, want %q", got.Name, "beta")
	}
	if got.Description != "rt" {
		t.Errorf("Description = %q, want %q", got.Description, "rt")
	}
	if got.DefaultBranch != "develop" {
		t.Errorf("DefaultBranch = %q, want %q", got.DefaultBranch, "develop")
	}
	if got.ApprovalMode != types.ApprovalPrompt {
		t.Errorf("ApprovalMode = %q, want %q", got.ApprovalMode, types.ApprovalPrompt)
	}
}

func TestGetProject_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	_, err := GetProject(context.Background(), db, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}

func TestListProjects_OrderedByCreatedAt(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	p1, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p1", RepoPath: "/r1", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}
	p2, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p2", RepoPath: "/r2", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}
	p3, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p3", RepoPath: "/r3", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create p3: %v", err)
	}

	list, err := ListProjects(context.Background(), db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d projects, want 3", len(list))
	}
	wantOrder := []string{p1.ID, p2.ID, p3.ID}
	for i, w := range wantOrder {
		if list[i].ID != w {
			t.Errorf("list[%d].ID = %q, want %q", i, list[i].ID, w)
		}
	}
}

func TestUpdateProject_EachPatchField(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	created, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name:          "orig",
		Description:   "orig desc",
		RepoPath:      "/r",
		DefaultBranch: "main",
		ApprovalMode:  types.ApprovalAutonomous,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Run("Name", func(t *testing.T) {
		newName := "renamed"
		p, err := UpdateProject(context.Background(), db, created.ID, ProjectUpdatePatch{Name: &newName})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if p.Name != newName {
			t.Errorf("Name = %q, want %q", p.Name, newName)
		}
		if p.Description != "orig desc" {
			t.Errorf("Description changed: %q", p.Description)
		}
	})

	t.Run("Description", func(t *testing.T) {
		newDesc := "new desc"
		p, err := UpdateProject(context.Background(), db, created.ID, ProjectUpdatePatch{Description: &newDesc})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if p.Description != newDesc {
			t.Errorf("Description = %q, want %q", p.Description, newDesc)
		}
	})

	t.Run("DefaultBranch", func(t *testing.T) {
		newBranch := "develop"
		p, err := UpdateProject(context.Background(), db, created.ID, ProjectUpdatePatch{DefaultBranch: &newBranch})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if p.DefaultBranch != newBranch {
			t.Errorf("DefaultBranch = %q, want %q", p.DefaultBranch, newBranch)
		}
	})

	t.Run("ApprovalMode", func(t *testing.T) {
		newMode := types.ApprovalOnFailure
		p, err := UpdateProject(context.Background(), db, created.ID, ProjectUpdatePatch{ApprovalMode: &newMode})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if p.ApprovalMode != newMode {
			t.Errorf("ApprovalMode = %q, want %q", p.ApprovalMode, newMode)
		}
	})
}

func TestUpdateProject_NilPatchBumpsUpdatedAt(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	created, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "touch", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// SQLite CURRENT_TIMESTAMP has 1s resolution.
	original := created.UpdatedAt
	time.Sleep(1100 * time.Millisecond)

	p, err := UpdateProject(context.Background(), db, created.ID, ProjectUpdatePatch{})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !p.UpdatedAt.After(original) {
		t.Errorf("UpdatedAt not bumped: original=%v now=%v", original, p.UpdatedAt)
	}
}

func TestUpdateProject_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	newName := "x"
	_, err := UpdateProject(context.Background(), db, "missing", ProjectUpdatePatch{Name: &newName})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}

func TestDeleteProject_RemovesRow(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	created, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "del", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := DeleteProject(context.Background(), db, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetProject(context.Background(), db, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete, get err = %v, want ErrNotFound", err)
	}
}

func TestDeleteProject_CascadesToIssues(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	created, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "cascade", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issue, err := CreateIssue(context.Background(), db, IssueCreateParams{
		ProjectID: created.ID,
		Title:     "child issue",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := DeleteProject(context.Background(), db, created.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := GetIssue(context.Background(), db, issue.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after project delete, issue err = %v, want ErrNotFound", err)
	}
}

func TestDeleteProject_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	err := DeleteProject(context.Background(), db, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}
