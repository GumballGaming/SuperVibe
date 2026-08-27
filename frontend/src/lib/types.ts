export type Provider = "claude" | "codex";

export interface Project {
  id: string;
  name: string;
  path: string;
  createdAt: number;
}

export interface Worktree {
  id: string;
  projectId: string;
  name: string;
  branch: string;
  path: string;
  isPrimary: boolean;
  createdAt: number;
}

export interface ProjectTree {
  project: Project;
  worktrees: Worktree[];
}

export interface Session {
  id: string;
  worktreeId: string;
  provider: Provider;
  model: string;
  status: "starting" | "idle" | "running" | "waiting" | "error" | "done";
  providerSessionId: string;
  error: string;
  lastMessage: string;
  cost: number;
  tokensIn: number;
  tokensOut: number;
  pid: number;
  createdAt: number;
  updatedAt: number;
  title?: string;
  titleLocked?: boolean;
  parentId?: string;
  baselineHead?: string;
  costKnown?: boolean;
  cachedTokens?: number;
  reasoningTokens?: number;
}

export interface FleetRow extends Session {
  worktreeName: string;
  branch: string;
  projectName: string;
  projectPath: string;
}

export interface SessionDetail {
  session: Session;
  messages: Message[];
}

export interface Message {
  id: number;
  sessionId: string;
  role: string;
  kind: string;
  content: string;
  meta: string;
  ts: number;
}

export interface GitStatus {
  branch: string;
  ahead: number;
  behind: number;
  staged: number;
  unstaged: number;
  untracked: number;
}

export interface WorktreeInfo {
  id: string;
  projectId: string;
  name: string;
  branch: string;
  path: string;
  isPrimary: boolean;
  createdAt: number;
  gitStatus: GitStatus | null;
  head: string;
}

export interface Branch {
  name: string;
  sha: string;
  isCurrent: boolean;
}

export interface DiffResult {
  stat: string;
  patch: string;
  stagedStat?: string;
  stagedPatch?: string;
  baselineHead?: string;
}

export type ModelSelection = "none" | "freeform" | "dynamic";

export interface ModelInfo {
  provider: string;
  id: string;
  label: string;
  contextWindow?: number;
  suggested?: boolean;
  fastMode?: boolean;
  reasoningEfforts?: string[];
}

export interface Capabilities {
  streaming: boolean;
  tools: boolean;
  fileEdit: boolean;
  shell: boolean;
  images: boolean;
  mcp: boolean;
  subagents: boolean;
  resume: boolean;
  usage: boolean;
  costReport: boolean;
  reasoningControls: boolean;
  nativeWebBrowse: boolean;
  modelSelection: ModelSelection;
}

export type ProviderHealthState =
  | "ready"
  | "not_installed"
  | "auth_required"
  | "misconfigured"
  | "error";

export interface ProviderHealth {
  provider: string;
  state: ProviderHealthState;
  version?: string;
  detail?: string;
}

export interface TerminalEvent {
  id: string;
  kind: "started" | "output" | "exit";
  data: string;
}

export interface PermissionRequest {
  requestID: string;
  tool: string;
  action: string;
  detail: string;
}

export interface ProcEvent {
  sessionID: string;
  worktreeID?: string;
  kind: "stdout" | "stderr" | "exit";
  line: string;
  code?: number;
}

export interface CommitInfo {
  sha: string;
  subject: string;
  author: string;
  when: number;
}

export interface WebResult {
  title: string;
  url: string;
  snippet: string;
}

export interface WebPage {
  url: string;
  title: string;
  contentType: string;
  text: string;
  bytes: number;
  truncated: boolean;
  timestampMs: number;
}

export interface SessionSearchHit {
  sessionId: string;
  field?: string;
  snippet?: string;
}

export interface SearchResult {
  fleet: FleetRow[];
  hits: SessionSearchHit[];
}

export type AgentEventType =
  | "status"
  | "delta"
  | "thinking_delta"
  | "message"
  | "tool_start"
  | "tool_end"
  | "file_change"
  | "result"
  | "error"
  | "provider_session"
  | "part_upsert";

export interface AgentEvent {
  sessionId?: string;
  type: AgentEventType;
  status?: string;
  role?: string;
  kind?: string;
  text?: string;
  partId?: string;
  toolCallId?: string;
  toolName?: string;
  toolInput?: string;
  toolResult?: string;
  paths?: string[];
  providerSessionId?: string;
  error?: string;
  costUsd?: number;
  tokensIn?: number;
  tokensOut?: number;
  cachedTokens?: number;
  reasoningTokens?: number;
  durationMs?: number;
  ts: number;
}

export interface SessionEvent {
  sessionId: string;
  event: AgentEvent;
}

export type ChatItem =
  | { id: string; type: "user"; text: string; ts: number }
  | { id: string; type: "assistant" | "thinking" | "system"; text: string; ts: number; streaming?: boolean }
  | { id: string; type: "tool"; key: string; name: string; action?: string; input?: string; result?: string; running: boolean; status?: "running" | "success" | "error" }
  | { id: string; type: "files"; paths: string[]; ts: number };

export interface Draft {
  text: string;
  thinking: string;
}

export interface SessionView {
  session: Session;
  info: WorktreeInfoLite;
  items: ChatItem[];
  draft: Draft;
}

export interface WorktreeInfoLite {
  worktreeName: string;
  branch: string;
  projectName: string;
  projectPath: string;
}
