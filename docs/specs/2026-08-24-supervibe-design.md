# SuperVibe — Design Spec

Date: 2026-08-24
Status: Approved

## Summary

SuperVibe is a Windows-first desktop GUI for orchestrating large numbers of coding
agents (Claude Code, Codex, opencode) across git projects and worktrees.
Conductor-inspired visual language: extremely dark, minimal, rounded, premium,
~95% grayscale with a sparing orange accent.

## Stack

- **Shell:** Wails v2 (frameless window, custom chrome) + Go 1.27
- **Frontend:** React + TypeScript + Vite, managed by Bun
- **State:** zustand; lists virtualized with @tanstack/react-virtual
- **Persistence:** SQLite via `modernc.org/sqlite` (pure Go, no CGO), WAL mode
- **Git:** shell out to `git` CLI
- **Icons/Fonts:** lucide-react; Inter + JetBrains Mono bundled via @fontsource

## Architecture

One real agent process per session, spoken to via its native structured protocol:

| Provider | Launch | Protocol | Resume |
|---|---|---|---|
| Claude Code | persistent process per session, `claude -p --input-format stream-json --output-format stream-json --verbose --include-partial-messages --permission-mode <mode>` | NDJSON on stdout (`system/init`, `stream_event`, `assistant`, `user`, `result`) | `--resume <provider_session_id>` |
| Codex | one process per turn: `codex exec --json [--model m] [--sandbox s]`, cwd = worktree | JSONL events (`thread.started`, `item.*`, `turn.completed`) | `codex exec resume --last` scoped by cwd |
| opencode | one `opencode serve` process per project; HTTP REST + SSE `/event` | sessions/messages/parts over HTTP, deltas over SSE | native session IDs |

Go packages:

- `internal/agent` — `Adapter` interface (Start/Send/Interrupt/Stop/Events),
  per-provider implementations + line parsers. Unified `AgentEvent` model:
  status/delta/message/tool_start/tool_end/thinking/file_change/result/error.
- `internal/supervisor` — registry of live sessions: lifecycle, crash detection,
  stderr/stdout ring buffers (for Output tab), delta coalescing (~50 ms flush)
  so hundreds of streams do not flood IPC.
- `internal/gitx` — repo detect, branches, worktree add/list/remove, status
  porcelain v2, diff.
- `internal/store` — SQLite schema + CRUD:
  `projects`, `worktrees`, `sessions`, `messages`, `settings`.
- `main` / `internal/app` — Wails bindings + event bridge
  (`agent:event:<sessionID>`).

Events flow: adapter → supervisor (coalesce) → store (persist) → Wails event →
zustand reducer → React.

## Data model

```
projects(id, name, path UNIQUE, created_at)
worktrees(id, project_id FK, name, branch, path UNIQUE, is_primary, created_at)
sessions(id, worktree_id FK, provider, model, status, provider_session_id,
         error, cost, tokens_in, tokens_out, last_message, pid,
         created_at, updated_at)
messages(id AUTOINC, session_id FK, role, kind, content, meta, ts)
settings(key PK, value)
```

Session status: `idle | running | waiting | done | error`.

## UI

- Custom frameless title bar (draggable, understated wordmark, dark controls).
- Sidebar: projects → nested worktrees as subtle rounded groups; small branch
  icons; active surface = slightly lighter bg, no aggressive highlight.
- Workspace: large rounded elevated container with tabs Chat / Diff / Output;
  header shows breadcrumb, provider+model selector, New Session.
- Chat: user turns as elevated rounded blocks; assistant text streamed with
  markdown rendering (marked + DOMPurify); tool calls as collapsible rows
  (name, arg preview, input/result monospace); thinking blocks muted italic.
- Composer: auto-growing textarea, Enter=send Shift+Enter=newline, send/stop.
- Fleet view: virtualized table of all sessions across projects — status dot,
  provider, project/worktree, last message preview, cost, duration, actions.
- Dialogs: Add Project, New Worktree, Settings (CLI paths, permission mode,
  sandbox mode, auto-approve).
- Motion: 120–180 ms ease transitions only.

## Design tokens

Exact palette from brief:

```css
--background:#0D0D0E; --sidebar:#151516; --surface:#1B1B1D;
--surface-elevated:#202022; --surface-hover:#27272A;
--border:#303034; --border-subtle:#252527;
--text-primary:#F2F2F2; --text-secondary:#A1A1A6; --text-muted:#68686E;
--accent:#FF6B3D; --accent-hover:#FF7A50;
--radius-sm:8px; --radius-md:12px; --radius-lg:16px; --radius-xl:20px;
```

## Scope guards (v1)

- Unattended runs: claude `--permission-mode` configurable; codex sandbox
  configurable; opencode permissions allowed via generated config file
  (`OPENCODE_CONFIG`). No interactive approval prompts in v1.
- One codex session per worktree at a time (resume-by-cwd constraint).
- Output tab = captured raw output ring buffer (no xterm emulation yet).
- No MCP config UI, no OS notifications.

## Verification

- Go: golden-fixture parser tests per provider; store CRUD test on temp DB;
  git module tests against temp repos; supervisor coalescer test.
- Frontend: bun tests for event reducer + formatting utils.
- Gates: `go vet ./...`, `go test ./...`, `bun run build`, final `wails build`.
