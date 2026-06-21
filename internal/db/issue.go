package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ijaihundal/ctrlroom/internal/types"
)

// IssueCreateParams holds the inputs for creating an issue.
// ID, Status, and SortOrder are assigned by the DB layer.
type IssueCreateParams struct {
	ProjectID   string
	Title       string
	Description string
	Priority    int
	Tags        []string
}

// CreateIssue inserts a new issue. SortOrder is computed as MAX(sort_order)+1
// within the project. Tags are marshaled to JSON ("[]" if empty, never null).
func CreateIssue(ctx context.Context, db *sql.DB, p IssueCreateParams) (*types.Issue, error) {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}

	issue := &types.Issue{
		ID:          NewID(),
		ProjectID:   p.ProjectID,
		Title:       p.Title,
		Description: p.Description,
		Status:      types.IssueTodo,
		Priority:    p.Priority,
		Tags:        tags,
	}

	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM issues WHERE project_id = ?;`, p.ProjectID,
	).Scan(&issue.SortOrder); err != nil {
		return nil, fmt.Errorf("compute sort_order: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO issues (id, project_id, title, description, status, priority, tags, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`,
		issue.ID, issue.ProjectID, issue.Title, issue.Description,
		string(issue.Status), issue.Priority, string(tagsJSON), issue.SortOrder,
	)
	if err != nil {
		return nil, fmt.Errorf("insert issue: %w", err)
	}
	return GetIssue(ctx, db, issue.ID)
}

// GetIssue returns the issue with the given id, or ErrNotFound.
func GetIssue(ctx context.Context, db *sql.DB, id string) (*types.Issue, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, project_id, title, COALESCE(description, ''), status, priority,
		       tags, sort_order, created_at, updated_at
		FROM issues WHERE id = ?;
	`, id)
	return scanIssue(row)
}

// ListIssuesByProject returns all issues for a project ordered by sort_order, created_at.
func ListIssuesByProject(ctx context.Context, db *sql.DB, projectID string) ([]*types.Issue, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, project_id, title, COALESCE(description, ''), status, priority,
		       tags, sort_order, created_at, updated_at
		FROM issues WHERE project_id = ?
		ORDER BY sort_order, created_at;
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query issues by project: %w", err)
	}
	defer rows.Close()
	return scanIssues(rows)
}

// ListIssuesByProjectAndStatus returns issues for a project filtered by status.
func ListIssuesByProjectAndStatus(
	ctx context.Context, db *sql.DB, projectID string, status types.IssueStatus,
) ([]*types.Issue, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, project_id, title, COALESCE(description, ''), status, priority,
		       tags, sort_order, created_at, updated_at
		FROM issues WHERE project_id = ? AND status = ?
		ORDER BY sort_order, created_at;
	`, projectID, string(status))
	if err != nil {
		return nil, fmt.Errorf("query issues by project+status: %w", err)
	}
	defer rows.Close()
	return scanIssues(rows)
}

// IssueUpdatePatch contains optional fields. nil pointers mean "don't update".
type IssueUpdatePatch struct {
	Title       *string
	Description *string
	Status      *types.IssueStatus
	Priority    *int
	Tags        *[]string
}

// UpdateIssue applies the non-nil patch fields and bumps updated_at.
// Returns ErrNotFound if no row matches id.
func UpdateIssue(ctx context.Context, db *sql.DB, id string, patch IssueUpdatePatch) (*types.Issue, error) {
	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
	if patch.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *patch.Title)
	}
	if patch.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *patch.Description)
	}
	if patch.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, string(*patch.Status))
	}
	if patch.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *patch.Priority)
	}
	if patch.Tags != nil {
		tags := *patch.Tags
		if tags == nil {
			tags = []string{}
		}
		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return nil, fmt.Errorf("marshal tags: %w", err)
		}
		sets = append(sets, "tags = ?")
		args = append(args, string(tagsJSON))
	}
	args = append(args, id)

	//nolint:gosec // sets holds static column identifiers only; values go through ? placeholders.
	query := "UPDATE issues SET " + strings.Join(sets, ", ") + " WHERE id = ?;"
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update issue: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrNotFound
	}
	return GetIssue(ctx, db, id)
}

// ReorderIssues sets sort_order = index in the given slice, in a single tx.
// All issues must belong to the same project (caller validates); the project_id
// is included in the UPDATE WHERE as a safety check.
func ReorderIssues(ctx context.Context, db *sql.DB, projectID string, orderedIDs []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reorder tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is a no-op after commit

	for i, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE issues SET sort_order = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND project_id = ?;
		`, i, id, projectID); err != nil {
			return fmt.Errorf("reorder issue %d (%s): %w", i, id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder: %w", err)
	}
	return nil
}

// DeleteIssue removes an issue by id. Idempotent.
// Returns ErrNotFound if no row matches.
func DeleteIssue(ctx context.Context, db *sql.DB, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM issues WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("delete issue: %w", err)
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

func scanIssues(rows *sql.Rows) ([]*types.Issue, error) {
	var out []*types.Issue
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, issue)
	}
	return out, rows.Err()
}

func scanIssue(s scanner) (*types.Issue, error) {
	i := &types.Issue{}
	var (
		status   string
		tagsJSON sql.NullString
	)
	err := s.Scan(
		&i.ID, &i.ProjectID, &i.Title, &i.Description, &status, &i.Priority,
		&tagsJSON, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan issue: %w", err)
	}
	i.Status = types.IssueStatus(status)
	i.Tags = []string{}
	if tagsJSON.Valid && tagsJSON.String != "" && tagsJSON.String != "null" {
		if err := json.Unmarshal([]byte(tagsJSON.String), &i.Tags); err != nil {
			return nil, fmt.Errorf("unmarshal tags %q: %w", tagsJSON.String, err)
		}
	}
	if i.Tags == nil {
		i.Tags = []string{}
	}
	return i, nil
}
