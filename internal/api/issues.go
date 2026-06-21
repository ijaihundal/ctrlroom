package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/types"
)

type issueRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    *int     `json:"priority"`
	Tags        []string `json:"tags"`
	Status      string   `json:"status"`
}

type issueResponse struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"project_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Priority    int      `json:"priority"`
	Tags        []string `json:"tags"`
	SortOrder   int      `json:"sort_order"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type issueListResponse struct {
	Issues []issueResponse `json:"issues"`
}

type reorderRequest struct {
	IDs []string `json:"ids"`
}

func toIssueResponse(i *types.Issue) issueResponse {
	tags := i.Tags
	if tags == nil {
		tags = []string{}
	}
	return issueResponse{
		ID:          i.ID,
		ProjectID:   i.ProjectID,
		Title:       i.Title,
		Description: i.Description,
		Status:      string(i.Status),
		Priority:    i.Priority,
		Tags:        tags,
		SortOrder:   i.SortOrder,
		CreatedAt:   i.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   i.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	if _, err := db.GetProject(r.Context(), s.db, projectID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Project not found")
			return
		}
		internalError(w, r, "Failed to get project", err)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	var issues []*types.Issue
	var err error
	if statusFilter != "" {
		st := types.IssueStatus(statusFilter)
		if !st.IsValid() {
			badRequest(w, r, "invalid_status", "status must be one of: todo, in_progress, review, done")
			return
		}
		issues, err = db.ListIssuesByProjectAndStatus(r.Context(), s.db, projectID, st)
	} else {
		issues, err = db.ListIssuesByProject(r.Context(), s.db, projectID)
	}
	if err != nil {
		internalError(w, r, "Failed to list issues", err)
		return
	}

	out := make([]issueResponse, 0, len(issues))
	for _, i := range issues {
		out = append(out, toIssueResponse(i))
	}
	_ = writeJSON(w, http.StatusOK, issueListResponse{Issues: out})
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	var req issueRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid_body", "Request body could not be parsed: "+err.Error())
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		badRequest(w, r, "missing_title", "title is required")
		return
	}

	if _, err := db.GetProject(r.Context(), s.db, projectID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Project not found")
			return
		}
		internalError(w, r, "Failed to get project", err)
		return
	}

	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	priority := 0
	if req.Priority != nil {
		priority = *req.Priority
	}

	issue, err := db.CreateIssue(r.Context(), s.db, db.IssueCreateParams{
		ProjectID:   projectID,
		Title:       req.Title,
		Description: req.Description,
		Priority:    priority,
		Tags:        tags,
	})
	if err != nil {
		internalError(w, r, "Failed to create issue", err)
		return
	}
	_ = writeJSON(w, http.StatusCreated, toIssueResponse(issue))
}

func (s *Server) handleReorderIssues(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	var req reorderRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid_body", "Request body could not be parsed: "+err.Error())
		return
	}
	if req.IDs == nil {
		req.IDs = []string{}
	}

	if _, err := db.GetProject(r.Context(), s.db, projectID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Project not found")
			return
		}
		internalError(w, r, "Failed to get project", err)
		return
	}

	if err := db.ReorderIssues(r.Context(), s.db, projectID, req.IDs); err != nil {
		internalError(w, r, "Failed to reorder issues", err)
		return
	}

	issues, err := db.ListIssuesByProject(r.Context(), s.db, projectID)
	if err != nil {
		internalError(w, r, "Failed to list issues after reorder", err)
		return
	}
	out := make([]issueResponse, 0, len(issues))
	for _, i := range issues {
		out = append(out, toIssueResponse(i))
	}
	_ = writeJSON(w, http.StatusOK, issueListResponse{Issues: out})
}

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req issueRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, ErrBodyEmpty) {
		badRequest(w, r, "invalid_body", "Request body could not be parsed: "+err.Error())
		return
	}

	patch := db.IssueUpdatePatch{}
	if req.Title != "" {
		title := strings.TrimSpace(req.Title)
		if title == "" {
			badRequest(w, r, "invalid_title", "title must not be blank")
			return
		}
		patch.Title = &title
	}
	if req.Description != "" {
		patch.Description = &req.Description
	}
	if req.Status != "" {
		st := types.IssueStatus(req.Status)
		if !st.IsValid() {
			badRequest(w, r, "invalid_status", "status must be one of: todo, in_progress, review, done")
			return
		}
		patch.Status = &st
	}
	if req.Priority != nil {
		patch.Priority = req.Priority
	}
	if req.Tags != nil {
		patch.Tags = &req.Tags
	}

	issue, err := db.UpdateIssue(r.Context(), s.db, id, patch)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Issue not found")
			return
		}
		internalError(w, r, "Failed to update issue", err)
		return
	}
	_ = writeJSON(w, http.StatusOK, toIssueResponse(issue))
}

func (s *Server) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := db.DeleteIssue(r.Context(), s.db, id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Issue not found")
			return
		}
		internalError(w, r, "Failed to delete issue", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
