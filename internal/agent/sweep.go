package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/types"
)

// SweepOrphans marks any workspace in a non-terminal "active" state as failed.
// Called on boot before the agent Manager starts, because agent subprocesses
// from the previous server lifetime are gone and their sessions are unrecoverable.
//
// Writes a system message to each swept workspace explaining what happened.
// Returns the count of swept workspaces for logging.
func SweepOrphans(ctx context.Context, database *sql.DB, logger *slog.Logger) (int, error) {
	now := time.Now()
	failed := types.WorkspaceFailed

	rows, err := database.QueryContext(ctx,
		`SELECT id FROM workspaces
		 WHERE status IN ('running','preparing','awaiting_input','resolving_conflict','awaiting_approval');`)
	if err != nil {
		return 0, fmt.Errorf("query active workspaces: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows err: %w", err)
	}

	for _, id := range ids {
		if _, err := db.UpdateWorkspace(ctx, database, id, db.WorkspaceUpdatePatch{
			Status:      &failed,
			CompletedAt: &now,
		}); err != nil {
			logger.Error("sweep: mark workspace failed", "workspace_id", id, "err", err)
			continue
		}
		if _, err := db.CreateMessage(ctx, database, id, "system",
			"Server was restarted while this workspace was active; agent session lost.",
			nil); err != nil {
			logger.Error("sweep: write system message", "workspace_id", id, "err", err)
		}
	}

	if len(ids) > 0 {
		logger.Info("swept orphaned workspaces", "count", len(ids))
	}
	return len(ids), nil
}
