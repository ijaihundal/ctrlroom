package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// CreateAPIToken inserts a new api_tokens row. workspaceID may be nil (NULL in DB).
func CreateAPIToken(
	ctx context.Context,
	db *sql.DB,
	userID, tokenHash string,
	workspaceID *string,
	expiresAt time.Time,
) (*types.APIToken, error) {
	t := &types.APIToken{
		ID:          NewID(),
		UserID:      userID,
		WorkspaceID: workspaceID,
		TokenHash:   tokenHash,
		ExpiresAt:   expiresAt,
	}
	var wsID any
	if workspaceID != nil {
		wsID = *workspaceID
	}
	_, err := db.ExecContext(ctx,
		"INSERT INTO api_tokens (id, user_id, workspace_id, token_hash, expires_at) VALUES (?, ?, ?, ?, ?);",
		t.ID, t.UserID, wsID, t.TokenHash, t.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert api_token: %w", err)
	}
	if err := db.QueryRowContext(ctx,
		"SELECT created_at FROM api_tokens WHERE id = ?;", t.ID,
	).Scan(&t.CreatedAt); err != nil {
		return nil, fmt.Errorf("fetch api_token created_at: %w", err)
	}
	return t, nil
}

// LookupAPITokenByHash returns the api_token with the given hash, or ErrNotFound.
// Expired tokens return ErrNotFound and are NOT auto-deleted (their workspace may still reference them).
func LookupAPITokenByHash(ctx context.Context, db *sql.DB, tokenHash string) (*types.APIToken, error) {
	t := &types.APIToken{}
	var wsID sql.NullString
	err := db.QueryRowContext(ctx,
		"SELECT id, user_id, workspace_id, token_hash, expires_at, created_at FROM api_tokens WHERE token_hash = ?;",
		tokenHash,
	).Scan(&t.ID, &t.UserID, &wsID, &t.TokenHash, &t.ExpiresAt, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup api_token: %w", err)
	}
	if wsID.Valid {
		s := wsID.String
		t.WorkspaceID = &s
	}
	if time.Now().After(t.ExpiresAt) {
		return nil, ErrNotFound
	}
	return t, nil
}

// DeleteAPIToken removes an api_token by id. Idempotent.
func DeleteAPIToken(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM api_tokens WHERE id = ?;", id)
	if err != nil {
		return fmt.Errorf("delete api_token: %w", err)
	}
	return nil
}
