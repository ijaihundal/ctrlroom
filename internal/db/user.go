package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// ErrNotFound is returned when a single-row query matches no rows.
var ErrNotFound = errors.New("not found")

// NewID returns a new ULID string. Used by Create* methods.
func NewID() string {
	return ulid.Make().String()
}

// CountUsers returns the number of user rows. Used at first-boot to decide
// whether to seed the admin user.
func CountUsers(ctx context.Context, db *sql.DB) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users;").Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CreateUser inserts a new user with a generated ULID and returns the populated row.
func CreateUser(ctx context.Context, db *sql.DB, username, passwordHash string) (*types.User, error) {
	u := &types.User{
		ID:           NewID(),
		Username:     username,
		PasswordHash: passwordHash,
	}
	res, err := db.ExecContext(ctx,
		"INSERT INTO users (id, username, password_hash) VALUES (?, ?, ?);",
		u.ID, u.Username, u.PasswordHash,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if rows != 1 {
		return nil, fmt.Errorf("expected 1 row inserted, got %d", rows)
	}
	if err := db.QueryRowContext(ctx,
		"SELECT created_at FROM users WHERE id = ?;", u.ID,
	).Scan(&u.CreatedAt); err != nil {
		return nil, fmt.Errorf("fetch created_at: %w", err)
	}
	return u, nil
}

// GetUserByUsername returns the user with the given username, or ErrNotFound.
func GetUserByUsername(ctx context.Context, db *sql.DB, username string) (*types.User, error) {
	return scanUser(ctx, db, "SELECT id, username, password_hash, created_at FROM users WHERE username = ?;", username)
}

// GetUserByID returns the user with the given ID, or ErrNotFound.
func GetUserByID(ctx context.Context, db *sql.DB, id string) (*types.User, error) {
	return scanUser(ctx, db, "SELECT id, username, password_hash, created_at FROM users WHERE id = ?;", id)
}

func scanUser(ctx context.Context, db *sql.DB, query string, args ...any) (*types.User, error) {
	u := &types.User{}
	err := db.QueryRowContext(ctx, query, args...).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}
