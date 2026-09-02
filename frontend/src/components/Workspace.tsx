import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  ChevronRight,
  ChevronDown,
  File,
  FileCode2,
  FileJson2,
  FileText,
  FileType2,
  Folder,
  Image,
  Braces,
  GitBranch,
  GitFork,
  GitPullRequest,
  Network,
  Pencil,
  TerminalSquare,
  Wrench,
  Copy,
  X,
  FolderOpen,
  Plus,
  Save,
  ArrowLeft,
  ArrowRight,
  RefreshCw,
  Globe,
} from "lucide-react";
import { TERMINAL_SLOTS, TERMINAL_SLOT_NUMBERS, useStore } from "../state/store";
import type { TerminalSession } from "../state/store";
import { api } from "../lib/backend";
import type { ChatItem, PermissionRequest } from "../lib/types";
import Markdown from "./Markdown";
import Composer from "./Composer";
import DiffView from "./DiffView";
import OutputView from "./OutputView";
import TerminalView, { type TerminalHandle } from "./TerminalView";
import PermCard from "./PermCard";
import { providerLabel, truncate } from "../lib/format";
import { applyRatios, clampRatioToPixels, columnsForWidth, computeLayout, defaultTree } from "../lib/terminalLayout";
import type { GutterRect } from "../lib/terminalLayout";
import { toolAction } from "../state/store";
import ProviderLogo from "./ProviderLogo";

export default function Workspace() {
  const selectedWorktreeId = useStore((s) => s.selectedWorktreeId);
  const projects = useStore((s) => s.projects);
  const sessions = useStore((s) => s.sessions);
  const selectedSession = useStore((s) => s.selectedSession);
  const terminalSessions = useStore((s) => s.terminalSessions);
  const setTerminalKind = useStore((s) => s.setTerminalKind);
  const configureTerminalSession = useStore((s) => s.configureTerminalSession);
  const createTerminalBatch = useStore((s) => s.createTerminalBatch);
  const updateTerminalWorktree = useStore((s) => s.updateTerminalWorktree);
  const closeTerminalSession = useStore((s) => s.closeTerminalSession);
  const selectedWorkspaceSession = useStore((s) => s.selectedWorkspaceSession);
  const selectWorkspaceSession = useStore((s) => s.selectWorkspaceSession);
  const tab = useStore((s) => s.tab);
  const setTab = useStore((s) => s.setTab);
  const setDialog = useStore((s) => s.setDialog);
  const openSession = useStore((s) => s.openSession);
  const forkSession = useStore((s) => s.forkSession);
  const loadedSessions = useStore((s) => s.loadedSessions);
  const [forking, setForking] = useState(false);
  const [toolsOpen, setToolsOpen] = useState(false);
  const [pendingTerminalBatch, setPendingTerminalBatch] = useState<{ worktreeId: string; count: number } | null>(null);
  const [browserPath, setBrowserPath] = useState("");
  const [railWidths, setRailWidths] = useState<Record<string, number>>({
    diff: 380,
    output: 380,
    terminal: 660,
  });

  const wt = useMemo(() => {
    for (const pt of projects) {
      const found = pt.worktrees.find((w) => w.id === selectedWorktreeId);
      if (found) return { worktree: found, project: pt.project };
    }
    return null;
  }, [projects, selectedWorktreeId]);

  const wtSessions = useMemo(() => {
    if (!selectedWorktreeId) return [];
    return Object.values(sessions)
      .filter((s) => s.worktreeId === selectedWorktreeId)
      .sort((a, b) => b.updatedAt - a.updatedAt);
  }, [sessions, selectedWorktreeId]);

  const allTerminals = useMemo(() => Object.values(terminalSessions), [terminalSessions]);

  // Imperative handles let the slot strip focus a live pane without remounting it.
  const paneHandles = useRef(new Map<string, TerminalHandle>());
  const registerPane = (id: string, handle: TerminalHandle | null) => {
    if (handle) paneHandles.current.set(id, handle);
    else paneHandles.current.delete(id);
  };
  const focusPane = (id: string) => {
    paneHandles.current.get(id)?.focus();
    const currentWorktreeId = wt?.worktree.id;
    if (currentWorktreeId) selectWorkspaceSession(currentWorktreeId, id);
  };

  const selectedWorkspaceSessionId = selectedWorktreeId ? selectedWorkspaceSession[selectedWorktreeId] : undefined;
  const terminal = selectedWorkspaceSessionId ? terminalSessions[selectedWorkspaceSessionId] : undefined;
  const currentSessionId = terminal ? null : selectedWorkspaceSessionId || (selectedWorktreeId && selectedSession[selectedWorktreeId]) || wtSessions[0]?.id || null;
  const session = currentSessionId ? sessions[currentSessionId] : null;
  const sessionItems = useStore((s) => (currentSessionId ? s.items[currentSessionId] : undefined));

  useEffect(() => {
    if (currentSessionId && !loadedSessions.has(currentSessionId)) {
      void openSession(currentSessionId);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentSessionId]);

  const lastDbMessageId = () => {
    const list = sessionItems || [];
    for (let i = list.length - 1; i >= 0; i--) {
      const m = /^db_(\d+)$/.exec(list[i].id);
      if (m) return Number(m[1]);
    }
    return 0;
  };

  const doFork = async () => {
    if (!session) return;
    setForking(true);
    const created = await forkSession(session.id, lastDbMessageId());
    setForking(false);
    if (created) void openSession(created.id);
  };

  if (!wt) {
    return (
      <div className="workspace">
        <EmptyWorkspace
          title="Select a project"
          detail="Add a git repository to start orchestrating agents across its branches and worktrees."
        />
      </div>
    );
  }

  const parent = session?.parentId ? sessions[session.parentId] : undefined;
  const projectWorktrees = projects.find((projectTree) => projectTree.project.id === wt.project.id)?.worktrees || [wt.worktree];

  const railKey = tab === "chat" ? "diff" : tab;
  const railWidth = railWidths[railKey] ?? 380;
  const startRailDrag = (e: React.PointerEvent) => {
    e.preventDefault();
    const startX = e.clientX;
    const startW = railWidth;
    const move = (ev: PointerEvent) => {
      const w = Math.min(980, Math.max(300, startW + (startX - ev.clientX)));
      setRailWidths((ws) => ({ ...ws, [railKey]: w }));
    };
    const up = () => {
      document.removeEventListener("pointermove", move);
      document.removeEventListener("pointerup", up);
    };
    document.addEventListener("pointermove", move);
    document.addEventListener("pointerup", up);
  };

  return (
    <div className="workspace">
      <div className="workspace__header">
        <div className="crumbs">
          <span className="crumb-project">{wt.project.name}</span>
          <span className="crumb-sep">/</span>
          <span className="crumb-branch">
            <GitBranch size={11.5} />
            {wt.worktree.branch}
          </span>
          {session && (
            <>
              <span className="crumb-sep">/</span>
              <span
                title={session.title || "Untitled"}
                style={{
                  fontSize: 12.5,
                  color: "var(--text-primary)",
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                }}
              >
                {truncate(session.title || "Untitled", 26)}
              </span>
              <button
                className="icon-btn"
                style={{ width: 22, height: 22 }}
                title="Rename session"
                onClick={() => setDialog({ kind: "rename", sessionId: session.id })}
              >
                <Pencil size={12} />
              </button>
              <button
                className="icon-btn"
                style={{ width: 22, height: 22 }}
                title="Fork session"
                disabled={forking}
                onClick={() => void doFork()}
              >
                <GitFork size={12} />
              </button>
              <button
                className="icon-btn"
                style={{ width: 22, height: 22 }}
                title="Spawn subagent"
                onClick={() => setDialog({ kind: "subagent", sessionId: session.id })}
              >
                <Network size={12} />
              </button>
              {session.parentId && (
                <button
                  className="pill"
                  style={{ cursor: "pointer" }}
                  title={`Open parent session ${session.parentId}`}
                  onClick={() => void openSession(session.parentId!)}
                >
                  child of {parent ? parent.title || parent.id.slice(0, 6) : session.parentId.slice(0, 6)}
                </button>
              )}
            </>
          )}
        </div>
        <div className="rail-toggles">
          <div className="tools-menu">
            <button className={`pill rail-toggle ${tab !== "chat" ? "pill--active" : ""}`} title="Open workspace tools" onClick={() => setToolsOpen((v) => !v)}>
              <Plus size={13} />
            </button>
            {toolsOpen && <div className="tools-menu__popup" onClick={() => setToolsOpen(false)}>
              <button onClick={() => setTab("diff")}><GitPullRequest size={13} />Diff</button>
              <button disabled={!session} onClick={() => setTab("output")}><TerminalSquare size={13} />Output</button>
              <button onClick={() => setTab("workspace")}><FolderOpen size={13} />Workspace</button>
              <button onClick={() => setTab("browser")}><Globe size={13} />Browser</button>
            </div>}
          </div>
        </div>
      </div>

      <div className="workspace__body">
        <div className="workspace__main">
          {/* Both panes stay mounted: unmounting a terminal kills its shell. */}
          <div className={`workspace__pane ${terminal ? "" : "workspace__pane--hidden"}`}>
            <TerminalDeck
              worktrees={projectWorktrees}
              worktreeId={wt.worktree.id}
              terminals={allTerminals}
              activeId={terminal?.id}
              onFocusPane={focusPane}
              onSummon={(count) => {
                setPendingTerminalBatch({ worktreeId: wt.worktree.id, count });
              }}
              onClose={(id) => closeTerminalSession(id)}
              onWorktreeChange={(terminalId, worktree) => updateTerminalWorktree(terminalId, worktree)}
              pendingBatch={pendingTerminalBatch}
              onBatchWorktreeChange={(worktreeId) => setPendingTerminalBatch((batch) => batch ? { ...batch, worktreeId } : batch)}
              onBatchCancel={() => setPendingTerminalBatch(null)}
              onChoose={(terminalId, kind) => {
                configureTerminalSession(terminalId, kind);
              }}
              onBatchChoose={(kind) => {
                if (!pendingTerminalBatch) return;
                createTerminalBatch(pendingTerminalBatch.worktreeId, pendingTerminalBatch.count, kind);
                setPendingTerminalBatch(null);
              }}
              onDetectedKind={(terminalId, kind) => setTerminalKind(terminalId, kind)}
              registerPane={registerPane}
            />
          </div>
          <div className={`workspace__pane ${terminal ? "workspace__pane--hidden" : ""}`}>
            {tab === "workspace" ? (
              <RepositoryView worktreeId={wt.worktree.id} onOpenBrowser={(path) => { setBrowserPath(path); setTab("browser"); }} />
            ) : tab === "browser" ? (
              <BrowserView worktreeId={wt.worktree.id} initialPath={browserPath} />
            ) : !session ? (
              <EmptyWorkspace
                title="No agent session"
                detail="Start a new agent from the sidebar to begin working in this branch."
              />
            ) : (
              <ChatPane sessionId={session.id} />
            )}
          </div>
        </div>
        {tab !== "chat" && tab !== "terminal" && tab !== "workspace" && tab !== "browser" && (
          <div className="rail-resize" onPointerDown={startRailDrag} title="Drag to resize (Double click to reset)" onDoubleClick={() => setRailWidths((ws) => ({ ...ws, [railKey]: railKey === "terminal" ? 660 : 380 }))} />
        )}
        {tab !== "chat" && tab !== "terminal" && tab !== "workspace" && tab !== "browser" && (
          <aside className="right-rail" style={{ width: railWidth }}>
            <div className="tabs right-rail__tabs">
              <button className={`tab ${tab === "diff" ? "tab--active" : ""}`} onClick={() => setTab("diff")}>
                Diff
              </button>
              <button
                className={`tab ${tab === "output" ? "tab--active" : ""}`}
                disabled={!session}
                onClick={() => setTab("output")}
              >
                Output
              </button>
              <span style={{ flex: 1 }} />
              <button className="icon-btn" style={{ width: 24, height: 24 }} title="Close sidebar (Alt+1)" onClick={() => setTab("chat")}>
                <X size={12} />
              </button>
            </div>
            <div className="right-rail__view">
              {tab === "diff" ? (
                <DiffView worktreeId={wt.worktree.id} sessionId={session?.id} />
              ) : (
                <OutputView sessionId={session?.id} />
              )}
            </div>
          </aside>
        )}
      </div>
    </div>
  );
}

function TerminalSetup({ worktrees, worktreeId, title, onWorktreeChange, onChoose, onCancel, count }: { worktrees: { id: string; branch: string; path: string }[]; worktreeId: string; title: string; onWorktreeChange: (id: string) => void; onChoose: (kind: "shell" | "codex" | "claude") => void; onCancel?: () => void; count?: number }) {
  const selected = worktrees.find((worktree) => worktree.id === worktreeId);
  const isBatch = count !== undefined;
  const terminalLabel = count === 1 ? "terminal" : "terminals";
  return <div className="terminal-setup">
    <div className="terminal-setup__head">
      <span className="terminal-setup__slot">{title}</span>
      {onCancel && <button className="terminal__btn" title="Cancel" onClick={onCancel}><X size={11} /></button>}
    </div>
    <div className="terminal-setup__title">{isBatch ? `Create ${count} ${terminalLabel}` : "Create terminal"}</div>
    <div className="terminal-setup__detail">{isBatch ? "Choose what these terminals should run." : "Choose what this terminal should run."}</div>
    <div className="terminal-setup__branch"><span>Run in branch</span><BranchDropdown worktrees={worktrees} selectedId={worktreeId} onChange={onWorktreeChange} /><small title={selected?.path}>{selected?.path}</small></div>
    <div className="terminal-setup__options"><button onClick={() => onChoose("shell")}><TerminalSquare size={17} /><span><b>Blank Terminal</b><small>Start a normal shell</small></span></button><button onClick={() => onChoose("codex")}><ProviderLogo provider="codex" size={17} /><span><b>Codex</b><small>Start Codex CLI</small></span></button><button onClick={() => onChoose("claude")}><ProviderLogo provider="claude" size={17} /><span><b>Claude</b><small>Start Claude Code</small></span></button></div>
  </div>;
}

function BranchDropdown({ worktrees, selectedId, onChange }: { worktrees: { id: string; branch: string; path: string }[]; selectedId: string; onChange: (id: string) => void }) {
  const [open, setOpen] = useState(false);
  const selected = worktrees.find((worktree) => worktree.id === selectedId);
  return <div className="branch-dropdown"><button className="branch-dropdown__trigger" onClick={() => setOpen((value) => !value)}><GitBranch size={12} />{selected?.branch || "Select branch"}<ChevronDown size={13} /></button>{open && <div className="branch-dropdown__menu">{worktrees.map((worktree) => <button key={worktree.id} className={worktree.id === selectedId ? "branch-dropdown__option--active" : ""} onClick={() => { onChange(worktree.id); setOpen(false); }}><GitBranch size={12} /><span><b>{worktree.branch}</b><small>{worktree.path}</small></span></button>)}</div>}</div>;
}

function ChatBubbleIcon() {
  return <span className="workspace-mode-switch__chat-icon" aria-hidden="true" />;
}

function kindLabel(kind: TerminalSession["kind"]): string {
  return kind === "shell" ? "Shell" : kind === "codex" ? "Codex" : kind === "claude" ? "Claude Code" : "New";
}

function TerminalDeck({ worktrees, worktreeId, terminals, activeId, onFocusPane, onSummon, onClose, onWorktreeChange, onChoose, pendingBatch, onBatchWorktreeChange, onBatchChoose, onBatchCancel, onDetectedKind, registerPane }: {
  worktrees: { id: string; branch: string; path: string }[];
  worktreeId: string;
  terminals: TerminalSession[];
  activeId?: string;
  onFocusPane: (id: string) => void;
  onSummon: (count: number) => void;
  onClose: (id: string) => void;
  onWorktreeChange: (terminalId: string, worktreeId: string) => void;
  onChoose: (terminalId: string, kind: "shell" | "codex" | "claude") => void;
  pendingBatch?: { worktreeId: string; count: number } | null;
  onBatchWorktreeChange: (worktreeId: string) => void;
  onBatchChoose: (kind: "shell" | "codex" | "claude") => void;
  onBatchCancel: () => void;
  onDetectedKind: (terminalId: string, kind: "shell" | "codex" | "claude") => void;
  registerPane: (id: string, handle: TerminalHandle | null) => void;
}) {
  const surfaceRef = useRef<HTMLDivElement | null>(null);
  const [surface, setSurface] = useState({ width: 0, height: 0 });
  const [dragRatios, setDragRatios] = useState<Record<string, number>>({});
  const [draggingKey, setDraggingKey] = useState<string | null>(null);
  const dragValue = useRef<number | null>(null);
  const splitRatios = useStore((s) => s.terminalSplitRatios[worktreeId]);
  const setSplitRatio = useStore((s) => s.setTerminalSplitRatio);
  const resetSplitRatio = useStore((s) => s.resetTerminalSplitRatio);

  const bySlot = new Map(
    terminals.filter((terminal) => terminal.worktreeId === worktreeId).map((terminal) => [terminal.slot, terminal]),
  );
  const full = bySlot.size >= TERMINAL_SLOTS;

  // Every terminal stays mounted for its whole lifetime: panes that belong to
  // another branch are hidden (display:none) instead of unmounted, so no shell
  // is ever restarted by a render, a reflow, or a branch switch.
  const ordered = [...terminals].sort((a, b) =>
    a.worktreeId === b.worktreeId ? a.slot - b.slot : a.worktreeId < b.worktreeId ? -1 : 1,
  );
  const visible = ordered.filter((terminal) => terminal.worktreeId === worktreeId);

  // Ratios are keyed by split path, which only means something for the branch
  // it was dragged in — drop any in-flight drag when the branch changes.
  useEffect(() => {
    dragValue.current = null;
    setDragRatios({});
  }, [worktreeId]);

  const deckMounted = !pendingBatch;
  useLayoutEffect(() => {
    const el = surfaceRef.current;
    if (!el) return;
    const measure = () => setSurface((prev) => (
      Math.abs(prev.width - el.clientWidth) < 0.5 && Math.abs(prev.height - el.clientHeight) < 0.5
        ? prev
        : { width: el.clientWidth, height: el.clientHeight }
    ));
    measure();
    // Also fires when the deck is revealed: a display:none pane measures 0x0.
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [deckMounted]);

  const startGutterDrag = (gutter: GutterRect, event: React.PointerEvent) => {
    const el = surfaceRef.current;
    if (!el || event.button !== 0) return;
    event.preventDefault();
    const bounds = el.getBoundingClientRect();
    // Measured against the span this split owns, not the whole surface, so the
    // seam stays under the pointer even for a nested split.
    const origin = (gutter.dir === "row" ? bounds.left : bounds.top) + gutter.axisStart;
    const total = gutter.axisLength;
    if (total <= 0) return;
    setDraggingKey(gutter.key);
    document.body.classList.add(gutter.dir === "row" ? "is-resizing-row" : "is-resizing-col");

    // A drag is held in local state and committed once on release: writing to
    // the store (and localStorage) on every pointermove would thrash both.
    const move = (ev: PointerEvent) => {
      const position = gutter.dir === "row" ? ev.clientX : ev.clientY;
      const ratio = clampRatioToPixels((position - origin) / total, total);
      dragValue.current = ratio;
      setDragRatios((prev) => ({ ...prev, [gutter.key]: ratio }));
    };
    const finish = () => {
      document.removeEventListener("pointermove", move);
      document.removeEventListener("pointerup", finish);
      document.removeEventListener("pointercancel", finish);
      document.body.classList.remove("is-resizing-row", "is-resizing-col");
      if (dragValue.current !== null) setSplitRatio(worktreeId, gutter.key, dragValue.current);
      dragValue.current = null;
      setDraggingKey(null);
      setDragRatios({});
    };
    document.addEventListener("pointermove", move);
    document.addEventListener("pointerup", finish);
    document.addEventListener("pointercancel", finish);
  };

  const base = visible.length
    ? defaultTree(visible.map((terminal) => terminal.id), columnsForWidth(surface.width))
    : null;
  const tree = base ? applyRatios(base, { ...(splitRatios || {}), ...dragRatios }) : null;
  const { rects, gutters } = computeLayout(tree, surface.width, surface.height);

  if (pendingBatch) {
    return <div className="terminal-deck terminal-deck--setup-only">
      <TerminalSetup
        worktrees={worktrees}
        worktreeId={pendingBatch.worktreeId}
        title={`Create ${pendingBatch.count} terminal${pendingBatch.count === 1 ? "" : "s"}`}
        count={pendingBatch.count}
        onWorktreeChange={onBatchWorktreeChange}
        onChoose={onBatchChoose}
        onCancel={onBatchCancel}
      />
    </div>;
  }

  return <div className="terminal-deck">
    <div className="terminal-deck__tabs">
      {TERMINAL_SLOT_NUMBERS.map((slot) => {
        const terminal = bySlot.get(slot);
        if (!terminal) {
          return <button key={slot} className="terminal-tab terminal-tab--empty" title={full ? `All ${TERMINAL_SLOTS} terminals are in use` : `Create ${slot} terminal${slot === 1 ? "" : "s"}`} disabled={full} onClick={() => onSummon(slot)}><Plus size={11} /><span>{slot}</span></button>;
        }
        const label = kindLabel(terminal.kind);
        return <button key={slot} className={`terminal-tab ${terminal.id === activeId ? "terminal-tab--active" : ""}`} title={`Terminal ${slot} · ${label}`} onClick={() => onFocusPane(terminal.id)}>
          <span className="terminal-tab__num">{slot}</span>
          {terminal.kind === "codex" || terminal.kind === "claude" ? <ProviderLogo provider={terminal.kind} size={11} /> : <TerminalSquare size={11} />}
          <span className="terminal-tab__label">{label}</span>
        </button>;
      })}
      <span style={{ flex: 1 }} />
      <span className="terminal-deck__count">{bySlot.size}/{TERMINAL_SLOTS}</span>
    </div>
    <div className="terminal-grid">
      <div className="terminal-grid__surface" ref={surfaceRef}>
        {ordered.map((terminal) => {
          const rect = terminal.worktreeId === worktreeId ? rects.get(terminal.id) : undefined;
          const style = rect
            ? { left: rect.x, top: rect.y, width: rect.w, height: rect.h }
            : { display: "none" };
          return <div key={terminal.id} className={`terminal-grid__cell ${terminal.id === activeId ? "terminal-grid__cell--active" : ""}`} style={style}>
            {terminal.kind === null ? (
              <TerminalSetup
                worktrees={worktrees}
                worktreeId={terminal.worktreeId}
                title={`Terminal ${terminal.slot}`}
                onWorktreeChange={(id) => onWorktreeChange(terminal.id, id)}
                onChoose={(kind) => onChoose(terminal.id, kind)}
                onCancel={() => onClose(terminal.id)}
              />
            ) : (
              <TerminalView
                ref={(handle) => registerPane(terminal.id, handle)}
                sessionKey={`${terminal.worktreeId}::${terminal.id}`}
                label={`Terminal ${terminal.slot} · ${kindLabel(terminal.kind)}`}
                kind={terminal.kind}
                initialCommand={!terminal.detected && terminal.kind === "codex" ? "codex" : !terminal.detected && terminal.kind === "claude" ? "claude" : undefined}
                onDetectedKind={(kind) => onDetectedKind(terminal.id, kind)}
                onFocus={() => onFocusPane(terminal.id)}
                onClose={() => onClose(terminal.id)}
              />
            )}
          </div>;
        })}
        {gutters.map((gutter) => (
          <div
            key={gutter.key}
            className={`split-gutter split-gutter--${gutter.dir}${draggingKey === gutter.key ? " split-gutter--dragging" : ""}`}
            style={{ left: gutter.x, top: gutter.y, width: gutter.w, height: gutter.h }}
            title="Drag to resize (Double click to reset)"
            onPointerDown={(event) => startGutterDrag(gutter, event)}
            onDoubleClick={() => resetSplitRatio(worktreeId, gutter.key)}
          />
        ))}
      </div>
    </div>
  </div>;
}

function BrowserView({ worktreeId, initialPath }: { worktreeId: string; initialPath: string }) {
  const [url, setUrl] = useState(initialPath);
  const [source, setSource] = useState("");
  const [externalUrl, setExternalUrl] = useState("");
  const [editing, setEditing] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  const load = async (path = url, push = true) => {
    const clean = path.replace(/^\/+/, "");
    if (!clean) { setSource(""); setError(""); return; }
    if (/^https?:\/\//i.test(clean)) {
      setUrl(clean); setExternalUrl(clean); setSource(""); setError(""); setLoading(false);
      if (push) {
        const next = history.slice(0, historyIndex + 1).concat(clean);
        setHistory(next); setHistoryIndex(next.length - 1);
      }
      return;
    }
    setExternalUrl("");
    setUrl(clean); setLoading(true); setError("");
    try {
      const text = await api.readFileText(worktreeId, clean, 500_000);
      setSource(text);
      if (push) {
        const next = history.slice(0, historyIndex + 1).concat(clean);
        setHistory(next); setHistoryIndex(next.length - 1);
      }
    } catch (e) { setSource(""); setError(String(e)); }
    finally { setLoading(false); }
  };
  useEffect(() => { setUrl(initialPath); setSource(""); setError(""); if (initialPath) void load(initialPath); }, [worktreeId, initialPath]);
  const goBack = () => { if (historyIndex > 0) { setHistoryIndex(historyIndex - 1); void load(history[historyIndex - 1], false); } };
  const goForward = () => { if (historyIndex < history.length - 1) { setHistoryIndex(historyIndex + 1); void load(history[historyIndex + 1], false); } };
  const save = async () => { await api.writeFileText(worktreeId, url, source); setEditing(false); };

  return <div className="browser-view">
    <div className="browser-toolbar">
      <button className="icon-btn" title="Back" disabled={historyIndex <= 0} onClick={goBack}><ArrowLeft size={15} /></button>
      <button className="icon-btn" title="Forward" disabled={historyIndex >= history.length - 1} onClick={goForward}><ArrowRight size={15} /></button>
      <button className="icon-btn" title="Reload" onClick={() => void load(url, false)}><RefreshCw size={14} /></button>
      <form className="browser-address" onSubmit={(e) => { e.preventDefault(); void load(); }}><Globe size={14} /><input value={url} onChange={(e) => setUrl(e.target.value)} aria-label="Browser address" /></form>
      <button className={`pill ${editing ? "pill--active" : ""}`} disabled={Boolean(externalUrl)} onClick={() => setEditing((v) => !v)}>{editing ? "Preview" : "Edit"}</button>
      {editing && <button className="pill" onClick={() => void save()}><Save size={13} />Save</button>}
    </div>
    <div className="browser-content">
      {editing && !externalUrl ? <textarea className="browser-editor" value={source} onChange={(e) => setSource(e.target.value)} /> : loading ? <div className="repository-inspector__empty">Loading {url}…</div> : error ? <div className="repository-inspector__empty">{error}</div> : externalUrl ? <iframe className="browser-page" title="In-house browser" src={externalUrl} /> : source ? <iframe className="browser-page" title="In-house browser" sandbox="allow-scripts" srcDoc={source} /> : <div className="browser-inspector-empty">Enter a URL or repository HTML path above.</div>}
    </div>
  </div>;
}

function RepositoryView({ worktreeId, onOpenBrowser }: { worktreeId: string; onOpenBrowser: (path: string) => void }) {
  const [files, setFiles] = useState<string[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [reading, setReading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    setError(null);
    api.searchRepo(worktreeId, "", 1000).then((paths) => {
      if (alive) setFiles(paths);
    }).catch((e) => {
      if (alive) setError(String(e));
    }).finally(() => alive && setLoading(false));
    return () => { alive = false; };
  }, [worktreeId]);

  const tree = useMemo(() => buildTree(files), [files]);
  const selectFile = async (path: string) => {
    setSelected(path);
    setEditing(false);
    setReading(true);
    setError(null);
    try {
      setContent(await api.readFileText(worktreeId, path, 250_000));
    } catch (e) {
      setContent("");
      setError(String(e));
    } finally {
      setReading(false);
    }
  };
  const isHtml = Boolean(selected && /\.(html?|xhtml)$/i.test(selected));
  const save = async () => {
    if (!selected) return;
    setSaving(true);
    try { await api.writeFileText(worktreeId, selected, content); setEditing(false); }
    finally { setSaving(false); }
  };

  return (
    <div className="repository-view">
      <div className="repository-view__heading">
        <div>
          <div className="empty-title">Repository files</div>
          <div className="empty-detail">Select a file to inspect it.</div>
        </div>
      </div>
      <div className="repository-view__body">
        <aside className="repository-tree">
          {loading ? <div className="file-tree__muted">Loading files…</div> : error && !files.length ? <div className="file-tree__muted">Unable to load files.</div> : files.length === 0 ? <div className="file-tree__muted">No files found.</div> : (
            <TreeContents node={tree} open={open} setOpen={setOpen} selected={selected} onSelect={selectFile} onOpenBrowser={onOpenBrowser} />
          )}
        </aside>
        <section className="repository-inspector">
          {!selected ? <div className="repository-inspector__empty">Choose a file from the repository tree.</div> : reading ? <div className="repository-inspector__empty">Reading {selected}…</div> : error ? <div className="repository-inspector__empty">Could not read <code>{selected}</code><br />{error}</div> : <><div className="repository-inspector__header"><File size={14} />{selected}<span style={{ flex: 1 }} /><button className="icon-btn" title="Edit file" onClick={() => setEditing((v) => !v)}>{editing ? "Preview" : "Edit"}</button>{editing && <button className="icon-btn" title="Save file" disabled={saving} onClick={() => void save()}><Save size={13} /></button>}</div>{editing ? <textarea className="repository-inspector__editor" value={content} onChange={(e) => setContent(e.target.value)} /> : isHtml ? <iframe className="repository-inspector__browser" title={`Preview ${selected}`} sandbox="allow-scripts" srcDoc={content} /> : <pre className="repository-inspector__content">{content}</pre>}</>}
        </section>
      </div>
    </div>
  );
}

function buildTree(files: string[]): TreeNode {
  const root: TreeNode = { name: "", path: "", directories: {}, files: [] };
  for (const file of files) {
    const parts = file.split("/").filter(Boolean);
    let node = root;
    parts.forEach((part, index) => {
      if (index === parts.length - 1) node.files.push({ name: part, path: file });
      else {
        node.directories[part] ||= { name: part, path: node.path ? `${node.path}/${part}` : part, directories: {}, files: [] };
        node = node.directories[part];
      }
    });
  }
  return root;
}

function TreeContents({ node, open, setOpen, selected, onSelect, onOpenBrowser, depth = 0 }: { node: TreeNode; open: Record<string, boolean>; setOpen: React.Dispatch<React.SetStateAction<Record<string, boolean>>>; selected: string | null; onSelect: (path: string) => void; onOpenBrowser: (path: string) => void; depth?: number }) {
  return <>
    {Object.values(node.directories).sort((a, b) => a.name.localeCompare(b.name)).map((child) => {
      const expanded = open[child.path] ?? depth === 0;
      return <div key={child.path}><button className="file-tree__folder" style={{ paddingLeft: 10 + depth * 18 }} onClick={() => setOpen((v) => ({ ...v, [child.path]: !expanded }))}><ChevronRight size={14} className={expanded ? "file-tree__chevron--open" : ""} /><Folder size={14} />{child.name}</button>{expanded && <TreeContents node={child} open={open} setOpen={setOpen} selected={selected} onSelect={onSelect} onOpenBrowser={onOpenBrowser} depth={depth + 1} />}</div>;
    })}
    {node.files.sort((a, b) => a.name.localeCompare(b.name)).map((file) => <button className={`file-tree__file ${selected === file.path ? "file-tree__file--selected" : ""}`} style={{ paddingLeft: 30 + depth * 18 }} key={file.path} onClick={() => onSelect(file.path)} onContextMenu={(event) => { if (/\.(html?|xhtml)$/i.test(file.path)) { event.preventDefault(); onOpenBrowser(file.path); } }} title={/\.(html?|xhtml)$/i.test(file.path) ? "Right-click to open in Browser" : undefined}><File size={14} />{file.name}</button>)}
  </>;
}


function FileTreeEmptyState({ worktreeId }: { worktreeId: string }) {
  const [files, setFiles] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [open, setOpen] = useState<Record<string, boolean>>({});

  useEffect(() => {
    let alive = true;
    setLoading(true);
    api
      .searchRepo(worktreeId, "", 300)
      .then((paths) => {
        if (alive) setFiles(paths);
      })
      .catch(() => {
        if (alive) setFiles([]);
      })
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [worktreeId]);

  const tree = useMemo(() => {
    const root: TreeNode = { name: "", path: "", directories: {}, files: [] };
    for (const file of files) {
      const parts = file.split("/").filter(Boolean);
      let node = root;
      parts.forEach((part, index) => {
        if (index === parts.length - 1) {
          node.files.push({ name: part, path: file });
          return;
        }
        node.directories[part] ||= { name: part, path: node.path ? `${node.path}/${part}` : part, directories: {}, files: [] };
        node = node.directories[part];
      });
    }
    return root;
  }, [files]);

  return (
    <div className="file-tree-empty">
      <div className="file-tree-empty__header">
        <div>
          <div className="empty-title">Repository files</div>
          <div className="empty-detail">Select a file to inspect it, or start a new agent from the sidebar.</div>
        </div>
      </div>
      <div className="file-tree">
        {loading ? (
          <div className="file-tree__muted">Loading files…</div>
        ) : files.length === 0 ? (
          <div className="file-tree__muted">No files found.</div>
        ) : (
          Object.values(tree.directories).sort((a, b) => a.name.localeCompare(b.name)).map((node) => (
            <TreeBranch key={node.path} node={node} open={open} setOpen={setOpen} />
          )).concat(
            tree.files.sort((a, b) => a.name.localeCompare(b.name)).map((file) => (
              <div className="file-tree__file" key={file.path}><File size={14} />{file.name}</div>
            )),
          )
        )}
      </div>
    </div>
  );
}

type TreeFile = { name: string; path: string };
type TreeNode = { name: string; path: string; directories: Record<string, TreeNode>; files: TreeFile[] };

function TreeBranch({
  node,
  open,
  setOpen,
  depth = 0,
}: {
  node: TreeNode;
  open: Record<string, boolean>;
  setOpen: React.Dispatch<React.SetStateAction<Record<string, boolean>>>;
  depth?: number;
}) {
  const expanded = open[node.path] ?? depth === 0;
  return (
    <div>
      <button className="file-tree__folder" style={{ paddingLeft: 10 + depth * 18 }} onClick={() => setOpen((v) => ({ ...v, [node.path]: !expanded }))}>
        <ChevronRight size={14} className={expanded ? "file-tree__chevron--open" : ""} />
        <Folder size={14} className="file-tree__icon file-tree__icon--folder" />
        {node.name}
      </button>
      {expanded && (
        <div>
          {Object.values(node.directories).sort((a, b) => a.name.localeCompare(b.name)).map((child) => (
            <TreeBranch key={child.path} node={child} open={open} setOpen={setOpen} depth={depth + 1} />
          ))}
          {node.files.sort((a, b) => a.name.localeCompare(b.name)).map((file) => (
            <div className="file-tree__file" style={{ paddingLeft: 30 + depth * 18 }} key={file.path}>
              <TreeFileIcon name={file.name} />
              {file.name}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function TreeFileIcon({ name }: { name: string }) {
  const extension = name.split(".").pop()?.toLowerCase();
  const Icon = extension === "ts" || extension === "tsx" || extension === "js" || extension === "jsx"
    ? FileCode2
    : extension === "json" || extension === "yaml" || extension === "yml" || extension === "toml"
      ? FileJson2
      : extension === "css" || extension === "scss" || extension === "html"
        ? FileType2
        : extension === "md" || extension === "txt" || extension === "doc" || extension === "pdf"
          ? FileText
          : ["png", "jpg", "jpeg", "gif", "svg", "webp"].includes(extension ?? "")
            ? Image
            : extension === "env" || extension === "config"
              ? Braces
              : File;
  const kind = Icon === FileCode2 ? "code" : Icon === FileJson2 ? "data" : Icon === FileType2 ? "markup" : Icon === FileText ? "text" : Icon === Image ? "image" : "generic";
  return <Icon size={14} className={`file-tree__icon file-tree__icon--${kind}`} aria-hidden="true" />;
}
function ChatPane({ sessionId }: { sessionId: string }) {
  const items = useStore((s) => s.items[sessionId]);
  const draft = useStore((s) => s.drafts[sessionId]);
  const session = useStore((s) => s.sessions[sessionId]);
  const pendingRequests = useStore((s) => s.pendingRequests);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const stickRef = useRef(true);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onScroll = () => {
      stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 90;
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [items, draft?.text, draft?.thinking]);

  const running =
    session?.status === "running" || session?.status === "waiting" || session?.status === "starting";
  const empty = !items?.length && !draft?.text && !draft?.thinking;

  return (
    <div style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
      <div className="chat" ref={scrollRef}>
        <div className="chat__inner">
          {empty && (
            <EmptyChat provider={session?.provider} model={session?.model} status={session?.status} />
          )}
          {(items || []).map((it) => (
            <ChatRow key={it.id} item={it} />
          ))}
          {draft?.thinking ? (
            <details className="thinking-block" open>
              <summary>Thinking</summary>
              <div className="thinking-body stream-caret">{draft.thinking}</div>
            </details>
          ) : null}
          {draft?.text ? <div className="msg-assistant md stream-caret">{draft.text}</div> : null}
          {pendingRequests.map((req: PermissionRequest) => (
            <PermCard key={req.requestID} req={req} />
          ))}
        </div>
      </div>
      <Composer sessionId={sessionId} busy={!!running} />
    </div>
  );
}

function ChatRow({ item }: { item: ChatItem }) {
  switch (item.type) {
    case "user":
      return <div className="msg-user">{item.text}</div>;
    case "assistant":
      return (
        <div className="msg-assistant">
          <Markdown text={item.text} />
        </div>
      );
    case "thinking":
      return (
        <details className="thinking-block">
          <summary>Thinking</summary>
          <div className="thinking-body">{item.text}</div>
        </details>
      );
    case "system":
      return <div className="msg-system">{item.text}</div>;
    case "files":
      return (
        <div className="files-pill-row">
          <span className="files-label">Files</span>
          {item.paths.slice(0, 12).map((p, i) => (
            <span key={`${p}_${i}`} className="file-pill">
              {p}
            </span>
          ))}
          {item.paths.length > 12 && <span className="file-pill">+{item.paths.length - 12}</span>}
        </div>
      );
    case "tool":
      return <ToolRow item={item} />;
  }
}

function ToolRow({ item }: { item: Extract<ChatItem, { type: "tool" }> }) {
  const preview = firstLine(item.input || item.result || "");
  const action = item.action || toolAction(item.name);
  return (
    <details className={`tool-row tool-row--${item.status || (item.running ? "running" : "success")}`}>
      <summary className="tool-row__summary">
        <Wrench size={12.5} className={item.running ? "tool-row__spin" : undefined} />
        <span className="tool-row__action">{action}</span>
        <span className="tool-row__name">{item.name}</span>
        <span className="tool-row__preview">{preview}</span>
      </summary>
      <div className="tool-row__body">
        <div className="tool-row__meta">{item.running ? "In progress" : item.status === "error" ? "Failed" : "Completed"}</div>
        {item.input ? (
          <>
            <div className="code-chunk code-chunk--label">Input</div>
            <pre className="code-chunk">{pretty(item.input)}</pre>
          </>
        ) : null}
        {item.result ? (
          <>
            <div className="code-chunk code-chunk--label">Result</div>
            <pre className="code-chunk">{item.result}</pre>
          </>
        ) : null}
        <CopyButton value={item.result || item.input || ""} />
      </div>
    </details>
  );
}

function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      className="btn btn--ghost"
      style={{ alignSelf: "flex-start", fontSize: 11 }}
      onClick={() => {
        void navigator.clipboard.writeText(value);
        setCopied(true);
        setTimeout(() => setCopied(false), 1200);
      }}
    >
      <Copy size={11} />
      {copied ? "Copied" : "Copy"}
    </button>
  );
}

function EmptyChat({
  provider,
  model,
  status,
}: {
  provider?: string;
  model?: string;
  status?: string;
}) {
  return (
    <div className="empty-state" style={{ paddingTop: 80 }}>
      <TerminalSquare size={22} strokeWidth={1.5} />
      <div className="empty-state__title">{status === "starting" ? "Starting…" : "Ready"}</div>
      <div className="empty-state__detail">
        {providerLabel(provider || "")}
        {model ? ` · ${model}` : ""} is listening. Send a task below — output streams here in real time.
      </div>
    </div>
  );
}

export function EmptyWorkspace({
  title,
  detail,
  action,
}: {
  title: string;
  detail: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="empty-state">
      <GitBranch size={22} strokeWidth={1.5} />
      <div className="empty-state__title">{title}</div>
      <div className="empty-state__detail">{detail}</div>
      {action}
    </div>
  );
}

function firstLine(s: string): string {
  const i = s.indexOf("\n");
  return i > 0 ? s.slice(0, i) : s;
}

function pretty(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}
