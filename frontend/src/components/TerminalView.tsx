import { useCallback, useEffect, useRef, useState } from "react";
import { Copy, RotateCcw, TerminalSquare } from "lucide-react";
import { api, onRuntimeEvent } from "../lib/backend";
import type { TerminalEvent } from "../lib/types";
import { useStore } from "../state/store";

export default function TerminalView({ worktreeId }: { worktreeId: string }) {
  const pushToast = useStore((s) => s.pushToast);
  const [running, setRunning] = useState(false);
  const [cwd, setCwd] = useState<string | null>(null);
  const [output, setOutput] = useState("");
  const [cmd, setCmd] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  const [histIdx, setHistIdx] = useState(-1);
  const [syncing, setSyncing] = useState(true);
  const subscribedRef = useRef<(() => void) | null>(null);
  const outputRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const bodyRef = useRef<HTMLDivElement | null>(null);

  const attach = useCallback(
    (sync: boolean) => {
      let synced = sync;
      subscribedRef.current?.();
      const off = onRuntimeEvent("terminal:event", (...args) => {
        const ev = args[0] as TerminalEvent | undefined;
        if (!ev || ev.id !== worktreeId) return;
        if (ev.kind === "started") {
          setCwd(ev.data);
          setRunning(true);
          setSyncing(false);
          return;
        }
        if (ev.kind === "exit") {
          setRunning(false);
          return;
        }
        if (synced && ev.kind === "output") {
          setOutput((o) => o + ev.data);
        }
      });
      subscribedRef.current = off;
      return off;
    },
    [worktreeId],
  );

  const start = useCallback(async () => {
    setSyncing(true);
    const off = attach(false);
    try {
      await api.startTerminal(worktreeId);
      await api.getTerminalOutput(worktreeId).then((t) => {
        off();
        attach(true);
        setOutput(t);
        setSyncing(false);
        setRunning(true);
      });
    } catch (e) {
      off();
      setOutput((o) => o + `\n[terminal] failed to start: ${String(e)}\n`);
      setSyncing(false);
    }
  }, [worktreeId, attach]);

  useEffect(() => {
    setOutput("");
    setCwd(null);
    setRunning(false);
    void start();
    return () => {
      subscribedRef.current?.();
    };
  }, [start, worktreeId]);

  useEffect(() => {
    const el = outputRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [output, running, syncing]);

  useEffect(() => {
    const el = bodyRef.current;
    if (!el || !worktreeId) return;
    const canvas = document.createElement("canvas");
    const ctx = canvas.getContext("2d");
    let throttle = 0;
    const measure = () => {
      const now = Date.now();
      if (now - throttle < 75) return;
      throttle = now;
      let charW = 7.4;
      if (ctx) {
        ctx.font = "12.3px 'JetBrains Mono', monospace";
        const m = ctx.measureText("M").width;
        if (m > 0) charW = m;
      }
      const cols = Math.floor((el.clientWidth - 24) / charW);
      const rows = Math.floor((el.clientHeight - 20) / 18.5);
      void api.terminalResize(worktreeId, Math.max(20, cols), Math.max(10, rows)).catch(
        () => undefined,
      );
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [worktreeId]);

  const run = async () => {
    const line = cmd.trim();
    if (!line || !running || syncing) return;
    setCmd("");
    setHistory((h) => [line, ...h]);
    setHistIdx(-1);
    try {
      await api.terminalInput(worktreeId, line + "\n");
    } catch (e) {
      setOutput((o) => o + `\n[terminal] input failed: ${String(e)}\n`);
    }
    inputRef.current?.focus();
  };

  const restart = async () => {
    try {
      await api.closeTerminal(worktreeId);
    } catch {
      /* nothing running */
    }
    setOutput("");
    await start();
    inputRef.current?.focus();
  };

  const copyAll = () => {
    void navigator.clipboard.writeText(output);
    pushToast({ kind: "success", title: "Output copied" });
  };

  return (
    <div className="terminal">
      <div className="terminal__header">
        <TerminalSquare size={12.5} />
        <span className="terminal__title">Terminal</span>
        {running && (
          <span className="terminal__live" title="Shell is running">
            live
          </span>
        )}
        {cwd && (
          <span className="terminal__dir" title={cwd}>
            {cwd}
          </span>
        )}
        <span style={{ flex: 1 }} />
        <button className="icon-btn" style={{ width: 24, height: 24 }} title="Copy output" onClick={copyAll}>
          <Copy size={11.5} />
        </button>
        <button
          className="icon-btn"
          style={{ width: 24, height: 24 }}
          title="Restart shell"
          disabled={syncing}
          onClick={() => void restart()}
        >
          <RotateCcw size={11.5} className={syncing ? "spin" : undefined} />
        </button>
      </div>
      <div className="terminal__body" ref={(el) => {
        bodyRef.current = el;
        outputRef.current = el;
      }}>
        {!running && output === "" && (
          <div className="terminal__dead">Shell exited — press restart to open a new one.</div>
        )}
        {syncing && <div className="terminal__dead">Starting shell…</div>}
        <pre className="terminal__out">{output}</pre>
      </div>
      <div className="terminal__input-row">
        <span className="terminal__prompt">❯</span>
        <input
          ref={inputRef}
          className="terminal__input"
          placeholder={running ? "Run a command…" : "Shell is not running — restart to use the terminal"}
          value={cmd}
          disabled={!running}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          onChange={(e) => setCmd(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              void run();
            } else if (e.key === "ArrowUp" && history.length > 0) {
              e.preventDefault();
              const next = Math.min(histIdx + 1, history.length - 1);
              setHistIdx(next);
              setCmd(history[next] ?? "");
            } else if (e.key === "ArrowDown") {
              e.preventDefault();
              const next = histIdx - 1;
              setHistIdx(next);
              setCmd(next >= 0 ? history[next] : "");
            }
          }}
        />
        <span className="terminal__keys">Enter to run</span>
      </div>
    </div>
  );
}