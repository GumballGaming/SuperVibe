import { useEffect, useState } from "react";
import { Bot, ChevronRight, FolderPlus, GitBranch, Layers, Pencil, Plus, Settings, Trash2, TerminalSquare, Sparkles } from "lucide-react";
import { useStore } from "../state/store";
import { providerLabel } from "../lib/format";
import ProviderLogo from "./ProviderLogo";
import type { ProjectTree } from "../lib/types";

export default function Sidebar() {
  const projects = useStore((s) => s.projects);
  const sessions = useStore((s) => s.sessions);
  const view = useStore((s) => s.view);
  const setView = useStore((s) => s.setView);
  const setDialog = useStore((s) => s.setDialog);
  const [contextMenu, setContextMenu] = useState<{ sessionId: string; x: number; y: number } | null>(null);

  useEffect(() => {
    const close = () => setContextMenu(null);
    window.addEventListener("click", close);
    window.addEventListener("blur", close);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("blur", close);
    };
  }, []);

  return (
    <aside className="sidebar">
      <button
        className={`sidebar__fleet-btn ${view === "fleet" ? "sidebar__fleet-btn--active" : ""}`}
        onClick={() => setView("fleet")}
      >
        <Layers size={14} />
        Fleet
        <span style={{ flex: 1 }} />
        <span className="worktree__count">{Object.keys(sessions).length}</span>
      </button>

      <div className="sidebar__section-label">
        Projects
        <button
          className="icon-btn"
          style={{ width: 20, height: 20 }}
          title="Add project"
          onClick={() => setDialog({ kind: "addProject" })}
        >
          <Plus size={13} />
        </button>
      </div>

      {projects.map((pt) => (
        <ProjectNode key={pt.project.id} tree={pt} onSessionContextMenu={(sessionId, event) => {
          event.preventDefault();
          event.stopPropagation();
          setContextMenu({ sessionId, x: event.clientX, y: event.clientY });
        }} />
      ))}

      {projects.length === 0 && (
        <div style={{ padding: "8px 8px", color: "var(--text-muted)", fontSize: 12 }}>
          No projects yet.
        </div>
      )}

      <div className="sidebar__spacer" />

      <div className="sidebar__footer">
        <button className="sidebar__footer-btn" onClick={() => setDialog({ kind: "addProject" })}>
          <FolderPlus size={14} />
          Add Project
        </button>
        <button className="sidebar__footer-btn" onClick={() => setDialog({ kind: "settings" })}>
          <Settings size={14} />
          Settings
        </button>
      </div>
      {contextMenu && <SessionContextMenu {...contextMenu} onClose={() => setContextMenu(null)} />}
    </aside>
  );
}

function ProjectNode({ tree, onSessionContextMenu }: { tree: ProjectTree; onSessionContextMenu: (sessionId: string, event: React.MouseEvent) => void }) {
  const [open, setOpen] = useState(true);
  const [projectMenu, setProjectMenu] = useState<{ x: number; y: number } | null>(null);
  const selectedWorktreeId = useStore((s) => s.selectedWorktreeId);
  const selectedSession = useStore((s) => s.selectedSession);
  const sessions = useStore((s) => s.sessions);
  const terminalSessions = useStore((s) => s.terminalSessions);
  const selectedWorkspaceSession = useStore((s) => s.selectedWorkspaceSession);
  const createTerminalSession = useStore((s) => s.createTerminalSession);
  const selectWorkspaceSession = useStore((s) => s.selectWorkspaceSession);
  const closeTerminalSession = useStore((s) => s.closeTerminalSession);
  const pushToast = useStore((s) => s.pushToast);
  const selectWorktree = useStore((s) => s.selectWorktree);
  const openSession = useStore((s) => s.openSession);
  const setDialog = useStore((s) => s.setDialog);

  useEffect(() => {
    const close = () => setProjectMenu(null);
    window.addEventListener("click", close);
    window.addEventListener("blur", close);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("blur", close);
    };
  }, []);

  const toggleProjectMenu = (event: React.MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    if (projectMenu) {
      setProjectMenu(null);
      return;
    }
    const rect = event.currentTarget.getBoundingClientRect();
    setProjectMenu({ x: Math.max(6, rect.left), y: rect.bottom + 4 });
  };
  const summonTerminal = (worktreeId: string) => {
    const id = createTerminalSession(worktreeId);
    setProjectMenu(null);
    if (id) selectWorkspaceSession(worktreeId, id);
    else pushToast({ kind: "info", title: "All six terminal slots are in use", detail: "Close one to summon another." });
  };

  return (
    <div className="project">
      <div className="project__header">
        <button className="project__toggle" onClick={() => setOpen(!open)}>
          <span className={`project__chevron ${open ? "project__chevron--open" : ""}`}>
            <ChevronRight size={13} />
          </span>
          <span className="project__name">{tree.project.name}</span>
        </button>
        <button
          className="icon-btn project__add"
          title="New agent or worktree"
          aria-label={`New agent or worktree in ${tree.project.name}`}
          onClick={toggleProjectMenu}
        >
          <Plus size={13} />
        </button>
        {projectMenu && (
          <div
            className="session-context-menu"
            style={{ left: projectMenu.x, top: projectMenu.y }}
            onClick={(event) => event.stopPropagation()}
          >
            <button
              onClick={() => {
                setProjectMenu(null);
                setDialog({ kind: "newAgent", projectId: tree.project.id });
              }}
            >
              <Bot size={13} />
              New Agent…
            </button>
            <button
              onClick={() => {
                setProjectMenu(null);
                setDialog({ kind: "newWorktree", projectId: tree.project.id });
              }}
            >
              <GitBranch size={13} />
              New Worktree
            </button>
            <button onClick={() => summonTerminal(selectedWorktreeId && tree.worktrees.some((wt) => wt.id === selectedWorktreeId) ? selectedWorktreeId : tree.worktrees[0]?.id || "")}><TerminalSquare size={13} />Terminal</button>
          </div>
        )}
      </div>
      {open && (
        <div className="worktrees">
          {tree.worktrees.map((wt) => {
            const wtSessions = Object.values(sessions)
              .filter((s) => s.worktreeId === wt.id)
              .sort((a, b) => b.updatedAt - a.updatedAt);
            const active = wtSessions.some(
              (s) => s.status === "running" || s.status === "waiting" || s.status === "starting"
            );
            const errored = wtSessions.some((s) => s.status === "error");
            const isSelected = selectedWorktreeId === wt.id;
            const wtTerminals = Object.values(terminalSessions).filter((terminal) => terminal.worktreeId === wt.id);

            return (
              <div className="worktree-group" key={wt.id}>
                <button
                  className={`worktree ${isSelected ? "worktree--active" : ""}`}
                  onClick={() => selectWorktree(wt.id)}
                  title={wt.path}
                >
                  <span className="worktree__icon">
                    <GitBranch size={11.5} />
                  </span>
                  <span
                    className={`pulse-dot ${
                      active
                        ? ""
                        : errored
                          ? "pulse-dot--error"
                          : wtSessions.length
                            ? "pulse-dot--idle"
                            : "pulse-dot--off"
                    }`}
                  />
                  <span className="worktree__branch">{wt.branch}</span>
                  {wtSessions.length > 0 && <span className="worktree__count">{wtSessions.length}</span>}
                </button>

                {(wtSessions.length > 0 || wtTerminals.length > 0) && (
                  <div className="worktree__agents">
                    {wtSessions.map((session) => {
                      const selected = selectedWorkspaceSession[wt.id] === session.id || (!selectedWorkspaceSession[wt.id] && selectedSession[wt.id] === session.id);
                      const label = session.title || providerLabel(session.provider);
                      return (
                        <div className="agent-row" key={session.id}>
                          <button
                            className={`agent ${selected ? "agent--active" : ""}`}
                            onClick={() => { window.dispatchEvent(new Event("supervibe:chat-workspace")); void openSession(session.id); }}
                            onContextMenu={(event) => onSessionContextMenu(session.id, event)}
                            title={`${label}${session.model ? ` · ${session.model}` : ""}`}
                          >
                            <span
                              className={`pulse-dot ${
                                session.status === "running" ||
                                session.status === "waiting" ||
                                session.status === "starting"
                                  ? ""
                                  : session.status === "error"
                                    ? "pulse-dot--error"
                                    : "pulse-dot--idle"
                              }`}
                            />
                            <span className="agent__label">{label}</span>
                            {session.model && <span className="agent__model">{session.model}</span>}
                          </button>
                          <button
                            className="agent__delete"
                            aria-label={`Delete ${label} session`}
                            title="Delete session"
                            onClick={(event) => {
                              event.stopPropagation();
                              setDialog({ kind: "deleteSession", sessionId: session.id });
                            }}
                          >
                            <Trash2 size={12} />
                          </button>
                        </div>
                      );
                    })}
                    {wtTerminals.map((terminal) => (
                      <div className="agent-row worktree-terminal-row" key={terminal.id}>
                        <button className={`agent ${selectedWorkspaceSession[wt.id] === terminal.id ? "agent--active" : ""}`} onClick={() => selectWorkspaceSession(wt.id, terminal.id)}>
                          {terminal.kind === "codex" || terminal.kind === "claude" ? <ProviderLogo provider={terminal.kind} size={12} /> : <TerminalSquare size={12} />}
                          <span className="agent__label">{terminal.title}</span>
                          <span className="agent__model">{terminal.kind === "shell" ? "Shell" : terminal.kind === "codex" ? "Codex" : terminal.kind === "claude" ? "Claude Code" : "Setup"}</span>
                        </button>
                        <button className="agent__delete" title="Close terminal" onClick={() => closeTerminalSession(terminal.id)}>
                          <Trash2 size={12} />
                        </button>
                      </div>
                    ))}
                  </div>
                )}
                </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function SessionContextMenu({ sessionId, x, y, onClose }: { sessionId: string; x: number; y: number; onClose: () => void }) {
  const setDialog = useStore((s) => s.setDialog);
  const session = useStore((s) => s.sessions[sessionId]);
  if (!session) return null;
  return (
    <div className="session-context-menu" style={{ left: x, top: y }} onClick={(event) => event.stopPropagation()}>
      <button onClick={() => { onClose(); setDialog({ kind: "rename", sessionId }); }}><Pencil size={13} />Rename</button>
      <button className="session-context-menu__danger" onClick={() => { onClose(); setDialog({ kind: "deleteSession", sessionId }); }}><Trash2 size={13} />Delete</button>
    </div>
  );
}
