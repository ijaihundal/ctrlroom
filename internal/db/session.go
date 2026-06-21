package db

// Session lifetime is governed by the auth layer (which issues tokens and sets
// cookie expiry). This file is pure storage.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// CreateSession inserts a new session row. tokenHash is the sha256 hex digest
// of the opaque token (the caller keeps the raw token only in the user's cookie).
func CreateSession(
	ctx context.Context, db *sql.DB, tokenHash, userID string, expiresAt time.Time,
) (*types.Session, error) {
	s := &types.Session{
		TokenHash: tokenHash,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}
	_, err := db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?);",
		s.TokenHash, s.UserID, s.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	if err := db.QueryRowContext(ctx,
		"SELECT created_at FROM sessions WHERE token_hash = ?;", s.TokenHash,
	).Scan(&s.CreatedAt); err != nil {
		return nil, fmt.Errorf("fetch session created_at: %w", err)
	}
	return s, nil
}

// LookupSession returns the session with the given tokenHash, or ErrNotFound.
// Expired sessions are returned as ErrNotFound as well, and deleted opportunistically.
func LookupSession(ctx context.Context, db *sql.DB, tokenHash string) (*types.Session, error) {
	s := &types.Session{}
	err := db.QueryRowContext(ctx,
		"SELECT token_hash, user_id, expires_at, created_at FROM sessions WHERE token_hash = ?;",
		tokenHash,
	).Scan(&s.TokenHash, &s.UserID, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup session: %w", err)
	}
	if time.Now().After(s.ExpiresAt) {
		_, _ = db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?;", tokenHash)
		return nil, ErrNotFound
	}
	return s, nil
}

// DeleteSession removes a session by token hash. Idempotent.
func DeleteSession(ctx context.Context, db *sql.DB, tokenHash string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?;", tokenHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions purges all sessions past their expiry. Returns rows deleted.
func DeleteExpiredSessions(ctx context.Context, db *sql.DB, now time.Time) (int64, error) {
	res, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?;", now)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}
