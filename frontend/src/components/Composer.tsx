import { useEffect, useRef, useState } from "react";
import { ArrowUp, BrainCircuit, Paperclip, RefreshCw, Square, X, Zap } from "lucide-react";
import { api, openFileDialog } from "../lib/backend";
import { parseMentions } from "../lib/format";
import Dropdown from "./Dropdown";
import ModelLogo from "./ModelLogo";
import { useStore } from "../state/store";

const MENTION_TOKENS = ["diff", "git", "tree"];
const DEFAULT_CODEX_MODEL = "gpt-5.6-sol";
const DEFAULT_REASONING_EFFORTS = ["off", "low", "medium", "high"];
const CLAUDE_REASONING_EFFORTS = ["off", "low", "medium", "high", "xhigh", "max", "ultracode"];
const CODEX_REASONING_EFFORTS = ["none", "low", "medium", "high", "xhigh", "max"];
const REASONING_LABELS: Record<string, string> = {
  off: "Off",
  none: "None",
  low: "Low",
  medium: "Medium",
  high: "High",
  xhigh: "Extra High",
  max: "Max",
  ultracode: "UltraCode",
};

function reasoningEffortsFor(provider: string | undefined, efforts?: string[]): string[] {
  if (efforts && efforts.length > 0) return efforts;
  if (provider === "claude") return CLAUDE_REASONING_EFFORTS;
  return provider === "codex" ? CODEX_REASONING_EFFORTS : DEFAULT_REASONING_EFFORTS;
}

export default function Composer({ sessionId, busy }: { sessionId: string; busy: boolean }) {
  const [text, setText] = useState("");
  const [files, setFiles] = useState<string[]>([]);
  const [mentionQuery, setMentionQuery] = useState<string | null>(null);
  const [repoHits, setRepoHits] = useState<string[]>([]);
  const [mentionItems, setMentionItems] = useState<string[]>([]);
  const [mentionIdx, setMentionIdx] = useState(0);
  const taRef = useRef<HTMLTextAreaElement | null>(null);
  const optimisticUserMessage = useStore((s) => s.optimisticUserMessage);
  const pushToast = useStore((s) => s.pushToast);
  const selectedWorktreeId = useStore((s) => s.selectedWorktreeId);
  const session = useStore((s) => s.sessions[sessionId]);
  const capsMap = useStore((s) => s.capabilities);
  const modelsMap = useStore((s) => s.models);
  const ensureCaps = useStore((s) => s.ensureCaps);
  const loadModels = useStore((s) => s.loadModels);
  const provider = session?.provider;
  const [selectedModel, setSelectedModel] = useState(session?.model || (session?.provider === "codex" ? DEFAULT_CODEX_MODEL : ""));
  const [thinking, setThinking] = useState("medium");
  const [fastMode, setFastMode] = useState(false);
  const [refreshingModels, setRefreshingModels] = useState(false);

  useEffect(() => {
    if (!provider) return;
    void ensureCaps(provider);
    void loadModels(provider);
  }, [provider, ensureCaps, loadModels]);

  const caps = provider ? capsMap[provider] : undefined;
  const models = provider ? modelsMap[provider] || [] : [];
  const showAttach = caps ? caps.images || caps.fileEdit : true;
  const modelOptions = [
    ...(provider === "codex" ? [] : [{ value: "", label: "Default" }]),
    ...(selectedModel && !models.some((m) => m.id === selectedModel)
      ? [{ value: selectedModel, label: selectedModel }]
      : []),
    ...models
      .filter((modelInfo, index, all) => all.findIndex((candidate) => candidate.id === modelInfo.id) === index)
      .map((m) => ({
        value: m.id,
        label: (
          <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            <ModelLogo provider={provider} modelId={m.id} size={13} />
            {m.label}
          </span>
        ),
      })),
  ];
  useEffect(() => {
    setSelectedModel(session?.model || (session?.provider === "codex" ? DEFAULT_CODEX_MODEL : ""));
    setThinking("medium");
    setFastMode(false);
  }, [sessionId, session?.model, session?.provider]);

  const selectedModelInfo = models.find((modelInfo) => modelInfo.id === selectedModel);
  const reasoningEfforts = reasoningEffortsFor(provider, selectedModelInfo?.reasoningEfforts);
  const reasoningOptions = reasoningEfforts.map((value) => ({
    value,
    label: provider === "claude" && value === "xhigh" ? "XHigh" : REASONING_LABELS[value] ?? value,
  }));
  const selectModel = (nextModel: string) => {
    setSelectedModel(nextModel);
    const nextInfo = models.find((modelInfo) => modelInfo.id === nextModel);
    const nextEfforts = reasoningEffortsFor(provider, nextInfo?.reasoningEfforts);
    setThinking((current) =>
      nextEfforts.includes(current) ? current : nextEfforts.includes("medium") ? "medium" : nextEfforts[0] ?? "",
    );
  };
  const supportsFastMode =
    provider === "claude" || Boolean(provider) && (!selectedModel || selectedModelInfo?.fastMode === true);

  const toggleFast = () => {
    setFastMode((current) => !current);
  };

  useEffect(() => {
    if (!supportsFastMode) setFastMode(false);
  }, [supportsFastMode]);


  useEffect(() => {
    const ta = taRef.current;
    if (!ta) return;
    ta.style.height = "auto";
    ta.style.height = `${Math.min(ta.scrollHeight, 180)}px`;
  }, [text, files]);

  useEffect(() => {
    setText("");
    setFiles([]);
    setMentionQuery(null);
  }, [sessionId]);

  useEffect(() => {
    if (mentionQuery === null || !selectedWorktreeId) {
      setRepoHits([]);
      return;
    }
    let alive = true;
    const t = setTimeout(() => {
      api
        .searchRepo(selectedWorktreeId, mentionQuery, 12)
        .then((r) => alive && setRepoHits(r))
        .catch(() => alive && setRepoHits([]));
    }, 250);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [mentionQuery, selectedWorktreeId]);

  useEffect(() => {
    if (mentionQuery === null) {
      setMentionItems([]);
      return;
    }
    const q = mentionQuery.toLowerCase();
    const fixed = MENTION_TOKENS.filter((t) => t.startsWith(q));
    const merged: string[] = [...repoHits];
    for (const token of fixed) {
      if (!merged.includes(token)) merged.push(token);
    }
    setMentionItems(merged.slice(0, 15));
    setMentionIdx(0);
  }, [mentionQuery, repoHits]);

  const applyText = (v: string) => {
    setText(v);
    const m = /(?:^|\s)@([^\s]*)$/.exec(v);
    setMentionQuery(m ? m[1] : null);
  };

  const insertMention = (token: string) => {
    if (mentionQuery === null) return;
    const head = text.slice(0, text.length - (mentionQuery.length + 1));
    taRef.current?.focus();
    applyText(`${head}@${token} `);
  };

  const pickFiles = async () => {
    const picked = await openFileDialog("Attach files");
    if (picked.length) {
      setFiles((fs) => [...fs, ...picked.filter((p) => !fs.includes(p))]);
    }
  };

  const refreshModelList = async () => {
    if (!provider) return;
    setRefreshingModels(true);
    try {
      await loadModels(provider, true);
    } finally {
      setRefreshingModels(false);
    }
  };

  const send = () => {
    const trimmed = text.trim();
    if (!trimmed || busy) return;
    const mentions = parseMentions(trimmed);
    const attachments = files;
    optimisticUserMessage(sessionId, trimmed);
    setText("");
    setFiles([]);
    setMentionQuery(null);
    const request =
      attachments.length > 0
        ? api.sendMessageExtended(sessionId, trimmed, mentions, attachments)
        : api.sendMessageConfigured(
            sessionId,
            trimmed,
            selectedModel.trim(),
            provider === "claude" && thinking === "ultracode" ? "max" : thinking,
            fastMode && supportsFastMode,
          );
    request.catch((e) => {
      const msg = String(e);
      if (/unavailable/i.test(msg) && attachments.length === 0) {
        api.sendMessage(sessionId, trimmed).catch((e2) =>
          pushToast({ kind: "error", title: "Send failed", detail: String(e2) })
        );
      } else {
        pushToast({ kind: "error", title: "Send failed", detail: msg });
      }
    });
  };

  return (
    <div className="composer">
      <div
        className={`composer__inner${fastMode ? " composer__inner--fast" : ""}${provider === "claude" && thinking === "ultracode" ? " composer__inner--ultracode" : ""}`}
        style={{ position: "relative" }}
      >
        <div className="composer__body">
          {files.length > 0 && (
            <div className="files-pill-row">
              {files.map((f) => (
                <span key={f} className="file-pill" style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                  {fileName(f)}
                  <button
                    className="icon-btn"
                    style={{ width: 14, height: 14 }}
                    title="Remove attachment"
                    onClick={() => setFiles((fs) => fs.filter((x) => x !== f))}
                  >
                    <X size={10} />
                  </button>
                </span>
              ))}
            </div>
          )}
          <textarea
            ref={taRef}
            rows={1}
            placeholder={busy ? "Agent is working… you can queue a follow-up" : "Send a task to the agent"}
            value={text}
            onChange={(e) => applyText(e.target.value)}
            onKeyDown={(e) => {
              if (mentionQuery !== null && mentionItems.length > 0) {
                if (e.key === "ArrowDown") {
                  e.preventDefault();
                  setMentionIdx((i) => (i + 1) % mentionItems.length);
                  return;
                }
                if (e.key === "ArrowUp") {
                  e.preventDefault();
                  setMentionIdx((i) => (i - 1 + mentionItems.length) % mentionItems.length);
                  return;
                }
                if (e.key === "Tab" || e.key === "Enter") {
                  e.preventDefault();
                  insertMention(mentionItems[Math.min(mentionIdx, mentionItems.length - 1)]);
                  return;
                }
                if (e.key === "Escape") {
                  e.preventDefault();
                  e.stopPropagation();
                  setMentionQuery(null);
                  return;
                }
              }
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                send();
              }
            }}
          />
          <div className="composer__controls">
            <label className="composer__control composer__control--model" title="Model used for the next message">
              <Dropdown
                value={selectedModel}
                options={modelOptions}
                onChange={selectModel}
                disabled={!provider}
                className="composer__dropdown"
                ariaLabel="Model"
              />
            </label>
            {supportsFastMode && (
              <button
                className={`composer__fast${fastMode ? " composer__fast--active" : ""}`}
                type="button"
                aria-pressed={fastMode}
                title="Use the model's faster service tier for the next message"
                onClick={toggleFast}
              >
                <Zap size={13} />
                Fast
              </button>
            )}
            {caps?.reasoningControls && (
              <label
                className={`composer__control composer__control--thinking${
                  provider === "claude" && thinking === "ultracode" ? " composer__control--ultracode" : ""
                }`}
                title="Reasoning effort for the next message"
              >
                <span className="composer__control-icon">
                  <BrainCircuit size={13} />
                </span>
                <Dropdown
                  value={thinking}
                  options={reasoningOptions}
                  onChange={setThinking}
                  className="composer__dropdown"
                  ariaLabel="Thinking effort"
                />
              </label>
            )}
            <button
              className="composer__refresh"
              title="Refresh models"
              onClick={() => void refreshModelList()}
              disabled={!provider || refreshingModels}
            >
              <RefreshCw size={13} className={refreshingModels ? "spin" : undefined} />
            </button>
            <div className="composer__controls-end">
              {showAttach && (
                <button className="icon-btn" title="Attach files" onClick={() => void pickFiles()}>
                  <Paperclip size={14} />
                </button>
              )}
              {busy ? (
                <button
                  className="send-btn send-btn--stop"
                  title="Interrupt agent"
                  onClick={() => {
                    void api.interruptSession(sessionId).catch(() => undefined);
                    api.stopSession(sessionId).catch(() => undefined);
                  }}
                >
                  <Square size={13} fill="currentColor" />
                </button>
              ) : (
                <button className="send-btn" disabled={!text.trim()} onClick={send} title="Send">
                  <ArrowUp size={15} strokeWidth={2.5} />
                </button>
              )}
            </div>
          </div>
        </div>
        {mentionQuery !== null && mentionItems.length > 0 && (
          <div
            style={{
              position: "absolute",
              bottom: "calc(100% - 4px)",
              left: 8,
              minWidth: 220,
              maxHeight: 220,
              overflowY: "auto",
              background: "var(--surface-elevated)",
              border: "1px solid var(--border)",
              borderRadius: "var(--radius-md)",
              padding: 4,
              zIndex: 50,
              boxShadow: "0 12px 32px rgba(0,0,0,.45)",
            }}
          >
            {mentionItems.map((tkn, i) => (
              <button
                key={tkn}
                onMouseDown={(e) => {
                  e.preventDefault();
                  insertMention(tkn);
                }}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  width: "100%",
                  padding: "6px 8px",
                  borderRadius: 7,
                  fontSize: 12.5,
                  fontFamily: MENTION_TOKENS.includes(tkn) ? undefined : "var(--font-mono)",
                  color: i === mentionIdx ? "var(--text-primary)" : "var(--text-secondary)",
                  background: i === mentionIdx ? "var(--surface-hover)" : "transparent",
                  textAlign: "left",
                }}
              >
                @{tkn}
              </button>
            ))}
          </div>
        )}
      </div>
      <div className="hint">Enter to send · Shift+Enter for newline · @ to mention files</div>
    </div>
  );
}

function fileName(p: string): string {
  const parts = p.split(/[\\/]/);
  return parts[parts.length - 1] || p;
}
