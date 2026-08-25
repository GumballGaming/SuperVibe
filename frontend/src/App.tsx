import { useEffect } from "react";
import { useStore } from "./state/store";
import { api } from "./lib/backend";
import { applyTheme } from "./lib/theme";
import TitleBar from "./components/TitleBar";
import Sidebar from "./components/Sidebar";
import Workspace from "./components/Workspace";
import FleetView from "./components/FleetView";
import Dialogs from "./components/Dialogs";
import Toasts from "./components/Toasts";

export default function App() {
  const view = useStore((s) => s.view);
  const degraded = useStore((s) => s.degraded);
  const ready = useStore((s) => s.ready);
  const init = useStore((s) => s.init);

  useEffect(() => {
    void init();
  }, [init]);
  useEffect(() => {
    let cancelled = false;
    api
      .getSettings()
      .then((settings) => {
        if (!cancelled) applyTheme(settings["appearance.theme"], settings["appearance.accent"]);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);


  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const st = useStore.getState();
      if (e.key === "Escape") {
        if (st.dialog) st.setDialog(null);
        return;
      }
      const target = e.target as HTMLElement | null;
      const tag = target?.tagName;
      const inField = tag === "INPUT" || tag === "TEXTAREA";
      const ctrl = e.ctrlKey || e.metaKey;
      if (ctrl && (e.key === "n" || e.key === "N")) {
        e.preventDefault();
        const worktreeId = st.selectedWorktreeId || st.projects[0]?.worktrees[0]?.id;
        if (worktreeId) st.setDialog({ kind: "newSession", worktreeId });
        return;
      }
      if (ctrl && (e.key === "f" || e.key === "F")) {
        e.preventDefault();
        st.setView("fleet");
        return;
      }
      if (!inField && !ctrl) {
        if (e.altKey && e.key === "1") st.setTab("chat");
        else if (e.altKey && e.key === "2") st.setTab("diff");
        else if (e.altKey && e.key === "3") st.setTab("output");
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="app">
      <TitleBar />
      <div className="body">
        <Sidebar />
        <main className="main">{degraded ? <Degraded /> : ready ? view === "fleet" ? <FleetView /> : <Workspace /> : null}</main>
      </div>
      <Dialogs />
      <Toasts />
    </div>
  );
}

function Degraded() {
  return (
    <div className="degraded">
      <div style={{ textAlign: "center" }}>
        <div className="empty-state__title">Running outside the desktop shell</div>
        <div className="empty-state__detail">
          Launch SuperVibe via <code>wails dev</code> or the built executable to connect to the agent backend.
        </div>
      </div>
    </div>
  );
}
