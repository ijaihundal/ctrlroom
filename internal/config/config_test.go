package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clearCtrlroomEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(key, "CTRLROOM_") {
			continue
		}
		val, ok := os.LookupEnv(key)
		os.Unsetenv(key)
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(key, val)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func TestLoadDevDefaults(t *testing.T) {
	clearCtrlroomEnv(t)
	t.Setenv("CTRLROOM_DATA_DIR", t.TempDir())

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if c.Env != "dev" {
		t.Errorf("Env = %q, want dev", c.Env)
	}
	if c.Port != 3000 {
		t.Errorf("Port = %d, want 3000", c.Port)
	}
	if c.Username != "admin" {
		t.Errorf("Username = %q, want admin", c.Username)
	}
	if c.Password != "admin" {
		t.Errorf("Password = %q, want admin", c.Password)
	}
	if c.SessionTTL != 720*time.Hour {
		t.Errorf("SessionTTL = %v, want 720h", c.SessionTTL)
	}
	if c.Argon2Memory != 65536 {
		t.Errorf("Argon2Memory = %d, want 65536", c.Argon2Memory)
	}
	if c.Argon2Time != 3 {
		t.Errorf("Argon2Time = %d, want 3", c.Argon2Time)
	}
	if c.Argon2Threads != 2 {
		t.Errorf("Argon2Threads = %d, want 2", c.Argon2Threads)
	}
	if c.GitBinPath != "git" {
		t.Errorf("GitBinPath = %q, want git", c.GitBinPath)
	}
	if c.DBPath != filepath.Join(c.DataDir, "ctrlroom.db") {
		t.Errorf("DBPath = %q, want %q", c.DBPath, filepath.Join(c.DataDir, "ctrlroom.db"))
	}
	if c.WorktreeDir != filepath.Join(c.DataDir, "worktrees") {
		t.Errorf("WorktreeDir = %q, want %q", c.WorktreeDir, filepath.Join(c.DataDir, "worktrees"))
	}
}

func TestLoadCustomValues(t *testing.T) {
	clearCtrlroomEnv(t)
	t.Setenv("CTRLROOM_ENV", "dev")
	t.Setenv("CTRLROOM_PORT", "8080")
	t.Setenv("CTRLROOM_DATA_DIR", t.TempDir())
	t.Setenv("CTRLROOM_USERNAME", "alice")
	t.Setenv("CTRLROOM_PASSWORD", "s3cret")
	t.Setenv("CTRLROOM_SESSION_TTL", "12h")
	t.Setenv("CTRLROOM_ARGON2_MEMORY", "32768")
	t.Setenv("CTRLROOM_ARGON2_TIME", "5")
	t.Setenv("CTRLROOM_ARGON2_THREADS", "4")
	t.Setenv("CTRLROOM_GIT_BIN", "git")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Port != 8080 {
		t.Errorf("Port = %d, want 8080", c.Port)
	}
	if c.Username != "alice" {
		t.Errorf("Username = %q, want alice", c.Username)
	}
	if c.Password != "s3cret" {
		t.Errorf("Password = %q, want s3cret", c.Password)
	}
	if c.SessionTTL != 12*time.Hour {
		t.Errorf("SessionTTL = %v, want 12h", c.SessionTTL)
	}
	if c.Argon2Memory != 32768 {
		t.Errorf("Argon2Memory = %d, want 32768", c.Argon2Memory)
	}
	if c.Argon2Time != 5 {
		t.Errorf("Argon2Time = %d, want 5", c.Argon2Time)
	}
	if c.Argon2Threads != 4 {
		t.Errorf("Argon2Threads = %d, want 4", c.Argon2Threads)
	}
}

func TestLoadProdRequiresCredentials(t *testing.T) {
	clearCtrlroomEnv(t)
	t.Setenv("CTRLROOM_ENV", "prod")
	t.Setenv("CTRLROOM_DATA_DIR", t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("Load: expected error for prod without credentials, got nil")
	}
}

func TestLoadInvalidPort(t *testing.T) {
	clearCtrlroomEnv(t)
	t.Setenv("CTRLROOM_PORT", "99999")
	t.Setenv("CTRLROOM_DATA_DIR", t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("Load: expected error for invalid port, got nil")
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	clearCtrlroomEnv(t)
	t.Setenv("CTRLROOM_SESSION_TTL", "not-a-duration")
	t.Setenv("CTRLROOM_DATA_DIR", t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("Load: expected error for invalid duration, got nil")
	}
}

func TestLoadDurationBelowMinimum(t *testing.T) {
	clearCtrlroomEnv(t)
	t.Setenv("CTRLROOM_SESSION_TTL", "30s")
	t.Setenv("CTRLROOM_DATA_DIR", t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("Load: expected error for ttl < 1m, got nil")
	}
}

func TestLoadArgon2MemoryBelowMinimum(t *testing.T) {
	clearCtrlroomEnv(t)
	t.Setenv("CTRLROOM_ARGON2_MEMORY", "512")
	t.Setenv("CTRLROOM_DATA_DIR", t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("Load: expected error for argon2 memory < 1024, got nil")
	}
}

func TestLoadDataDirCreated(t *testing.T) {
	clearCtrlroomEnv(t)
	base := t.TempDir()
	nested := filepath.Join(base, "a", "b", "c")
	t.Setenv("CTRLROOM_DATA_DIR", nested)

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	info, err := os.Stat(c.DataDir)
	if err != nil {
		t.Fatalf("Stat DataDir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("DataDir %q is not a directory", c.DataDir)
	}
}

func TestLoadInvalidEnv(t *testing.T) {
	clearCtrlroomEnv(t)
	t.Setenv("CTRLROOM_ENV", "staging")
	t.Setenv("CTRLROOM_DATA_DIR", t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("Load: expected error for invalid env, got nil")
	}
}

func TestLoadGitBinaryNotFound(t *testing.T) {
	clearCtrlroomEnv(t)
	t.Setenv("CTRLROOM_DATA_DIR", t.TempDir())
	t.Setenv("CTRLROOM_GIT_BIN", "definitely-not-a-real-git-binary-xyz")

	_, err := Load()
	if err == nil {
		t.Fatal("Load: expected error for missing git binary, got nil")
	}
}
