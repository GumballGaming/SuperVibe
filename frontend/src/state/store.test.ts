import { beforeEach, describe, expect, test } from "bun:test";
import { PROC_LINE_CAP, appendProcLine, useStore } from "./store";
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
