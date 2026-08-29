import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { PROC_LINE_CAP, TERMINAL_SLOTS, appendProcLine, terminalsForWorktree, useStore } from "./store";
import { fmtCostKnown, parseMentions, titleFromPrompt } from "../lib/format";
import type { AgentEvent, PermissionRequest, ProcEvent, Session } from "../lib/types";

function makeSession(): Session {
  return {
    id: "s1",
    worktreeId: "w1",
    provider: "claude",
    model: "",
    status: "running",
    providerSessionId: "",
    error: "",
    lastMessage: "",
    cost: 0,
    tokensIn: 0,
    tokensOut: 0,
    pid: 0,
    createdAt: 1,
    updatedAt: 1,
  };
}

function ev(partial: Partial<AgentEvent>): AgentEvent {
  return { type: "result", ts: 100, ...partial };
}

function proc(partial: Partial<ProcEvent>): ProcEvent {
  return { sessionID: "s1", kind: "stdout", line: "", ...partial };
}

function reset() {
  useStore.setState({
    sessions: {},
    items: {},
    drafts: {},
    procLines: {},
    pendingRequests: [],
    toasts: [],
  });
}

describe("usage token accumulation", () => {
  beforeEach(reset);

  test("applyEvent accumulates cached and reasoning tokens across events", () => {
    useStore.setState({ sessions: { s1: makeSession() } });
    useStore.getState().applyEvent("s1", ev({ type: "result", cachedTokens: 120, reasoningTokens: 40 }));
    useStore.getState().applyEvent("s1", ev({ type: "result", cachedTokens: 30, reasoningTokens: 10 }));
    const s = useStore.getState().sessions["s1"];
    expect(s.cachedTokens).toBe(150);
    expect(s.reasoningTokens).toBe(50);
  });

  test("events without token fields leave counts untouched", () => {
    useStore.setState({ sessions: { s1: makeSession() } });
    useStore.getState().applyEvent("s1", ev({ type: "result" }));
    const s = useStore.getState().sessions["s1"];
    expect(s.cachedTokens || 0).toBe(0);
    expect(s.reasoningTokens || 0).toBe(0);
  });

  test("accumulation works for sessions created on the fly", () => {
    reset();
    useStore.getState().applyEvent("ghost", ev({ type: "result", cachedTokens: 7 }));
    const s = useStore.getState().sessions["ghost"];
    expect(s.cachedTokens).toBe(7);
  });
});

describe("procLines", () => {
  test("appendProcLine caps at PROC_LINE_CAP keeping newest lines", () => {
    let lines: string[] = [];
    for (let i = 0; i < PROC_LINE_CAP + 25; i++) {
      lines = appendProcLine(lines, proc({ line: `l${i}` }));
    }
    expect(lines.length).toBe(PROC_LINE_CAP);
    expect(lines[0]).toBe(`l25`);
    expect(lines[lines.length - 1]).toBe(`l${PROC_LINE_CAP + 24}`);
  });

  test("exit events render an exit marker", () => {
    const lines = appendProcLine([], proc({ kind: "exit", line: "", code: 3 }));
    expect(lines).toEqual(["[exit code 3]"]);
  });

  test("empty stdout lines are dropped", () => {
    expect(appendProcLine(["a"], proc({ line: "" }))).toEqual(["a"]);
  });
});

describe("pendingRequests", () => {
  beforeEach(reset);

  test("perm requests are added without duplicating requestID", () => {
    const req: PermissionRequest = { requestID: "r1", tool: "run_command", action: "exec", detail: "ls" };
    useStore.setState({ pendingRequests: [req] });
    const duplicate = { ...req, detail: "ls -la" };
    useStore.setState({
      pendingRequests: [
        ...useStore.getState().pendingRequests.filter((r) => r.requestID !== duplicate.requestID),
        duplicate,
      ],
    });
    const list = useStore.getState().pendingRequests;
    expect(list.length).toBe(1);
    expect(list[0].detail).toBe("ls -la");
  });

  test("approve removes the request", async () => {
    const req: PermissionRequest = { requestID: "r2", tool: "write_file", action: "patch", detail: "a.go" };
    useStore.setState({ pendingRequests: [req] });
    await useStore.getState().approve(req, true);
    expect(useStore.getState().pendingRequests.length).toBe(0);
  });

  test("approve keeps unrelated requests", async () => {
    const a: PermissionRequest = { requestID: "a", tool: "t", action: "x", detail: "" };
    const b: PermissionRequest = { requestID: "b", tool: "t", action: "y", detail: "" };
    useStore.setState({ pendingRequests: [a, b] });
    await useStore.getState().approve(a, false);
    const list = useStore.getState().pendingRequests;
    expect(list.length).toBe(1);
    expect(list[0].requestID).toBe("b");
  });
});

describe("terminal slots", () => {
  beforeEach(() => {
    useStore.setState({ terminalSessions: {}, selectedWorkspaceSession: {} });
  });

  test("summoned terminals fill slots 1..6 and then refuse", () => {
    for (let i = 0; i < TERMINAL_SLOTS; i++) {
      expect(useStore.getState().createTerminalSession("w1")).toBeTruthy();
    }
    const terminals = terminalsForWorktree(useStore.getState().terminalSessions, "w1");
    expect(terminals.map((terminal) => terminal.slot)).toEqual([1, 2, 3, 4, 5, 6]);
    expect(useStore.getState().createTerminalSession("w1")).toBeNull();
  });

  test("a terminal can be summoned straight into an empty slot", () => {
    useStore.getState().createTerminalSession("w1");
    const id = useStore.getState().createTerminalSession("w1", 4);
    expect(useStore.getState().terminalSessions[id!].slot).toBe(4);
    expect(useStore.getState().createTerminalSession("w1", 4)).toBeNull();
    expect(useStore.getState().createTerminalSession("w1", 9)).toBeNull();
  });

  test("batch setup configures the requested number of terminals", () => {
    const setupId = useStore.getState().createTerminalSession("w1")!;
    const ids = useStore.getState().configureTerminalBatch(setupId, 3, "codex");
    const terminals = terminalsForWorktree(useStore.getState().terminalSessions, "w1");
    expect(ids).toHaveLength(3);
    expect(terminals.map((terminal) => terminal.slot)).toEqual([1, 2, 3]);
    expect(terminals.every((terminal) => terminal.kind === "codex")).toBe(true);
    expect(useStore.getState().tab).toBe("terminal");
  });

  test("batch setup does not replace existing terminals", () => {
    useStore.getState().createTerminalSession("w1");
    const setupId = useStore.getState().createTerminalSession("w1")!;
    const ids = useStore.getState().configureTerminalBatch(setupId, 6, "shell");
    expect(ids).toHaveLength(5);
    expect(terminalsForWorktree(useStore.getState().terminalSessions, "w1")).toHaveLength(6);
  });

  test("slots are numbered per worktree", () => {
    useStore.getState().createTerminalSession("w1");
    useStore.getState().createTerminalSession("w2");
    expect(terminalsForWorktree(useStore.getState().terminalSessions, "w1").map((t) => t.slot)).toEqual([1]);
    expect(terminalsForWorktree(useStore.getState().terminalSessions, "w2").map((t) => t.slot)).toEqual([1]);
  });

  test("closing the active terminal falls back to the next open slot", () => {
    const ids = [1, 2, 3].map(() => useStore.getState().createTerminalSession("w1")!);
    expect(useStore.getState().selectedWorkspaceSession["w1"]).toBe(ids[2]);
    useStore.getState().closeTerminalSession(ids[2]);
    expect(useStore.getState().selectedWorkspaceSession["w1"]).toBe(ids[1]);
    useStore.getState().closeTerminalSession(ids[1]);
    expect(useStore.getState().selectedWorkspaceSession["w1"]).toBe(ids[0]);
    useStore.getState().closeTerminalSession(ids[0]);
    expect(useStore.getState().selectedWorkspaceSession["w1"]).toBeUndefined();
  });

  test("closing a background terminal keeps the active one", () => {
    const ids = [1, 2].map(() => useStore.getState().createTerminalSession("w1")!);
    useStore.getState().closeTerminalSession(ids[0]);
    expect(useStore.getState().selectedWorkspaceSession["w1"]).toBe(ids[1]);
  });

  test("a closed slot is reusable", () => {
    const first = useStore.getState().createTerminalSession("w1")!;
    useStore.getState().createTerminalSession("w1");
    useStore.getState().closeTerminalSession(first);
    const reused = useStore.getState().createTerminalSession("w1", 1)!;
    expect(useStore.getState().terminalSessions[reused].slot).toBe(1);
  });

  test("batch creation fills the lowest slots with the requested count", () => {
    const ids = useStore.getState().createTerminalBatch("w1", 3, "claude");
    const terminals = terminalsForWorktree(useStore.getState().terminalSessions, "w1");
    expect(ids).toHaveLength(3);
    expect(terminals.map((terminal) => terminal.slot)).toEqual([1, 2, 3]);
    expect(terminals.every((terminal) => terminal.kind === "claude")).toBe(true);
    expect(useStore.getState().tab).toBe("terminal");
  });

  test("batch creation adopts an unconfigured terminal instead of skipping it", () => {
    // Asking for six must yield six, even though summoning the first terminal
    // already claimed slot 1 and left it sitting on the setup pane.
    useStore.getState().createTerminalSession("w1");
    const ids = useStore.getState().createTerminalBatch("w1", 6, "codex");
    const terminals = terminalsForWorktree(useStore.getState().terminalSessions, "w1");
    expect(ids).toHaveLength(6);
    expect(terminals.map((terminal) => terminal.slot)).toEqual([1, 2, 3, 4, 5, 6]);
    expect(terminals.every((terminal) => terminal.kind === "codex")).toBe(true);
  });

  test("batch creation never replaces a running terminal", () => {
    useStore.setState({ toasts: [] });
    const running = useStore.getState().createTerminalSession("w1", 1)!;
    useStore.getState().configureTerminalSession(running, "codex");
    const ids = useStore.getState().createTerminalBatch("w1", 6, "claude");
    const terminals = terminalsForWorktree(useStore.getState().terminalSessions, "w1");
    expect(ids).toHaveLength(5);
    expect(terminals).toHaveLength(6);
    expect(useStore.getState().terminalSessions[running].kind).toBe("codex");
    expect(terminals.filter((terminal) => terminal.kind === "claude")).toHaveLength(5);
    expect(useStore.getState().toasts.some((toast) => toast.kind === "info")).toBe(true);
  });

  test("batch creation is clamped to the six-slot limit", () => {
    const ids = useStore.getState().createTerminalBatch("w1", 99, "shell");
    expect(ids).toHaveLength(6);
    expect(terminalsForWorktree(useStore.getState().terminalSessions, "w1")).toHaveLength(6);
  });
});

describe("terminal split ratios", () => {
  const KEY = "supervibe.terminal-layout.v1";
  const storage = new Map<string, string>();

  beforeEach(() => {
    storage.clear();
    // Bun has no window/localStorage, and the store no-ops without one.
    (globalThis as { window?: unknown }).window = {
      localStorage: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => void storage.set(key, value),
        removeItem: (key: string) => void storage.delete(key),
      },
    };
    useStore.setState({
      terminalSplitRatios: {},
      terminalSessions: {},
      selectedWorkspaceSession: {},
      projects: [],
    });
  });

  afterEach(() => {
    delete (globalThis as { window?: unknown }).window;
  });

  test("a dragged split is written through to localStorage", () => {
    useStore.getState().setTerminalSplitRatio("w1", "row:0", 0.7);
    expect(useStore.getState().terminalSplitRatios.w1["row:0"]).toBeCloseTo(0.7);
    const saved = JSON.parse(storage.get(KEY)!);
    expect(saved.terminalSplitRatios.w1["row:0"]).toBeCloseTo(0.7);
  });

  test("ratios are clamped so neither pane can be dragged away", () => {
    useStore.getState().setTerminalSplitRatio("w1", "row:0", 0.99);
    useStore.getState().setTerminalSplitRatio("w1", "row:0.1", -4);
    const ratios = useStore.getState().terminalSplitRatios.w1;
    expect(ratios["row:0"]).toBe(0.9);
    expect(ratios["row:0.1"]).toBe(0.1);
  });

  test("each branch keeps its own arrangement", () => {
    useStore.getState().setTerminalSplitRatio("w1", "row:0", 0.8);
    useStore.getState().setTerminalSplitRatio("w2", "row:0", 0.2);
    const { w1, w2 } = useStore.getState().terminalSplitRatios;
    expect(w1["row:0"]).toBeCloseTo(0.8);
    expect(w2["row:0"]).toBeCloseTo(0.2);
  });

  test("resetting a split clears just that seam", () => {
    useStore.getState().setTerminalSplitRatio("w1", "row:0", 0.7);
    useStore.getState().setTerminalSplitRatio("w1", "row:0.1", 0.4);
    useStore.getState().resetTerminalSplitRatio("w1", "row:0");
    const ratios = useStore.getState().terminalSplitRatios.w1;
    expect("row:0" in ratios).toBe(false);
    expect(ratios["row:0.1"]).toBeCloseTo(0.4);
  });

  test("a restore drops ratios for worktrees that no longer exist", () => {
    useStore.setState({
      projects: [{
        project: { id: "p1", name: "p1", path: "/p1", createdAt: 0 },
        worktrees: [{ id: "w1", projectId: "p1", name: "w1", branch: "main", path: "/p1", isPrimary: true, createdAt: 0 }],
      }],
    });
    storage.set(KEY, JSON.stringify({
      terminalSessions: {},
      selectedWorkspaceSession: {},
      selectedWorktreeId: null,
      terminalSplitRatios: {
        w1: { "row:0": 0.7, "row:0.1": "not-a-number" },
        gone: { "row:0": 0.3 },
      },
    }));
    useStore.getState().restoreTerminalSessions();
    expect(Object.keys(useStore.getState().terminalSplitRatios)).toEqual(["w1"]);
    expect(useStore.getState().terminalSplitRatios.w1).toEqual({ "row:0": 0.7 });
  });
});

describe("format helpers", () => {
  test("titleFromPrompt strips mentions, newlines and punctuation, caps at 8 words", () => {
    expect(titleFromPrompt("@diff fix the bug\nin parser.ts, please")).toBe(
      "fix the bug in parserts please"
    );
    expect(titleFromPrompt("one two three four five six seven eight nine ten")).toBe(
      "one two three four five six seven eight"
    );
    expect(titleFromPrompt("@git @tree only")).toBe("only");
    expect(titleFromPrompt("!!!")).toBe("");
  });

  test("parseMentions extracts @tokens", () => {
    expect(parseMentions("check @src/app.ts and @diff now")).toEqual(["src/app.ts", "diff"]);
    expect(parseMentions("@git status")).toEqual(["git"]);
    expect(parseMentions("email me at bob@example.com")).toEqual([]);
  });

  test("fmtCostKnown renders em dash when cost unknown and zero", () => {
    expect(fmtCostKnown(0, false)).toBe("—");
    expect(fmtCostKnown(0.5, false)).toBe("$0.50");
    expect(fmtCostKnown(0, true)).toBe("$0.00");
    expect(fmtCostKnown(1.25)).toBe("$1.25");
  });
});
