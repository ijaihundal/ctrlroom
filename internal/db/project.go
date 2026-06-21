package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// ProjectCreateParams holds the inputs for creating a project.
// ID is assigned by the DB layer; CreatedAt/UpdatedAt are defaulted by SQL.
type ProjectCreateParams struct {
	Name          string
	Description   string
	RepoPath      string
	DefaultBranch string
	ApprovalMode  types.ApprovalMode
}

// CreateProject inserts a new project with a generated ULID and returns the populated row.
func CreateProject(ctx context.Context, db *sql.DB, p ProjectCreateParams) (*types.Project, error) {
	if p.ApprovalMode == "" {
		p.ApprovalMode = types.ApprovalAutonomous
	}
	project := &types.Project{
		ID:            NewID(),
		Name:          p.Name,
		Description:   p.Description,
		RepoPath:      p.RepoPath,
		DefaultBranch: p.DefaultBranch,
		ApprovalMode:  p.ApprovalMode,
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, description, repo_path, default_branch, approval_mode)
		VALUES (?, ?, ?, ?, ?, ?);
	`,
		project.ID, project.Name, project.Description, project.RepoPath,
		project.DefaultBranch, string(project.ApprovalMode),
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}
	return GetProject(ctx, db, project.ID)
}

// GetProject returns the project with the given id, or ErrNotFound.
func GetProject(ctx context.Context, db *sql.DB, id string) (*types.Project, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(description, ''), repo_path, default_branch, approval_mode, created_at, updated_at
		FROM projects WHERE id = ?;
	`, id)
	return scanProject(row)
}

// ListProjects returns all projects ordered by created_at.
func ListProjects(ctx context.Context, db *sql.DB) ([]*types.Project, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, COALESCE(description, ''), repo_path, default_branch, approval_mode, created_at, updated_at
		FROM projects ORDER BY created_at, id;
	`)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()
	var out []*types.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectUpdatePatch contains optional fields. nil pointers mean "don't update".
type ProjectUpdatePatch struct {
	Name          *string
	Description   *string
	DefaultBranch *string
	ApprovalMode  *types.ApprovalMode
}

// UpdateProject applies the non-nil patch fields and bumps updated_at.
// Returns ErrNotFound if no row matches id.
func UpdateProject(ctx context.Context, db *sql.DB, id string, patch ProjectUpdatePatch) (*types.Project, error) {
	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
	if patch.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *patch.Description)
	}
	if patch.DefaultBranch != nil {
		sets = append(sets, "default_branch = ?")
		args = append(args, *patch.DefaultBranch)
	}
	if patch.ApprovalMode != nil {
		sets = append(sets, "approval_mode = ?")
		args = append(args, string(*patch.ApprovalMode))
	}
	args = append(args, id)

	//nolint:gosec // sets holds static column identifiers only; values go through ? placeholders.
	query := "UPDATE projects SET " + strings.Join(sets, ", ") + " WHERE id = ?;"
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrNotFound
	}
	return GetProject(ctx, db, id)
}

// DeleteProject removes a project by id. Cascades to issues (and their workspaces).
// Returns ErrNotFound if no row matches.
func DeleteProject(ctx context.Context, db *sql.DB, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
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

// scanner abstracts *sql.Row and *sql.Rows so scanProject can serve both.
type scanner interface {
	Scan(dest ...any) error
}

func scanProject(s scanner) (*types.Project, error) {
	p := &types.Project{}
	var approvalMode string
	err := s.Scan(
		&p.ID, &p.Name, &p.Description, &p.RepoPath, &p.DefaultBranch,
		&approvalMode, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	p.ApprovalMode = types.ApprovalMode(approvalMode)
	return p, nil
}
