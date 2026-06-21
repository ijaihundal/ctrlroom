-- 003_workspaces: workspaces + messages (Phase 2)
--
-- NOTE: api_tokens.workspace_id (created in 001_auth.sql) deliberately has no
-- FOREIGN KEY constraint pointing at workspaces(id). Adding it now would
-- require rebuilding api_tokens (SQLite has no ALTER TABLE ADD CONSTRAINT),
-- which means a 4-step rename/copy/drop pattern — out of scope for Phase 2.
-- The application layer is responsible for cleaning up api_tokens rows when
-- the referenced workspace is deleted.

CREATE TABLE workspaces (
    id              TEXT PRIMARY KEY,
    issue_id        TEXT REFERENCES issues(id) ON DELETE SET NULL,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    branch          TEXT NOT NULL,
    agent_type      TEXT NOT NULL,
    model           TEXT,
    status          TEXT NOT NULL DEFAULT 'queued',
    prompt          TEXT,
    process_pid     INTEGER,
    worktree_path   TEXT,
    target_ref      TEXT,
    pending_merge   TEXT,           -- JSON {target, attempt, max_attempts} or NULL
    orchestrator    BOOLEAN NOT NULL DEFAULT 0,
    tokens_in       INTEGER NOT NULL DEFAULT 0,
    tokens_out      INTEGER NOT NULL DEFAULT 0,
    cost_usd        REAL NOT NULL DEFAULT 0,
    started_at      DATETIME,
    completed_at    DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_workspaces_project ON workspaces(project_id);
CREATE INDEX idx_workspaces_issue   ON workspaces(issue_id);
CREATE INDEX idx_workspaces_status  ON workspaces(status);

CREATE TABLE messages (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,         -- user, assistant, tool, system
    content      TEXT,
    metadata     TEXT,                  -- JSON
    seq          INTEGER,               -- per-workspace monotonic
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_messages_workspace_seq ON messages(workspace_id, seq);
