# CtrlRoom

Self-hosted kanban + coding-agent workspace orchestrator. Single Go binary, React frontend, SQLite. Controls Claude Code, Codex CLI, and OpenCode via their native protocols.

## What It Does

User creates issues on a kanban board → starts a workspace (agent on a git branch) → agent streams live output (text, tool calls, file changes) → user reviews diff, merges or sends follow-up.

A built-in scheduler can fire agents on cron schedules. An orchestrator agent uses the `ctrlroom` CLI to manage other workspaces — check issues, spawn agents, merge results. Fully autonomous CI/CD loop if you want it.

## Tech Stack

- **Backend**: Go (single binary, goroutines for streaming, stdlib net/http + os/exec)
- **Frontend**: React + TypeScript + Vite + Tailwind CSS
- **Database**: SQLite (embedded, zero config)
- **Deployment**: Single Docker image (Go binary + embedded frontend + Node.js for npx)

## Architecture

```
┌─────────────────────────────────────────────────┐
│                    Browser                         │
│   Kanban Board │ Issue Panel │ Chat │ Diff │ Jobs  │
└──────────────┬────────────────────────────────────┘
               │ WebSocket + REST
┌──────────────▼────────────────────────────────────┐
│            Go Backend (single binary)               │
│                                                      │
│  ┌──────────┐  ┌───────────┐  ┌────────────────┐  │
│  │  Kanban   │  │ Workspace │  │  Agent Runner   │  │
│  │  CRUD     │  │ Manager   │  │  (3 adapters)   │  │
│  └─────┬─────┘  └─────┬─────┘  └───────┬────────┘  │
│        │              │                 │            │
│  ┌─────▼──────────────▼─────────────────▼────────┐ │
│  │      SQLite + Git + Scheduler + CLI            │ │
│  └────────────────────────────────────────────────┘ │
│                                                      │
│  `ctrlroom` CLI (wraps own REST API for agents)     │
└──────────────────────────────────────────────────────┘
               │ spawns subprocesses
        ┌──────┼──────────┐
        ▼      ▼          ▼
   Claude    Codex    OpenCode
   (stdio)   (stdio)  (HTTP+SSE)
```

## Agent Adapters

The core abstraction. Each agent speaks a different protocol — the adapter normalizes everything to one event stream.

### Common Event Type

```go
type AgentEvent struct {
    Type     string                 // "text" | "tool_call" | "file_change" | "reasoning" | "error" | "done"
    Content  string
    Metadata map[string]any         // tool?, command?, file?, diff?, cost?, tokens?
}
```

### Claude Code

- **Spawn**: `npx -y @anthropic-ai/claude-code --input-format stream-json --output-format stream-json --verbose --allowedTools "Read,Edit,Write,Bash,Glob,Grep" --permission-mode dontAsk --cwd <worktree>`
- **Protocol**: NDJSON over stdin/stdout
- **Send prompt**: Write `{"type":"user","message":{"role":"user","content":"<prompt>"}}` to stdin
- **Events**: `system/init`, `assistant` (text + tool calls), `user` (tool results), `stream_event` (token deltas), `result` (final)
- **Multi-turn**: Keep stdin open, write new messages as they come
- **Auth**: `ANTHROPIC_API_KEY` env var

### Codex CLI

- **Spawn**: `codex app-server --listen stdio://`
- **Protocol**: JSON-RPC over stdin/stdout (bidirectional — server sends requests to client too)
- **Flow**: `initialize` → `thread/start` (cwd, sandbox, approval policy) → `turn/start` (prompt) → stream `turn/*` notifications
- **Events**: `turn/started`, `turn/completed`, `turn/diffUpdated`, thread items (`agentMessage`, `commandExecution`, `fileChange`, `reasoning`)
- **Follow-ups**: `turn/steer` (inject mid-turn)
- **Approval**: `never` for autonomous, `on-failure` for semi-auto
- **Auth**: `OPENAI_API_KEY` env var

### OpenCode

- **Spawn**: `opencode serve --hostname 127.0.0.1 --port 0` in the worktree directory
- **Protocol**: HTTP REST + SSE (Server-Sent Events)
- **Flow**: `POST /session` → `POST /session/:id/prompt_async` → stream via `GET /event` SSE
- **Events**: `session.next.text.delta` (streaming text), `session.next.tool.called` (tool calls), `session.next.tool.success` (results), `session.next.step.ended` (cost/tokens), `session.next.reasoning.delta`
- **Approval**: Config in `opencode.json` (`allow`/`ask`/`deny` per tool). Set all to `allow` for autonomous.
- **Auth**: HTTP basic auth (optional), provider API keys via env

### Adapter Interface

```go
type AgentAdapter interface {
    Spawn(ctx context.Context, cwd string, config AgentConfig) error
    SendPrompt(ctx context.Context, prompt string) error
    Events() <-chan AgentEvent
    RespondApproval(ctx context.Context, id string, approved bool) error
    Stop() error
}
```

Three implementations: ClaudeAdapter (~600 LOC), CodexAdapter (~800 LOC), OpenCodeAdapter (~700 LOC).

## Scheduler + Orchestrator

### Scheduler

A single Go goroutine (~150 LOC) that checks a cron table every 30 seconds:

```go
func RunScheduler(ctx context.Context, db *sql.DB) {
    ticker := time.NewTicker(30 * time.Second)
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            jobs := loadDueJobs(db)
            for _, job := range jobs {
                go startWorkspace(db, job.RepoID, job.AgentType, job.Prompt)
            }
        }
    }
}
```

### Orchestrator via CLI

The orchestrator is just a regular agent with Bash access + the `ctrlroom` CLI in PATH. Its prompt defines behavior. The CLI wraps our own REST API:

```
ctrlroom
├── auth login              # set API URL + token
├── issues
│   ├── list                # --project, --status, --tag
│   ├── create              # --project, --title, --body, --tags
│   └── close               # <id>
├── projects
│   ├── list
│   └── show                # <id>
├── workspace
│   ├── start               # --repo, --agent, --model, --prompt, --branch
│   ├── status              # <id>
│   ├── logs                # <id> --follow
│   ├── diff                # <id>
│   ├── stop                # <id>
│   └── merge               # <id>
├── jobs
│   ├── list
│   ├── create              # --schedule, --repo, --agent, --prompt
│   ├── pause               # <id>
│   └── resume              # <id>
└── me                      # current user/session
```

Example orchestration flow:
```
Schedule fires (cron: "0 9 * * *")
  ↓
CtrlRoom spawns orchestrator agent with prompt
  ↓
Agent runs: ctrlroom issues list --status open
  ↓
Agent runs: ctrlroom workspace start --repo X --agent codex --prompt "Fix tests"
  ↓
Agent polls: ctrlroom workspace status <id>
  ↓
Agent runs: ctrlroom workspace merge <id>
```

No special orchestration code. The agent IS the orchestrator. The CLI is the integration point.

## Database Schema

```sql
-- Projects (kanban boards)
CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    repo_path   TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Issues (kanban cards)
CREATE TABLE issues (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT DEFAULT 'todo',   -- todo, in_progress, review, done
    priority    INTEGER DEFAULT 0,
    tags        TEXT,                   -- JSON array
    sort_order  INTEGER DEFAULT 0,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Workspaces (agent sessions on git branches)
CREATE TABLE workspaces (
    id           TEXT PRIMARY KEY,
    issue_id     TEXT REFERENCES issues(id),
    project_id   TEXT NOT NULL REFERENCES projects(id),
    branch       TEXT NOT NULL,
    agent_type   TEXT NOT NULL,         -- claude, codex, opencode
    model        TEXT,
    status       TEXT DEFAULT 'idle',   -- idle, running, completed, failed
    prompt       TEXT,
    process_pid  INTEGER,
    started_at   DATETIME,
    completed_at DATETIME,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Messages (agent conversation log)
CREATE TABLE messages (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id),
    role          TEXT NOT NULL,        -- user, assistant, tool, system
    content       TEXT,
    metadata      TEXT,                 -- JSON: tool calls, diffs, etc
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Scheduled jobs
CREATE TABLE jobs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    project_id  TEXT REFERENCES projects(id),
    repo_path   TEXT,
    agent_type  TEXT NOT NULL,
    model       TEXT,
    prompt      TEXT NOT NULL,
    schedule    TEXT,                   -- cron expression
    enabled     BOOLEAN DEFAULT 1,
    last_run    DATETIME,
    next_run    DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Auth
CREATE TABLE users (
    id           TEXT PRIMARY KEY,
    username     TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,        -- Argon2
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    expires_at  DATETIME NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## REST API

```
# Auth
POST   /api/auth/login              { username, password } → { token }
POST   /api/auth/logout
GET    /api/auth/me

# Projects
GET    /api/projects
POST   /api/projects                { name, description, repo_path }
GET    /api/projects/:id
PATCH  /api/projects/:id
DELETE /api/projects/:id

# Issues
GET    /api/projects/:id/issues
POST   /api/projects/:id/issues     { title, description, tags, priority }
PATCH  /api/issues/:id              { title?, description?, status?, tags?, priority? }
DELETE /api/issues/:id

# Workspaces
POST   /api/workspaces              { issue_id?, project_id, agent_type, model?, prompt }
GET    /api/workspaces/:id
POST   /api/workspaces/:id/stop
GET    /api/workspaces/:id/diff
POST   /api/workspaces/:id/merge
POST   /api/workspaces/:id/message  { content }  # follow-up prompt

# Streaming
GET    /api/workspaces/:id/stream   # WebSocket upgrade (live agent output)

# Jobs
GET    /api/jobs
POST   /api/jobs                    { name, project_id, repo_path, agent_type, prompt, schedule }
PATCH  /api/jobs/:id                { enabled?, schedule?, prompt? }
DELETE /api/jobs/:id

# CLI Auth
POST   /api/cli/token               { } → { token } (for ctrlroom CLI auth)
```

## Project Structure

```
ctrlroom/
├── cmd/
│   ├── server/
│   │   └── main.go              # HTTP server entry point
│   └── ctrlroom/
│       └── main.go              # CLI entry point
├── internal/
│   ├── api/
│   │   ├── server.go            # chi router, middleware
│   │   ├── handlers.go          # REST handlers
│   │   ├── websocket.go         # WebSocket streaming
│   │   └── auth.go              # session auth, Argon2
│   ├── agent/
│   │   ├── adapter.go           # AgentAdapter interface + AgentEvent
│   │   ├── claude.go            # Claude Code adapter
│   │   ├── codex.go             # Codex CLI adapter
│   │   ├── opencode.go          # OpenCode adapter
│   │   └── manager.go           # process lifecycle, log streaming
│   ├── db/
│   │   ├── db.go                # sql.DB connection + migrations
│   │   ├── models.go            # structs + queries
│   │   └── migrations/
│   │       └── 001_init.sql
│   ├── git/
│   │   └── git.go               # branch, diff, merge, commit
│   ├── scheduler/
│   │   └── scheduler.go         # cron ticker, job execution
│   └── config/
│       └── config.go            # env vars, CLI flags
├── web/                         # React frontend (embedded via Go embed.FS)
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── api/
│   │   │   └── client.ts        # REST + WebSocket client
│   │   ├── components/
│   │   │   ├── Kanban/
│   │   │   ├── Chat/
│   │   │   ├── Diff/
│   │   │   ├── Jobs/
│   │   │   └── common/
│   │   ├── hooks/
│   │   ├── stores/
│   │   └── types/
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite.config.ts
│   └── tailwind.config.ts
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Target LOC: ~25-30k

| Module | Est. LOC |
|---|---|
| Backend (Go) | ~8,000 |
| - HTTP server + WebSocket + auth | 2,000 |
| - SQLite models + migrations | 1,500 |
| - Git operations | 800 |
| - Agent adapters (3) | 2,100 |
| - Workspace manager | 600 |
| - Kanban CRUD | 500 |
| - Scheduler | 150 |
| - Config + main | 350 |
| Frontend (React + TS) | ~18,000 |
| - Kanban board (drag-drop) | 3,000 |
| - Issue detail panel | 2,000 |
| - Workspace chat (streaming) | 4,000 |
| - Diff viewer | 2,000 |
| - Agent selector + config | 1,000 |
| - Jobs/scheduler UI | 1,500 |
| - Auth/login | 500 |
| - Shared state + API client | 2,000 |
| - UI components | 2,000 |
| CLI (`ctrlroom`) | ~500 |

## Dockerfile

```dockerfile
# Stage 1: Build frontend
FROM node:22-slim AS web-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.23-bookworm AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /app/web/dist ./web/dist
RUN CGO_ENABLED=1 go build -o /ctrlroom ./cmd/server
RUN CGO_ENABLED=0 go build -o /ctrlroom-cli ./cmd/ctrlroom

# Stage 3: Runtime
FROM node:22-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates git tini openssh-client \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=go-builder /ctrlroom /usr/local/bin/ctrlroom
COPY --from=go-builder /ctrlroom-cli /usr/local/bin/ctrlroom-cli
ENV CTRLROOM_NO_BROWSER=1
EXPOSE 3000
VOLUME ["/data"]
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["ctrlroom", "serve"]
```

## Build Phases

### Phase 1 — Core Backend (Week 1)
- Go project, go.mod, chi router
- SQLite connection + migrations (schema above)
- Auth: username/password (Argon2), session cookies
- Kanban CRUD: projects, issues, statuses, tags
- Basic REST API

### Phase 2 — Workspace + Git (Week 1-2)
- Workspace model (name, branch, repo path, agent config)
- Git operations: create branch, diff, merge, commit
- Process spawning framework (context-based lifecycle)

### Phase 3 — Agent Adapters (Week 2-3)
- Claude Code adapter (NDJSON over stdin/stdout)
- Codex adapter (JSON-RPC over stdio)
- OpenCode adapter (HTTP + SSE)
- WebSocket streaming to browser
- Normalized event display

### Phase 4 — Frontend (Week 3-4)
- Login page
- Kanban board with drag-drop
- Issue detail + workspace creation
- Chat interface (streaming agent output)
- Diff viewer with syntax highlighting
- Agent/model selector

### Phase 5 — Scheduler + CLI (Week 4-5)
- Cron-based job scheduler
- `ctrlroom` CLI (wraps REST API)
- Orchestrator agent support (CLI in PATH + token)
- Jobs management UI

### Phase 6 — Deploy (Week 5)
- Dockerfile (multi-stage: Go + frontend + Node.js)
- docker-compose.yml
- Traefik config for ctrlroom.jaihundal.com
- Test with real repos

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `CTRLROOM_USERNAME` | yes | Login username |
| `CTRLROOM_PASSWORD` | yes | Login password (hashed with Argon2) |
| `CTRLROOM_SESSION_SECRET` | no | HMAC key for session cookies (auto-generated if unset) |
| `CTRLROOM_PORT` | no | Listen port (default 3000) |
| `CTRLROOM_DATA_DIR` | no | SQLite + data path (default /data) |
| `ANTHROPIC_API_KEY` | no | For Claude Code agent |
| `OPENAI_API_KEY` | no | For Codex agent |

## License

MIT
