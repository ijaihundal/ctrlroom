-- 001_auth: bootstrap auth tables (Phase 1)

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at   DATETIME NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_sessions_user    ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- workspace_id targets the workspaces(id) PK. The FK clause is intentionally
    -- omitted here: with PRAGMA foreign_keys=ON SQLite resolves the parent table
    -- at DML time and would reject inserts/deletes with "no such table:
    -- workspaces" until 003_workspaces.sql creates it. Phase 2 must rebuild this
    -- table (SQLite has no ALTER TABLE ADD CONSTRAINT) to add:
    --   workspace_id TEXT REFERENCES workspaces(id) ON DELETE CASCADE
    workspace_id TEXT,
    token_hash   TEXT NOT NULL UNIQUE,
    expires_at   DATETIME NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_api_tokens_hash    ON api_tokens(token_hash);
CREATE INDEX idx_api_tokens_expires ON api_tokens(expires_at);
