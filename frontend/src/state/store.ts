import { create } from "zustand";
import { api, hasBackend, onRuntimeEvent } from "../lib/backend";
import { playDing } from "../lib/sound";
import type {
  AgentEvent,
  Capabilities,
  ChatItem,
  Draft,
  FleetRow,
  Message,
  ModelInfo,
  PermissionRequest,
  ProcEvent,
  ProjectTree,
  Session,
  WorktreeInfoLite,
} from "../lib/types";

export type ViewMode = "fleet" | "workspace";
export type Tab = "chat" | "diff" | "output" | "terminal" | "workspace" | "browser";
export type TerminalKind = "shell" | "codex" | "claude";
export interface TerminalSession {
  id: string;
  worktreeId: string;
  title: string;
  slot: number;
  kind: TerminalKind | null;
  detected?: boolean;
  mode: "auto" | "approve-all" | "manual";
}

// A worktree owns six fixed terminal slots. A slot keeps its number for the
// lifetime of the terminal in it, so the numbered tab strip is stable.
export const TERMINAL_SLOTS = 6;
export const TERMINAL_SLOT_NUMBERS = Array.from({ length: TERMINAL_SLOTS }, (_, index) => index + 1);

export function terminalsForWorktree(terminals: Record<string, TerminalSession>, worktreeId: string): TerminalSession[] {
  return Object.values(terminals)
    .filter((terminal) => terminal.worktreeId === worktreeId)
    .sort((a, b) => a.slot - b.slot);
}

function nextFreeSlot(terminals: TerminalSession[], worktreeId: string): number | null {
  const taken = new Set(terminals.filter((terminal) => terminal.worktreeId === worktreeId).map((terminal) => terminal.slot));
  for (const slot of TERMINAL_SLOT_NUMBERS) {
    if (!taken.has(slot)) return slot;
  }
  return null;
}

export type DialogState =
  | { kind: "addProject" }
  | { kind: "newWorktree"; projectId: string }
  | { kind: "newAgent"; projectId: string; initialWorktreeId?: string }
  | { kind: "settings" }
  | { kind: "rename"; sessionId: string }
  | { kind: "deleteSession"; sessionId: string }
  | { kind: "subagent"; sessionId: string }
  | { kind: "browseFiles"; worktreeId: string }
  | null;

export interface Toast {
  id: number;
  kind: "error" | "info" | "success";
  title: string;
  detail?: string;
}

interface AppState {
  ready: boolean;
  degraded: boolean;
  projects: ProjectTree[];
  sessions: Record<string, Session>;
  sessionInfo: Record<string, WorktreeInfoLite>;
  items: Record<string, ChatItem[]>;
  drafts: Record<string, Draft>;
  loadedSessions: Set<string>;
  view: ViewMode;
  tab: Tab;
  selectedWorktreeId: string | null;
  selectedSession: Record<string, string>;
  dialog: DialogState;
  toasts: Toast[];
  clis: Record<string, boolean>;
  capabilities: Record<string, Capabilities>;
  models: Record<string, ModelInfo[]>;
  permissions: Record<string, string>;
  pendingRequests: PermissionRequest[];
  procLines: Record<string, string[]>;
  settings: Record<string, string>;
  terminalSessions: Record<string, TerminalSession>;
  selectedWorkspaceSession: Record<string, string>;
  // worktreeId -> split key ("${dir}:${path}") -> ratio, see lib/terminalLayout.
  terminalSplitRatios: Record<string, Record<string, number>>;
}

interface AppActions {
  init: () => Promise<void>;
  restoreTerminalSessions: () => void;
  loadAll: () => Promise<void>;
  refreshProjects: () => Promise<void>;
  refreshSessionLists: () => Promise<void>;
  setView: (v: ViewMode) => void;
  setTab: (t: Tab) => void;
  selectWorktree: (id: string | null) => void;
  openSession: (sessionId: string) => Promise<void>;
  newSessionSelected: (worktreeId: string, sessionId: string) => void;
  setDialog: (d: DialogState) => void;
  pushToast: (t: Omit<Toast, "id">) => void;
  dismissToast: (id: number) => void;
  detectClis: () => Promise<void>;
  applyEvent: (sessionId: string, ev: AgentEvent) => void;
  optimisticUserMessage: (sessionId: string, text: string) => void;
  ensureCaps: (provider: string) => Promise<Capabilities | null>;
  loadModels: (provider: string, refresh?: boolean) => Promise<ModelInfo[]>;
  refreshSettings: () => Promise<void>;
  approve: (req: PermissionRequest, allow: boolean) => Promise<void>;
  renameSession: (id: string, title: string) => Promise<void>;
  deleteSession: (id: string) => Promise<void>;
  forkSession: (id: string, upToMessageId: number) => Promise<Session | null>;
  spawnSubagent: (parentId: string, task: string, provider: string, model: string) => Promise<Session | null>;
  retryLast: (id: string) => Promise<void>;
  selectWorkspaceSession: (worktreeId: string, sessionId: string) => void;
  createTerminalSession: (worktreeId: string, slot?: number) => string | null;
  createTerminalBatch: (worktreeId: string, count: number, kind: TerminalKind) => string[];
  configureTerminalBatch: (id: string, count: number, kind: TerminalKind) => string[];
  configureTerminalSession: (id: string, kind: TerminalKind) => void;
  setTerminalKind: (id: string, kind: TerminalKind) => void;
  updateTerminalWorktree: (id: string, worktreeId: string) => void;
  closeTerminalSession: (id: string) => void;
  setTerminalSplitRatio: (worktreeId: string, key: string, ratio: number) => void;
  resetTerminalSplitRatio: (worktreeId: string, key: string) => void;
}

const TERMINAL_LAYOUT_STORAGE_KEY = "supervibe.terminal-layout.v1";

interface PersistedTerminalLayout {
  terminalSessions: Record<string, TerminalSession>;
  selectedWorkspaceSession: Record<string, string>;
  selectedWorktreeId: string | null;
  terminalSplitRatios: Record<string, Record<string, number>>;
}

function persistTerminalLayout(state: Pick<AppState, "terminalSessions" | "selectedWorkspaceSession" | "selectedWorktreeId" | "terminalSplitRatios">): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(TERMINAL_LAYOUT_STORAGE_KEY, JSON.stringify({
      terminalSessions: state.terminalSessions,
      selectedWorkspaceSession: state.selectedWorkspaceSession,
      selectedWorktreeId: state.selectedWorktreeId,
      terminalSplitRatios: state.terminalSplitRatios,
    } satisfies PersistedTerminalLayout));
  } catch {
    // Storage may be unavailable in restricted WebView/private browsing modes.
  }
}

/** Ratios are cheap to keep but meaningless once a worktree is gone. */
function readSplitRatios(raw: Record<string, Record<string, number>> | undefined, worktreeIds: Set<string>): Record<string, Record<string, number>> {
  const out: Record<string, Record<string, number>> = {};
  for (const [worktreeId, ratios] of Object.entries(raw || {})) {
    if (!worktreeIds.has(worktreeId) || !ratios) continue;
    const clean: Record<string, number> = {};
    for (const [key, ratio] of Object.entries(ratios)) {
      if (typeof ratio === "number" && Number.isFinite(ratio)) clean[key] = Math.min(0.9, Math.max(0.1, ratio));
    }
    if (Object.keys(clean).length) out[worktreeId] = clean;
  }
  return out;
}

let toastSeq = 1;
let itemSeq = 1;
const nextItemId = () => `it_${Date.now().toString(36)}_${(itemSeq++).toString(36)}`;

const emptyDraft = (): Draft => ({ text: "", thinking: "" });

export function messagesToItems(msgs: Message[]): ChatItem[] {
  const items: ChatItem[] = [];
  const toolByKey = new Map<string, ChatItem>();
  for (const m of msgs) {
    let meta: Record<string, string> = {};
    try {
      if (m.meta) meta = JSON.parse(m.meta);
    } catch {
      meta = {};
    }
    switch (`${m.role}:${m.kind}`) {
      case "user:text":
        items.push({ id: `db_${m.id}`, type: "user", text: m.content, ts: m.ts });
        break;
      case "assistant:text":
      case "assistant:meta": {
        const id = meta.partId ? `part_${meta.partId}` : `db_${m.id}`;
        upsertText(items, id, "assistant", m.content, m.ts);
        break;
      }
      case "assistant:thinking": {
        const id = meta.partId ? `part_${meta.partId}` : `db_${m.id}`;
        upsertText(items, id, "thinking", m.content, m.ts);
        break;
      }
      case "tool:tool_start": {
        const item: ChatItem = {
          id: `db_${m.id}`,
          type: "tool",
          key: meta.id || `t_${m.id}`,
          name: m.content || "tool",
          action: toolAction(m.content || "tool"),
          input: meta.input,
          running: true,
          status: "running",
        };
        toolByKey.set(item.type === "tool" ? item.key : "", item);
        items.push(item);
        break;
      }
      case "tool:tool_end": {
        const existing = meta.id ? toolByKey.get(meta.id) : undefined;
        if (existing && existing.type === "tool") {
          existing.result = meta.result;
          existing.running = false;
          existing.status = "success";
        } else {
          const item: ChatItem = {
            id: `db_${m.id}`,
            type: "tool",
            key: meta.id || `t_${m.id}`,
            name: "tool",
            result: meta.result,
            running: false,
            status: "success",
          };
          items.push(item);
        }
        break;
      }
      case "system:files":
        items.push({
          id: `db_${m.id}`,
          type: "files",
          paths: m.content.split("\n").filter(Boolean),
          ts: m.ts,
        });
        break;
      case "system:error":
        items.push({ id: `db_${m.id}`, type: "system", text: m.content, ts: m.ts });
        break;
      default:
        break;
    }
  }
  return items;
}

function upsertText(items: ChatItem[], id: string, type: "assistant" | "thinking", text: string, ts: number) {
  const idx = items.findIndex((it) => it.id === id);
  if (idx >= 0 && (items[idx].type === "assistant" || items[idx].type === "thinking")) {
    items[idx] = { ...items[idx], text, ts } as ChatItem;
  } else if (idx < 0) {
    items.push({ id, type, text, ts });
  }
}

function flushDraft(draft: Draft, items: ChatItem[]) {
  if (draft.thinking.trim()) {
    items.push({ id: nextItemId(), type: "thinking", text: draft.thinking, ts: Date.now() });
  }
  if (draft.text.trim()) {
    items.push({ id: nextItemId(), type: "assistant", text: draft.text, ts: Date.now() });
  }
  draft.text = "";
  draft.thinking = "";
}

export function reduceEvent(
  session: Session,
  items: ChatItem[],
  draft: Draft,
  ev: AgentEvent
): { toast?: { kind: Toast["kind"]; title: string; detail?: string }; lastMessage?: string } {
  let toast: { kind: Toast["kind"]; title: string; detail?: string } | undefined;
  let lastMessage: string | undefined;
  if (ev.cachedTokens) session.cachedTokens = (session.cachedTokens || 0) + ev.cachedTokens;
  if (ev.reasoningTokens) session.reasoningTokens = (session.reasoningTokens || 0) + ev.reasoningTokens;
  switch (ev.type) {
    case "status":
      if (ev.status) session.status = normalizeStatus(ev.status, session.status);
      break;
    case "provider_session":
      session.providerSessionId = ev.providerSessionId || session.providerSessionId;
      break;
    case "delta":
      draft.text += ev.text || "";
      break;
    case "thinking_delta":
      draft.thinking += ev.text || "";
      break;
    case "message": {
      const kind = ev.kind === "thinking" ? "thinking" : "assistant";
      if (kind === "thinking") {
        if (draft.thinking.trim() && draft.thinking === ev.text) {
          items.push({ id: nextItemId(), type: "thinking", text: draft.thinking, ts: ev.ts });
          draft.thinking = "";
        } else {
          flushDraft(draft, items);
          items.push({ id: nextItemId(), type: "thinking", text: ev.text || "", ts: ev.ts });
        }
      } else {
        if (draft.text.trim() && draft.text === ev.text) {
          items.push({ id: nextItemId(), type: "assistant", text: draft.text, ts: ev.ts });
          draft.text = "";
        } else {
          flushDraft(draft, items);
          items.push({ id: nextItemId(), type: "assistant", text: ev.text || "", ts: ev.ts });
        }
        lastMessage = ev.text;
      }
      break;
    }
    case "part_upsert": {
      flushDraft(draft, items);
      const id = `part_${ev.partId}`;
      if (ev.kind === "tool") {
        const idx = items.findIndex((it) => it.id === id);
        const toolItem: ChatItem = {
          id,
          type: "tool",
          key: ev.partId || id,
          name: ev.toolName || "tool",
          action: toolAction(ev.toolName || "tool"),
          input: ev.toolInput,
          result: ev.toolResult,
          running: !toolFinished(ev),
          status: toolFinished(ev) ? "success" : "running",
        };
        if (idx >= 0) items[idx] = toolItem;
        else items.push(toolItem);
      } else if (ev.kind === "thinking") {
        upsertText(items, id, "thinking", ev.text || "", ev.ts);
      } else {
        upsertText(items, id, "assistant", ev.text || "", ev.ts);
        lastMessage = ev.text;
      }
      break;
    }
    case "tool_start":
      flushDraft(draft, items);
      items.push({
        id: nextItemId(),
        type: "tool",
        key: ev.toolCallId || nextItemId(),
        name: ev.toolName || "tool",
        action: toolAction(ev.toolName || "tool"),
        input: ev.toolInput,
        running: true,
        status: "running",
      });
      break;
    case "tool_end": {
      const key = ev.toolCallId || "";
      for (let i = items.length - 1; i >= 0; i--) {
        const it = items[i];
        if (it.type === "tool" && it.key === key && it.running) {
          items[i] = { ...it, result: ev.toolResult, running: false, status: "success" };
          return { toast, lastMessage };
        }
      }
      items.push({
        id: nextItemId(),
        type: "tool",
        key: key || nextItemId(),
        name: "tool",
        result: ev.toolResult,
        running: false,
        status: "success",
      });
      break;
    }
    case "file_change":
      flushDraft(draft, items);
      if (ev.paths && ev.paths.length) {
        items.push({ id: nextItemId(), type: "files", paths: ev.paths, ts: ev.ts });
      }
      break;
    case "result":
      flushDraft(draft, items);
      session.cost += ev.costUsd || 0;
      session.tokensIn += ev.tokensIn || 0;
      session.tokensOut += ev.tokensOut || 0;
      session.updatedAt = ev.ts || Date.now();
      session.status = ev.status === "error" ? "error" : "idle";
      if (ev.status === "error") {
        toast = { kind: "error", title: "Agent run failed", detail: ev.error };
      }
      break;
    case "error":
      flushDraft(draft, items);
      session.status = "error";
      session.error = ev.error || "unknown error";
      items.push({ id: nextItemId(), type: "system", text: ev.error || "unknown error", ts: ev.ts });
      toast = { kind: "error", title: "Agent error", detail: ev.error };
      break;
  }
  return { toast, lastMessage };
}

function toolFinished(ev: AgentEvent): boolean {
  return Boolean(ev.toolResult) || ev.status === "completed" || ev.status === "success" || ev.status === "error";
}

export function toolAction(name: string): string {
  const value = name.toLowerCase();
  if (value.includes("command") || value === "shell" || value.includes("bash") || value.includes("exec")) return "Running command";
  if (value.includes("edit") || value.includes("write") || value.includes("patch") || value.includes("file")) return "Editing files";
  if (value.includes("search") || value.includes("grep") || value.includes("glob")) return "Searching files";
  if (value.includes("web") || value.includes("browser")) return "Searching the web";
  if (value.includes("mcp")) return "Calling MCP tool";
  return "Using tool";
}

function normalizeStatus(status: string, current: Session["status"]): Session["status"] {
  switch (status) {
    case "running":
    case "waiting":
    case "idle":
    case "error":
      return status as Session["status"];
    default:
      return current;
  }
}

export const PROC_LINE_CAP = 500;

export function appendProcLine(lines: string[], ev: ProcEvent): string[] {
  const line = ev.kind === "exit" ? `[exit code ${ev.code ?? 0}]` : ev.line;
  if (!line) return lines;
  const next = [...lines, line];
  if (next.length > PROC_LINE_CAP) return next.slice(next.length - PROC_LINE_CAP);
  return next;
}

function rowToParts(row: FleetRow): { session: Session; info: WorktreeInfoLite } {
  const { worktreeName, branch, projectName, projectPath, ...session } = row;
  return {
    session: session as Session,
    info: { worktreeName, branch, projectName, projectPath },
  };
}

export const useStore = create<AppState & AppActions>((set, get) => ({
  ready: false,
  degraded: !hasBackend(),
  projects: [],
  sessions: {},
  sessionInfo: {},
  items: {},
  drafts: {},
  loadedSessions: new Set(),
  view: "workspace",
  tab: "chat",
  selectedWorktreeId: null,
  selectedSession: {},
  dialog: null,
  toasts: [],
  clis: {},
  capabilities: {},
  models: {},
  permissions: {},
  pendingRequests: [],
  procLines: {},
  settings: {},
  terminalSessions: {},
  selectedWorkspaceSession: {},
  terminalSplitRatios: {},

  async init() {
    await get().refreshSettings();
    if (!hasBackend()) {
      set({ degraded: true, ready: true });
      return;
    }
    onRuntimeEvent("agent:event", (...args: unknown[]) => {
      let payload = args[0] as { sessionId: string; event: AgentEvent | string } | string;
      if (typeof payload === "string") {
        try {
          payload = JSON.parse(payload) as { sessionId: string; event: AgentEvent };
        } catch {
          return;
        }
      }
      if (payload?.sessionId && payload?.event) {
        const event = typeof payload.event === "string"
          ? (() => {
              try { return JSON.parse(payload.event) as AgentEvent; } catch { return null; }
            })()
          : payload.event;
        if (event) get().applyEvent(payload.sessionId, event);
      }
    });
    onRuntimeEvent("perm:request", (...args: unknown[]) => {
      const req = args[0] as PermissionRequest;
      if (!req?.requestID) return;
      set((s) => ({
        pendingRequests: [...s.pendingRequests.filter((r) => r.requestID !== req.requestID), req],
      }));
    });
    onRuntimeEvent("proc:event", (...args: unknown[]) => {
      const ev = args[0] as ProcEvent;
      if (!ev?.sessionID) return;
      set((s) => ({
        procLines: {
          ...s.procLines,
          [ev.sessionID]: appendProcLine(s.procLines[ev.sessionID] || [], ev),
        },
      }));
    });
    await get().loadAll();
    get().restoreTerminalSessions();
    await get().detectClis();
    try {
      set({ permissions: await api.getPermissions() });
    } catch {
      set({ permissions: {} });
    }
    set({ ready: true });
  },

  async loadAll() {
    await Promise.all([get().refreshProjects(), get().refreshSessionLists()]);
  },

  restoreTerminalSessions() {
    if (typeof window === "undefined") return;
    try {
      const raw = window.localStorage.getItem(TERMINAL_LAYOUT_STORAGE_KEY);
      if (!raw) return;
      const saved = JSON.parse(raw) as {
        terminalSessions?: Record<string, Partial<TerminalSession>>;
        selectedWorkspaceSession?: Record<string, string>;
        selectedWorktreeId?: string | null;
        terminalSplitRatios?: Record<string, Record<string, number>>;
      };
      const worktreeIds = new Set(get().projects.flatMap((project) => project.worktrees.map((worktree) => worktree.id)));
      const terminalSessions: Record<string, TerminalSession> = {};
      const occupied = new Set<string>();
      for (const [id, terminal] of Object.entries(saved.terminalSessions || {})) {
        const restoredWorktreeId = terminal?.worktreeId;
        if (!terminal || !restoredWorktreeId || !worktreeIds.has(restoredWorktreeId)) continue;
        const slot = Number(terminal.slot);
        const kind = terminal.kind === "shell" || terminal.kind === "codex" || terminal.kind === "claude" ? terminal.kind : null;
        const slotKey = `${restoredWorktreeId}:${slot}`;
        if (!id || !Number.isInteger(slot) || slot < 1 || slot > TERMINAL_SLOTS || occupied.has(slotKey)) continue;
        occupied.add(slotKey);
        terminalSessions[id] = {
          id,
          worktreeId: restoredWorktreeId,
          title: terminal.title || `Terminal ${slot}`,
          slot,
          kind,
          detected: false,
          mode: terminal.mode === "approve-all" || terminal.mode === "manual" ? terminal.mode : "auto",
        };
      }
      const selectedWorkspaceSession = Object.fromEntries(
        Object.entries(saved.selectedWorkspaceSession || {}).filter(([worktreeId, terminalId]) =>
          terminalSessions[terminalId]?.worktreeId === worktreeId,
        ),
      );
      const selectedWorktreeId = saved.selectedWorktreeId && worktreeIds.has(saved.selectedWorktreeId)
        ? saved.selectedWorktreeId
        : Object.values(terminalSessions)[0]?.worktreeId || null;
      set({
        terminalSessions,
        selectedWorkspaceSession,
        selectedWorktreeId,
        terminalSplitRatios: readSplitRatios(saved.terminalSplitRatios, worktreeIds),
        tab: selectedWorktreeId && Object.values(terminalSessions).some((terminal) => terminal.worktreeId === selectedWorktreeId) ? "terminal" : get().tab,
        view: selectedWorktreeId ? "workspace" : get().view,
      });
    } catch {
      // Ignore malformed or unavailable persisted terminal state.
    }
  },

  async refreshProjects() {
    try {
      const projects = await api.listProjects();
      set({ projects });
    } catch (e) {
      get().pushToast({ kind: "error", title: "Failed to load projects", detail: String(e) });
    }
  },

  async refreshSessionLists() {
    try {
      const rows = await api.listFleet(null);
      const sessions: Record<string, Session> = { ...get().sessions };
      const infos: Record<string, WorktreeInfoLite> = { ...get().sessionInfo };
      for (const row of rows) {
        const { session, info } = rowToParts(row);
        sessions[session.id] = session;
        infos[session.id] = info;
      }
      set({ sessions, sessionInfo: infos });
    } catch (e) {
      get().pushToast({ kind: "error", title: "Failed to load sessions", detail: String(e) });
    }
  },

  async detectClis() {
    try {
      const clis = await api.detectClis();
      set({ clis });
    } catch {
      set({ clis: {} });
    }
  },

  setView(view) {
    set({ view });
    if (view === "fleet") void get().refreshSessionLists();
  },

  setTab(tab) {
    set({ tab });
  },

  selectWorktree(id) {
    const sid = id ? get().selectedSession[id] : null;
    set((s) => ({ selectedWorktreeId: id, view: "workspace", tab: "chat", selectedWorkspaceSession: id && sid ? { ...s.selectedWorkspaceSession, [id]: sid } : s.selectedWorkspaceSession }));
    persistTerminalLayout(get());
    if (sid) void get().openSession(sid);
  },

  selectWorkspaceSession(worktreeId, sessionId) {
    set((s) => ({ selectedWorktreeId: worktreeId, selectedWorkspaceSession: { ...s.selectedWorkspaceSession, [worktreeId]: sessionId }, view: "workspace", tab: "chat" }));
    persistTerminalLayout(get());
  },

  createTerminalSession(worktreeId, slot) {
    const terminals = Object.values(get().terminalSessions);
    const target = slot ?? nextFreeSlot(terminals, worktreeId);
    if (target === null || target < 1 || target > TERMINAL_SLOTS) return null;
    if (terminals.some((item) => item.worktreeId === worktreeId && item.slot === target)) return null;
    const id = `terminal-${crypto.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`}`;
    set((s) => ({ terminalSessions: { ...s.terminalSessions, [id]: { id, worktreeId, title: `Terminal ${target}`, slot: target, kind: null, mode: "auto" } }, selectedWorkspaceSession: { ...s.selectedWorkspaceSession, [worktreeId]: id }, tab: "chat", view: "workspace" }));
    persistTerminalLayout(get());
    return id;
  },

  createTerminalBatch(worktreeId, count, kind) {
    const requested = Math.max(1, Math.min(TERMINAL_SLOTS, Math.floor(count) || 1));
    const bySlot = new Map<number, TerminalSession>();
    for (const terminal of Object.values(get().terminalSessions)) {
      if (terminal.worktreeId === worktreeId) bySlot.set(terminal.slot, terminal);
    }

    // The batch claims the lowest `requested` slots, so asking for N always
    // yields N terminals. An empty slot is filled; a slot whose terminal is
    // still unconfigured (sitting on the setup pane) is adopted into the batch
    // rather than left behind. A slot already running a terminal is never
    // replaced - it is only reported as kept.
    const created: TerminalSession[] = [];
    const adopted: TerminalSession[] = [];
    let kept = 0;
    for (const slot of TERMINAL_SLOT_NUMBERS.slice(0, requested)) {
      const occupant = bySlot.get(slot);
      if (!occupant) {
        const id = `terminal-${crypto.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`}`;
        created.push({ id, worktreeId, title: `Terminal ${slot}`, slot, kind, mode: "auto" as const });
      } else if (occupant.kind === null) {
        adopted.push({ ...occupant, kind, detected: false, title: occupant.title || `Terminal ${occupant.slot}` });
      } else {
        kept += 1;
      }
    }

    const changed = [...created, ...adopted];
    if (changed.length) {
      set((s) => ({
        terminalSessions: {
          ...s.terminalSessions,
          ...Object.fromEntries(changed.map((terminal) => [terminal.id, terminal])),
        },
        selectedWorktreeId: worktreeId,
        selectedWorkspaceSession: { ...s.selectedWorkspaceSession, [worktreeId]: changed[0].id },
        tab: "terminal",
        view: "workspace",
      }));
      persistTerminalLayout(get());
    }
    if (kept) {
      get().pushToast({ kind: "info", title: `Kept ${kept} existing terminal${kept === 1 ? "" : "s"}`, detail: "Slots already running a terminal were left alone." });
    }
    return changed.map((terminal) => terminal.id);
  },

  configureTerminalBatch(id, count, kind) {
    const requested = Math.max(1, Math.min(TERMINAL_SLOTS, Math.floor(count) || 1));
    const source = get().terminalSessions[id];
    if (!source) return [];
    const existing = Object.values(get().terminalSessions);
    const available = TERMINAL_SLOT_NUMBERS.filter((slot) =>
      !existing.some((terminal) => terminal.id !== id && terminal.worktreeId === source.worktreeId && terminal.slot === slot),
    );
    const slots = available.slice(0, requested);
    const created: TerminalSession[] = [];
    for (const slot of slots) {
      const terminalId = slot === source.slot ? id : get().createTerminalSession(source.worktreeId, slot);
      if (terminalId) created.push({ ...get().terminalSessions[terminalId], kind });
    }
    if (!created.length) return [];
    set((s) => {
      const terminalSessions = { ...s.terminalSessions };
      for (const terminal of created) terminalSessions[terminal.id] = terminal;
      return {
        terminalSessions,
        selectedWorkspaceSession: { ...s.selectedWorkspaceSession, [source.worktreeId]: created[0].id },
        tab: "terminal",
        view: "workspace",
      };
    });
    persistTerminalLayout(get());
    if (created.length < requested) {
      get().pushToast({ kind: "info", title: `Created ${created.length} of ${requested} terminals`, detail: "No existing terminals were replaced." });
    }
    return created.map((terminal) => terminal.id);
  },

  configureTerminalSession(id, kind) {
    set((s) => {
      const terminal = s.terminalSessions[id];
      if (!terminal) return s;
      return { terminalSessions: { ...s.terminalSessions, [id]: { ...terminal, kind, detected: false, title: terminal.title || `Terminal ${terminal.slot}` } } };
    });
    persistTerminalLayout(get());
  },

  setTerminalKind(id, kind) {
    set((s) => {
      const terminal = s.terminalSessions[id];
      if (!terminal || (terminal.kind === kind && terminal.detected)) return s;
      return { terminalSessions: { ...s.terminalSessions, [id]: { ...terminal, kind, detected: true } } };
    });
    persistTerminalLayout(get());
  },

  updateTerminalWorktree(id, worktreeId) {
    set((s) => {
      const terminal = s.terminalSessions[id];
      if (!terminal) return s;
      return { terminalSessions: { ...s.terminalSessions, [id]: { ...terminal, worktreeId } } };
    });
    persistTerminalLayout(get());
  },

  closeTerminalSession(id) {
    const terminal = get().terminalSessions[id];
    if (!terminal) return;
    void api.closeTerminal(`${terminal.worktreeId}::${terminal.id}`).catch(() => undefined);
    set((s) => {
      const terminalSessions = { ...s.terminalSessions }; delete terminalSessions[id];
      const selectedWorkspaceSession = { ...s.selectedWorkspaceSession };
      if (selectedWorkspaceSession[terminal.worktreeId] === id) {
        // Fall back to the neighbouring slot that is still open (lower slot
        // first) so closing one terminal does not blank the workspace.
        const remaining = terminalsForWorktree(terminalSessions, terminal.worktreeId);
        const next = remaining.filter((item) => item.slot < terminal.slot).sort((a, b) => b.slot - a.slot)[0] ?? remaining[0];
        if (next) selectedWorkspaceSession[terminal.worktreeId] = next.id;
        else delete selectedWorkspaceSession[terminal.worktreeId];
      }
      return { terminalSessions, selectedWorkspaceSession };
    });
    persistTerminalLayout(get());
  },

  setTerminalSplitRatio(worktreeId, key, ratio) {
    if (!Number.isFinite(ratio)) return;
    set((s) => {
      const current = s.terminalSplitRatios[worktreeId];
      const next = Math.min(0.9, Math.max(0.1, ratio));
      if (current?.[key] === next) return s;
      return {
        terminalSplitRatios: {
          ...s.terminalSplitRatios,
          [worktreeId]: { ...(current || {}), [key]: next },
        },
      };
    });
    persistTerminalLayout(get());
  },

  resetTerminalSplitRatio(worktreeId, key) {
    set((s) => {
      const current = s.terminalSplitRatios[worktreeId];
      if (!current || !(key in current)) return s;
      const next = { ...current };
      delete next[key];
      return { terminalSplitRatios: { ...s.terminalSplitRatios, [worktreeId]: next } };
    });
    persistTerminalLayout(get());
  },

  async openSession(sessionId) {
    const sess = get().sessions[sessionId];
    if (!sess) return;
    set((s) => ({
      selectedWorktreeId: sess.worktreeId,
      selectedSession: { ...s.selectedSession, [sess.worktreeId]: sessionId },
      selectedWorkspaceSession: { ...s.selectedWorkspaceSession, [sess.worktreeId]: sessionId },
      view: "workspace",
      tab: "chat",
    }));
    try {
      const detail = await api.getSessionDetail(sessionId);
      const items = messagesToItems(detail.messages);
      set((s) => ({
        sessions: { ...s.sessions, [sessionId]: detail.session },
        items: { ...s.items, [sessionId]: items },
        drafts: { ...s.drafts, [sessionId]: emptyDraft() },
        loadedSessions: new Set(s.loadedSessions).add(sessionId),
      }));
    } catch (e) {
      get().pushToast({ kind: "error", title: "Failed to open session", detail: String(e) });
    }
  },

  newSessionSelected(worktreeId, sessionId) {
    set((s) => ({
      selectedSession: { ...s.selectedSession, [worktreeId]: sessionId },
      items: { ...s.items, [sessionId]: [] },
      drafts: { ...s.drafts, [sessionId]: emptyDraft() },
      tab: "chat",
    }));
  },

  setDialog(dialog) {
    set({ dialog });
  },

  pushToast(t) {
    const id = toastSeq++;
    set((s) => ({ toasts: [...s.toasts, { ...t, id }] }));
  },

  dismissToast(id) {
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
  },

  applyEvent(sessionId, ev) {
    if (ev.type === "result" && ev.status !== "error") {
      const prev = get().sessions[sessionId];
      if (prev && (prev.status === "running" || prev.status === "waiting")) {
        playDing();
      }
    }
    set((state) => {
      const sessions = { ...state.sessions };
      const itemsMap = { ...state.items };
      const draftsMap = { ...state.drafts };
      let session = sessions[sessionId];
      if (!session) {
        session = {
          id: sessionId,
          worktreeId: "",
          provider: "claude",
          model: "",
          status: "idle",
          providerSessionId: "",
          error: "",
          lastMessage: "",
          cost: 0,
          tokensIn: 0,
          tokensOut: 0,
          pid: 0,
          createdAt: Date.now(),
          updatedAt: Date.now(),
          title: "",
          titleLocked: false,
          parentId: "",
          baselineHead: "",
          costKnown: false,
          cachedTokens: 0,
          reasoningTokens: 0,
        };
      }
      session = { ...session };
      const items = [...(itemsMap[sessionId] || [])];
      const draft = { ...(draftsMap[sessionId] || emptyDraft()) };

      const res = reduceEvent(session, items, draft, ev);

      sessions[sessionId] = session;
      itemsMap[sessionId] = items;
      draftsMap[sessionId] = draft;

      const patch: Partial<AppState> = { sessions, items: itemsMap, drafts: draftsMap };
      if (res.lastMessage !== undefined) {
        session.lastMessage = res.lastMessage;
      }

      let toasts = state.toasts;
      if (res.toast) {
        toasts = [...toasts, { ...res.toast, id: toastSeq++ }];
      }
      patch.toasts = toasts;
      return patch as AppState & AppActions;
    });
  },

  optimisticUserMessage(sessionId, text) {
    set((state) => ({
      items: {
        ...state.items,
        [sessionId]: [
          ...(state.items[sessionId] || []),
          { id: nextItemId(), type: "user" as const, text, ts: Date.now() },
        ],
      },
    }));
  },

  async ensureCaps(provider) {
    const existing = get().capabilities[provider];
    if (existing) return existing;
    try {
      const caps = await api.getCapabilities(provider);
      set((s) => ({ capabilities: { ...s.capabilities, [provider]: caps } }));
      return caps;
    } catch {
      const fallback: Capabilities = {
        streaming: false,
        tools: false,
        fileEdit: false,
        shell: false,
        images: false,
        mcp: false,
        subagents: false,
        resume: false,
        usage: false,
        costReport: false,
        reasoningControls: false,
        nativeWebBrowse: false,
        modelSelection: "freeform",
      };
      set((s) => ({ capabilities: { ...s.capabilities, [provider]: fallback } }));
      return fallback;
    }
  },

  async loadModels(provider, refresh) {
    const cached = get().models[provider];
    if (cached && !refresh) return cached;
    try {
      const caps = await get().ensureCaps(provider);
      if (caps && caps.modelSelection === "none") {
        const empty: ModelInfo[] = [];
        set((s) => ({ models: { ...s.models, [provider]: empty } }));
        return empty;
      }
      const list = await (refresh ? api.refreshModels(provider) : api.listModels(provider));
      set((s) => ({ models: { ...s.models, [provider]: list } }));
      return list;
    } catch {
      const empty: ModelInfo[] = [];
      set((s) => ({ models: { ...s.models, [provider]: empty } }));
      return empty;
    }
  },

  async refreshSettings() {
    try {
      set({ settings: await api.getSettings() });
    } catch {
      set({ settings: {} });
    }
  },

  async approve(req, allow) {
    set((s) => ({
      pendingRequests: s.pendingRequests.filter((r) => r.requestID !== req.requestID),
    }));
    try {
      await api.approvePermission(req.requestID, allow);
    } catch (e) {
      get().pushToast({ kind: "error", title: "Permission update failed", detail: String(e) });
    }
  },

  async renameSession(id, title) {
    try {
      await api.renameSession(id, title);
      set((s) => {
        const sess = s.sessions[id];
        if (!sess) return {};
        return { sessions: { ...s.sessions, [id]: { ...sess, title, titleLocked: true } } };
      });
      get().pushToast({ kind: "success", title: "Session renamed" });
    } catch (e) {
      get().pushToast({ kind: "error", title: "Rename failed", detail: String(e) });
    }
  },

  async deleteSession(id) {
    try {
      await api.deleteSession(id);
      set((s) => {
        const sessions = { ...s.sessions };
        delete sessions[id];
        const sessionInfo = { ...s.sessionInfo };
        delete sessionInfo[id];
        const items = { ...s.items };
        delete items[id];
        const drafts = { ...s.drafts };
        delete drafts[id];
        const procLines = { ...s.procLines };
        delete procLines[id];
        const loadedSessions = new Set(s.loadedSessions);
        loadedSessions.delete(id);
        const selectedSession = { ...s.selectedSession };
        for (const k of Object.keys(selectedSession)) {
          if (selectedSession[k] === id) delete selectedSession[k];
        }
        return {
          sessions,
          sessionInfo,
          items,
          drafts,
          procLines,
          loadedSessions,
          selectedSession,
        };
      });
      await get().refreshSessionLists();
      get().pushToast({ kind: "success", title: "Session deleted" });
    } catch (e) {
      get().pushToast({ kind: "error", title: "Delete failed", detail: String(e) });
    }
  },

  async forkSession(id, upToMessageId) {
    try {
      const sess = await api.forkSession(id, upToMessageId);
      if (sess) {
        set((s) => ({ sessions: { ...s.sessions, [sess.id]: sess } }));
        await get().refreshSessionLists();
        get().pushToast({ kind: "success", title: "Session forked" });
      }
      return sess ?? null;
    } catch (e) {
      get().pushToast({ kind: "error", title: "Fork failed", detail: String(e) });
      return null;
    }
  },

  async spawnSubagent(parentId, task, provider, model) {
    try {
      const sess = await api.spawnSubagent(parentId, task, provider, model);
      if (sess) {
        set((s) => ({ sessions: { ...s.sessions, [sess.id]: sess } }));
        await get().refreshSessionLists();
        get().pushToast({ kind: "success", title: "Subagent started" });
      }
      return sess ?? null;
    } catch (e) {
      get().pushToast({ kind: "error", title: "Could not spawn subagent", detail: String(e) });
      return null;
    }
  },

  async retryLast(id) {
    try {
      await api.retryLastMessage(id);
      get().pushToast({ kind: "success", title: "Retry queued" });
    } catch (e) {
      get().pushToast({ kind: "error", title: "Retry failed", detail: String(e) });
    }
  },
}));

export function sessionsForWorktree(state: AppState, worktreeId: string): Session[] {
  return Object.values(state.sessions)
    .filter((s) => s.worktreeId === worktreeId)
    .sort((a, b) => b.updatedAt - a.updatedAt);
}
