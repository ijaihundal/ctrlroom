package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// helper local to message_test.go: seed a workspace to FK against.
// Returns the same *sql.DB the workspace lives in so callers reuse one connection
// (each testDB(t) call opens a fresh in-memory database).
func seedWorkspace(t *testing.T) (*sql.DB, *types.Workspace) {
	t.Helper()
	database := testDB(t)
	project, err := CreateProject(context.Background(), database, ProjectCreateParams{
		Name: "t", RepoPath: "/tmp", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ws, err := CreateWorkspace(context.Background(), database, WorkspaceCreateParams{
		ProjectID: project.ID, AgentType: types.AgentOpenCode, Status: types.WorkspaceIdle,
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return database, ws
}

func TestCreateMessage_AssignsIncrementalSeq(t *testing.T) {
	db, ws := seedWorkspace(t)
	ctx := context.Background()

	m1, err := CreateMessage(ctx, db, ws.ID, "user", "first", nil)
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	if m1.Seq != 0 {
		t.Errorf("m1.Seq=%d, want 0", m1.Seq)
	}

	m2, err := CreateMessage(ctx, db, ws.ID, "assistant", "second", nil)
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	if m2.Seq != 1 {
		t.Errorf("m2.Seq=%d, want 1", m2.Seq)
	}

	m3, err := CreateMessage(ctx, db, ws.ID, "system", "third", map[string]any{"foo": "bar"})
	if err != nil {
		t.Fatalf("create m3: %v", err)
	}
	if m3.Seq != 2 {
		t.Errorf("m3.Seq=%d, want 2", m3.Seq)
	}
	if m3.Metadata["foo"] != "bar" {
		t.Errorf("metadata=%v, want foo=bar", m3.Metadata)
	}
	if m3.CreatedAt.IsZero() {
		t.Error("CreatedAt not populated")
	}
}

func TestCreateMessage_RejectsInvalidRole(t *testing.T) {
	db, ws := seedWorkspace(t)
	_, err := CreateMessage(context.Background(), db, ws.ID, "robot", "x", nil)
	if err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestGetMessage_NotFound(t *testing.T) {
	db := testDB(t)
	_, err := GetMessage(context.Background(), db, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("err=%v, want ErrNotFound", err)
	}
}

func TestListMessages_SinceFilter(t *testing.T) {
	db, ws := seedWorkspace(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := CreateMessage(ctx, db, ws.ID, "user", "msg", nil)
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	got, err := ListMessages(ctx, db, ws.ID, 1, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len=%d, want 3", len(got))
	}
	for i, m := range got {
		want := int64(i) + 2
		if m.Seq != want {
			t.Errorf("got[%d].Seq=%d, want %d", i, m.Seq, want)
		}
	}
}

func TestListMessages_LimitDefault(t *testing.T) {
	db, ws := seedWorkspace(t)
	ctx := context.Background()
	for i := 0; i < 600; i++ {
		_, err := CreateMessage(ctx, db, ws.ID, "system", "x", nil)
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	got, err := ListMessages(ctx, db, ws.ID, -1, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 500 {
		t.Errorf("len=%d, want 500", len(got))
	}
}

func TestLastSeq(t *testing.T) {
	db, ws := seedWorkspace(t)
	ctx := context.Background()

	n, err := LastSeq(ctx, db, ws.ID)
	if err != nil {
		t.Fatalf("last seq: %v", err)
	}
	if n != -1 {
		t.Errorf("empty workspace: LastSeq=%d, want -1", n)
	}

	for i := 0; i < 3; i++ {
		_, err := CreateMessage(ctx, db, ws.ID, "user", "x", nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	n, err = LastSeq(ctx, db, ws.ID)
	if err != nil {
		t.Fatalf("last seq 2: %v", err)
	}
	if n != 2 {
		t.Errorf("LastSeq=%d, want 2", n)
	}
}

func TestListAllMessagesByWorkspace(t *testing.T) {
	db, ws := seedWorkspace(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := CreateMessage(ctx, db, ws.ID, "user", "x", nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	got, err := ListAllMessagesByWorkspace(ctx, db, ws.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len=%d, want 3", len(got))
	}
}

func TestDeleteMessagesByWorkspace(t *testing.T) {
	db, ws := seedWorkspace(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := CreateMessage(ctx, db, ws.ID, "user", "x", nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	n, err := DeleteMessagesByWorkspace(ctx, db, ws.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 3 {
		t.Errorf("deleted=%d, want 3", n)
	}
	got, _ := ListAllMessagesByWorkspace(ctx, db, ws.ID)
	if len(got) != 0 {
		t.Errorf("post-delete len=%d, want 0", len(got))
	}
}

func TestCreateMessage_MetadataRoundTrip(t *testing.T) {
	db, ws := seedWorkspace(t)
	meta := map[string]any{
		"string": "value",
		"int":    float64(42), // JSON unmarshals numbers as float64
		"bool":   true,
		"nested": map[string]any{"k": "v"},
	}
	m, err := CreateMessage(context.Background(), db, ws.ID, "tool", "", meta)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := GetMessage(context.Background(), db, m.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Metadata["string"] != "value" {
		t.Errorf("string field lost: %v", got.Metadata)
	}
	if got.Metadata["bool"] != true {
		t.Errorf("bool field lost: %v", got.Metadata)
	}
}

func TestCreateMessage_EmptyMetadata(t *testing.T) {
	db, ws := seedWorkspace(t)
	m, err := CreateMessage(context.Background(), db, ws.ID, "user", "x", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.Metadata != nil {
		t.Errorf("Metadata=%v, want nil", m.Metadata)
	}
}
