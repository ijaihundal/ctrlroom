package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ijaihundal/ctrlroom/internal/db"
)

type messageResponse struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Role        string         `json:"role"`
	Content     string         `json:"content,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Seq         int64          `json:"seq"`
	CreatedAt   string         `json:"created_at"`
}

type messageListResponse struct {
	Messages []messageResponse `json:"messages"`
	LastSeq  int64             `json:"last_seq"`
}

func toMessageResponse(m *db.Message) messageResponse {
	return messageResponse{
		ID:          m.ID,
		WorkspaceID: m.WorkspaceID,
		Role:        m.Role,
		Content:     m.Content,
		Metadata:    m.Metadata,
		Seq:         m.Seq,
		CreatedAt:   m.CreatedAt.Format("2006-01-02T15:04:05.999Z07:00"),
	}
}

// handleListMessages returns persisted messages for a workspace with seq > ?since=N.
// Used for initial load on page open and for catching up after a WebSocket drop.
// Caps at ?limit=M (default 500).
func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var since int64 = -1
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			badRequest(w, r, "invalid_since", "since must be an integer")
			return
		}
		since = n
	}

	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		limit = n
	}

	// Verify workspace exists.
	if _, err := db.GetWorkspace(r.Context(), s.db, id); err != nil {
		notFound(w, r, "Workspace not found")
		return
	}

	msgs, err := db.ListMessages(r.Context(), s.db, id, since, limit)
	if err != nil {
		internalError(w, r, "Failed to list messages", err)
		return
	}

	last, err := db.LastSeq(r.Context(), s.db, id)
	if err != nil {
		internalError(w, r, "Failed to get last seq", err)
		return
	}

	out := make([]messageResponse, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, toMessageResponse(m))
	}
	_ = writeJSON(w, http.StatusOK, messageListResponse{Messages: out, LastSeq: last})
}
