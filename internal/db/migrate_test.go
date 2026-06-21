package db

import (
	"context"
	"testing"
)

func TestApply_FreshDB(t *testing.T) {
	t.Parallel()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Apply(context.Background(), db); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

func TestApply_Idempotent(t *testing.T) {
	t.Parallel()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Apply(context.Background(), db); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := Apply(context.Background(), db); err != nil {
		t.Fatalf("second apply: %v", err)
	}
}

func TestApply_CreatesExpectedTables(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	want := []string{"users", "sessions", "api_tokens", "schema_migrations"}
	for _, name := range want {
		var got string
		err := db.QueryRowContext(context.Background(),
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?;", name,
		).Scan(&got)
		if err != nil {
			t.Errorf("table %q missing or query failed: %v", name, err)
			continue
		}
		if got != name {
			t.Errorf("sqlite_master returned %q, want %q", got, name)
		}
	}
}

func TestApply_RecordsVersion(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	rows, err := db.QueryContext(context.Background(), "SELECT version FROM schema_migrations ORDER BY version;")
	if err != nil {
		t.Fatalf("query versions: %v", err)
	}
	defer rows.Close()

	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	wantVersions := []string{"001_auth", "002_kanban", "003_workspaces"}
	if len(versions) != len(wantVersions) {
		t.Fatalf("got %d versions, want %d (%v)", len(versions), len(wantVersions), versions)
	}
	for i, w := range wantVersions {
		if versions[i] != w {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], w)
		}
	}
}
