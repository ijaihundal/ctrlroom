package db

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

func TestCreateIssue_AssignsIncrementalSortOrder(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	a, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "a"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "b"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	c, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "c"})
	if err != nil {
		t.Fatalf("create c: %v", err)
	}
	if a.SortOrder != 0 {
		t.Errorf("a.SortOrder = %d, want 0", a.SortOrder)
	}
	if b.SortOrder != 1 {
		t.Errorf("b.SortOrder = %d, want 1", b.SortOrder)
	}
	if c.SortOrder != 2 {
		t.Errorf("c.SortOrder = %d, want 2", c.SortOrder)
	}
}

func TestCreateIssue_EmptyTagsIsNonNull(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateIssue(context.Background(), db, IssueCreateParams{
		ProjectID: proj.ID,
		Title:     "no-tags",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Tags == nil {
		t.Fatal("created.Tags is nil, want []string{}")
	}
	if len(created.Tags) != 0 {
		t.Errorf("created.Tags = %v, want empty", created.Tags)
	}

	var raw string
	if err := db.QueryRowContext(context.Background(),
		"SELECT tags FROM issues WHERE id = ?;", created.ID,
	).Scan(&raw); err != nil {
		t.Fatalf("read raw tags: %v", err)
	}
	if raw != "[]" {
		t.Errorf("raw tags JSON = %q, want %q", raw, "[]")
	}

	got, err := GetIssue(context.Background(), db, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Tags == nil {
		t.Fatal("scanned Tags is nil, want []string{}")
	}
	if len(got.Tags) != 0 {
		t.Errorf("scanned Tags = %v, want empty", got.Tags)
	}
}

func TestCreateIssue_TagsRoundTrip(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateIssue(context.Background(), db, IssueCreateParams{
		ProjectID: proj.ID,
		Title:     "tagged",
		Tags:      []string{"bug", "ui"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Tags) != 2 {
		t.Fatalf("created.Tags len = %d, want 2", len(created.Tags))
	}

	got, err := GetIssue(context.Background(), db, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	want := []string{"bug", "ui"}
	if len(got.Tags) != len(want) {
		t.Fatalf("got.Tags len = %d, want %d", len(got.Tags), len(want))
	}
	for i, w := range want {
		if got.Tags[i] != w {
			t.Errorf("got.Tags[%d] = %q, want %q", i, got.Tags[i], w)
		}
	}
}

func TestListIssuesByProject_OrderedBySortOrder(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	a, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "a"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "b"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	c, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "c"})
	if err != nil {
		t.Fatalf("create c: %v", err)
	}

	list, err := ListIssuesByProject(context.Background(), db, proj.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d, want 3", len(list))
	}
	want := []string{a.ID, b.ID, c.ID}
	for i, w := range want {
		if list[i].ID != w {
			t.Errorf("list[%d].ID = %q, want %q", i, list[i].ID, w)
		}
	}
}

func TestListIssuesByProjectAndStatus_Filters(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	a, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "a"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "b"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	progStatus := types.IssueInProgress
	if _, err := UpdateIssue(context.Background(), db, b.ID, IssueUpdatePatch{Status: &progStatus}); err != nil {
		t.Fatalf("update b status: %v", err)
	}

	got, err := ListIssuesByProjectAndStatus(context.Background(), db, proj.ID, types.IssueTodo)
	if err != nil {
		t.Fatalf("list todo: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d todo issues, want 1", len(got))
	}
	if got[0].ID != a.ID {
		t.Errorf("got[0].ID = %q, want %q", got[0].ID, a.ID)
	}
}

func TestUpdateIssue_EachPatchField(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateIssue(context.Background(), db, IssueCreateParams{
		ProjectID:   proj.ID,
		Title:       "orig",
		Description: "orig desc",
		Priority:    0,
		Tags:        []string{"x"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	title := "new title"
	desc := "new desc"
	status := types.IssueReview
	prio := 5
	tags := []string{"bug", "ui", "p0"}
	nilTags := []string(nil)

	cases := []struct {
		name  string
		patch IssueUpdatePatch
		check func(*types.Issue) error
	}{
		{
			name:  "Title",
			patch: IssueUpdatePatch{Title: &title},
			check: func(i *types.Issue) error {
				if i.Title != title {
					return fmt.Errorf("Title = %q, want %q", i.Title, title)
				}
				return nil
			},
		},
		{
			name:  "Description",
			patch: IssueUpdatePatch{Description: &desc},
			check: func(i *types.Issue) error {
				if i.Description != desc {
					return fmt.Errorf("Description = %q, want %q", i.Description, desc)
				}
				return nil
			},
		},
		{
			name:  "Status",
			patch: IssueUpdatePatch{Status: &status},
			check: func(i *types.Issue) error {
				if i.Status != status {
					return fmt.Errorf("Status = %q, want %q", i.Status, status)
				}
				return nil
			},
		},
		{
			name:  "Priority",
			patch: IssueUpdatePatch{Priority: &prio},
			check: func(i *types.Issue) error {
				if i.Priority != prio {
					return fmt.Errorf("Priority = %d, want %d", i.Priority, prio)
				}
				return nil
			},
		},
		{
			name:  "Tags",
			patch: IssueUpdatePatch{Tags: &tags},
			check: func(i *types.Issue) error {
				if len(i.Tags) != len(tags) {
					return fmt.Errorf("Tags len = %d, want %d", len(i.Tags), len(tags))
				}
				for j, w := range tags {
					if i.Tags[j] != w {
						return fmt.Errorf("Tags[%d] = %q, want %q", j, i.Tags[j], w)
					}
				}
				return nil
			},
		},
		{
			name:  "TagsNilBecomesEmpty",
			patch: IssueUpdatePatch{Tags: &nilTags},
			check: func(i *types.Issue) error {
				if i.Tags == nil || len(i.Tags) != 0 {
					return fmt.Errorf("Tags = %v, want []string{}", i.Tags)
				}
				return nil
			},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			i, err := UpdateIssue(context.Background(), db, created.ID, c.patch)
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			if err := c.check(i); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestReorderIssues_SetsSortOrder(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	a, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "a"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "b"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	c, err := CreateIssue(context.Background(), db, IssueCreateParams{ProjectID: proj.ID, Title: "c"})
	if err != nil {
		t.Fatalf("create c: %v", err)
	}

	if err := ReorderIssues(context.Background(), db, proj.ID, []string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	list, err := ListIssuesByProject(context.Background(), db, proj.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{c.ID, a.ID, b.ID}
	for i, w := range want {
		if list[i].ID != w {
			t.Errorf("list[%d].ID = %q, want %q", i, list[i].ID, w)
		}
		if list[i].SortOrder != i {
			t.Errorf("list[%d].SortOrder = %d, want %d", i, list[i].SortOrder, i)
		}
	}
}

func TestDeleteIssue_RemovesRow(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	proj, err := CreateProject(context.Background(), db, ProjectCreateParams{
		Name: "p", RepoPath: "/r", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateIssue(context.Background(), db, IssueCreateParams{
		ProjectID: proj.ID, Title: "x",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := DeleteIssue(context.Background(), db, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetIssue(context.Background(), db, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete, err = %v, want ErrNotFound", err)
	}
}

func TestDeleteIssue_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	err := DeleteIssue(context.Background(), db, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}
