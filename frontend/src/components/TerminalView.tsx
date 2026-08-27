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
  const [inputValue, setInputValue] = useState("");
  const [syncing, setSyncing] = useState(true);
  const subscribedRef = useRef<(() => void) | null>(null);
  const outputRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const inputQueueRef = useRef(Promise.resolve());

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
    setInputValue("");
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
    if (running && !syncing) inputRef.current?.focus();
  }, [running, syncing]);

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

  const sendInput = (data: string) => {
    if (!data || !running || syncing) return;
    inputQueueRef.current = inputQueueRef.current
      .then(() => api.terminalInput(worktreeId, data))
      .catch((e) => {
        setOutput((o) => o + `\n[terminal] input failed: ${String(e)}\n`);
      });
  };

  const controlSequence = (event: React.KeyboardEvent<HTMLInputElement>): string | null => {
    if (event.ctrlKey && event.key.length === 1) {
      const code = event.key.toUpperCase().charCodeAt(0);
      if (code >= 64 && code <= 95) return String.fromCharCode(code - 64);
    }
    if (event.altKey && event.key.length === 1) return `\x1b${event.key}`;
    return {
      Enter: "\r",
      Backspace: "\x7f",
      Delete: "\x1b[3~",
      Tab: "\t",
      Escape: "\x1b",
      ArrowUp: "\x1b[A",
      ArrowDown: "\x1b[B",
      ArrowRight: "\x1b[C",
      ArrowLeft: "\x1b[D",
      Home: "\x1b[H",
      End: "\x1b[F",
      PageUp: "\x1b[5~",
      PageDown: "\x1b[6~",
      Insert: "\x1b[2~",
    }[event.key] ?? null;
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (!running || syncing) {
      event.preventDefault();
      return;
    }
    const sequence = controlSequence(event);
    if (sequence === null) return;
    event.preventDefault();
    setInputValue("");
    sendInput(sequence);
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
          <div className="terminal__dead">Shell exited - press restart to open a new one.</div>
        )}
        {syncing && <div className="terminal__dead">Starting shell...</div>}
        <pre className="terminal__out">{output}</pre>
      </div>
      <div
        className={`terminal__input-row${running && !syncing ? " terminal__input-row--active" : ""}`}
        onClick={() => inputRef.current?.focus()}
      >
        <span className="terminal__prompt" aria-hidden="true">&#x276F;</span>
        <input
          ref={inputRef}
          className="terminal__input"
          aria-label="Interactive terminal input"
          placeholder={running ? "Type directly into the shell..." : "Shell is not running - restart to use the terminal"}
          value={inputValue}
          disabled={!running || syncing}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          onChange={(e) => {
            sendInput(e.target.value);
            setInputValue("");
          }}
          onKeyDown={handleKeyDown}
        />
        <span className="terminal__keys">Interactive shell / Ctrl+C to interrupt</span>
      </div>
    </div>
  );
}
