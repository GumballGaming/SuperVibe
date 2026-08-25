# SuperVibe Expansion — Frozen API Contract

This document freezes the interfaces added by the capability expansion. Go and
TypeScript implementations MUST match these signatures exactly. UI layout,
styling, and component placement are unchanged; only additive bindings/events
described here may surface in the existing components.

## New Go packages

- `supervibe/internal/modelsx` — model discovery, capabilities, provider health
- `supervibe/internal/browser` — safe web search/fetch/extract
- `supervibe/internal/tools` — internal tool registry + `procx` command runtime
- `supervibe/internal/contextx` — repo snapshots, mention resolution, file search

### modelsx

```go
type ModelInfo struct {
    Provider string `json:"provider"`
    ID       string `json:"id"`      // e.g. "openai/gpt-5.6" or "claude-sonnet-4-5"
    Label    string `json:"label"`
    ContextWindow int64 `json:"contextWindow,omitempty"`
    Suggested bool  `json:"suggested,omitempty"` // curated fallback, not discovered
}
type ModelSelection string // "none" | "freeform" | "dynamic"
type Capabilities struct {
    Streaming, Tools, FileEdit, Shell, Images, MCP bool
    Subagents, Resume, Usage, CostReport, ReasoningControls bool
    NativeWebBrowse bool
    ModelSelection ModelSelection `json:"modelSelection"`
}
func CapabilitiesFor(provider string) Capabilities
type HealthState string // "ready" | "not_installed" | "auth_required" | "misconfigured" | "error"
type Health struct { Provider string `json:"provider"`; State HealthState `json:"state"`; Version string `json:"version,omitempty"`; Detail string `json:"detail,omitempty"` }
func DiscoverOpencode(ctx context.Context, baseURL string) ([]ModelInfo, error)
func SuggestionsFor(provider string) []ModelInfo            // static fallbacks (marked Suggested)
func ProbeHealth(ctx context.Context, provider, bin string) Health // runs `<bin> --version`, classifies
```

### browser

```go
type Result struct { Title, URL, Snippet string }
type Page struct {
    URL, Title, ContentType string
    Text string        // readable extraction, capped
    Bytes int          // downloaded bytes
    Truncated bool
    TimestampMs int64
}
func ValidateURL(raw string) error           // http/https only; blocks loopback/RFC1918/link-local/metadata IPs
func Fetch(ctx context.Context, raw string, maxBytes int64) (*Page, error)
func Search(ctx context.Context, query string, limit int) ([]Result, error) // DuckDuckGo HTML endpoint
func ExtractText(html string, maxRunes int) string
```

### tools / procx

```go
package procx
type Options struct { Dir string; Env []string; Timeout time.Duration }
type OutputLine struct { Stream string /*stdout|stderr*/; Text string; Ts int64 }
type Proc struct{ ... }
func Start(ctx context.Context, name string, args []string, o Options) (*Proc, error)
func (p *Proc) Out() <-chan OutputLine
func (p *Proc) Wait() (exitCode int, err error)      // kills tree on timeout/cancel via taskkill /T /F on windows
func Shell(ctx context.Context, dir, command string, o Options) (*Proc, error) // powershell -NoProfile -NonInteractive -Command (windows-first)

package tools
type EventType string // started|output|completed|failed
type Event struct { CallID, Tool string; Type EventType; Text string; Fields map[string]string; Ts int64 }
type Decision string // allow|deny
type PermFunc func(tool, action string) Decision
type Deps struct {
    WorktreePath string
    Perm PermFunc            // nil = deny-by-default for mutating ops? nil = allow (tests)
    Browse func() Browser     // optional web delegate: interface{ Search/Fetch like browser pkg }
    CommandTimeout time.Duration
}
type Registry struct{...}
func New(d Deps) *Registry
func (r *Registry) Execute(ctx context.Context, callID, tool string, argsJSON []byte, emit func(Event)) (summary string, err error)
```

Tool names: `read_file write_file patch_file list_dir grep_files search_files
git_status git_diff git_log git_stage git_unstage git_commit run_command
web_search web_fetch project_meta`. All paths resolve inside the worktree;
`..`/absolute escapes rejected. `run_command` uses procx.Shell with timeout.

### contextx

```go
type Limits struct { TreeEntries, MaxFileBytes, TotalBytes int }
func RepoBrief(root string, lim Limits) (string, error)   // branch/ahead-behind/dirty/tree(≤lim)/recent commits/readme head
func ParseMentions(text string) (cleanText string, tokens []string)
func ResolveMention(ctx context.Context, root, token string, lim Limits, fetcher func(url string) (string, error)) (block string, err error)
// tokens: "@diff" "@git" "@tree" "@src/app.ts" "@https://..."
func FileSearch(root, query string, limit int) ([]string, error) // rel paths, skips VCS/vendor dirs
```

## Store schema v2 (applied by coordinator)

```sql
ALTER TABLE sessions ADD COLUMN title TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN title_locked INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN parent_id TEXT REFERENCES sessions(id) ON DELETE SET NULL;
ALTER TABLE sessions ADD COLUMN baseline_head TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN cost_known INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN cached_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN reasoning_tokens INTEGER NOT NULL DEFAULT 0;
```

New store methods: `RenameSession`, `AutoTitle`, `SetBaselineHead`,
`UpdateSessionUsage`, `GetSessionChildren`, `DeleteSessionByID`, `ForkSession`,
`SearchSessions`.

## Wails bindings added on App

```go
ListModels(provider string) ([]modelsx.ModelInfo, error)
RefreshModels(provider string) ([]modelsx.ModelInfo, error)
GetCapabilities(provider string) (modelsx.Capabilities, error)
GetProviderHealth() ([]modelsx.Health, error)
RenameSession(id, title string) error
DeleteSession(id string) error
ForkSession(id string, upToMessageID int64) (*store.Session, error)
RetryLastMessage(id string) error
SpawnSubagent(parentID, task, provider, model string) (*store.Session, error)
GetPermissions() (map[string]string, error)
SetPermissions(map[string]string) error
ApprovePermission(requestID string, allow bool) error
GitStage(worktreeID string, paths []string) error
GitUnstage(worktreeID string, paths []string) error
GitCommit(worktreeID string, message string) error
RevertSessionChanges(sessionID string) error
GetSessionDiff(sessionID string) (*DiffResult, error)   // diff vs session baseline_head
GetRecentCommits(worktreeID string, n int) ([]gitx.CommitInfo, error)
SearchRepo(worktreeID, query string, limit int) ([]string, error)
ReadFileText(worktreeID, relPath string, maxBytes int) (string, error)
SendMessageExtended(sessionID, text string, mentions, attachmentPaths []string) error
WebSearch(query string) ([]browser.Result, error)
WebFetch(url string) (*browser.Page, error)
RunCommand(worktreeID, command string, timeoutSec int) (int, error)
```

## Events (Wails topics)

- `agent:event` (unchanged topic). `AgentEvent` gains:
  `cachedTokens`, `reasoningTokens` int64 fields.
- NEW `proc:event` → `{sessionID, worktreeID, kind:"stdout"|"stderr"|"exit", line, code}`
- NEW `perm:request` → `{requestID, tool, action, detail}`; resolved via `ApprovePermission`.

## Permission settings keys (policy = ask|session|always|deny)

`perm.file_write perm.command perm.network perm.git_commit perm.git_destructive perm.spawn_subagent`
Defaults: `perm.file_write=always perm.command=session perm.network=session perm.git_commit=ask perm.git_destructive=deny perm.spawn_subagent=session`.

Runtime keys: `runtime.maxConcurrent` (default 8), `runtime.cmdTimeoutSec` (default 120),
`browse.enabled` (default true), `defaults.provider` (default claude).

## Session lifecycle semantics

- Baseline HEAD recorded at session start (`baseline_head`) for session-scoped diffs/revert.
- Auto-title from first user prompt (≤8 words, cleaned); skipped once locked (manual rename).
- Cancel: stops children first (recursively), marks system message "cancelled by user", status idle.
- Subagent depth limit 1 (children cannot spawn); concurrency cap from `runtime.maxConcurrent`.
- Usage accumulation per turn; `cost_known=true` only when provider reported cost.

## TypeScript additions (frontend/src/lib/types.ts + backend.ts mirror all of the above)

AgentEvent += cachedTokens?, reasoningTokens?. Session += title, titleLocked,
parentId, baselineHead, costKnown, cachedTokens, reasoningTokens. api object gains
1:1 methods for every binding above. Events: `proc:event`, `perm:request`.
