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

type projectRequest struct {
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	RepoPath      string  `json:"repo_path"`
	DefaultBranch *string `json:"default_branch"`
	ApprovalMode  string  `json:"approval_mode"`
}

type projectResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	RepoPath      string `json:"repo_path"`
	DefaultBranch string `json:"default_branch"`
	ApprovalMode  string `json:"approval_mode"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type projectListResponse struct {
	Projects []projectResponse `json:"projects"`
}

func toProjectResponse(p *types.Project) projectResponse {
	return projectResponse{
		ID:            p.ID,
		Name:          p.Name,
		Description:   p.Description,
		RepoPath:      p.RepoPath,
		DefaultBranch: p.DefaultBranch,
		ApprovalMode:  string(p.ApprovalMode),
		CreatedAt:     p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     p.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := db.ListProjects(r.Context(), s.db)
	if err != nil {
		internalError(w, r, "Failed to list projects", err)
		return
	}
	out := make([]projectResponse, 0, len(projects))
	for _, p := range projects {
		out = append(out, toProjectResponse(p))
	}
	_ = writeJSON(w, http.StatusOK, projectListResponse{Projects: out})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req projectRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid_body", "Request body could not be parsed: "+err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.RepoPath = strings.TrimSpace(req.RepoPath)
	if req.Name == "" {
		badRequest(w, r, "missing_name", "name is required")
		return
	}
	if req.RepoPath == "" {
		badRequest(w, r, "missing_repo_path", "repo_path is required")
		return
	}

	var approvalMode types.ApprovalMode
	if req.ApprovalMode != "" {
		approvalMode = types.ApprovalMode(req.ApprovalMode)
		if !approvalMode.IsValid() {
			badRequest(w, r, "invalid_approval_mode", "approval_mode must be one of: autonomous, prompt, on_failure")
			return
		}
	}

	if s.gitClient == nil || !s.gitClient.IsRepo(req.RepoPath) {
		badRequest(w, r, "invalid_repo_path", "repo_path does not point to a git repository")
		return
	}

	defaultBranch := ""
	if req.DefaultBranch != nil {
		defaultBranch = strings.TrimSpace(*req.DefaultBranch)
	}
	if defaultBranch == "" {
		detected, err := s.gitClient.DefaultBranch(req.RepoPath)
		if err != nil {
			internalError(w, r, "Failed to detect default branch", err)
			return
		}
		defaultBranch = detected
	}

	project, err := db.CreateProject(r.Context(), s.db, db.ProjectCreateParams{
		Name:          req.Name,
		Description:   req.Description,
		RepoPath:      req.RepoPath,
		DefaultBranch: defaultBranch,
		ApprovalMode:  approvalMode,
	})
	if err != nil {
		internalError(w, r, "Failed to create project", err)
		return
	}

	_ = writeJSON(w, http.StatusCreated, toProjectResponse(project))
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	project, err := db.GetProject(r.Context(), s.db, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Project not found")
			return
		}
		internalError(w, r, "Failed to get project", err)
		return
	}
	_ = writeJSON(w, http.StatusOK, toProjectResponse(project))
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	patch := db.ProjectUpdatePatch{}
	var req projectRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, ErrBodyEmpty) {
		badRequest(w, r, "invalid_body", "Request body could not be parsed: "+err.Error())
		return
	}

	if req.Name != "" {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			badRequest(w, r, "invalid_name", "name must not be blank")
			return
		}
		patch.Name = &name
	}
	if req.Description != "" {
		patch.Description = &req.Description
	}
	if req.DefaultBranch != nil {
		b := strings.TrimSpace(*req.DefaultBranch)
		patch.DefaultBranch = &b
	}
	var approvalMode types.ApprovalMode
	if req.ApprovalMode != "" {
		approvalMode = types.ApprovalMode(req.ApprovalMode)
		if !approvalMode.IsValid() {
			badRequest(w, r, "invalid_approval_mode", "approval_mode must be one of: autonomous, prompt, on_failure")
			return
		}
		patch.ApprovalMode = &approvalMode
	}

	project, err := db.UpdateProject(r.Context(), s.db, id, patch)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Project not found")
			return
		}
		internalError(w, r, "Failed to update project", err)
		return
	}
	_ = writeJSON(w, http.StatusOK, toProjectResponse(project))
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if s.workspaceMgr != nil {
		workspaces, err := db.ListWorkspacesByProject(r.Context(), s.db, id)
		if err != nil {
			internalError(w, r, "Failed to list workspaces for cleanup", err)
			return
		}
		for _, ws := range workspaces {
			if cerr := s.workspaceMgr.Cleanup(r.Context(), ws.ID); cerr != nil {
				s.logger.Warn("project delete: workspace cleanup failed",
					"workspace_id", ws.ID, "project_id", id, "err", cerr)
			}
		}
	}

	if err := db.DeleteProject(r.Context(), s.db, id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Project not found")
			return
		}
		internalError(w, r, "Failed to delete project", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
