package agent

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/git"
	"github.com/ijaihundal/ctrlroom/internal/testutil"
	"github.com/ijaihundal/ctrlroom/internal/types"
	"github.com/ijaihundal/ctrlroom/internal/workspace"
)

type managerFixture struct {
	database *sql.DB
	mgr      *Manager
	ws       *types.Workspace
	project  *types.Project
}

func setupManager(t *testing.T) *managerFixture {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Apply(context.Background(), database); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	repo := testutil.TempRepo(t)
	project, err := db.CreateProject(context.Background(), database, db.ProjectCreateParams{
		Name: "t", RepoPath: repo, DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	ws, err := db.CreateWorkspace(context.Background(), database, db.WorkspaceCreateParams{
		ProjectID: project.ID, AgentType: types.AgentOpenCode, Status: types.WorkspaceQueued,
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	gitc, err := git.New()
	if err != nil {
		t.Fatalf("git new: %v", err)
	}
	wsMgr := workspace.NewManager(database, gitc, t.TempDir(), slog.Default())
	prepared, err := wsMgr.Prepare(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	factory := func(types.AgentType) (AgentAdapter, error) {
		return NewFakeAdapter(), nil
	}
	mgr := NewManager(database, factory, slog.Default())

	return &managerFixture{database: database, mgr: mgr, ws: prepared, project: project}
}

func waitForStatus(t *testing.T, database *sql.DB, wsID string, want types.WorkspaceStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ws, err := db.GetWorkspace(context.Background(), database, wsID)
		if err == nil && ws.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	ws, _ := db.GetWorkspace(context.Background(), database, wsID)
	t.Fatalf("workspace %s never reached %s (still %s) within %s", wsID, want, ws.Status, timeout)
}

func TestManager_Start_HappyPath(t *testing.T) {
	f := setupManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := f.mgr.Start(ctx, f.ws.ID, "hello"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, f.database, f.ws.ID, types.WorkspaceAwaitingInput, 2*time.Second)

	msgs, err := db.ListMessages(ctx, f.database, f.ws.ID, -1, 100)
	if err != nil {
		t.Fatalf("list msgs: %v", err)
	}
	var hasUserMsg bool
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, "hello") {
			hasUserMsg = true
		}
	}
	if !hasUserMsg {
		t.Error("expected user message persisted")
	}
}

func TestManager_Start_InvalidTransition(t *testing.T) {
	f := setupManager(t)
	running := types.WorkspaceRunning
	if _, err := db.UpdateWorkspace(context.Background(), f.database, f.ws.ID, db.WorkspaceUpdatePatch{Status: &running}); err != nil {
		t.Fatalf("update: %v", err)
	}

	_, err := f.mgr.Start(context.Background(), f.ws.ID, "x")
	if err == nil {
		t.Fatal("expected error for running→running")
	}
	if !errors.Is(err, workspace.ErrInvalidTransition) {
		t.Errorf("err=%v, want ErrInvalidTransition", err)
	}
}

func TestManager_Start_SpawnFailure(t *testing.T) {
	f := setupManager(t)
	boom := errors.New("boom spawn")
	f.mgr.factory = func(types.AgentType) (AgentAdapter, error) {
		return NewFakeAdapter(WithSpawnError(boom)), nil
	}

	_, err := f.mgr.Start(context.Background(), f.ws.ID, "x")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err=%v, want boom spawn", err)
	}
	ws, _ := db.GetWorkspace(context.Background(), f.database, f.ws.ID)
	if ws.Status != types.WorkspaceFailed {
		t.Errorf("status=%s, want failed", ws.Status)
	}
}

func TestManager_Start_MissingWorktree(t *testing.T) {
	f := setupManager(t)
	empty := ""
	if _, err := db.UpdateWorkspace(context.Background(), f.database, f.ws.ID, db.WorkspaceUpdatePatch{WorktreePath: &empty}); err != nil {
		t.Fatalf("update: %v", err)
	}

	_, err := f.mgr.Start(context.Background(), f.ws.ID, "x")
	if err == nil || !strings.Contains(err.Error(), "no worktree") {
		t.Errorf("err=%v, want no-worktree error", err)
	}
}

func TestManager_SendPrompt_FollowUp(t *testing.T) {
	f := setupManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := f.mgr.Start(ctx, f.ws.ID, "first"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, f.database, f.ws.ID, types.WorkspaceAwaitingInput, 2*time.Second)

	if err := f.mgr.SendPrompt(ctx, f.ws.ID, "second"); err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	waitForStatus(t, f.database, f.ws.ID, types.WorkspaceAwaitingInput, 2*time.Second)

	msgs, _ := db.ListMessages(ctx, f.database, f.ws.ID, -1, 100)
	var userCount int
	for _, m := range msgs {
		if m.Role == "user" {
			userCount++
		}
	}
	if userCount != 2 {
		t.Errorf("user msgs=%d, want 2", userCount)
	}
}

func TestManager_SendPrompt_NotActive(t *testing.T) {
	f := setupManager(t)
	err := f.mgr.SendPrompt(context.Background(), f.ws.ID, "x")
	if !errors.Is(err, ErrWorkspaceNotActive) {
		t.Errorf("err=%v, want ErrWorkspaceNotActive", err)
	}
}

func TestManager_Stop_TransitionsToCancelled(t *testing.T) {
	f := setupManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := f.mgr.Start(ctx, f.ws.ID, "x"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := f.mgr.Stop(ctx, f.ws.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	ws, _ := db.GetWorkspace(ctx, f.database, f.ws.ID)
	if ws.Status != types.WorkspaceCancelled {
		t.Errorf("status=%s, want cancelled", ws.Status)
	}
	if err := f.mgr.Stop(ctx, f.ws.ID); err != nil {
		t.Errorf("stop again: %v", err)
	}
}

func TestManager_Complete_FromAwaitingInput(t *testing.T) {
	f := setupManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := f.mgr.Start(ctx, f.ws.ID, "x"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, f.database, f.ws.ID, types.WorkspaceAwaitingInput, 2*time.Second)

	if err := f.mgr.Complete(ctx, f.ws.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	ws, _ := db.GetWorkspace(ctx, f.database, f.ws.ID)
	if ws.Status != types.WorkspaceCompleted {
		t.Errorf("status=%s, want completed", ws.Status)
	}
}

func TestManager_Complete_AlreadyCompleted(t *testing.T) {
	f := setupManager(t)
	ctx := context.Background()
	completed := types.WorkspaceCompleted
	if _, err := db.UpdateWorkspace(ctx, f.database, f.ws.ID, db.WorkspaceUpdatePatch{Status: &completed}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := f.mgr.Complete(ctx, f.ws.ID); err != nil {
		t.Errorf("complete on completed: %v", err)
	}
}

func TestManager_Complete_InvalidTransition(t *testing.T) {
	f := setupManager(t)
	ctx := context.Background()
	queued := types.WorkspaceQueued
	if _, err := db.UpdateWorkspace(ctx, f.database, f.ws.ID, db.WorkspaceUpdatePatch{Status: &queued}); err != nil {
		t.Fatalf("update: %v", err)
	}
	err := f.mgr.Complete(ctx, f.ws.ID)
	if !errors.Is(err, workspace.ErrInvalidTransition) {
		t.Errorf("err=%v, want ErrInvalidTransition", err)
	}
}

func TestManager_Subscribe_ReceivesEvents(t *testing.T) {
	f := setupManager(t)
	ch, unsub := f.mgr.Subscribe(f.ws.ID)
	defer unsub()

	f.mgr.broadcast(f.ws.ID, AgentEvent{Type: EventText, Content: "hi"})
	f.mgr.broadcast(f.ws.ID, AgentEvent{Type: EventDone, Metadata: map[string]any{"reason": "turn_complete"}})

	var seen []EventType
	timeout := time.After(500 * time.Millisecond)
	for len(seen) < 2 {
		select {
		case ev := <-ch:
			seen = append(seen, ev.Type)
		case <-timeout:
			t.Fatalf("timed out; seen=%v", seen)
		}
	}
	if seen[0] != EventText || seen[1] != EventDone {
		t.Errorf("seen=%v, want [text done]", seen)
	}
}

func TestManager_Subscribe_UnsubscribeStopsBroadcast(t *testing.T) {
	f := setupManager(t)
	_, unsub := f.mgr.Subscribe(f.ws.ID)
	unsub()
	f.mgr.broadcast(f.ws.ID, AgentEvent{Type: EventText})
}

func TestManager_Drain_CostAccumulates(t *testing.T) {
	f := setupManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	script := []AgentEvent{
		E(EventText, "thinking"),
		E(EventCost, "", "tokens_in", int64(100), "tokens_out", int64(50), "cost_usd", 0.01, "model", "fake"),
		E(EventCost, "", "tokens_in", int64(200), "tokens_out", int64(10), "cost_usd", 0.02, "model", "fake"),
		E(EventDone, "", "reason", string(DoneTurnComplete)),
	}
	f.mgr.factory = func(types.AgentType) (AgentAdapter, error) {
		return NewFakeAdapter(WithScripts([][]AgentEvent{script})), nil
	}

	if _, err := f.mgr.Start(ctx, f.ws.ID, "go"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, f.database, f.ws.ID, types.WorkspaceAwaitingInput, 2*time.Second)

	ws, _ := db.GetWorkspace(ctx, f.database, f.ws.ID)
	if ws.TokensIn != 300 {
		t.Errorf("tokens_in=%d, want 300", ws.TokensIn)
	}
	if ws.TokensOut != 60 {
		t.Errorf("tokens_out=%d, want 60", ws.TokensOut)
	}
	if ws.CostUSD < 0.029 || ws.CostUSD > 0.031 {
		t.Errorf("cost_usd=%f, want ~0.03", ws.CostUSD)
	}
}

func TestManager_Drain_FatalErrorMarksFailed(t *testing.T) {
	f := setupManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	script := []AgentEvent{
		E(EventError, "kaboom", "fatal", true),
		E(EventDone, "", "reason", string(DoneError)),
	}
	f.mgr.factory = func(types.AgentType) (AgentAdapter, error) {
		return NewFakeAdapter(WithScripts([][]AgentEvent{script})), nil
	}
	if _, err := f.mgr.Start(ctx, f.ws.ID, "x"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, f.database, f.ws.ID, types.WorkspaceFailed, 2*time.Second)
}

func TestManager_Active_ListsRunningWorkspaces(t *testing.T) {
	f := setupManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if got := f.mgr.Active(); len(got) != 0 {
		t.Errorf("pre-start Active=%v, want empty", got)
	}
	if _, err := f.mgr.Start(ctx, f.ws.ID, "x"); err != nil {
		t.Fatalf("start: %v", err)
	}
	got := f.mgr.Active()
	if len(got) != 1 || got[0] != f.ws.ID {
		t.Errorf("Active=%v, want [%s]", got, f.ws.ID)
	}
}
