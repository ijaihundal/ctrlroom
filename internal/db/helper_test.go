package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// testDB returns a migrated in-memory SQLite database closed automatically at test end.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Apply(context.Background(), db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

// createUserRow inserts a user row for FK-referencing tests and returns it.
func createUserRow(t *testing.T, db *sql.DB, username string) *types.User {
	t.Helper()
	u, err := CreateUser(context.Background(), db, username, "hashed-secret")
	if err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
	return u
}
