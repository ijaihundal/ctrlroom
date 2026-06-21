package api

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/auth"
	"github.com/ijaihundal/ctrlroom/internal/config"
	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/git"
	"github.com/ijaihundal/ctrlroom/internal/types"
	"github.com/ijaihundal/ctrlroom/internal/workspace"
)

type testServer struct {
	server       *httptest.Server
	database     *sql.DB
	admin        *types.User
	password     string
	gitClient    *git.Client
	worktreeDir  string
	workspaceMgr *workspace.Manager
}

func setup(t *testing.T) *testServer {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Apply(context.Background(), database); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		Env:           "dev",
		Port:          0,
		SessionTTL:    time.Hour,
		Argon2Memory:  1024,
		Argon2Time:    1,
		Argon2Threads: 1,
	}
	password := "test-password-123"
	hash, err := auth.Hash(password, cfg)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user, err := db.CreateUser(context.Background(), database, "admin", hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	gitClient, err := git.New()
	if err != nil {
		t.Fatalf("git new: %v", err)
	}
	worktreeDir := t.TempDir()
	workspaceMgr := workspace.NewManager(database, gitClient, worktreeDir, slog.Default())

	srv := New(cfg, database, slog.Default(), gitClient, workspaceMgr)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	return &testServer{
		server:       httpSrv,
		database:     database,
		admin:        user,
		password:     password,
		gitClient:    gitClient,
		worktreeDir:  worktreeDir,
		workspaceMgr: workspaceMgr,
	}
}
