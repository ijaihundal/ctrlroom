package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/ijaihundal/ctrlroom/internal/db"
)

// streamFrame is the JSON shape sent over the WebSocket.
type streamFrame struct {
	Seq   int64           `json:"seq,omitempty"`
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event,omitempty"`
	TS    string          `json:"ts"`
}

// handleStreamWorkspace upgrades to a WebSocket and streams live agent events
// for a workspace. Performs an initial replay of persisted messages with seq >
// ?since=N (default 0), then live fan-out from the agent Manager.
//
// Client → server messages are accepted but currently ignored. Future:
// follow_up / approve / stop via WS.
func (s *Server) handleStreamWorkspace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if _, err := db.GetWorkspace(r.Context(), s.db, id); err != nil {
		notFound(w, r, "Workspace not found")
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		s.logger.Warn("ws accept", "workspace_id", id, "err", err)
		return
	}
	defer func() { _ = c.CloseNow() }()

	c.SetReadLimit(64 * 1024)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	liveCh, unsub := s.agentMgr.Subscribe(id)
	defer unsub()

	since := int64(-1)
	if v := r.URL.Query().Get("since"); v != "" {
		if n, parseErr := strconv.ParseInt(v, 10, 64); parseErr == nil {
			since = n
		}
	}
	if err := s.replayPersisted(ctx, c, id, since); err != nil {
		s.logger.Warn("ws replay", "workspace_id", id, "err", err)
		return
	}

	go func() {
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()

	var writeMu sync.Mutex
	send := func(frame streamFrame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return wsWriteJSON(ctx, c, frame)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-liveCh:
			if !ok {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				s.logger.Warn("ws marshal event", "workspace_id", id, "err", err)
				continue
			}
			if err := send(streamFrame{
				Type:  "agent",
				Event: payload,
				TS:    time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				s.logger.Debug("ws write closed", "workspace_id", id, "err", err)
				return
			}
		}
	}
}

func (s *Server) replayPersisted(ctx context.Context, c *websocket.Conn, workspaceID string, since int64) error {
	msgs, err := db.ListMessages(ctx, s.db, workspaceID, since, 1000)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		envelope := map[string]any{
			"role": m.Role,
			"body": m.Content,
			"meta": m.Metadata,
		}
		if t, ok := m.Metadata["type"].(string); ok {
			envelope["type"] = t
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			continue
		}
		frame := streamFrame{
			Seq:   m.Seq,
			Type:  "replay",
			Event: payload,
			TS:    m.CreatedAt.Format(time.RFC3339Nano),
		}
		if err := wsWriteJSON(ctx, c, frame); err != nil {
			return err
		}
	}
	return nil
}

func wsWriteJSON(ctx context.Context, c *websocket.Conn, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, payload)
}
