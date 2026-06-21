package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Apply runs all unapplied migrations in lexical order.
// Each migration runs in its own transaction. On failure the tx is rolled back
// and the function returns the error; no partial state is left in the DB.
//
// A schema_migrations table tracks applied versions (the migration filename
// without the .sql extension, e.g. "001_auth").
func Apply(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := loadApplied(ctx, db)
	if err != nil {
		return err
	}

	files, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	pending := make([]string, 0, len(files))
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || len(name) < 5 || name[len(name)-4:] != ".sql" {
			continue
		}
		version := name[:len(name)-4]
		if _, ok := applied[version]; ok {
			continue
		}
		pending = append(pending, name)
	}
	sort.Strings(pending)

	for _, name := range pending {
		if err := applyOne(ctx, db, name); err != nil {
			return err
		}
	}
	return nil
}

func loadApplied(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations;")
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		out[v] = true
	}
	return out, rows.Err()
}

func applyOne(ctx context.Context, db *sql.DB, filename string) error {
	version := filename[:len(filename)-4]
	body, err := migrationsFS.ReadFile("migrations/" + filename)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", filename, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", filename, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is a no-op after commit

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("exec %s: %w", filename, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?);", version); err != nil {
		return fmt.Errorf("record %s: %w", filename, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", filename, err)
	}
	slog.Info("migration applied", "version", version)
	return nil
}
