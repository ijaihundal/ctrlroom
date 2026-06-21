package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateSession_LookupRoundTrip(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	user := createUserRow(t, db, "alice")

	want, err := CreateSession(context.Background(), db, "hash-abc", user.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if want.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	got, err := LookupSession(context.Background(), db, "hash-abc")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.TokenHash != "hash-abc" {
		t.Errorf("TokenHash = %q, want %q", got.TokenHash, "hash-abc")
	}
	if got.UserID != user.ID {
		t.Errorf("UserID = %q, want %q", got.UserID, user.ID)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
	if got.CreatedAt.IsZero() {
		t.Error("lookup CreatedAt is zero")
	}
}

func TestLookupSession_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	_, err := LookupSession(context.Background(), db, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}

func TestLookupSession_ExpiredDeletedAndNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	user := createUserRow(t, db, "alice")

	past := time.Now().Add(-time.Minute)
	if _, err := CreateSession(context.Background(), db, "expired-hash", user.ID, past); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := LookupSession(context.Background(), db, "expired-hash")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("lookup expired: got err = %v, want ErrNotFound", err)
	}

	var n int
	if err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM sessions WHERE token_hash = ?;", "expired-hash",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("expired session row count = %d, want 0 (should be auto-deleted)", n)
	}
}

func TestDeleteSession_Idempotent(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	if err := DeleteSession(context.Background(), db, "never-existed"); err != nil {
		t.Errorf("delete missing: %v", err)
	}
	if err := DeleteSession(context.Background(), db, "never-existed"); err != nil {
		t.Errorf("delete missing twice: %v", err)
	}
}

func TestDeleteSession_RemovesRow(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	user := createUserRow(t, db, "alice")

	if _, err := CreateSession(context.Background(), db, "hash-del", user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := DeleteSession(context.Background(), db, "hash-del"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := LookupSession(context.Background(), db, "hash-del"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete, lookup err = %v, want ErrNotFound", err)
	}
}

func TestDeleteExpiredSessions_OnlyPurgesPast(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	user := createUserRow(t, db, "alice")

	now := time.Now()
	if _, err := CreateSession(context.Background(), db, "old-1", user.ID, now.Add(-time.Hour)); err != nil {
		t.Fatalf("create old-1: %v", err)
	}
	if _, err := CreateSession(context.Background(), db, "old-2", user.ID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("create old-2: %v", err)
	}
	if _, err := CreateSession(context.Background(), db, "live-1", user.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("create live-1: %v", err)
	}

	n, err := DeleteExpiredSessions(context.Background(), db, now)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n != 2 {
		t.Errorf("purged %d, want 2", n)
	}
	if _, err := LookupSession(context.Background(), db, "live-1"); err != nil {
		t.Errorf("live-1 should still exist, got err = %v", err)
	}
}
