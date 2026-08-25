import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { api } from "../lib/backend";
import type { DiffResult } from "../lib/types";
import { useStore } from "../state/store";

export default function DiffView({
  worktreeId,
  sessionId,
}: {
  worktreeId: string;
  sessionId?: string;
}) {
  const [diff, setDiff] = useState<DiffResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const tab = useStore((s) => s.tab);
  const pushToast = useStore((s) => s.pushToast);
  const baselineSession = useStore((s) => (sessionId ? s.sessions[sessionId] : undefined));
  const sessionMode = !!(sessionId && baselineSession?.baselineHead);
  const [committing, setCommitting] = useState(false);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    const load = () => {
      if (tab !== "diff") return;
      const req =
        sessionMode && sessionId ? api.getSessionDiff(sessionId) : api.getDiff(worktreeId);
      req
        .then((d) => {
          if (alive) {
            setDiff(d);
            setError(null);
          }
        })
        .catch((e) => alive && setError(String(e)));
    };
    load();
    const t = setInterval(load, 4000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [worktreeId, sessionId, sessionMode, tab]);

  const stageAll = async () => {
    try {
      await api.gitStage(worktreeId);
      pushToast({ kind: "success", title: "Staged all changes" });
    } catch (e) {
      pushToast({ kind: "error", title: "Stage failed", detail: cleanError(e) });
    }
  };

  const commit = async () => {
    const msg = message.trim();
    if (!msg || busy) return;
    setBusy(true);
    try {
      await api.gitCommit(worktreeId, msg);
      setMessage("");
      setCommitting(false);
      pushToast({ kind: "success", title: "Committed" });
    } catch (e) {
      pushToast({ kind: "error", title: "Commit failed", detail: cleanError(e) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="diff-view">
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 12 }}>
        <button className="btn" onClick={() => void stageAll()}>
          Stage all
        </button>
        {!committing && (
          <button className="btn btn--primary" onClick={() => setCommitting(true)}>
            Commit
          </button>
        )}
      </div>
      {committing && (
        <div className="composer__inner" style={{ marginBottom: 12, padding: "6px 8px" }}>
          <input
            autoFocus
            placeholder="Commit message"
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && void commit()}
            style={{ flex: 1, background: "transparent", border: "none", padding: "4px 2px", fontSize: 12.5 }}
          />
          <button className="btn btn--primary" disabled={!message.trim() || busy} onClick={() => void commit()}>
            Commit
          </button>
          <button className="icon-btn" title="Cancel commit" onClick={() => setCommitting(false)}>
            <X size={13} />
          </button>
        </div>
      )}
      {sessionMode && (
        <div className="files-label" style={{ display: "block", marginBottom: 6 }}>
          since {(baselineSession?.baselineHead || "").slice(0, 7)}
        </div>
      )}
      {error ? (
        <div className="msg-system">{cleanError(error)}</div>
      ) : !diff ? (
        <div style={{ color: "var(--text-muted)" }}>Loading…</div>
      ) : (
        <>
          <pre className="diff-stat">{diff.stat || "No changes against HEAD"}</pre>
          <Patch patch={diff.patch} />
          {diff.patch && (
            <button
              className="btn"
              style={{ marginTop: 12 }}
              onClick={() => {
                void navigator.clipboard.writeText(diff.patch);
                pushToast({ kind: "success", title: "Patch copied" });
              }}
            >
              Copy patch
            </button>
          )}
        </>
      )}
    </div>
  );
}

function Patch({ patch }: { patch: string }) {
  if (!patch) return null;
  const lines = patch.split("\n");
  return (
    <div className="patch">
      {lines.map((line, i) => {
        const cls = line.startsWith("+") && !line.startsWith("+++")
          ? "add"
          : line.startsWith("-") && !line.startsWith("---")
            ? "del"
            : line.startsWith("@@") || line.startsWith("diff ") || line.startsWith("index ")
              ? "meta-line"
              : undefined;
        return (
          <span key={i} className={cls}>
            {line}
            {"\n"}
          </span>
        );
      })}
    </div>
  );
}

export function cleanError(e: unknown): string {
  return String(e).replace(/^Error:/, "").trim() || "Unknown error";
}
