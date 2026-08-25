import { describe, expect, test } from "bun:test";
import { messagesToItems, reduceEvent } from "./store";
import type { AgentEvent, ChatItem, Message, Session } from "../lib/types";

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
  return { type: "status", ts: 100, ...partial };
}

function text(items: ChatItem[]): string {
  return items.map((i) => (i.type === "user" || i.type === "assistant" || i.type === "thinking" || i.type === "system" ? i.text : `[${i.type}]`)).join("|");
}

describe("reduceEvent", () => {
  test("deltas accumulate in draft", () => {
    const s = makeSession();
    const items: ChatItem[] = [];
    const draft = { text: "", thinking: "" };
    reduceEvent(s, items, draft, ev({ type: "delta", text: "Hel" }));
    reduceEvent(s, items, draft, ev({ type: "delta", text: "lo" }));
    expect(draft.text).toBe("Hello");
    expect(items.length).toBe(0);
  });

  test("finalized message matching draft replaces it without duplication", () => {
    const s = makeSession();
    const items: ChatItem[] = [];
    const draft = { text: "", thinking: "" };
    reduceEvent(s, items, draft, ev({ type: "delta", text: "Hello world" }));
    reduceEvent(s, items, draft, ev({ type: "message", role: "assistant", kind: "text", text: "Hello world" }));
    expect(draft.text).toBe("");
    const assistantItems = items.filter((i) => i.type === "assistant");
    expect(assistantItems.length).toBe(1);
    expect(text(items)).toBe("Hello world");
  });

  test("message without prior delta is appended directly", () => {
    const s = makeSession();
    const items: ChatItem[] = [];
    const draft = { text: "", thinking: "" };
    reduceEvent(s, items, draft, ev({ type: "message", role: "assistant", kind: "text", text: "codex reply" }));
    expect(items.length).toBe(1);
  });

  test("thinking deltas and blocks pair up", () => {
    const s = makeSession();
    const items: ChatItem[] = [];
    const draft = { text: "", thinking: "" };
    reduceEvent(s, items, draft, ev({ type: "thinking_delta", text: "hmm" }));
    reduceEvent(s, items, draft, ev({ type: "message", role: "assistant", kind: "thinking", text: "hmm" }));
    expect(items.filter((i) => i.type === "thinking").length).toBe(1);
  });

  test("tool start/end pair by call id", () => {
    const s = makeSession();
    const items: ChatItem[] = [];
    const draft = { text: "", thinking: "" };
    reduceEvent(s, items, draft, ev({ type: "tool_start", toolCallId: "t1", toolName: "Bash", toolInput: "{}" }));
    reduceEvent(s, items, draft, ev({ type: "tool_end", toolCallId: "t1", toolResult: "ok" }));
    expect(items.length).toBe(1);
    const tool = items[0];
    if (tool.type !== "tool") throw new Error("wrong item");
    expect(tool.running).toBe(false);
    expect(tool.result).toBe("ok");
  });

  test("result updates cost and status", () => {
    const s = makeSession();
    const items: ChatItem[] = [];
    const draft = { text: "", thinking: "" };
    reduceEvent(s, items, draft, ev({ type: "result", status: "idle", costUsd: 0.5, tokensIn: 10, tokensOut: 20 }));
    expect(s.status).toBe("idle");
    expect(s.cost).toBeCloseTo(0.5);
    expect(s.tokensOut).toBe(20);
  });

  test("error event sets error state and system item", () => {
    const s = makeSession();
    const items: ChatItem[] = [];
    const draft = { text: "", thinking: "" };
    reduceEvent(s, items, draft, ev({ type: "error", error: "boom" }));
    expect(s.status).toBe("error");
    expect(items[0].type).toBe("system");
  });

  test("part_upsert upserts by part id", () => {
    const s = makeSession();
    const items: ChatItem[] = [];
    const draft = { text: "", thinking: "" };
    reduceEvent(s, items, draft, ev({ type: "part_upsert", partId: "p1", kind: "text", text: "v1" }));
    reduceEvent(s, items, draft, ev({ type: "part_upsert", partId: "p1", kind: "text", text: "v2 full" }));
    expect(items.length).toBe(1);
    expect(text(items)).toBe("v2 full");
  });
});

describe("messagesToItems", () => {
  test("maps persisted rows back to transcript", () => {
    const msgs: Message[] = [
      { id: 1, sessionId: "s1", role: "user", kind: "text", content: "hi", meta: "", ts: 1 },
      {
        id: 2,
        sessionId: "s1",
        role: "tool",
        kind: "tool_start",
        content: "Bash",
        meta: JSON.stringify({ id: "t9", input: "ls" }),
        ts: 2,
      },
      {
        id: 3,
        sessionId: "s1",
        role: "tool",
        kind: "tool_end",
        content: "",
        meta: JSON.stringify({ id: "t9", result: "a.go" }),
        ts: 3,
      },
      { id: 4, sessionId: "s1", role: "assistant", kind: "text", content: "done", meta: "", ts: 4 },
    ];
    const items = messagesToItems(msgs);
    expect(items.length).toBe(3);
    const tool = items.find((i) => i.type === "tool");
    if (tool?.type !== "tool") throw new Error("missing tool");
    expect(tool.name).toBe("Bash");
    expect(tool.result).toBe("a.go");
    expect(tool.running).toBe(false);
  });
});
