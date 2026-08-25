import { useState } from "react";
import { ChevronRight, FolderPlus, GitBranch, Layers, Plus, Settings, Trash2 } from "lucide-react";
import { useStore } from "../state/store";
import { providerLabel } from "../lib/format";
import type { ProjectTree } from "../lib/types";

export default function Sidebar() {
  const projects = useStore((s) => s.projects);
  const sessions = useStore((s) => s.sessions);
  const view = useStore((s) => s.view);
  const setView = useStore((s) => s.setView);
  const setDialog = useStore((s) => s.setDialog);

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
        <ProjectNode key={pt.project.id} tree={pt} />
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
    </aside>
  );
}

function ProjectNode({ tree }: { tree: ProjectTree }) {
  const [open, setOpen] = useState(true);
  const selectedWorktreeId = useStore((s) => s.selectedWorktreeId);
  const selectedSession = useStore((s) => s.selectedSession);
  const sessions = useStore((s) => s.sessions);
  const selectWorktree = useStore((s) => s.selectWorktree);
  const openSession = useStore((s) => s.openSession);
  const setDialog = useStore((s) => s.setDialog);

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
          title="New worktree"
          aria-label={`New worktree in ${tree.project.name}`}
          onClick={() => setDialog({ kind: "newWorktree", projectId: tree.project.id })}
        >
          <Plus size={13} />
        </button>
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

                {wtSessions.length > 0 && (
                  <div className="worktree__agents">
                    {wtSessions.map((session) => {
                      const selected = selectedSession[wt.id] === session.id;
                      const label = session.title || providerLabel(session.provider);
                      return (
                        <div className="agent-row" key={session.id}>
                          <button
                            className={`agent ${selected ? "agent--active" : ""}`}
                            onClick={() => void openSession(session.id)}
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
