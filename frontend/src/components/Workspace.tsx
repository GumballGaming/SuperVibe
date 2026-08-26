import { useEffect, useMemo, useRef, useState } from "react";
import {
  ChevronRight,
  File,
  Folder,
  GitBranch,
  GitFork,
  Network,
  Pencil,
  TerminalSquare,
  Wrench,
  Copy,
} from "lucide-react";
import { useStore } from "../state/store";
import { api } from "../lib/backend";
import type { ChatItem, PermissionRequest } from "../lib/types";
import Markdown from "./Markdown";
import Composer from "./Composer";
import DiffView from "./DiffView";
import OutputView from "./OutputView";
import PermCard from "./PermCard";
import { providerLabel, truncate } from "../lib/format";
import { toolAction } from "../state/store";

export default function Workspace() {
  const selectedWorktreeId = useStore((s) => s.selectedWorktreeId);
  const projects = useStore((s) => s.projects);
  const sessions = useStore((s) => s.sessions);
  const selectedSession = useStore((s) => s.selectedSession);
  const tab = useStore((s) => s.tab);
  const setTab = useStore((s) => s.setTab);
  const setDialog = useStore((s) => s.setDialog);
  const openSession = useStore((s) => s.openSession);
  const forkSession = useStore((s) => s.forkSession);
  const loadedSessions = useStore((s) => s.loadedSessions);
  const [forking, setForking] = useState(false);

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

  const currentSessionId =
    (selectedWorktreeId && selectedSession[selectedWorktreeId]) || wtSessions[0]?.id || null;
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
      </div>

      <div className="tabs">
        <button className={`tab ${tab === "chat" ? "tab--active" : ""}`} onClick={() => setTab("chat")}>
          Chat
        </button>
        <button className={`tab ${tab === "diff" ? "tab--active" : ""}`} onClick={() => setTab("diff")}>
          Diff
        </button>
        <button className={`tab ${tab === "output" ? "tab--active" : ""}`} onClick={() => setTab("output")}>
          Output
        </button>
      </div>

      <div className="workspace__content">
        {!session ? (
          <FileTreeEmptyState worktreeId={wt.worktree.id} />
        ) : tab === "chat" ? (
          <ChatPane sessionId={session.id} />
        ) : tab === "diff" ? (
          <DiffView worktreeId={wt.worktree.id} sessionId={session.id} />
        ) : (
          <OutputView sessionId={session.id} />
        )}
      </div>
    </div>
  );
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
        <Folder size={14} />
        {node.name}
      </button>
      {expanded && (
        <div>
          {Object.values(node.directories).sort((a, b) => a.name.localeCompare(b.name)).map((child) => (
            <TreeBranch key={child.path} node={child} open={open} setOpen={setOpen} depth={depth + 1} />
          ))}
          {node.files.sort((a, b) => a.name.localeCompare(b.name)).map((file) => (
            <div className="file-tree__file" style={{ paddingLeft: 30 + depth * 18 }} key={file.path}><File size={14} />{file.name}</div>
          ))}
        </div>
      )}
    </div>
  );
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
