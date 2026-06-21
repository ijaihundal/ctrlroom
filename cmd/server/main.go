package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/agent"
	"github.com/ijaihundal/ctrlroom/internal/api"
	"github.com/ijaihundal/ctrlroom/internal/auth"
	"github.com/ijaihundal/ctrlroom/internal/config"
	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/git"
	"github.com/ijaihundal/ctrlroom/internal/logging"
	"github.com/ijaihundal/ctrlroom/internal/types"
	"github.com/ijaihundal/ctrlroom/internal/version"
	"github.com/ijaihundal/ctrlroom/internal/workspace"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg.Env)
	logging.SetDefault(logger)
	logger.Info("starting ctrlroom",
		"version", version.Version,
		"commit", version.Commit,
		"env", cfg.Env,
		"data_dir", cfg.DataDir,
	)

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() {
		if cerr := database.Close(); cerr != nil {
			logger.Error("close db", "err", cerr)
		}
	}()

	if err := db.Apply(context.Background(), database); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	if err := bootstrapAdmin(context.Background(), cfg, database, logger); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	gitClient, err := git.New()
	if err != nil {
		return fmt.Errorf("init git client: %w", err)
	}
	if err := os.MkdirAll(cfg.WorktreeDir, 0o755); err != nil {
		return fmt.Errorf("create worktree dir: %w", err)
	}
	workspaceMgr := workspace.NewManager(database, gitClient, cfg.WorktreeDir, logger)

	// Sweep orphaned workspaces from a previous server lifetime.
	if _, err := agent.SweepOrphans(context.Background(), database, logger); err != nil {
		return fmt.Errorf("sweep orphans: %w", err)
	}

	agentFactory := agent.NewAdapterFactory(agent.Binaries{
		types.AgentOpenCode: "opencode",
	})
	agentMgr := agent.NewManager(database, agentFactory, logger)

	server := api.New(cfg, database, logger, gitClient, workspaceMgr, agentMgr)
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       0, // no limit — agents stream indefinitely
		WriteTimeout:      0, // no limit — /start + /stream are long-lived
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http serve: %w", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("stopped cleanly")
	return nil
}

func bootstrapAdmin(ctx context.Context, cfg *config.Config, database *sql.DB, logger *slog.Logger) error {
	n, err := db.CountUsers(ctx, database)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		logger.Info("admin user already exists", "count", n)
		return nil
	}
	hash, err := auth.Hash(cfg.Password, cfg)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	u, err := db.CreateUser(ctx, database, cfg.Username, hash)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	logger.Info("bootstrap admin created", "id", u.ID, "username", u.Username)
	return nil
}
