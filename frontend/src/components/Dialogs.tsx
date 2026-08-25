import { useEffect, useRef, useState } from "react";
import { AlertTriangle, FolderOpen, Palette, RefreshCw, X } from "lucide-react";
import { useStore } from "../state/store";
import { api, openDirectoryDialog } from "../lib/backend";
import type { Provider } from "../lib/types";
import Dropdown from "./Dropdown";
import ProviderLogo from "./ProviderLogo";
import { providerLabel } from "../lib/format";
import { cleanError } from "./DiffView";
import { ACCENT_OPTIONS, applyTheme, THEME_OPTIONS } from "../lib/theme";

export default function Dialogs() {
  const dialog = useStore((s) => s.dialog);
  const setDialog = useStore((s) => s.setDialog);

  if (!dialog) return null;
  const compactAgentDialog = dialog.kind === "newSession" || dialog.kind === "subagent";
  return (
    <div className="modal-overlay" onClick={() => setDialog(null)}>
      <div
        className={`modal${compactAgentDialog ? " modal--agent" : ""}`}
        onClick={(e) => e.stopPropagation()}
      >
        {dialog.kind === "addProject" && <AddProject />}
        {dialog.kind === "newWorktree" && <NewWorktree projectId={dialog.projectId} />}
        {dialog.kind === "newSession" && <NewSession worktreeId={dialog.worktreeId} />}
        {dialog.kind === "settings" && <SettingsDialog />}
        {dialog.kind === "deleteSession" && <DeleteSessionDialog sessionId={dialog.sessionId} />}
        {dialog.kind === "subagent" && <SubagentDialog sessionId={dialog.sessionId} />}
        {dialog.kind === "browseFiles" && <BrowseFilesDialog worktreeId={dialog.worktreeId} />}
      </div>
    </div>
  );
}

function ModalShell({
  title,
  children,
  footer,
}: {
  title: string;
  children: React.ReactNode;
  footer: React.ReactNode;
}) {
  const setDialog = useStore((s) => s.setDialog);
  return (
    <>
      <div className="modal__header">
        <span className="modal__title">{title}</span>
        <button className="icon-btn" onClick={() => setDialog(null)}>
          <X size={14} />
        </button>
      </div>
      <div className="modal__body">{children}</div>
      <div className="modal__footer">{footer}</div>
    </>
  );
}

function AddProject() {
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const refreshProjects = useStore((s) => s.refreshProjects);
  const selectWorktree = useStore((s) => s.selectWorktree);
  const pushToast = useStore((s) => s.pushToast);
  const setDialog = useStore((s) => s.setDialog);

  const choosePath = async () => {
    const selected = await openDirectoryDialog("Choose repository folder");
    if (selected) setPath(selected);
  };

  const submit = async () => {
    if (!path.trim()) return;
    setBusy(true);
    try {
      const tree = await api.addProject(path.trim());
      await refreshProjects();
      const first = tree.worktrees[0];
      if (first) selectWorktree(first.id);
      setDialog(null);
      pushToast({ kind: "success", title: `Added ${tree.project.name}` });
    } catch (e) {
      pushToast({ kind: "error", title: "Could not add project", detail: cleanError(e) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <ModalShell
      title="Add Project"
      footer={
        <>
          <button className="btn" onClick={() => setDialog(null)}>
            Cancel
          </button>
          <button className="btn btn--primary" disabled={!path.trim() || busy} onClick={() => void submit()}>
            {busy ? "Adding…" : "Add"}
          </button>
        </>
      }
    >
      <div className="field">
        <label>Repository path</label>
        <div className="field-row">
          <input
            autoFocus
            placeholder="C:\code\my-project"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && void submit()}
          />
          <button className="btn" type="button" onClick={() => void choosePath()} disabled={busy}>
            <FolderOpen size={14} />
            Browse
          </button>
        </div>
      </div>
    </ModalShell>
  );
}

function NewWorktree({ projectId }: { projectId: string }) {
  const [branch, setBranch] = useState("");
  const [base, setBase] = useState("");
  const [branches, setBranches] = useState<{ name: string; isCurrent: boolean }[]>([]);
  const refreshProjects = useStore((s) => s.refreshProjects);
  const selectWorktree = useStore((s) => s.selectWorktree);
  const pushToast = useStore((s) => s.pushToast);
  const setDialog = useStore((s) => s.setDialog);


  useEffect(() => {
    api
      .listBranches(projectId)
      .then(setBranches)
      .catch(() => setBranches([]));
  }, [projectId]);

  return (
    <ModalShell
      title="New Worktree"
      footer={
        <>
          <button className="btn" onClick={() => setDialog(null)}>
            Cancel
          </button>
          <button
            className="btn btn--primary"
            disabled={!branch.trim()}
            onClick={async () => {
              try {
                const created = await api.createWorktree(projectId, branch.trim(), base.trim());
                await refreshProjects();
                selectWorktree(created.id);
                setDialog(null);
              } catch (e) {
                pushToast({ kind: "error", title: "Worktree failed", detail: cleanError(e) });
              }
            }}
          >
            Create
          </button>
        </>
      }
    >
      <div className="field">
        <label>New branch</label>
        <input
          autoFocus
          placeholder="feature/my-feature"
          value={branch}
          onChange={(e) => setBranch(e.target.value)}
        />
      </div>
      <div className="field">
        <label>Base branch</label>
        <Dropdown
          value={base}
          options={[
            { value: "", label: "Default (current HEAD)" },
            ...branches.map((branch) => ({ value: branch.name, label: branch.name })),
          ]}
          onChange={setBase}
          ariaLabel="Base branch"
        />
      </div>
    </ModalShell>
  );
}

const PROVIDERS: Provider[] = ["claude", "codex", "opencode"];

function NewSession({ worktreeId }: { worktreeId: string }) {
  const clis = useStore((s) => s.clis);
  const ensureCaps = useStore((s) => s.ensureCaps);
  const loadModels = useStore((s) => s.loadModels);
  const capsMap = useStore((s) => s.capabilities);
  const modelsMap = useStore((s) => s.models);
  const [provider, setProvider] = useState<Provider>(() => {
    for (const p of PROVIDERS) if (clis[p]) return p;
    return "claude";
  });
  const [model, setModel] = useState("");
  const newSessionSelected = useStore((s) => s.newSessionSelected);
  const openSession = useStore((s) => s.openSession);
  const pushToast = useStore((s) => s.pushToast);
  const setDialog = useStore((s) => s.setDialog);

  useEffect(() => {
    setModel("");
    void ensureCaps(provider);
    void loadModels(provider);
  }, [provider, ensureCaps, loadModels]);

  const caps = capsMap[provider];
  const models = modelsMap[provider] || [];
  const selection = caps?.modelSelection;

  const submit = async () => {
    try {
      const sess = await api.startSession(worktreeId, provider, model.trim());
      newSessionSelected(worktreeId, sess.id);
      await useStore.getState().refreshSessionLists();
      void openSession(sess.id);
      setDialog(null);
    } catch (e) {
      pushToast({ kind: "error", title: "Could not start session", detail: cleanError(e) });
    }
  };

  return (
    <ModalShell
      title="New Agent Session"
      footer={
        <>
          <button className="btn" onClick={() => setDialog(null)}>
            Cancel
          </button>
          <button className="btn btn--primary" onClick={() => void submit()}>
            Start
          </button>
        </>
      }
    >
      <div className="field">
        <label>Agent</label>
        <div className="provider-grid">
          {PROVIDERS.map((p) => (
            <button
              key={p}
              className={`provider-card ${provider === p ? "provider-card--selected" : ""} ${
                clis[p] === false ? "provider-card--disabled" : ""
              }`}
              onClick={() => setProvider(p)}
            >
              <ProviderLogo provider={p} size={22} />
              <span className="provider-card__copy">
                <span className="provider-name">{providerLabel(p)}</span>
                <span className="provider-status">{clis[p] === false ? "not found" : "\u00A0"}</span>
              </span>
            </button>
          ))}
        </div>
      </div>
      {selection && selection !== "none" && (
        <div className="field">
          <label>{models.length > 0 ? "Model" : "Model override (optional)"}</label>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            {models.length > 0 ? (
              <Dropdown
                style={{ flex: 1 }}
                value={model}
                options={[
                  { value: "", label: "Default model" },
                  ...models.map((m) => ({ value: m.id, label: `${m.label}${m.fastMode ? " · Fast" : ""}` })),
                ]}
                onChange={setModel}
                ariaLabel="Model"
              />
            ) : (
              <input
                style={{ flex: 1 }}
                placeholder="model id (optional)"
                value={model}
                onChange={(e) => setModel(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && void submit()}
              />
            )}
            <button
              className="icon-btn"
              title="Refresh models"
              onClick={() => void loadModels(provider, true)}
            >
              <RefreshCw size={13} />
            </button>
          </div>
        </div>
      )}
    </ModalShell>
  );
}

function SettingsDialog() {
  const [values, setValues] = useState<Record<string, string>>({});
  const pushToast = useStore((s) => s.pushToast);
  const setDialog = useStore((s) => s.setDialog);
  const initialAppearance = useRef({
    theme: document.documentElement.dataset.theme || "dark",
    accent: document.documentElement.dataset.accent || "orange",
  });
  const saved = useRef(false);

  useEffect(() => {
    api
      .getSettings()
      .then((settings) =>
        setValues({
          ...settings,
          "appearance.theme": settings["appearance.theme"] || "dark",
          "appearance.accent": settings["appearance.accent"] || "orange",
        }),
      )
      .catch(() =>
        setValues({
          "appearance.theme": "dark",
          "appearance.accent": "orange",
        }),
      );
  }, []);

  useEffect(() => {
    if (!values["appearance.theme"] && !values["appearance.accent"]) return;
    applyTheme(values["appearance.theme"], values["appearance.accent"]);
  }, [values]);

  useEffect(
    () => () => {
      if (!saved.current) applyTheme(initialAppearance.current.theme, initialAppearance.current.accent);
    },
    [],
  );

  const setValue = (key: string, value: string) => {
    setValues((current) => ({ ...current, [key]: value }));
  };
  const theme = values["appearance.theme"] || "dark";
  const accent = values["appearance.accent"] || "orange";
  const selectedTheme = THEME_OPTIONS.find((option) => option.value === theme);

  const save = async () => {
    try {
      await api.setSettings(values);
      saved.current = true;
      setDialog(null);
      pushToast({ kind: "success", title: "Settings saved" });
    } catch (e) {
      pushToast({ kind: "error", title: "Save failed", detail: cleanError(e) });
    }
  };

  const pathFields: { key: string; label: string; placeholder: string }[] = [
    { key: "paths.claude", label: "Claude Code", placeholder: "Use PATH (claude)" },
    { key: "paths.codex", label: "Codex", placeholder: "Use PATH (codex)" },
    { key: "paths.opencode", label: "OpenCode", placeholder: "Use PATH (opencode)" },
  ];

  return (
    <ModalShell
      title="Settings"
      footer={
        <>
          <button className="btn" onClick={() => setDialog(null)}>
            Cancel
          </button>
          <button className="btn btn--primary" onClick={() => void save()}>
            Save changes
          </button>
        </>
      }
    >
      <div className="settings-grid">
        <section className="settings-section">
          <div className="settings-section__heading">
            <div className="settings-section__icon">
              <Palette size={14} />
            </div>
            <div>
              <div className="settings-section__title">Appearance</div>
              <div className="settings-section__description">Personalize the workspace without changing your layout.</div>
            </div>
          </div>
          <div className="settings-fields settings-fields--two">
            <div className="field">
              <label htmlFor="settings-theme">Theme</label>
              <Dropdown
                id="settings-theme"
                value={theme}
                options={THEME_OPTIONS.map((option) => ({ value: option.value, label: option.label }))}
                onChange={(value) => setValue("appearance.theme", value)}
                ariaLabel="Theme"
              />
              <div className="field-help">{selectedTheme?.description}</div>
            </div>
            <div className="field">
              <label>Accent</label>
              <div className="accent-options" role="radiogroup" aria-label="Accent color">
                {ACCENT_OPTIONS.map((option) => (
                  <button
                    className={`accent-option${accent === option.value ? " accent-option--selected" : ""}`}
                    key={option.value}
                    type="button"
                    role="radio"
                    aria-checked={accent === option.value}
                    aria-label={`${option.label} accent`}
                    onClick={() => setValue("appearance.accent", option.value)}
                  >
                    <span className="accent-option__swatch" style={{ background: option.color }} />
                    <span>{option.label}</span>
                  </button>
                ))}
              </div>
            </div>
          </div>
        </section>

        <section className="settings-section">
          <div className="settings-section__heading">
            <div className="settings-section__icon settings-section__icon--muted">CLI</div>
            <div>
              <div className="settings-section__title">Command line tools</div>
              <div className="settings-section__description">Optional overrides. Leave blank to use the executable from PATH.</div>
            </div>
          </div>
          <div className="settings-fields settings-fields--two">
            {pathFields.map((field) => (
              <div className="field" key={field.key}>
                <label htmlFor={`settings-${field.key}`}>{field.label} executable</label>
                <input
                  id={`settings-${field.key}`}
                  placeholder={field.placeholder}
                  value={values[field.key] ?? ""}
                  onChange={(e) => setValue(field.key, e.target.value)}
                />
              </div>
            ))}
          </div>
        </section>

        <section className="settings-section">
          <div className="settings-section__heading">
            <div className="settings-section__icon settings-section__icon--muted">RUN</div>
            <div>
              <div className="settings-section__title">Execution</div>
              <div className="settings-section__description">Choose how each provider can modify your workspace.</div>
            </div>
          </div>
          <div className="settings-fields settings-fields--two">
            <div className="field">
              <label htmlFor="settings-claude-mode">Claude permission mode</label>
              <Dropdown
                id="settings-claude-mode"
                value={values["claude.permissionMode"] || "acceptEdits"}
                options={[
                  { value: "default", label: "Default" },
                  { value: "acceptEdits", label: "Accept edits" },
                  { value: "plan", label: "Plan only" },
                  { value: "bypassPermissions", label: "Bypass permissions" },
                ]}
                onChange={(value) => setValue("claude.permissionMode", value)}
                ariaLabel="Claude permission mode"
              />
            </div>
            <div className="field">
              <label htmlFor="settings-codex-sandbox">Codex sandbox mode</label>
              <Dropdown
                id="settings-codex-sandbox"
                value={values["codex.sandbox"] || "workspace-write"}
                options={[
                  { value: "read-only", label: "Read only" },
                  { value: "workspace-write", label: "Workspace write" },
                  { value: "danger-full-access", label: "Full access" },
                ]}
                onChange={(value) => setValue("codex.sandbox", value)}
                ariaLabel="Codex sandbox mode"
              />
            </div>
            <label className="settings-toggle">
              <input
                type="checkbox"
                checked={(values["opencode.autoApprove"] || "true") !== "false"}
                onChange={(e) => setValue("opencode.autoApprove", String(e.target.checked))}
              />
              <span>
                <strong>Auto-approve OpenCode tools</strong>
                <small>Allow file edits and shell commands without prompts.</small>
              </span>
            </label>
          </div>
        </section>
      </div>
    </ModalShell>
  );
}

function RenameDialog({ sessionId }: { sessionId: string }) {
  const session = useStore((s) => s.sessions[sessionId]);
  const renameSession = useStore((s) => s.renameSession);
  const setDialog = useStore((s) => s.setDialog);
  const [title, setTitle] = useState(session?.title || "");
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!title.trim() || busy) return;
    setBusy(true);
    await renameSession(sessionId, title.trim());
    setBusy(false);
    setDialog(null);
  };

  return (
    <ModalShell
      title="Rename Session"
      footer={
        <>
          <button className="btn" onClick={() => setDialog(null)}>
            Cancel
          </button>
          <button className="btn btn--primary" disabled={!title.trim() || busy} onClick={() => void submit()}>
            Save
          </button>
        </>
      }
    >
      <div className="field">
        <label>Title</label>
        <input
          autoFocus
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && void submit()}
        />
      </div>
    </ModalShell>
  );
}

function DeleteSessionDialog({ sessionId }: { sessionId: string }) {
  const session = useStore((s) => s.sessions[sessionId]);
  const deleteSession = useStore((s) => s.deleteSession);
  const setDialog = useStore((s) => s.setDialog);
  const [busy, setBusy] = useState(false);
  const label = session?.title || (session ? providerLabel(session.provider) : "this session");

  const submit = async () => {
    if (busy) return;
    setBusy(true);
    await deleteSession(sessionId);
    setDialog(null);
  };

  return (
    <ModalShell
      title="Delete session"
      footer={
        <>
          <button className="btn" autoFocus disabled={busy} onClick={() => setDialog(null)}>
            Cancel
          </button>
          <button className="btn btn--danger" disabled={busy} onClick={() => void submit()}>
            {busy ? "Deleting…" : "Delete session"}
          </button>
        </>
      }
    >
      <div className="delete-dialog">
        <div className="delete-dialog__icon" aria-hidden="true">
          <AlertTriangle size={18} />
        </div>
        <div>
          <div className="delete-dialog__title">Delete “{label}”?</div>
          <p className="delete-dialog__copy">
            This permanently removes the session and its chat history. This action cannot be undone.
          </p>
        </div>
      </div>
    </ModalShell>
  );
}

function SubagentDialog({ sessionId }: { sessionId: string }) {
  const session = useStore((s) => s.sessions[sessionId]);
  const clis = useStore((s) => s.clis);
  const spawnSubagent = useStore((s) => s.spawnSubagent);
  const openSession = useStore((s) => s.openSession);
  const setDialog = useStore((s) => s.setDialog);
  const [task, setTask] = useState("");
  const [model, setModel] = useState("");
  const [busy, setBusy] = useState(false);
  const [provider, setProvider] = useState<Provider>(() => {
    for (const p of PROVIDERS) if (clis[p]) return p;
    return "claude";
  });
  const blocked = !!session?.parentId;

  const submit = async () => {
    if (!task.trim() || blocked || busy) return;
    setBusy(true);
    const created = await spawnSubagent(sessionId, task.trim(), provider, model.trim());
    setBusy(false);
    if (created) {
      void openSession(created.id);
      setDialog(null);
    }
  };

  return (
    <ModalShell
      title="Spawn Subagent"
      footer={
        <>
          <button className="btn" onClick={() => setDialog(null)}>
            Cancel
          </button>
          <button
            className="btn btn--primary"
            disabled={!task.trim() || blocked || busy}
            onClick={() => void submit()}
          >
            {busy ? "Spawning…" : "Spawn"}
          </button>
        </>
      }
    >
      {blocked && (
        <div className="msg-system">This session is already a subagent — nesting is limited to depth 1.</div>
      )}
      <div className="field">
        <label>Task</label>
        <textarea
          autoFocus
          rows={4}
          placeholder="Describe the focused task for the subagent"
          value={task}
          onChange={(e) => setTask(e.target.value)}
          style={{ resize: "vertical" }}
        />
      </div>
      <div className="field">
        <label>Agent</label>
        <div className="provider-grid">
          {PROVIDERS.map((p) => (
            <button
              key={p}
              className={`provider-card ${provider === p ? "provider-card--selected" : ""} ${
                clis[p] === false ? "provider-card--disabled" : ""
              }`}
              onClick={() => setProvider(p)}
            >
              <ProviderLogo provider={p} size={22} />
              <span className="provider-card__copy">
                <span className="provider-name">{providerLabel(p)}</span>
                <span className="provider-status">{clis[p] === false ? "not found" : "\u00A0"}</span>
              </span>
            </button>
          ))}
        </div>
      </div>
      <div className="field">
        <label>Model (optional)</label>
        <input
          placeholder="model id (optional)"
          value={model}
          onChange={(e) => setModel(e.target.value)}
        />
      </div>
    </ModalShell>
  );
}

function BrowseFilesDialog({ worktreeId }: { worktreeId: string }) {
  const setDialog = useStore((s) => s.setDialog);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<string[]>([]);
  const [searching, setSearching] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [preview, setPreview] = useState("");
  const [loadingFile, setLoadingFile] = useState(false);

  useEffect(() => {
    const q = query.trim();
    if (!q) {
      setResults([]);
      return;
    }
    let alive = true;
    const t = setTimeout(() => {
      setSearching(true);
      api
        .searchRepo(worktreeId, q, 50)
        .then((r) => {
          if (alive) {
            setResults(r);
            setSearching(false);
          }
        })
        .catch(() => {
          if (alive) {
            setResults([]);
            setSearching(false);
          }
        });
    }, 250);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [query, worktreeId]);

  const open = (path: string) => {
    setSelected(path);
    setLoadingFile(true);
    setPreview("");
    api
      .readFileText(worktreeId, path, 48000)
      .then(setPreview)
      .catch((e) => setPreview(cleanError(e)))
      .finally(() => setLoadingFile(false));
  };

  return (
    <ModalShell
      title="Browse Files"
      footer={
        <>
          <button
            className="btn"
            disabled={!selected}
            onClick={() => selected && void navigator.clipboard.writeText(selected)}
          >
            Copy path
          </button>
          <button
            className="btn"
            disabled={!preview || loadingFile}
            onClick={() => void navigator.clipboard.writeText(preview)}
          >
            Copy contents
          </button>
          <button className="btn" onClick={() => setDialog(null)}>
            Close
          </button>
        </>
      }
    >
      <div className="field">
        <label>Search repository</label>
        <input
          autoFocus
          placeholder="file name or path fragment"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      <div style={{ maxHeight: 160, overflowY: "auto", display: "flex", flexDirection: "column", gap: 2 }}>
        {results.map((r) => (
          <button
            key={r}
            onClick={() => open(r)}
            style={{
              textAlign: "left",
              fontFamily: "var(--font-mono)",
              fontSize: 11.5,
              padding: "5px 8px",
              borderRadius: 7,
              color: selected === r ? "var(--text-primary)" : "var(--text-secondary)",
              background: selected === r ? "var(--surface-hover)" : "transparent",
            }}
          >
            {r}
          </button>
        ))}
        {query.trim() && !searching && results.length === 0 && (
          <span style={{ color: "var(--text-muted)", fontSize: 12 }}>No matches.</span>
        )}
      </div>
      {selected && (
        <>
          <div className="code-chunk code-chunk--label">{selected}</div>
          <pre className="code-chunk" style={{ maxHeight: 240 }}>
            {loadingFile ? "Loading…" : preview || "(empty file)"}
          </pre>
        </>
      )}
    </ModalShell>
  );
}
