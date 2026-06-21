# CtrlRoom — Design

Picks up where `README.md` leaves off. README is the *what*; this is the *how*. Decisions get baked in here and referenced from code.

## Decision Log

| # | Decision | Choice | Rationale |
|---|---|---|---|
| D1 | Concurrent agent isolation | `git worktree add` per workspace | True concurrency on same repo, native git, cheap cleanup |
| D2 | Sandbox boundary (MVP) | Trust worktree dir as cwd; full host access | Ship fast; isolation becomes a per-project opt-in later |
| D3 | Merge conflict handling | Try merge → on conflict ask user → recommend "let agent resolve" → same-session follow-up turn with a precise prompt; auto-retry merge on turn end (cap 2) | Reuses agent context, user stays in the loop, bounded retries |
| D4 | MVP adapter scope | All 3 adapters built in parallel against a fixed contract | Forces the contract to be right; no retrofit risk |
| D5 | Repo source | Project points at an existing on-disk repo path (`projects.repo_path`) | No clone management for MVP; remote-friendly later |
| D6 | Frontend state | Zustand (ephemeral/streaming) + React Query (REST cache); `@dnd-kit` for board | Minimal boilerplate, fits streaming workload |
| D7 | Migrations | Embedded `*.sql`, forward-only, applied in tx on boot | No external runner dep |
| D8 | Logging | `log/slog` JSON handler + `x-request-id` | Stdlib, structured, cheap |
| D9 | SQLite driver | `mattn/go-sqlite3` (CGO) | Faster, mature, matches existing Dockerfile; needs gcc in CI |
| D10 | IDs | ULIDs (`oklog/ulid/v2`) | Sortable, URL-friendly, low index contention |
| D11 | Sessions | Opaque server-side tokens (sha256-hashed in `sessions` table) | Revocable, simple, fits SQLite; `CTRLROOM_SESSION_SECRET` env var dropped |
| D12 | Bootstrap user | First-boot seed of one admin from env (no registration UI) | Schema supports multi-user; UI/registration deferred |
| D13 | Domain types | Pure `internal/types/` leaf package (structs + enums only) | Clean cross-package naming; imported by all layers, imports nothing internal |
| D14 | Migrations | Incremental per phase (`001_auth.sql`, `002_kanban.sql`, …) | Smaller diffs, realistic if schema evolves; tx-per-file, forward-only |
| D15 | Routing | `chi/v5` | Middleware composition, idiomatic, stable |
| D16 | Phase 3+4 scope | **OpenCode-first**: build the agent contract + OpenCode adapter fully + live-verify with the locally-installed `opencode` binary. Claude and Codex adapters are deferred (factory returns `ErrAgentNotImplemented` for those agent types until later). | OpenCode binary is already installed in dev; Z_AI_API_KEY provider configured in `~/.config/opencode/opencode.json`. Avoids blocking on `ANTHROPIC_API_KEY` procurement or `codex` binary install. Once OpenCode end-to-end works, the contract is proven and other adapters plug in. |
| D17 | Codex mode (when built) | `codex app-server` JSON-RPC over stdio | Persistent process for multi-turn; matches Claude's stream-json model. |
| D18 | OpenCode server scope | One `opencode serve` per workspace | Simple isolation, no SSE filtering by sessionID needed. |
| D19 | WebSocket library | `github.com/coder/websocket` | Stdlib-friendly, MIT, actively maintained, context-aware. |

Open (defer until needed): container isolation, Prometheus metrics, multi-repo projects, GitHub PR target.

---

## Worktree Lifecycle (D1)

**Paths**
- Worktree: `$CTRLROOM_DATA_DIR/worktrees/<repo_slug>/<workspace_id>/`
- Where `repo_slug = sha1(repo_path)[:12]` (stable across renames within reason; collisions acceptable for MVP)

**On workspace start** (`status: queued → preparing`)
```
git -C <repo_path> worktree add -b ctrlroom/<workspace_short> <worktree_path> <target_ref>
```
- `workspace_short` = first 8 chars of ULID-ish workspace id (e.g., `ws_01HXY...`)
- Branch name: `ctrlroom/<workspace_short>` — short, unique, greppable in `git log`
- `target_ref` = `project.default_branch` (auto-detect `main` vs `master` on project create; overridable)

**On workspace stop / turn end** (`→ idle`): keep the worktree so diff & merge still work.

**Cleanup** (`→ archived`): `git worktree remove <path>`. Triggered by:
- Explicit user action ("Archive workspace")
- Project deletion (cascades to all its workspaces)
- Background compactor: idle workspaces older than `CTRLROOM_WORKTREE_TTL` (default 14d), branch retained

**Failure path**: if `worktree add` fails (dirty index, branch exists), workspace → `failed` with the git stderr captured in `messages` as a `system` role entry.

---

## Workspace State Machine

```
                 ┌──────────┐
   create        │  queued  │
        ───────► │          │
                 └────┬─────┘
                      │ worktree add
                      ▼
                 ┌───────────┐
                 │ preparing │
                 └────┬──────┘
                      │ adapter.Spawn ok
                      ▼
        ┌───────  running  ◄────────┐
        │             │             │
        │     turn end│  user       │
        │             ▼  follow-up  │
        │      awaiting_input ──────┘
        │             │
        │             │ user: merge
        │             ▼
        │      completed/merged ◄──┐
        │                          │
        ▼          conflict + user │
   failed       accepts auto-resolve
        │                          │
        │                          ▼
        │              resolving_conflict
        │                          │
        │              turn end +   │
        │              retry merge ─┘  (cap 2)
        │
        ▼
   cancelled (user stop)
```

States persisted in `workspaces.status`. Transitions validated server-side; illegal transition → 409.

**Pending merge side-state**: when entering `resolving_conflict`, write to `workspaces.pending_merge` (JSON: `{target, attempt, max_attempts:2}`). Cleared on success or after `max_attempts`.

---

## Merge Flow (D3) — Detailed

```
POST /api/workspaces/:id/merge?branch=<optional override>

  server:
    target = override || project.default_branch
    ws.status must be in {completed, resolving_conflict}
    git -C <repo> fetch <remote>           # if remote configured
    git -C <repo> merge-tree --write-tree <target> <ws.branch>
      ├─ exit 0, stdout = merged tree SHA  → clean
      └─ exit 1, stdout = conflict report  → conflict

  clean:
    git update-ref refs/heads/<target> <merged_tree>
    # (or: git merge --ff-only if simpler; we use update-ref for atomicity)
    ws.status → merged
    return 200 { target, tree_sha }

  conflict:
    ws.pending_merge = {target, attempt:0, max_attempts:2}
    ws.status → (unchanged; client decides)
    return 409 {
      conflict_files: [...],
      base: <sha>, head: <sha>,
      recommendation: "auto_resolve"
    }
```

**Client UI on 409**
```
Merge conflicts in <n> files:
  - src/foo.go
  - src/bar.go

Recommendation: let the agent resolve these. It has full
context of what it just wrote and usually fixes conflicts
in one pass.

  [Let agent resolve]   [I'll fix manually]   [Abort]
```

**"Let agent resolve" path**
```
POST /api/workspaces/:id/message
  body: { kind: "merge_conflict", target: "<target>", files: [...] }

server:
  ws.status → resolving_conflict
  ws.pending_merge.attempt += 1
  injects the following as a user turn into the SAME session:

  ─── prompt begins ───
  Your branch has conflicts merging into `<target>`.

  Conflicted files:
    - <file>
    - <file>

  Resolve them now. Rules:
  1. Run `git status` then inspect each file's conflict markers.
  2. Prefer <target>'s refactors; keep your feature additions.
     Keep both sides only when they don't logically conflict.
  3. Do NOT modify files outside the conflicted set.
  4. Do NOT amend, rebase, or rewrite history. Just `git add`
     the resolved files and `git commit --no-edit`.
  5. Do not consider yourself done until `git status` is clean
     on your branch.

  When you finish, the merge will be retried automatically.
  ─── prompt ends ───

  on adapter done event:
    if ws.pending_merge:
      retry the merge-tree check
        ├─ clean  → merge, ws.status → merged, clear pending
        ├─ conflict & attempt < max → another follow-up turn
        │                            (sharper prompt, files listed)
        └─ conflict & attempt >= max → ws.status → conflict_stuck
                                       surface to user, stop auto
```

**"I'll fix manually" path**: server returns the conflicted file list and base/head SHAs. User edits in their own editor, commits on `ws.branch`, calls `POST /merge` again.

**Safety**
- Cap `max_attempts = 2` (configurable per project). After that the agent is stopped and the user is asked to intervene.
- `resolving_conflict` is a *running* sub-state: the agent keeps its normal tool access. No additional risk beyond the original workspace trust model (D2).
- Merge target update uses `git update-ref` (atomic) rather than a working-tree checkout — server never needs to touch `<repo>`'s working tree.

---

## Agent Adapter Contract (D4)

Concrete addendum to `README.md`'s interface. All three adapters must satisfy this exactly.

### Lifecycle invariants
- `Spawn` is **synchronous**: returns only after the protocol handshake is complete (Claude `system/init`; Codex `initialize` + `thread/start` ack; OpenCode `POST /session` 2xx). Errors here → `Spawn` returns error, manager marks workspace `failed`.
- `SendPrompt` may be called zero or more times after `Spawn`. Follow-ups queued if a turn is in flight (only one in-flight prompt per workspace).
- `Events()` returns a **buffered** channel (cap 64). Adapter never blocks on a full channel — on overflow it drops the oldest non-terminal event, emits `{type:"lag", dropped:N}`, and continues.
- `Stop()` is idempotent. Sends the graceful signal, waits up to 5s, then `SIGKILL` the process tree.

### Terminal events
An adapter MUST emit exactly one of:
- `{type:"done", metadata:{reason:"turn_complete"|"stopped"}}` — normal
- `{type:"error", content: "...", metadata:{fatal:true}}` then `{type:"done", metadata:{reason:"error"}}` — fatal

After terminal event, `Events()` channel is closed.

### Event taxonomy (canonical)

| `type` | `content` | `metadata` |
|---|---|---|
| `text` | streaming text delta | `{}` |
| `reasoning` | reasoning delta | `{}` |
| `tool_call` | tool name | `{tool, input, id}` |
| `tool_result` | result summary | `{id, is_error, diff?, path?}` |
| `file_change` | path | `{path, diff, kind: "create"\|"modify"\|"delete"}` |
| `cost` | — | `{tokens_in, tokens_out, cost_usd, model}` |
| `approval_request` | — | `{id, tool, command?, path?, diff?}` |
| `lag` | — | `{dropped}` |
| `error` | message | `{fatal}` |
| `done` | — | `{reason}` |

`approval_request` is only emitted in `prompt` approval mode (not in MVP `autonomous`).

### Per-adapter mapping (summary)

| Event | Claude | Codex | OpenCode |
|---|---|---|---|
| text | `assistant` content delta | `agentMessage` delta | `session.next.text.delta` |
| reasoning | `thinking` delta | `reasoning` delta | `session.next.reasoning.delta` |
| tool_call | `assistant.tool_use` | `commandExecution` start | `session.next.tool.called` |
| tool_result | `user.tool_result` | `commandExecution` end | `session.next.tool.success` |
| file_change | derived from `Edit`/`Write` tool results | `fileChange` item | derived from `Edit`/`Write` tool results |
| cost | `result.message.usage` | `turn/completed` | `session.next.step.ended` |
| done | `result` top-level | `turn/completed` (final) | `session.next.step.ended` (final, with `stop_reason`) |

### Conformance test (`internal/agent/conformance_test.go`)

A `FakeAdapter` replays recorded traces from `testdata/fixtures/*.ndjson` (one line per AgentEvent). The manager, websocket layer, and HTTP handlers all run their unit tests against `FakeAdapter`. Each *real* adapter has an integration smoke test (`//go:build integration`) that spawns the actual CLI binary; skipped by default, runs in a manual `make test-integration` target.

---

## Per-Workspace Injection

Before `Spawn`, the manager writes into the worktree root:

**`AGENTS.md`** (read by Claude, Codex, OpenCode natively)
```markdown
# Workspace: #<issue_id> — <issue_title>

Project: **<project_name>**
Branch: `<branch>` (from `<target>`)

## Goal
<issue.description, indented as blockquote>

## Done criteria
<derived from issue.tags or default: tests pass, changes committed,
nothing unrelated modified>

## ctrlroom CLI (on PATH if you have shell access)
- `ctrlroom issues close <id>` — when work is complete
- `ctrlroom workspace status` — your own state
- Do NOT start other workspaces from inside this one.
- Do NOT call `ctrlroom workspace merge` — merging is the server's job.

## Constraints
- Stay within this worktree.
- Don't rewrite history.
- Commit with `--no-edit`; the server handles branch hygiene.
```

**`opencode.json`** (OpenCode only) — derived from project tool policy.

**Environment** (all adapters)
- `CTRLROOM_WORKSPACE_ID`, `CTRLROOM_ISSUE_ID`, `CTRLROOM_PROJECT_ID`
- `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` from server env (passed through)
- `CTRLROOM_API_TOKEN` + `CTRLROOM_API_URL` — only if workspace is flagged `orchestrator` (see CLI Auth)
- `GIT_AUTHOR_NAME=ctrlroom`, `GIT_AUTHOR_EMAIL=ctrlroom@local`, same for committer — so agent commits are identifiable

`.gitignore` appended in worktree with: `AGENTS.md`, `opencode.json`, `.ctrlroom/`.

---

## WebSocket Streaming Protocol

**Endpoint**: `GET /api/workspaces/:id/stream` (WS upgrade, auth via cookie or `?token=` for CLI).

**Server → Client envelope**
```json
{ "seq": 42, "type": "agent", "event": { /* AgentEvent */ }, "ts": "..." }
{ "seq": 43, "type": "status", "workspace": { "status": "running", "agent_type": "claude" } }
{ "seq": 44, "type": "approval_request", "id": "...", "tool": "Bash", "command": "..." }
```
`seq` is per-workspace, monotonic, gap-free. Persisted to `messages.seq`.

**Client → Server**
```json
{ "type": "resume", "after": 41 }                  // replay missed
{ "type": "follow_up", "content": "..." }          // == POST /message
{ "type": "approve", "id": "...", "decision": true }
{ "type": "stop" }                                 // == POST /stop
```

**Resume**: client sends last `seq` seen on connect. Server replays from `messages` table (where `seq > after`), then switches to live fan-out.

**Backpressure**: outgoing buffer cap 256 events/socket. On overflow, server sends `{type:"lag", dropped:N}` and the client falls back to REST (`GET /messages?since=N`).

**Persistence**: every AgentEvent is written to `messages` (role derived from event type, content in `content`, full event in `metadata` JSON, `seq` assigned by a per-workspace counter in DB).

---

## Cost & Token Normalization

Each adapter emits `cost` events as it learns them. Final totals rolled up on `done`.

**Pricing table** (`internal/agent/pricing.go`, hand-maintained):
```go
var Pricing = map[string]ModelPricing{
    "claude-sonnet-4-5": {In: 3e-6, Out: 15e-6},
    "claude-opus-4-1":   {In: 15e-6, Out: 75e-6},
    "gpt-5-codex":       {In: 5e-6, Out: 20e-6},
    // ...
}
```
Unknown model → `cost_usd: null`, tokens still tracked.

**Schema additions** to README's `workspaces`:
```sql
ALTER TABLE workspaces ADD COLUMN tokens_in    INTEGER DEFAULT 0;
ALTER TABLE workspaces ADD COLUMN tokens_out   INTEGER DEFAULT 0;
ALTER TABLE workspaces ADD COLUMN cost_usd     REAL    DEFAULT 0;
ALTER TABLE workspaces ADD COLUMN pending_merge TEXT;       -- JSON or NULL
```
Project totals computed as `SUM(...)` queries; not denormalized.

---

## CLI Auth (Orchestrator Token)

New table (cleaner than overloading `sessions`):
```sql
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id),
    workspace_id TEXT REFERENCES workspaces(id),  -- NULL = user-issued
    token_hash   TEXT NOT NULL,                   -- sha256
    expires_at   DATETIME NOT NULL,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**Flow**
1. User creates a workspace with `orchestrator: true` (or a Job spawns one).
2. On `preparing → running`, server mints a 32-byte random token, stores its sha256, writes plaintext to `<worktree>/.ctrlroom/credentials` (chmod 0600, gitignored).
3. The orchestrator's `AGENTS.md` includes a setup hint; `ctrlroom auth login --from-file` reads it into `~/.config/ctrlroom/credentials` inside the worktree user's HOME.
4. Every CLI call sends `Authorization: Bearer <token>`.
5. Tokens expire (default 24h, configurable per project) and are revoked when the workspace leaves `running`.

User-issued tokens (for using `ctrlroom` CLI from a laptop against the server) are created via `POST /api/cli/token` and have `workspace_id = NULL`.

---

## Frontend State (D6)

**Libraries**
- Routing: `react-router` v6
- REST cache: `@tanstack/react-query` v5
- Streaming/ephemeral: Zustand v4 (`stores/*.ts`)
- DnD: `@dnd-kit/core` + `@dnd-kit/sortable`
- Diff: `react-diff-view` + `diff` parser
- Markdown: `react-markdown` + `rehype-sanitize`

**Zustand slices**
- `authStore` — user, login/logout
- `projectsStore` — list + selected id
- `boardStore` — issues by project; optimistic status updates with rollback
- `workspaceStore` — active workspace, event ring buffer (cap 1000), approval queue, connection state
- `jobsStore` — scheduler entries

**WebSocket**: single multiplexed connection per active workspace view. Reconnect with exponential backoff, always resumes via `seq`.

---

## Migrations (D7)

```go
//go:embed migrations/*.sql
var migrationFS embed.FS
```

- `migrations/001_init.sql`, `002_workspaces_costs.sql`, … — ordered, forward-only.
- `schema_migrations(version TEXT PRIMARY KEY, applied_at DATETIME)`.
- On boot: list embedded, sort by filename, apply unapplied in a tx, record version. Failure → fatal exit (no partial schema).

No down migrations. Recovery = restore backup + restart.

---

## Testing

| Layer | Tooling | What |
|---|---|---|
| Unit | `go test` | git wrapper, pricing, merge-tree parser, cron expr |
| Adapter | `FakeAdapter` + fixtures | manager, websocket, handlers against recorded traces |
| HTTP | `httptest` + in-memory SQLite | auth, CRUD, merge states |
| Integration | `//go:build integration` | each real adapter vs. its binary; `make test-integration` |
| Frontend | Vitest + Testing Library | board DnD, workspace event reducer, approval modal |
| E2E | Playwright (defer to Phase 7) | login → create issue → start workspace → merge |

Fixtures live in `internal/agent/testdata/fixtures/`:
- `claude_edit_file.ndjson`
- `codex_run_tests.ndjson`
- `opencode_multi_tool.ndjson`
- `claude_merge_conflict.ndjson`

Each is a real recorded session, redacted.

---

## Observability (D8)

- `slog.New(slog.NewJSONHandler(os.Stdout, nil))` — structured logs everywhere; `slog.With("workspace_id", ...)` per request scope.
- `x-request-id` middleware; propagated to logs and to outgoing adapter logs.
- `GET /api/health` → `{ db: "ok", agents: { claude: "present", codex: "missing", opencode: "present" } }`. Agent presence = `exec.LookPath`.
- `GET /api/stats` → workspace counts by status, cost totals (24h / all-time), active workspaces list. Powers a future dashboard; no Prometheus until there's a second consumer.

---

## Revised Build Phases (D4 — all adapters in parallel)

### Phase 1 — Skeleton (3-4 days)
- `go.mod`, chi router, slog, request-id middleware
- SQLite open + migration runner + `001_init.sql`
- Argon2 auth + session cookies
- Config from env (with sane defaults)
- Makefile: `make run / test / lint / fmt`
- CI: `golangci-lint`, `go test`, frontend `tsc --noEmit`

### Phase 2 — Kanban + Git (3-4 days)
- Projects, issues CRUD + REST handlers
- `internal/git` wrapper: branch, worktree add/remove, merge-tree, diff, default-branch detect
- Worktree lifecycle + workspace state machine
- Unit tests against a temp bare repo per test

### Phase 3 — Adapter Contract + Claude (4-5 days)
- `AgentAdapter` interface + `AgentEvent`
- `AgentManager`: spawn, stream, persist, lifecycle
- `ClaudeAdapter` (real)
- `FakeAdapter` + first fixtures
- Persistence of events to `messages` with `seq`

### Phase 4 — Codex + OpenCode (4-5 days)
- `CodexAdapter` (JSON-RPC over stdio)
- `OpenCodeAdapter` (HTTP + SSE)
- All three pass the same conformance test
- Unified key/env handling

### Phase 5 — Streaming + Frontend (5-6 days)
- WebSocket server with `seq` + resume
- React shell, login, routing, React Query setup
- Kanban board with dnd-kit
- Workspace view: streaming chat, tool calls, file diffs
- Approval modal (UI only; autonomous mode in MVP)

### Phase 6 — Merge + CLI + Scheduler (3-4 days)
- `POST /merge` with full conflict-resolution loop (D3)
- `cmd/ctrlroom` (Cobra): all subcommands from README
- Cron scheduler goroutine + `jobs` CRUD/UI
- Orchestrator token issuance

### Phase 7 — Deploy + Harden (2-3 days)
- Multi-stage Dockerfile (already drafted in README)
- `docker-compose.yml` (ctrlroom + Traefik labels)
- Smoke run against a real repo, all 3 agents
- Docs: trust model (D2), env reference, recovery, backup

**Total**: ~4-5 weeks solo, ~3 weeks with two people (Phase 3+4 parallelizes cleanly).

---

## Schema Additions (consolidated)

Add to README's schema:

```sql
-- workspaces
ALTER TABLE workspaces ADD COLUMN default_branch TEXT;
ALTER TABLE workspaces ADD COLUMN tokens_in      INTEGER DEFAULT 0;
ALTER TABLE workspaces ADD COLUMN tokens_out     INTEGER DEFAULT 0;
ALTER TABLE workspaces ADD COLUMN cost_usd       REAL    DEFAULT 0;
ALTER TABLE workspaces ADD COLUMN pending_merge  TEXT;   -- JSON {target, attempt, max_attempts} or NULL
ALTER TABLE workspaces ADD COLUMN orchestrator   BOOLEAN DEFAULT 0;

-- projects
ALTER TABLE projects ADD COLUMN default_branch TEXT;     -- 'main'/'master'/custom
ALTER TABLE projects ADD COLUMN approval_mode  TEXT DEFAULT 'autonomous'; -- autonomous|prompt|on_failure

-- messages
ALTER TABLE messages ADD COLUMN seq INTEGER;             -- per-workspace monotonic

-- api_tokens (new table — see CLI Auth)
```

---

## Open Questions (next round)

1. **Issue body format**: plain text vs. markdown vs. structured YAML front-matter for "done criteria"? Leaning markdown only.
2. **Secrets per project**: store provider keys per-project (encrypted at rest) or only via server env? Leaning env-only for MVP.
3. **Webhooks**: notify external systems on merge? Defer.
4. **Quotas**: per-user concurrent workspace cap? Yes, default 4, configurable.
5. **Branch prefix collisions** across projects pointing at the same repo: namespace as `ctrlroom/<project_slug>/<workspace_short>`? Probably yes.
