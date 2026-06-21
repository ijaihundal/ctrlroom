package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/types"
	"github.com/ijaihundal/ctrlroom/internal/workspace"
)

// Manager owns the lifetime of running agent sessions. It is the bridge between
// HTTP handlers (which call Start/SendPrompt/Stop/Complete) and the per-workspace
// AgentAdapter (which talks to the actual subprocess).
//
// Concurrency: one session per workspace ID. Methods are safe for concurrent use.
type Manager struct {
	db     *sql.DB
	logger *slog.Logger

	factory AdapterFactory

	mu       sync.Mutex
	sessions map[string]*session

	subsMu sync.Mutex
	subs   map[string]map[string]chan AgentEvent
}

func NewManager(database *sql.DB, factory AdapterFactory, logger *slog.Logger) *Manager {
	return &Manager{
		db:       database,
		logger:   logger,
		factory:  factory,
		sessions: make(map[string]*session),
		subs:     make(map[string]map[string]chan AgentEvent),
	}
}

var (
	ErrWorkspaceNotFound  = errors.New("workspace not found")
	ErrWorkspaceNotActive = errors.New("workspace has no active agent session")
	ErrBusy               = errors.New("a turn is in flight; wait for it to finish or stop")
)

// Start spawns an agent for the workspace and sends the initial prompt.
// Status transition: idle → running.
//
// On success the workspace is in WorkspaceRunning. The drain goroutine runs in
// the background; events are persisted to the messages table and fanned out to
// subscribers. The workspace transitions to WorkspaceAwaitingInput when the
// adapter emits EventDone{reason:turn_complete}, or to WorkspaceFailed on error.
func (m *Manager) Start(ctx context.Context, workspaceID, prompt string) (AgentAdapter, error) {
	ws, err := db.GetWorkspace(ctx, m.db, workspaceID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrWorkspaceNotFound
		}
		return nil, fmt.Errorf("load workspace: %w", err)
	}

	if !workspace.CanTransition(ws.Status, types.WorkspaceRunning) {
		return nil, fmt.Errorf("%w: %s → running", workspace.ErrInvalidTransition, ws.Status)
	}
	if ws.WorktreePath == "" {
		return nil, fmt.Errorf("workspace %s has no worktree; call Prepare first", workspaceID)
	}

	adapter, err := m.factory(ws.AgentType)
	if err != nil {
		return nil, fmt.Errorf("factory: %w", err)
	}

	cfg := AgentConfig{
		AgentType: ws.AgentType,
		Model:     ws.Model,
	}
	if err := adapter.Spawn(ctx, ws.WorktreePath, cfg); err != nil {
		m.markFailed(ctx, ws.ID, fmt.Errorf("spawn: %w", err))
		return nil, fmt.Errorf("spawn: %w", err)
	}

	now := time.Now()
	running := types.WorkspaceRunning
	if _, err := db.UpdateWorkspace(ctx, m.db, ws.ID, db.WorkspaceUpdatePatch{
		Status:    &running,
		StartedAt: &now,
	}); err != nil {
		_ = adapter.Stop()
		return nil, fmt.Errorf("transition to running: %w", err)
	}

	sessCtx, cancel := context.WithCancel(context.Background())
	sess := &session{
		workspaceID: workspaceID,
		adapter:     adapter,
		cancel:      cancel,
	}

	m.mu.Lock()
	if existing, ok := m.sessions[workspaceID]; ok {
		m.mu.Unlock()
		cancel()
		_ = existing.adapter.Stop()
		return nil, fmt.Errorf("workspace %s already has an active session", workspaceID)
	}
	m.sessions[workspaceID] = sess
	m.mu.Unlock()

	if err := adapter.SendPrompt(ctx, prompt); err != nil {
		m.cleanupSession(workspaceID)
		m.markFailed(ctx, workspaceID, fmt.Errorf("initial prompt: %w", err))
		return nil, fmt.Errorf("initial prompt: %w", err)
	}

	if _, err := db.CreateMessage(ctx, m.db, workspaceID, "user", prompt, nil); err != nil {
		m.logger.Warn("persist user prompt", "workspace_id", workspaceID, "err", err)
	}

	go m.drain(sessCtx, workspaceID, adapter)
	return adapter, nil
}

func (m *Manager) SendPrompt(ctx context.Context, workspaceID, prompt string) error {
	m.mu.Lock()
	sess, ok := m.sessions[workspaceID]
	m.mu.Unlock()
	if !ok {
		return ErrWorkspaceNotActive
	}

	ws, err := db.GetWorkspace(ctx, m.db, workspaceID)
	if err != nil {
		return fmt.Errorf("reload workspace: %w", err)
	}
	if !workspace.CanTransition(ws.Status, types.WorkspaceRunning) {
		return fmt.Errorf("%w: %s → running", workspace.ErrInvalidTransition, ws.Status)
	}
	running := types.WorkspaceRunning
	if _, err := db.UpdateWorkspace(ctx, m.db, workspaceID, db.WorkspaceUpdatePatch{
		Status: &running,
	}); err != nil {
		return fmt.Errorf("transition to running: %w", err)
	}

	if err := sess.adapter.SendPrompt(ctx, prompt); err != nil {
		m.markFailed(ctx, workspaceID, fmt.Errorf("send prompt: %w", err))
		return fmt.Errorf("send prompt: %w", err)
	}

	if _, err := db.CreateMessage(ctx, m.db, workspaceID, "user", prompt, nil); err != nil {
		m.logger.Warn("persist user prompt", "workspace_id", workspaceID, "err", err)
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context, workspaceID string) error {
	m.mu.Lock()
	sess, ok := m.sessions[workspaceID]
	m.mu.Unlock()
	// Idempotent: if there's no active session, the workspace is already stopped.
	// Don't error — handlers may call Stop after the drain loop has naturally
	// cleaned up (e.g. after the adapter emitted a terminal event).
	if !ok {
		return nil
	}

	// Cancel the session context FIRST so the drain goroutine exits cleanly via
	// ctx.Done() rather than treating the channel close as a subprocess crash.
	if sess.cancel != nil {
		sess.cancel()
	}
	if err := sess.adapter.Stop(); err != nil {
		m.logger.Warn("adapter stop", "workspace_id", workspaceID, "err", err)
	}

	ws, err := db.GetWorkspace(ctx, m.db, workspaceID)
	if err == nil && !workspace.IsTerminal(ws.Status) && workspace.CanTransition(ws.Status, types.WorkspaceCancelled) {
		cancelled := types.WorkspaceCancelled
		now := time.Now()
		if _, err := db.UpdateWorkspace(ctx, m.db, workspaceID, db.WorkspaceUpdatePatch{
			Status:      &cancelled,
			CompletedAt: &now,
		}); err != nil {
			m.logger.Warn("mark cancelled", "workspace_id", workspaceID, "err", err)
		}
	}
	m.cleanupSession(workspaceID)
	return nil
}

func (m *Manager) Complete(ctx context.Context, workspaceID string) error {
	ws, err := db.GetWorkspace(ctx, m.db, workspaceID)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}
	if ws.Status == types.WorkspaceCompleted {
		return nil
	}
	if !workspace.CanTransition(ws.Status, types.WorkspaceCompleted) {
		return fmt.Errorf("%w: %s → completed", workspace.ErrInvalidTransition, ws.Status)
	}
	completed := types.WorkspaceCompleted
	now := time.Now()
	if _, err := db.UpdateWorkspace(ctx, m.db, workspaceID, db.WorkspaceUpdatePatch{
		Status:      &completed,
		CompletedAt: &now,
	}); err != nil {
		return fmt.Errorf("transition to completed: %w", err)
	}
	return nil
}

// Subscribe returns a receive-only channel of live agent events for a workspace,
// plus an unsubscribe function. Buffer cap 256. Drops on overflow are silent.
func (m *Manager) Subscribe(workspaceID string) (events <-chan AgentEvent, unsubscribe func()) {
	ch := make(chan AgentEvent, 256)
	subID := ulidLike()

	m.subsMu.Lock()
	if m.subs[workspaceID] == nil {
		m.subs[workspaceID] = make(map[string]chan AgentEvent)
	}
	m.subs[workspaceID][subID] = ch
	m.subsMu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			m.subsMu.Lock()
			delete(m.subs[workspaceID], subID)
			if len(m.subs[workspaceID]) == 0 {
				delete(m.subs, workspaceID)
			}
			m.subsMu.Unlock()
			close(ch)
		})
	}
}

// Active returns the workspace IDs that currently have running agent sessions.
func (m *Manager) Active() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		out = append(out, id)
	}
	return out
}

type session struct {
	workspaceID string
	adapter     AgentAdapter
	cancel      context.CancelFunc
}

func (m *Manager) drain(ctx context.Context, workspaceID string, adapter AgentAdapter) {
	defer m.cleanupSession(workspaceID)

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-adapter.Events():
			if !ok {
				// Channel closed = adapter terminated. If we didn't see an
				// EventDone{stopped/error} first, treat as unexpected failure.
				m.logger.Warn("adapter stream closed", "workspace_id", workspaceID)
				ws, err := db.GetWorkspace(ctx, m.db, workspaceID)
				if err == nil && !workspace.IsTerminal(ws.Status) {
					m.markFailed(context.Background(), workspaceID, errors.New("agent stream closed unexpectedly"))
				}
				return
			}
			m.handleEvent(ctx, workspaceID, ev)
			if IsTerminal(ev) {
				// EventDone ends a TURN, not the adapter. Transition the
				// workspace state but keep draining for the next turn.
				m.handleTerminal(context.Background(), workspaceID, ev)
			}
		}
	}
}

func (m *Manager) handleEvent(ctx context.Context, workspaceID string, ev AgentEvent) {
	if err := m.persistEvent(ctx, workspaceID, ev); err != nil {
		m.logger.Warn("persist event", "workspace_id", workspaceID, "event_type", ev.Type, "err", err)
	}
	if ev.Type == EventCost {
		m.applyCost(ctx, workspaceID, ev)
	}
	m.broadcast(workspaceID, ev)
}

func (m *Manager) persistEvent(ctx context.Context, workspaceID string, ev AgentEvent) error {
	role := MessageRole(ev)
	var metadata map[string]any
	if len(ev.Metadata) > 0 {
		metadata = ev.Metadata
	}
	_, err := db.CreateMessage(ctx, m.db, workspaceID, role, ev.Content, metadata)
	return err
}

func (m *Manager) applyCost(ctx context.Context, workspaceID string, ev AgentEvent) {
	c, ok := CostOf(ev)
	if !ok {
		return
	}
	ws, err := db.GetWorkspace(ctx, m.db, workspaceID)
	if err != nil {
		return
	}
	in := ws.TokensIn + int(c.TokensIn)
	out := ws.TokensOut + int(c.TokensOut)
	cost := ws.CostUSD + c.CostUSD
	if _, err := db.UpdateWorkspace(ctx, m.db, workspaceID, db.WorkspaceUpdatePatch{
		TokensIn:  &in,
		TokensOut: &out,
		CostUSD:   &cost,
	}); err != nil {
		m.logger.Warn("apply cost", "workspace_id", workspaceID, "err", err)
	}
}

func (m *Manager) broadcast(workspaceID string, ev AgentEvent) {
	m.subsMu.Lock()
	subs := m.subs[workspaceID]
	m.subsMu.Unlock()
	if len(subs) == 0 {
		return
	}
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (m *Manager) handleTerminal(ctx context.Context, workspaceID string, ev AgentEvent) {
	reason := DoneReasonOf(ev)
	switch reason {
	case DoneTurnComplete:
		awaiting := types.WorkspaceAwaitingInput
		if _, err := db.UpdateWorkspace(ctx, m.db, workspaceID, db.WorkspaceUpdatePatch{Status: &awaiting}); err != nil {
			m.logger.Error("transition to awaiting_input", "workspace_id", workspaceID, "err", err)
		}
	case DoneStopped:
		// Stop() handles the cancelled transition explicitly.
	case DoneError, DoneProcessExit:
		m.markFailed(ctx, workspaceID, fmt.Errorf("agent: %s", reason))
	default:
		m.logger.Warn("unknown done reason", "workspace_id", workspaceID, "reason", reason)
		m.markFailed(ctx, workspaceID, fmt.Errorf("agent: unknown reason %s", reason))
	}
}

func (m *Manager) markFailed(ctx context.Context, workspaceID string, cause error) {
	ws, err := db.GetWorkspace(ctx, m.db, workspaceID)
	if err != nil {
		m.logger.Error("markFailed: load workspace", "workspace_id", workspaceID, "err", err)
		return
	}
	if workspace.IsTerminal(ws.Status) {
		return
	}
	failed := types.WorkspaceFailed
	if !workspace.CanTransition(ws.Status, failed) {
		m.logger.Warn("forcing failed transition", "workspace_id", workspaceID, "from", ws.Status)
	}
	now := time.Now()
	if _, err := db.UpdateWorkspace(ctx, m.db, workspaceID, db.WorkspaceUpdatePatch{
		Status:      &failed,
		CompletedAt: &now,
	}); err != nil {
		m.logger.Error("markFailed: update workspace", "workspace_id", workspaceID, "err", err)
	}
	if _, err := db.CreateMessage(ctx, m.db, workspaceID, "system", "Workspace failed: "+cause.Error(), nil); err != nil {
		m.logger.Error("markFailed: write system message", "workspace_id", workspaceID, "err", err)
	}
	m.logger.Info("workspace failed", "workspace_id", workspaceID, "cause", cause.Error())
}

func (m *Manager) cleanupSession(workspaceID string) {
	m.mu.Lock()
	sess, ok := m.sessions[workspaceID]
	if ok {
		delete(m.sessions, workspaceID)
	}
	m.mu.Unlock()
	if sess != nil && sess.cancel != nil {
		sess.cancel()
	}
}

func ulidLike() string {
	return fmt.Sprintf("%016x", time.Now().UnixNano())
}
