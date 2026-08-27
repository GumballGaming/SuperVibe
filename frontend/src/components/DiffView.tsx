import { useEffect, useState } from "react";
import { Copy, FileMinus2, FilePlus2, FileText, RefreshCw, X } from "lucide-react";
import { api } from "../lib/backend";
import type { DiffLine, DiffFile, StatSummary } from "../lib/diff";
import { parsePatch, parseStat, TRUNCATED_MARKER } from "../lib/diff";
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
  const [loading, setLoading] = useState(true);
  const [refreshKey, setRefreshKey] = useState(0);
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
            setLoading(false);
          }
        })
        .catch((e) => {
          if (alive) {
            setError(String(e));
            setLoading(false);
          }
        });
    };
    load();
    const t = setInterval(load, 4000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [worktreeId, sessionId, sessionMode, tab, refreshKey]);

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

  const copyPatch = (patch: string | undefined | null) => {
    if (!patch) return;
    void navigator.clipboard.writeText(patch);
    pushToast({ kind: "success", title: "Patch copied" });
  };

  const summary = diff ? parseStat(diff.stat) : null;
  const files = diff ? parsePatch(diff.patch) : [];

  return (
    <div className="diff-view">
      <div className="diff-view__toolbar">
        <span className="diff-view__title">
          {sessionMode ? "Session changes" : "Uncommitted changes"}
        </span>
        <span style={{ flex: 1 }} />
        <button
          className="icon-btn"
          style={{ width: 26, height: 26 }}
          title="Refresh diff"
          disabled={loading}
          onClick={() => {
            setLoading(true);
            setRefreshKey((k) => k + 1);
          }}
        >
          <RefreshCw size={13} className={loading ? "tool-row__spin" : undefined} />
        </button>
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
      ) : files.length === 0 ? (
        <div className="diff-empty">
          <span style={{ fontSize: 15 }}>✓</span>
          <span>{summary && summary.files > 0 ? "Binary changes only" : "No changes against HEAD"}</span>
        </div>
      ) : (
        <>
          <DiffSummary summary={summary} />
          {diff.patch.includes(TRUNCATED_MARKER) && (
            <div className="diff-truncated">Diff is large — showing first ~300 KB only.</div>
          )}
          {files.map((file) => (
            <DiffFileCard key={file.path + file.raw.length} file={file} onCopy={() => copyPatch(file.raw)} />
          ))}
          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <button className="btn" onClick={() => copyPatch(diff.patch)}>
              Copy patch
            </button>
          </div>
        </>
      )}
    </div>
  );
}

function DiffSummary({ summary }: { summary: StatSummary | null }) {
  if (!summary) return null;
  return (
    <div className="diff-summary">
      <span className="diff-summary__chip">
        {summary.files} file{summary.files === 1 ? "" : "s"} changed
      </span>
      {summary.insertions > 0 && (
        <span className="diff-summary__chip diff-summary__chip--add">+{summary.insertions}</span>
      )}
      {summary.deletions > 0 && (
        <span className="diff-summary__chip diff-summary__chip--del">−{summary.deletions}</span>
      )}
    </div>
  );
}

function DiffFileCard({ file, onCopy }: { file: DiffFile; onCopy: () => void }) {
  return (
    <div className="diff-file">
      <div className="diff-file__head">
        <span className="diff-file__icon">
          {file.status === "added" ? (
            <FilePlus2 size={13} />
          ) : file.status === "deleted" ? (
            <FileMinus2 size={13} />
          ) : (
            <FileText size={13} />
          )}
        </span>
        <span className="diff-file__path" title={file.path}>
          {file.path}
        </span>
        {file.binary && <span className="diff-file__badge">binary</span>}
        {file.additions > 0 && <span className="diff-file__cnt diff-file__cnt--add">+{file.additions}</span>}
        {file.deletions > 0 && <span className="diff-file__cnt diff-file__cnt--del">−{file.deletions}</span>}
        <button className="icon-btn diff-file__copy" title="Copy file patch" onClick={onCopy}>
          <Copy size={12} />
        </button>
      </div>
      {file.binary && (
        <div className="diff-file__binary">Binary file — patch omitted</div>
      )}
      {file.hunks.map((hunk, i) => (
        <div key={i} className="diff-hunk">
          <div className="diff-hunk__header">{hunk.header}</div>
          {hunk.lines.map((line, j) => (
            <DiffRow key={j} line={line} />
          ))}
        </div>
      ))}
    </div>
  );
}

function DiffRow({ line }: { line: DiffLine }) {
  const sign = line.kind === "add" ? "+" : line.kind === "del" ? "−" : line.kind === "meta" ? "" : " ";
  return (
    <div className={`diff-line diff-line--${line.kind}`}>
      <span className="diff-line__no">{line.oldNo ?? ""}</span>
      <span className="diff-line__no">{line.newNo ?? ""}</span>
      <span className="diff-line__sign">{sign}</span>
      <span className="diff-line__text">{line.text}</span>
    </div>
  );
}

export function cleanError(e: unknown): string {
  return String(e).replace(/^Error:/, "").trim() || "Unknown error";
}
