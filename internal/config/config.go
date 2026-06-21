package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Env           string
	Port          int
	DataDir       string
	DBPath        string
	WorktreeDir   string
	Username      string
	Password      string
	SessionTTL    time.Duration
	Argon2Memory  uint32
	Argon2Time    uint32
	Argon2Threads uint8
	GitBinPath    string
}

func Load() (*Config, error) {
	env, err := loadEnv()
	if err != nil {
		return nil, err
	}
	port, err := loadPort()
	if err != nil {
		return nil, err
	}
	dataDir, dbPath, worktreeDir, err := loadPaths()
	if err != nil {
		return nil, err
	}
	username, password, err := loadCredentials(env)
	if err != nil {
		return nil, err
	}
	ttl, err := loadSessionTTL()
	if err != nil {
		return nil, err
	}
	mem, err := loadArgon2Memory()
	if err != nil {
		return nil, err
	}
	iters, err := envUint32("CTRLROOM_ARGON2_TIME", 3)
	if err != nil {
		return nil, fmt.Errorf("invalid CTRLROOM_ARGON2_TIME: %w", err)
	}
	threads, err := envUint8("CTRLROOM_ARGON2_THREADS", 2)
	if err != nil {
		return nil, fmt.Errorf("invalid CTRLROOM_ARGON2_THREADS: %w", err)
	}
	gitBin, err := loadGitBin()
	if err != nil {
		return nil, err
	}

	return &Config{
		Env:           env,
		Port:          port,
		DataDir:       dataDir,
		DBPath:        dbPath,
		WorktreeDir:   worktreeDir,
		Username:      username,
		Password:      password,
		SessionTTL:    ttl,
		Argon2Memory:  mem,
		Argon2Time:    iters,
		Argon2Threads: threads,
		GitBinPath:    gitBin,
	}, nil
}

func loadEnv() (string, error) {
	env := envStr("CTRLROOM_ENV", "dev")
	if env != "dev" && env != "prod" {
		return "", fmt.Errorf("invalid CTRLROOM_ENV %q: must be dev or prod", env)
	}
	return env, nil
}

func loadPort() (int, error) {
	port, err := envInt("CTRLROOM_PORT", 3000)
	if err != nil {
		return 0, fmt.Errorf("invalid CTRLROOM_PORT: %w", err)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port %d: must be in [1, 65535]", port)
	}
	return port, nil
}

func loadPaths() (dataDir, dbPath, worktreeDir string, err error) {
	raw := envStr("CTRLROOM_DATA_DIR", "./data")
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve data dir %q: %w", raw, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", "", "", fmt.Errorf("create data dir %q: %w", abs, err)
	}
	return abs, filepath.Join(abs, "ctrlroom.db"), filepath.Join(abs, "worktrees"), nil
}

func loadCredentials(env string) (username, password string, err error) {
	username = os.Getenv("CTRLROOM_USERNAME")
	password = os.Getenv("CTRLROOM_PASSWORD")
	if env == "prod" {
		if username == "" {
			return "", "", errors.New("CTRLROOM_USERNAME is required in prod mode")
		}
		if password == "" {
			return "", "", errors.New("CTRLROOM_PASSWORD is required in prod mode")
		}
		return username, password, nil
	}
	if username == "" {
		slog.Warn("CTRLROOM_USERNAME unset; defaulting to admin (dev only)")
		username = "admin"
	}
	if password == "" {
		slog.Warn("CTRLROOM_PASSWORD unset; defaulting to admin (dev only)")
		password = "admin"
	}
	return username, password, nil
}

func loadSessionTTL() (time.Duration, error) {
	ttl, err := envDuration("CTRLROOM_SESSION_TTL", 720*time.Hour)
	if err != nil {
		return 0, fmt.Errorf("invalid CTRLROOM_SESSION_TTL: %w", err)
	}
	if ttl < time.Minute {
		return 0, fmt.Errorf("invalid session ttl %s: must be >= 1m", ttl)
	}
	return ttl, nil
}

func loadArgon2Memory() (uint32, error) {
	mem, err := envUint32("CTRLROOM_ARGON2_MEMORY", 65536)
	if err != nil {
		return 0, fmt.Errorf("invalid CTRLROOM_ARGON2_MEMORY: %w", err)
	}
	if mem < 1024 {
		return 0, fmt.Errorf("invalid argon2 memory %d: must be >= 1024 KiB", mem)
	}
	return mem, nil
}

func loadGitBin() (string, error) {
	gitBin := envStr("CTRLROOM_GIT_BIN", "git")
	if _, err := exec.LookPath(gitBin); err != nil {
		return "", fmt.Errorf("git binary %q not found in PATH: %w", gitBin, err)
	}
	return gitBin, nil
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("parse %q as int: %w", v, err)
	}
	return n, nil
}

func envUint32(key string, def uint32) (uint32, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %q as uint32: %w", v, err)
	}
	return uint32(n), nil
}

func envUint8(key string, def uint8) (uint8, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseUint(v, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("parse %q as uint8: %w", v, err)
	}
	return uint8(n), nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("parse %q as duration: %w", v, err)
	}
	return d, nil
}
