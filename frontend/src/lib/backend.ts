import type {
  Branch,
  Capabilities,
  CommitInfo,
  DiffResult,
  FleetRow,
  ModelInfo,
  ProjectTree,
  ProviderHealth,
  SearchResult,
  Session,
  SessionDetail,
  WebPage,
  WebResult,
  WorktreeInfo,
} from "./types";

type GoApp = Record<string, (...args: unknown[]) => Promise<unknown>>;

interface WailsRuntime {
  EventsOn: (name: string, cb: (...data: unknown[]) => void) => () => void;
  WindowMinimise: () => void;
  WindowToggleMaximise: () => void;
  WindowHide: () => void;
  Quit: () => void;
}


function goApp(): GoApp | undefined {
  if (typeof window === "undefined") return undefined;
  const w = window as unknown as { go?: { app?: { App?: GoApp } } };
  return w.go?.app?.App;
}

function rt(): WailsRuntime | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { runtime?: WailsRuntime }).runtime;
}

export function hasBackend(): boolean {
  const app = goApp();
  return !!app && Object.keys(app).length > 0;
}

export function runtimeEvents(): WailsRuntime | undefined {
  return rt();
}

export function onRuntimeEvent(name: string, callback: (...data: unknown[]) => void): () => void {
  const events = rt();
  if (!events) return () => undefined;
  return events.EventsOn(name, callback);
}

export async function openFileDialog(title: string): Promise<string[]> {
  try {
    const picked = await call<string[]>("OpenMultipleFilesDialog", title);
    return Array.isArray(picked) ? picked : [];
  } catch {
    return [];
  }
}

export async function openDirectoryDialog(title: string): Promise<string> {
  try {
    return (await call<string>("OpenDirectoryDialog", title)) ?? "";
  } catch {
    return "";
  }
}

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const app = goApp();
  if (!app || typeof app[method] !== "function") {
    throw new Error(`backend unavailable (${method})`);
  }
  return app[method](...args) as Promise<T>;
}

export const api = {
  listProjects: () => call<ProjectTree[]>("ListProjects"),
  addProject: (path: string) => call<ProjectTree>("AddProject", path),
  removeProject: (id: string) => call<void>("RemoveProject", id),
  createWorktree: (projectId: string, branch: string, baseRef: string) =>
    call<WorktreeInfo>("CreateWorktree", projectId, branch, baseRef),
  deleteWorktree: (id: string, force: boolean) => call<void>("DeleteWorktree", id, force),
  getWorktreeInfo: (id: string) => call<WorktreeInfo>("GetWorktreeInfo", id),
  listBranches: (worktreeId: string) => call<Branch[]>("ListBranches", worktreeId),

  startSession: (worktreeId: string, provider: string, model: string) =>
    call<Session>("StartSession", worktreeId, provider, model),
  sendMessage: (sessionId: string, prompt: string) => call<void>("SendMessage", sessionId, prompt),
  sendMessageConfigured: (sessionId: string, prompt: string, model: string, reasoningEffort: string, fastMode: boolean) =>
    call<void>("SendMessageConfigured", sessionId, prompt, model, reasoningEffort, fastMode),
  interruptSession: (sessionId: string) => call<void>("InterruptSession", sessionId),
  stopSession: (sessionId: string) => call<void>("StopSession", sessionId),
  resumeSession: (sessionId: string) => call<void>("ResumeSession", sessionId),

  getSessionDetail: (sessionId: string) => call<SessionDetail>("GetSessionDetail", sessionId),
  listFleet: (statuses: string[] | null) => call<FleetRow[]>("ListFleet", statuses),
  getDiff: (worktreeId: string) => call<DiffResult>("GetDiff", worktreeId),
  getOutputTail: (sessionId: string, maxBytes: number) =>
    call<string>("GetOutputTail", sessionId, maxBytes),

  detectClis: () => call<Record<string, boolean>>("DetectCLIs"),
  getSettings: () => call<Record<string, string>>("GetSettings"),
  setSettings: (values: Record<string, string>) => call<void>("SetSettings", values),

  listModels: (provider: string) => call<ModelInfo[]>("ListModels", provider),
  refreshModels: (provider: string) => call<ModelInfo[]>("RefreshModels", provider),
  getCapabilities: (provider: string) => call<Capabilities>("GetCapabilities", provider),
  getProviderHealth: () => call<ProviderHealth[]>("GetProviderHealth"),

  renameSession: (id: string, title: string) => call<void>("RenameSession", id, title),
  deleteSession: (id: string) => call<void>("DeleteSession", id),
  forkSession: (id: string, upToMessageID: number) =>
    call<Session>("ForkSession", id, upToMessageID),
  retryLastMessage: (id: string) => call<void>("RetryLastMessage", id),
  spawnSubagent: (parentID: string, task: string, provider: string, model: string) =>
    call<Session>("SpawnSubagent", parentID, task, provider, model),

  getPermissions: () => call<Record<string, string>>("GetPermissions"),
  setPermissions: (values: Record<string, string>) => call<void>("SetPermissions", values),
  approvePermission: (requestID: string, allow: boolean) =>
    call<void>("ApprovePermission", requestID, allow),

  gitStage: (worktreeId: string, paths?: string[]) => call<void>("GitStage", worktreeId, paths),
  gitUnstage: (worktreeId: string, paths?: string[]) => call<void>("GitUnstage", worktreeId, paths),
  gitCommit: (worktreeId: string, message: string) => call<void>("GitCommit", worktreeId, message),
  revertSessionChanges: (sessionId: string) => call<void>("RevertSessionChanges", sessionId),
  getSessionDiff: (sessionId: string) => call<DiffResult>("GetSessionDiff", sessionId),
  getRecentCommits: (worktreeId: string, n: number) =>
    call<CommitInfo[]>("GetRecentCommits", worktreeId, n),

  searchRepo: (worktreeId: string, query: string, limit: number) =>
    call<string[]>("SearchRepo", worktreeId, query, limit),
  readFileText: (worktreeId: string, relPath: string, maxBytes: number) =>
    call<string>("ReadFileText", worktreeId, relPath, maxBytes),
  sendMessageExtended: (sessionId: string, text: string, mentions: string[], attachmentPaths: string[]) =>
    call<void>("SendMessageExtended", sessionId, text, mentions, attachmentPaths),
  searchSessions: (query: string, limit?: number) =>
    call<SearchResult>("SearchSessions", query, limit ?? 200),

  webSearch: (query: string) => call<WebResult[]>("WebSearch", query),
  webFetch: (url: string) => call<WebPage>("WebFetch", url),
  runCommand: (worktreeId: string, command: string, timeoutSec: number) =>
    call<number>("RunCommand", worktreeId, command, timeoutSec),
};

export function windowControls() {
  return {
    minimise: () => rt()?.WindowMinimise(),
    toggleMaximise: () => rt()?.WindowToggleMaximise(),
    quit: () => rt()?.WindowHide(),
  };
}
