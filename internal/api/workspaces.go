package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/types"
	"github.com/ijaihundal/ctrlroom/internal/workspace"
)

type workspaceRequest struct {
	ProjectID string  `json:"project_id"`
	IssueID   *string `json:"issue_id"`
	AgentType string  `json:"agent_type"`
	Model     string  `json:"model"`
	Prompt    string  `json:"prompt"`
}

type workspaceResponse struct {
	ID           string              `json:"id"`
	IssueID      *string             `json:"issue_id"`
	ProjectID    string              `json:"project_id"`
	Branch       string              `json:"branch"`
	AgentType    string              `json:"agent_type"`
	Model        string              `json:"model"`
	Status       string              `json:"status"`
	Prompt       string              `json:"prompt"`
	WorktreePath string              `json:"worktree_path"`
	TargetRef    string              `json:"target_ref"`
	PendingMerge *types.PendingMerge `json:"pending_merge"`
	Orchestrator bool                `json:"orchestrator"`
	StartedAt    *string             `json:"started_at"`
	CompletedAt  *string             `json:"completed_at"`
	CreatedAt    string              `json:"created_at"`
	UpdatedAt    string              `json:"updated_at"`
}

type workspaceListResponse struct {
	Workspaces []workspaceResponse `json:"workspaces"`
}

type diffResponse struct {
	Diff string `json:"diff"`
}

type mergeResponse struct {
	Merged    bool              `json:"merged"`
	Conflicts []string          `json:"conflicts,omitempty"`
	Target    string            `json:"target"`
	Workspace workspaceResponse `json:"workspace"`
}

type messageRequest struct {
	Content string `json:"content"`
	Kind    string `json:"kind"`
}

func ptrTimeRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func toWorkspaceResponse(ws *types.Workspace) workspaceResponse {
	return workspaceResponse{
		ID:           ws.ID,
		IssueID:      ws.IssueID,
		ProjectID:    ws.ProjectID,
		Branch:       ws.Branch,
		AgentType:    string(ws.AgentType),
		Model:        ws.Model,
		Status:       string(ws.Status),
		Prompt:       ws.Prompt,
		WorktreePath: ws.WorktreePath,
		TargetRef:    ws.TargetRef,
		PendingMerge: ws.PendingMerge,
		Orchestrator: ws.Orchestrator,
		StartedAt:    ptrTimeRFC3339(ws.StartedAt),
		CompletedAt:  ptrTimeRFC3339(ws.CompletedAt),
		CreatedAt:    ws.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    ws.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req workspaceRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid_body", "Request body could not be parsed: "+err.Error())
		return
	}

	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if req.ProjectID == "" {
		badRequest(w, r, "missing_project_id", "project_id is required")
		return
	}

	agentType := types.AgentType(strings.TrimSpace(req.AgentType))
	if !agentType.IsValid() {
		badRequest(w, r, "invalid_agent_type", "agent_type must be one of: claude, codex, opencode")
		return
	}

	proj, err := db.GetProject(r.Context(), s.db, req.ProjectID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Project not found")
			return
		}
		internalError(w, r, "Failed to get project", err)
		return
	}

	if req.IssueID != nil && *req.IssueID != "" {
		issue, gerr := db.GetIssue(r.Context(), s.db, *req.IssueID)
		if gerr != nil {
			if errors.Is(gerr, db.ErrNotFound) {
				notFound(w, r, "Issue not found")
				return
			}
			internalError(w, r, "Failed to get issue", err)
			return
		}
		if issue.ProjectID != proj.ID {
			badRequest(w, r, "issue_project_mismatch", "issue does not belong to this project")
			return
		}
	}

	ws, err := db.CreateWorkspace(r.Context(), s.db, db.WorkspaceCreateParams{
		ProjectID: req.ProjectID,
		IssueID:   req.IssueID,
		AgentType: agentType,
		Model:     req.Model,
		Prompt:    req.Prompt,
		Status:    types.WorkspaceQueued,
	})
	if err != nil {
		internalError(w, r, "Failed to create workspace", err)
		return
	}

	if s.workspaceMgr != nil {
		prepared, perr := s.workspaceMgr.Prepare(r.Context(), ws.ID)
		if perr != nil {
			s.logger.Error("prepare workspace",
				"workspace_id", ws.ID, "err", perr)
			if refreshed, rerr := db.GetWorkspace(r.Context(), s.db, ws.ID); rerr == nil {
				ws = refreshed
			}
		} else {
			ws = prepared
		}
	}

	_ = writeJSON(w, http.StatusCreated, toWorkspaceResponse(ws))
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ws, err := db.GetWorkspace(r.Context(), s.db, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Workspace not found")
			return
		}
		internalError(w, r, "Failed to get workspace", err)
		return
	}
	_ = writeJSON(w, http.StatusOK, toWorkspaceResponse(ws))
}

func (s *Server) handleListWorkspacesByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	if _, err := db.GetProject(r.Context(), s.db, projectID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			notFound(w, r, "Project not found")
			return
		}
		internalError(w, r, "Failed to get project", err)
		return
	}

	list, err := db.ListWorkspacesByProject(r.Context(), s.db, projectID)
	if err != nil {
		internalError(w, r, "Failed to list workspaces", err)
		return
	}
	out := make([]workspaceResponse, 0, len(list))
	for _, ws := range list {
		out = append(out, toWorkspaceResponse(ws))
	}
	_ = writeJSON(w, http.StatusOK, workspaceListResponse{Workspaces: out})
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if s.workspaceMgr == nil {
		internalError(w, r, "Workspace manager not configured", nil)
		return
	}

	out, err := s.workspaceMgr.Diff(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, workspace.ErrWorkspaceNotFound), errors.Is(err, workspace.ErrProjectNotFound):
			notFound(w, r, "Workspace not found")
		case errors.Is(err, workspace.ErrNotPrepared):
			writeError(w, r, http.StatusConflict, "not_prepared", "Workspace worktree is not prepared", nil)
		default:
			internalError(w, r, "Failed to compute diff", err)
		}
		return
	}
	_ = writeJSON(w, http.StatusOK, diffResponse{Diff: out})
}

func (s *Server) handleStopWorkspace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if s.workspaceMgr == nil {
		internalError(w, r, "Workspace manager not configured", nil)
		return
	}

	ws, err := s.workspaceMgr.Cancel(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, workspace.ErrWorkspaceNotFound), errors.Is(err, workspace.ErrProjectNotFound):
			notFound(w, r, "Workspace not found")
		case errors.Is(err, workspace.ErrInvalidTransition):
			writeError(w, r, http.StatusConflict, "invalid_transition",
				"Workspace cannot be cancelled from its current state", nil)
		default:
			internalError(w, r, "Failed to stop workspace", err)
		}
		return
	}
	_ = writeJSON(w, http.StatusOK, toWorkspaceResponse(ws))
}

func (s *Server) handleMergeWorkspace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if s.workspaceMgr == nil {
		internalError(w, r, "Workspace manager not configured", nil)
		return
	}

	resp, err := s.workspaceMgr.Merge(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, workspace.ErrWorkspaceNotFound), errors.Is(err, workspace.ErrProjectNotFound):
			notFound(w, r, "Workspace not found")
		case errors.Is(err, workspace.ErrInvalidTransition):
			writeError(w, r, http.StatusConflict, "invalid_transition",
				"Merge requires the workspace to be completed or resolving_conflict", nil)
		default:
			internalError(w, r, "Failed to merge workspace", err)
		}
		return
	}

	payload := mergeResponse{
		Merged:    resp.Merged,
		Conflicts: resp.Conflicts,
		Target:    resp.Target,
		Workspace: toWorkspaceResponse(resp.Workspace),
	}

	if resp.Merged {
		_ = writeJSON(w, http.StatusOK, payload)
		return
	}

	if resp.Workspace.Status == types.WorkspaceConflictStuck {
		writeError(w, r, http.StatusConflict, "conflict_stuck",
			"Merge conflicts could not be resolved within max attempts",
			map[string]any{
				"conflicts": resp.Conflicts,
				"target":    resp.Target,
			})
		return
	}

	_ = writeJSON(w, http.StatusConflict, payload)
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	var req messageRequest
	_ = decodeJSON(r, &req)
	writeError(w, r, http.StatusNotImplemented, "not_implemented",
		"Workspace messaging is implemented in Phase 3", nil)
}
