package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// WorkspaceCreateParams holds the inputs for creating a workspace.
// ID and default columns are assigned by SQL.
type WorkspaceCreateParams struct {
	IssueID      *string
	ProjectID    string
	AgentType    types.AgentType
	Model        string
	Branch       string
	TargetRef    string
	Prompt       string
	Status       types.WorkspaceStatus
	Orchestrator bool
}

// CreateWorkspace inserts a new workspace with a generated ULID.
// Status defaults to WorkspaceQueued if unset.
func CreateWorkspace(ctx context.Context, db *sql.DB, p WorkspaceCreateParams) (*types.Workspace, error) {
	if p.Status == "" {
		p.Status = types.WorkspaceQueued
	}
	w := &types.Workspace{
		ID:           NewID(),
		IssueID:      p.IssueID,
		ProjectID:    p.ProjectID,
		Branch:       p.Branch,
		AgentType:    p.AgentType,
		Model:        p.Model,
		Status:       p.Status,
		Prompt:       p.Prompt,
		TargetRef:    p.TargetRef,
		Orchestrator: p.Orchestrator,
	}

	var issueID any
	if p.IssueID != nil {
		issueID = *p.IssueID
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO workspaces (id, issue_id, project_id, branch, agent_type, model, status, prompt, target_ref, orchestrator)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`,
		w.ID, issueID, w.ProjectID, w.Branch, string(w.AgentType),
		w.Model, string(w.Status), w.Prompt, w.TargetRef, w.Orchestrator,
	)
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return GetWorkspace(ctx, db, w.ID)
}

// GetWorkspace returns the workspace with the given id, or ErrNotFound.
func GetWorkspace(ctx context.Context, db *sql.DB, id string) (*types.Workspace, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, issue_id, project_id, branch, agent_type, COALESCE(model, ''), status,
		       COALESCE(prompt, ''), COALESCE(worktree_path, ''), COALESCE(target_ref, ''),
		       pending_merge, orchestrator, tokens_in, tokens_out, cost_usd,
		       started_at, completed_at, created_at, updated_at
		FROM workspaces WHERE id = ?;
	`, id)
	return scanWorkspace(row)
}

// ListWorkspacesByProject returns workspaces for a project, newest first.
func ListWorkspacesByProject(ctx context.Context, db *sql.DB, projectID string) ([]*types.Workspace, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, issue_id, project_id, branch, agent_type, COALESCE(model, ''), status,
		       COALESCE(prompt, ''), COALESCE(worktree_path, ''), COALESCE(target_ref, ''),
		       pending_merge, orchestrator, tokens_in, tokens_out, cost_usd,
		       started_at, completed_at, created_at, updated_at
		FROM workspaces WHERE project_id = ?
		ORDER BY created_at DESC, id DESC;
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query workspaces by project: %w", err)
	}
	defer rows.Close()
	return scanWorkspaces(rows)
}

// ListWorkspacesByIssue returns workspaces attached to an issue.
func ListWorkspacesByIssue(ctx context.Context, db *sql.DB, issueID string) ([]*types.Workspace, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, issue_id, project_id, branch, agent_type, COALESCE(model, ''), status,
		       COALESCE(prompt, ''), COALESCE(worktree_path, ''), COALESCE(target_ref, ''),
		       pending_merge, orchestrator, tokens_in, tokens_out, cost_usd,
		       started_at, completed_at, created_at, updated_at
		FROM workspaces WHERE issue_id = ?
		ORDER BY created_at DESC, id DESC;
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("query workspaces by issue: %w", err)
	}
	defer rows.Close()
	return scanWorkspaces(rows)
}

// ListWorkspacesByStatus returns workspaces in a given status.
func ListWorkspacesByStatus(ctx context.Context, db *sql.DB, status types.WorkspaceStatus) ([]*types.Workspace, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, issue_id, project_id, branch, agent_type, COALESCE(model, ''), status,
		       COALESCE(prompt, ''), COALESCE(worktree_path, ''), COALESCE(target_ref, ''),
		       pending_merge, orchestrator, tokens_in, tokens_out, cost_usd,
		       started_at, completed_at, created_at, updated_at
		FROM workspaces WHERE status = ?
		ORDER BY created_at DESC, id DESC;
	`, string(status))
	if err != nil {
		return nil, fmt.Errorf("query workspaces by status: %w", err)
	}
	defer rows.Close()
	return scanWorkspaces(rows)
}

// WorkspaceUpdatePatch contains optional fields. nil pointers mean "don't update".
// ClearPendingMerge, when true, sets pending_merge to NULL regardless of PendingMerge.
type WorkspaceUpdatePatch struct {
	Status            *types.WorkspaceStatus
	WorktreePath      *string
	TargetRef         *string
	PendingMerge      *types.PendingMerge
	ClearPendingMerge bool
	ProcessPID        *int
	Model             *string
	StartedAt         *time.Time
	CompletedAt       *time.Time
	TokensIn          *int
	TokensOut         *int
	CostUSD           *float64
	Orchestrator      *bool
}

// UpdateWorkspace applies the non-nil patch fields and bumps updated_at.
// Returns ErrNotFound if no row matches id.
func UpdateWorkspace(ctx context.Context, db *sql.DB, id string, patch WorkspaceUpdatePatch) (*types.Workspace, error) {
	sets, args, err := patch.setClause()
	if err != nil {
		return nil, err
	}
	args = append(args, id)

	//nolint:gosec // sets holds static column identifiers only; values go through ? placeholders.
	query := "UPDATE workspaces SET " + strings.Join(sets, ", ") + " WHERE id = ?;"
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update workspace: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrNotFound
	}
	return GetWorkspace(ctx, db, id)
}

// setClause translates the non-nil patch fields into SET fragments and bound
// args. updated_at is always touched.
func (p WorkspaceUpdatePatch) setClause() (sets []string, args []any, err error) {
	sets = []string{"updated_at = CURRENT_TIMESTAMP"}
	args = []any{}
	if p.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, string(*p.Status))
	}
	if p.WorktreePath != nil {
		sets = append(sets, "worktree_path = ?")
		args = append(args, *p.WorktreePath)
	}
	if p.TargetRef != nil {
		sets = append(sets, "target_ref = ?")
		args = append(args, *p.TargetRef)
	}
	if p.ClearPendingMerge {
		sets = append(sets, "pending_merge = NULL")
	} else if p.PendingMerge != nil {
		raw, mErr := json.Marshal(*p.PendingMerge)
		if mErr != nil {
			return nil, nil, fmt.Errorf("marshal pending_merge: %w", mErr)
		}
		sets = append(sets, "pending_merge = ?")
		args = append(args, string(raw))
	}
	if p.ProcessPID != nil {
		sets = append(sets, "process_pid = ?")
		args = append(args, *p.ProcessPID)
	}
	if p.Model != nil {
		sets = append(sets, "model = ?")
		args = append(args, *p.Model)
	}
	if p.StartedAt != nil {
		sets = append(sets, "started_at = ?")
		args = append(args, *p.StartedAt)
	}
	if p.CompletedAt != nil {
		sets = append(sets, "completed_at = ?")
		args = append(args, *p.CompletedAt)
	}
	if p.TokensIn != nil {
		sets = append(sets, "tokens_in = ?")
		args = append(args, *p.TokensIn)
	}
	if p.TokensOut != nil {
		sets = append(sets, "tokens_out = ?")
		args = append(args, *p.TokensOut)
	}
	if p.CostUSD != nil {
		sets = append(sets, "cost_usd = ?")
		args = append(args, *p.CostUSD)
	}
	if p.Orchestrator != nil {
		sets = append(sets, "orchestrator = ?")
		args = append(args, *p.Orchestrator)
	}
	return sets, args, nil
}

// DeleteWorkspace removes a workspace by id. Idempotent.
// Returns ErrNotFound if no row matches.
func DeleteWorkspace(ctx context.Context, db *sql.DB, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func scanWorkspaces(rows *sql.Rows) ([]*types.Workspace, error) {
	var out []*types.Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func scanWorkspace(s scanner) (*types.Workspace, error) {
	w := &types.Workspace{}
	var (
		issueID     sql.NullString
		agentType   string
		status      string
		pendingJSON sql.NullString
		startedAt   sql.NullTime
		completedAt sql.NullTime
	)
	err := s.Scan(
		&w.ID, &issueID, &w.ProjectID, &w.Branch, &agentType, &w.Model, &status,
		&w.Prompt, &w.WorktreePath, &w.TargetRef, &pendingJSON, &w.Orchestrator,
		&w.TokensIn, &w.TokensOut, &w.CostUSD, &startedAt, &completedAt,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan workspace: %w", err)
	}
	if issueID.Valid {
		v := issueID.String
		w.IssueID = &v
	}
	w.AgentType = types.AgentType(agentType)
	w.Status = types.WorkspaceStatus(status)
	if pendingJSON.Valid && pendingJSON.String != "" && pendingJSON.String != "null" {
		var pm types.PendingMerge
		if err := json.Unmarshal([]byte(pendingJSON.String), &pm); err != nil {
			return nil, fmt.Errorf("unmarshal pending_merge %q: %w", pendingJSON.String, err)
		}
		w.PendingMerge = &pm
	}
	if startedAt.Valid {
		t := startedAt.Time
		w.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		w.CompletedAt = &t
	}
	return w, nil
}
