package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Message is a persisted agent event / user prompt / system note in a workspace.
type Message struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Role        string         `json:"role"` // "user" | "assistant" | "tool" | "system"
	Content     string         `json:"content,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Seq         int64          `json:"seq"`
	CreatedAt   time.Time      `json:"created_at"`
}

// CreateMessage inserts a message and assigns the next per-workspace seq atomically.
// The seq assignment + insert happens in a single tx so concurrent writers can't collide.
// Returns the inserted row (with seq + created_at populated).
func CreateMessage(
	ctx context.Context, db *sql.DB,
	workspaceID, role, content string,
	metadata map[string]any,
) (*Message, error) {
	if role != "user" && role != "assistant" && role != "tool" && role != "system" {
		return nil, fmt.Errorf("invalid message role %q", role)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // safe: no-op after commit

	var nextSeq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), -1) + 1 FROM messages WHERE workspace_id = ?;`,
		workspaceID,
	).Scan(&nextSeq); err != nil {
		return nil, fmt.Errorf("compute next seq: %w", err)
	}

	id := ulid.Make().String()
	var metadataJSON any
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		metadataJSON = string(b)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO messages (id, workspace_id, role, content, metadata, seq) VALUES (?, ?, ?, ?, ?, ?);`,
		id, workspaceID, role, content, metadataJSON, nextSeq,
	); err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// Fetch back to get created_at populated.
	return GetMessage(ctx, db, id)
}

// GetMessage returns a message by id, or ErrNotFound.
func GetMessage(ctx context.Context, db *sql.DB, id string) (*Message, error) {
	m := &Message{}
	var metadataJSON sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT id, workspace_id, role, COALESCE(content, ''), COALESCE(metadata, ''), seq, created_at
         FROM messages WHERE id = ?;`,
		id,
	).Scan(&m.ID, &m.WorkspaceID, &m.Role, &m.Content, &metadataJSON, &m.Seq, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan message: %w", err)
	}
	if metadataJSON.Valid && metadataJSON.String != "" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &m.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}
	return m, nil
}

// ListMessages returns messages for a workspace with seq > since, ordered by seq ascending.
// Caps at limit (use 0 or negative for default 500).
func ListMessages(ctx context.Context, db *sql.DB, workspaceID string, since int64, limit int) ([]*Message, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, workspace_id, role, COALESCE(content, ''), COALESCE(metadata, ''), seq, created_at
         FROM messages
         WHERE workspace_id = ? AND seq > ?
         ORDER BY seq ASC
         LIMIT ?;`,
		workspaceID, since, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// ListAllMessagesByWorkspace returns every message for a workspace, ordered by seq.
// Used by sweep / debug tooling. Prefer ListMessages for paginated client use.
func ListAllMessagesByWorkspace(ctx context.Context, db *sql.DB, workspaceID string) ([]*Message, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, workspace_id, role, COALESCE(content, ''), COALESCE(metadata, ''), seq, created_at
         FROM messages
         WHERE workspace_id = ?
         ORDER BY seq ASC;`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// LastSeq returns the highest seq for a workspace, or -1 if no messages exist.
// Useful for clients to know where to resume from.
func LastSeq(ctx context.Context, db *sql.DB, workspaceID string) (int64, error) {
	var seq sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM messages WHERE workspace_id = ?;`,
		workspaceID,
	).Scan(&seq)
	if err != nil {
		return -1, fmt.Errorf("query last seq: %w", err)
	}
	if !seq.Valid {
		return -1, nil
	}
	return seq.Int64, nil
}

// DeleteMessagesByWorkspace removes all messages for a workspace. Used on workspace hard-delete.
// Normally the FK ON DELETE CASCADE handles this; this function is for explicit cleanup.
func DeleteMessagesByWorkspace(ctx context.Context, db *sql.DB, workspaceID string) (int64, error) {
	res, err := db.ExecContext(ctx,
		`DELETE FROM messages WHERE workspace_id = ?;`,
		workspaceID,
	)
	if err != nil {
		return 0, fmt.Errorf("delete messages: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

func scanMessages(rows *sql.Rows) ([]*Message, error) {
	var out []*Message
	for rows.Next() {
		m := &Message{}
		var metadataJSON sql.NullString
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Role, &m.Content, &metadataJSON, &m.Seq, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if metadataJSON.Valid && metadataJSON.String != "" {
			if err := json.Unmarshal([]byte(metadataJSON.String), &m.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal metadata: %w", err)
			}
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}
	return out, nil
}
