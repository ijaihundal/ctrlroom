package db

import (
	"context"
	"testing"
)

func TestOpen_Memory(t *testing.T) {
	t.Parallel()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if db == nil {
		t.Fatal("db is nil")
	}
}

func TestOpen_Pragmas(t *testing.T) {
	t.Parallel()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var fk string
	if err := db.QueryRowContext(context.Background(), "PRAGMA foreign_keys;").Scan(&fk); err != nil {
		t.Fatalf("read foreign_keys pragma: %v", err)
	}
	if fk != "1" {
		t.Errorf("foreign_keys = %q, want %q", fk, "1")
	}
}
