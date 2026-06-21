package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateAPIToken_NilWorkspaceLookupRoundTrip(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	user := createUserRow(t, db, "alice")

	created, err := CreateAPIToken(context.Background(), db, user.ID, "hash-1", nil, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Error("ID is empty")
	}
	if created.WorkspaceID != nil {
		t.Errorf("WorkspaceID = %v, want nil", *created.WorkspaceID)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	got, err := LookupAPITokenByHash(context.Background(), db, "hash-1")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.UserID != user.ID {
		t.Errorf("UserID = %q, want %q", got.UserID, user.ID)
	}
	if got.WorkspaceID != nil {
		t.Errorf("WorkspaceID = %v, want nil", *got.WorkspaceID)
	}
}

func TestCreateAPIToken_NonNilWorkspacePreserved(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	user := createUserRow(t, db, "alice")
	wsID := "ws-xyz"

	created, err := CreateAPIToken(context.Background(), db, user.ID, "hash-2", &wsID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.WorkspaceID == nil || *created.WorkspaceID != wsID {
		t.Errorf("created WorkspaceID = %v, want %q", created.WorkspaceID, wsID)
	}

	got, err := LookupAPITokenByHash(context.Background(), db, "hash-2")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.WorkspaceID == nil {
		t.Fatal("WorkspaceID nil after lookup")
	}
	if *got.WorkspaceID != wsID {
		t.Errorf("WorkspaceID = %q, want %q", *got.WorkspaceID, wsID)
	}
}

func TestLookupAPITokenByHash_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	_, err := LookupAPITokenByHash(context.Background(), db, "no-such-hash")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}

func TestLookupAPITokenByHash_ExpiredReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	user := createUserRow(t, db, "alice")

	past := time.Now().Add(-time.Minute)
	if _, err := CreateAPIToken(context.Background(), db, user.ID, "expired", nil, past); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := LookupAPITokenByHash(context.Background(), db, "expired")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("lookup expired: got err = %v, want ErrNotFound", err)
	}

	// Spec: expired tokens are NOT auto-deleted.
	var n int
	if err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM api_tokens WHERE token_hash = ?;", "expired",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expired token row count = %d, want 1 (must NOT be auto-deleted)", n)
	}
}

func TestDeleteAPIToken_Idempotent(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	if err := DeleteAPIToken(context.Background(), db, "never-existed"); err != nil {
		t.Errorf("delete missing: %v", err)
	}
	if err := DeleteAPIToken(context.Background(), db, "never-existed"); err != nil {
		t.Errorf("delete missing twice: %v", err)
	}
}

func TestDeleteAPIToken_RemovesRow(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	user := createUserRow(t, db, "alice")

	created, err := CreateAPIToken(context.Background(), db, user.ID, "del-hash", nil, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := DeleteAPIToken(context.Background(), db, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := LookupAPITokenByHash(context.Background(), db, "del-hash"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete, lookup err = %v, want ErrNotFound", err)
	}
}
