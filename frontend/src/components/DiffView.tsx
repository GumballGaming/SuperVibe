import { useEffect, useMemo, useState } from "react";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Clock3,
  Copy,
  FileMinus2,
  FilePlus2,
  FileText,
  GitCommitHorizontal,
  History,
  RefreshCw,
  X,
} from "lucide-react";
import { api } from "../lib/backend";
import type { DiffLine, DiffFile, StatSummary } from "../lib/diff";
import { parsePatch, parseStat, TRUNCATED_MARKER } from "../lib/diff";
import type { CommitInfo, DiffResult } from "../lib/types";
import { useStore } from "../state/store";

type ChangeGroup = "staged" | "unstaged";

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
  const [selectedKey, setSelectedKey] = useState("");
  const [committing, setCommitting] = useState(false);
  const [message, setMessage] = useState("");
  const [amend, setAmend] = useState(false);
  const [busy, setBusy] = useState(false);
  const [stageBusy, setStageBusy] = useState<Record<string, boolean>>({});
  const [historyOpen, setHistoryOpen] = useState(false);
  const [recentCommits, setRecentCommits] = useState<CommitInfo[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const tab = useStore((s) => s.tab);
  const pushToast = useStore((s) => s.pushToast);
  const baselineSession = useStore((s) => (sessionId ? s.sessions[sessionId] : undefined));
  const sessionMode = !!(sessionId && baselineSession?.baselineHead);

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
    const timer = setInterval(load, 4000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [worktreeId, sessionId, sessionMode, tab, refreshKey]);

  const stagedFiles = useMemo(
    () => (diff ? parsePatch(diff.stagedPatch || "") : []),
    [diff],
  );
  const unstagedFiles = useMemo(
    () => (diff ? parsePatch(diff.patch) : []),
    [diff],
  );
  const visibleFiles = sessionMode ? unstagedFiles : [...stagedFiles, ...unstagedFiles];
  const selectedFile = visibleFiles.find(
    (file) => `${stagedFiles.includes(file) ? "staged" : "unstaged"}:${file.path}` === selectedKey,
  ) || visibleFiles[0];
  const selectedGroup: ChangeGroup = selectedFile && stagedFiles.includes(selectedFile) ? "staged" : "unstaged";

  useEffect(() => {
    if (!visibleFiles.length) {
      setSelectedKey("");
      return;
    }
    const stillVisible = visibleFiles.some((file) =>
      `${stagedFiles.includes(file) ? "staged" : "unstaged"}:${file.path}` === selectedKey,
    );
    if (!stillVisible) {
      const first = visibleFiles[0];
      setSelectedKey(`${stagedFiles.includes(first) ? "staged" : "unstaged"}:${first.path}`);
    }
  }, [visibleFiles, stagedFiles, selectedKey]);

  const summary = diff ? parseStat(diff.stat) : null;
  const stagedSummary = diff ? parseStat(diff.stagedStat || "") : null;
  const totalSummary = combineSummaries(summary, stagedSummary);
  const hasStagedChanges = stagedFiles.length > 0;

  const refresh = () => {
    setLoading(true);
    setRefreshKey((key) => key + 1);
  };

  const stageAll = async () => {
    try {
      await api.gitStage(worktreeId);
      pushToast({ kind: "success", title: "Staged all changes" });
      refresh();
    } catch (e) {
      pushToast({ kind: "error", title: "Stage failed", detail: cleanError(e) });
    }
  };

  const updateStage = async (file: DiffFile, group: ChangeGroup) => {
    const key = `${group}:${file.path}`;
    setStageBusy((current) => ({ ...current, [key]: true }));
    try {
      if (group === "staged") {
        await api.gitUnstage(worktreeId, [file.path]);
        pushToast({ kind: "success", title: "File unstaged" });
      } else {
        await api.gitStage(worktreeId, [file.path]);
        pushToast({ kind: "success", title: "File staged" });
      }
      refresh();
    } catch (e) {
      pushToast({ kind: "error", title: group === "staged" ? "Unstage failed" : "Stage failed", detail: cleanError(e) });
    } finally {
      setStageBusy((current) => ({ ...current, [key]: false }));
    }
  };

  const unstageAll = async () => {
    try {
      await api.gitUnstage(worktreeId);
      pushToast({ kind: "success", title: "Unstaged all changes" });
      refresh();
    } catch (e) {
      pushToast({ kind: "error", title: "Unstage failed", detail: cleanError(e) });
    }
  };

  const loadHistory = async () => {
    if (recentCommits.length || historyLoading) return;
    setHistoryLoading(true);
    try {
      setRecentCommits(await api.getRecentCommits(worktreeId, 20));
    } catch (e) {
      pushToast({ kind: "error", title: "Could not load commit history", detail: cleanError(e) });
    } finally {
      setHistoryLoading(false);
    }
  };

  const toggleHistory = () => {
    const next = !historyOpen;
    setHistoryOpen(next);
    if (next) void loadHistory();
  };

  const enableAmend = () => {
    setAmend((current) => !current);
    if (!amend && !message.trim()) {
      void loadHistory().then(() => {
        setRecentCommits((commits) => {
          if (commits[0]) setMessage(commits[0].subject);
          return commits;
        });
      });
    }
  };

  const commit = async () => {
    const msg = message.trim();
    if (!msg || !hasStagedChanges || busy) return;
    setBusy(true);
    try {
      await api.gitCommit(worktreeId, msg, amend);
      setMessage("");
      setAmend(false);
      setCommitting(false);
      pushToast({ kind: "success", title: amend ? "Commit amended" : "Committed" });
      refresh();
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

  return (
    <div className="diff-view">
      <div className="diff-view__toolbar">
        <div>
          <div className="diff-view__title">{sessionMode ? "Session changes" : "Changes"}</div>
          <div className="diff-view__subtitle">
            {sessionMode ? `since ${(baselineSession?.baselineHead || "").slice(0, 7)}` : "Review, stage, and commit"}
          </div>
        </div>
        <span style={{ flex: 1 }} />
        {!sessionMode && (
          <div className="commit-history-wrap">
            <button className={`icon-btn${historyOpen ? " icon-btn--active" : ""}`} title="Recent commits" onClick={toggleHistory}>
              <History size={13} />
            </button>
            {historyOpen && (
              <CommitHistory commits={recentCommits} loading={historyLoading} onSelect={(subject) => {
                setMessage(subject);
                setCommitting(true);
                setHistoryOpen(false);
              }} />
            )}
          </div>
        )}
        <button className="icon-btn" style={{ width: 26, height: 26 }} title="Refresh changes" disabled={loading} onClick={refresh}>
          <RefreshCw size={13} className={loading ? "tool-row__spin" : undefined} />
        </button>
        {!sessionMode && unstagedFiles.length > 0 && <button className="btn" onClick={() => void stageAll()}>Stage all</button>}
        {!sessionMode && stagedFiles.length > 0 && <button className="btn" onClick={() => void unstageAll()}>Unstage all</button>}
        {!sessionMode && !committing && (
          <button className="btn btn--primary" disabled={!hasStagedChanges} onClick={() => setCommitting(true)}>
            <GitCommitHorizontal size={13} />
            Commit{hasStagedChanges ? ` ${stagedFiles.length}` : ""}
          </button>
        )}
      </div>

      {!sessionMode && committing && (
        <div className="commit-editor">
          <div className="commit-editor__heading">
            <GitCommitHorizontal size={14} />
            <span>Commit staged changes</span>
            <span className="commit-editor__count">{stagedFiles.length} file{stagedFiles.length === 1 ? "" : "s"}</span>
          </div>
          <textarea
            autoFocus
            className="commit-editor__message"
            placeholder="Describe what changed..."
            value={message}
            rows={3}
            onChange={(e) => setMessage(e.target.value)}
          />
          <div className="commit-editor__footer">
            <label className="commit-editor__amend">
              <input type="checkbox" checked={amend} onChange={enableAmend} />
              Amend latest commit
            </label>
            <span style={{ flex: 1 }} />
            <button className="btn" onClick={() => { setCommitting(false); setAmend(false); }}>Cancel</button>
            <button className="btn btn--primary" disabled={!message.trim() || !hasStagedChanges || busy} onClick={() => void commit()}>
              {busy ? "Committing..." : amend ? "Amend commit" : "Commit changes"}
            </button>
          </div>
        </div>
      )}

      {error ? (
        <div className="msg-system">{cleanError(error)}</div>
      ) : !diff ? (
        <div style={{ color: "var(--text-muted)" }}>Loading...</div>
      ) : visibleFiles.length === 0 ? (
        <div className="diff-empty">
          <span style={{ fontSize: 15 }}>✓</span>
          <span>{summary && summary.files > 0 ? "Binary changes only" : "No changes against HEAD"}</span>
        </div>
      ) : (
        <>
          <DiffSummary summary={totalSummary} />
          {!sessionMode ? (
            <>
              <ChangeGroupList title="Staged changes" group="staged" files={stagedFiles} selectedKey={selectedKey} onSelect={setSelectedKey} onStage={updateStage} busy={stageBusy} />
              <ChangeGroupList title="Unstaged changes" group="unstaged" files={unstagedFiles} selectedKey={selectedKey} onSelect={setSelectedKey} onStage={updateStage} busy={stageBusy} />
            </>
          ) : null}
          {diff.patch.includes(TRUNCATED_MARKER) && <div className="diff-truncated">Diff is large - showing first ~300 KB only.</div>}
          {selectedFile && <DiffFileCard file={selectedFile} onCopy={() => copyPatch(selectedFile.raw)} group={selectedGroup} />}
          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <button className="btn" onClick={() => copyPatch(selectedFile?.raw || diff.patch)}>Copy patch</button>
          </div>
        </>
      )}
    </div>
  );
}

function ChangeGroupList({
  title,
  group,
  files,
  selectedKey,
  onSelect,
  onStage,
  busy,
}: {
  title: string;
  group: ChangeGroup;
  files: DiffFile[];
  selectedKey: string;
  onSelect: (key: string) => void;
  onStage: (file: DiffFile, group: ChangeGroup) => void;
  busy: Record<string, boolean>;
}) {
  return (
    <section className={`change-group change-group--${group}`}>
      <div className="change-group__heading">
        <ChevronDown size={13} />
        <span>{title}</span>
        <span className="change-group__count">{files.length}</span>
      </div>
      {files.length === 0 ? (
        <div className="change-group__empty">No files</div>
      ) : (
        <div className="change-list">
          {files.map((file) => {
            const key = `${group}:${file.path}`;
            return (
              <div className={`change-row${selectedKey === key ? " change-row--selected" : ""}`} key={key} onClick={() => onSelect(key)}>
                <span className={`change-row__check change-row__check--${group}`} aria-hidden="true">
                  {group === "staged" && <Check size={11} />}
                </span>
                <span className="change-row__icon">
                  {file.status === "added" ? <FilePlus2 size={13} /> : file.status === "deleted" ? <FileMinus2 size={13} /> : <FileText size={13} />}
                </span>
                <span className="change-row__path" title={file.path}>{file.path}</span>
                <span className="change-row__stats">
                  {file.additions > 0 && <span className="diff-file__cnt--add">+{file.additions}</span>}
                  {file.deletions > 0 && <span className="diff-file__cnt--del">-{file.deletions}</span>}
                </span>
                <button
                  className="icon-btn change-row__stage"
                  title={group === "staged" ? "Unstage file" : "Stage file"}
                  disabled={busy[key]}
                  onClick={(event) => { event.stopPropagation(); onStage(file, group); }}
                >
                  {group === "staged" ? <ChevronRight size={13} /> : <ChevronRight size={13} />}
                </button>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

function CommitHistory({
  commits,
  loading,
  onSelect,
}: {
  commits: CommitInfo[];
  loading: boolean;
  onSelect: (subject: string) => void;
}) {
  return (
    <div className="commit-history">
      <div className="commit-history__heading">Recent commits</div>
      {loading ? <div className="commit-history__empty">Loading history...</div> : commits.length === 0 ? <div className="commit-history__empty">No commits found</div> : commits.map((commit) => (
        <button className="commit-history__item" key={commit.sha} onClick={() => onSelect(commit.subject)}>
          <span className="commit-history__subject">{commit.subject}</span>
          <span className="commit-history__meta"><code>{commit.sha.slice(0, 7)}</code><span>{formatCommitDate(commit.when)}</span></span>
        </button>
      ))}
    </div>
  );
}

function DiffSummary({ summary }: { summary: StatSummary | null }) {
  if (!summary) return null;
  return (
    <div className="diff-summary">
      <span className="diff-summary__chip">{summary.files} file{summary.files === 1 ? "" : "s"} changed</span>
      {summary.insertions > 0 && <span className="diff-summary__chip diff-summary__chip--add">+{summary.insertions}</span>}
      {summary.deletions > 0 && <span className="diff-summary__chip diff-summary__chip--del">-{summary.deletions}</span>}
    </div>
  );
}

function DiffFileCard({ file, onCopy, group }: { file: DiffFile; onCopy: () => void; group: ChangeGroup }) {
  return (
    <div className="diff-file">
      <div className="diff-file__head">
        <span className={`diff-file__state diff-file__state--${group}`}>{group === "staged" ? "Staged" : "Unstaged"}</span>
        <span className="diff-file__icon">{file.status === "added" ? <FilePlus2 size={13} /> : file.status === "deleted" ? <FileMinus2 size={13} /> : <FileText size={13} />}</span>
        <span className="diff-file__path" title={file.path}>{file.path}</span>
        {file.binary && <span className="diff-file__badge">binary</span>}
        {file.additions > 0 && <span className="diff-file__cnt diff-file__cnt--add">+{file.additions}</span>}
        {file.deletions > 0 && <span className="diff-file__cnt diff-file__cnt--del">-{file.deletions}</span>}
        <button className="icon-btn diff-file__copy" title="Copy file patch" onClick={onCopy}><Copy size={12} /></button>
      </div>
      {file.binary && <div className="diff-file__binary">Binary file - patch omitted</div>}
      {file.hunks.map((hunk, i) => (
        <div key={i} className="diff-hunk">
          <div className="diff-hunk__header">{hunk.header}</div>
          {hunk.lines.map((line, j) => <DiffRow key={j} line={line} />)}
        </div>
      ))}
    </div>
  );
}

function DiffRow({ line }: { line: DiffLine }) {
  const sign = line.kind === "add" ? "+" : line.kind === "del" ? "-" : line.kind === "meta" ? "" : " ";
  return (
    <div className={`diff-line diff-line--${line.kind}`}>
      <span className="diff-line__no">{line.oldNo ?? ""}</span>
      <span className="diff-line__no">{line.newNo ?? ""}</span>
      <span className="diff-line__sign">{sign}</span>
      <span className="diff-line__text">{line.text}</span>
    </div>
  );
}

function combineSummaries(first: StatSummary | null, second: StatSummary | null): StatSummary | null {
  if (!first && !second) return null;
  return {
    files: (first?.files || 0) + (second?.files || 0),
    insertions: (first?.insertions || 0) + (second?.insertions || 0),
    deletions: (first?.deletions || 0) + (second?.deletions || 0),
  };
}

function formatCommitDate(timestamp: number): string {
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(new Date(timestamp * 1000));
}

export function cleanError(e: unknown): string {
  return String(e).replace(/^Error:/, "").trim() || "Unknown error";
}
