import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { Copy, RotateCcw, TerminalSquare } from "lucide-react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "xterm";
import "xterm/css/xterm.css";
import { api, onRuntimeEvent } from "../lib/backend";
import type { TerminalEvent } from "../lib/types";
import { useStore } from "../state/store";
import type { TerminalKind } from "../state/store";
import ProviderLogo from "./ProviderLogo";

export interface TerminalHandle {
  focus: () => void;
}

interface TerminalViewProps {
  // Backend session key: `${worktreeId}::${terminalId}`.
  sessionKey: string;
  label: string;
  kind?: TerminalKind | null;
  initialCommand?: string;
  onDetectedKind?: (kind: TerminalKind) => void;
  onFocus?: () => void;
  onClose?: () => void;
}

// The launch command belongs to the backend session, not to a React mount:
// remounting must never start a second copy of codex/claude inside a shell
// that is already running it.
const launchedCommands = new Set<string>();

const TerminalView = forwardRef<TerminalHandle, TerminalViewProps>(function TerminalView(
  { sessionKey, label, kind, initialCommand, onDetectedKind, onFocus, onClose },
  ref,
) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const pushToast = useStore((s) => s.pushToast);
  const [restartToken, setRestartToken] = useState(0);
  const onDetectedKindRef = useRef(onDetectedKind);
  const detectionBufferRef = useRef("");
  const detectedKindRef = useRef<TerminalKind | null>(null);

  useEffect(() => {
    onDetectedKindRef.current = onDetectedKind;
  }, [onDetectedKind]);

  // Kept in a ref so a new callback identity never re-runs the effect below
  // (that would rebuild xterm and restart the session).
  const onFocusRef = useRef(onFocus);
  useEffect(() => {
    onFocusRef.current = onFocus;
  });

  useImperativeHandle(ref, () => ({ focus: () => termRef.current?.focus() }), []);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    let disposed = false;
    let frame = 0;

    const term = new Terminal({
      cursorBlink: true,
      fontFamily: '"JetBrains Mono Variable", "Cascadia Mono", Consolas, monospace',
      fontSize: 12.5,
      letterSpacing: 0,
      lineHeight: 1,
      scrollback: 10000,
      theme: { background: "#08080a", foreground: "#d7d9df", cursor: "#f2f4f8", selectionBackground: "#3b4558" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    termRef.current = term;

    // Each pane fits itself and pushes its own cols/rows to the backend.
    const syncSize = () => {
      if (disposed) return;
      try {
        fit.fit();
      } catch {
        return; // pane not laid out yet (hidden); ResizeObserver retries
      }
      void api.terminalResize(sessionKey, term.cols, term.rows).catch(() => undefined);
    };
    const scheduleSyncSize = () => {
      if (frame) cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        frame = 0;
        syncSize();
      });
    };

    const inputSub = term.onData((data) => {
      void api.terminalInput(sessionKey, data).catch(() => undefined);
    });

    // Exactly one subscription per terminal session, removed on cleanup.
    const off = onRuntimeEvent("terminal:event", (...args) => {
      const event = args[0] as TerminalEvent | undefined;
      if (!event || event.id !== sessionKey || event.kind !== "output") return;
      if (disposed) return;
      if (!detectedKindRef.current) {
        detectionBufferRef.current = `${detectionBufferRef.current}${event.data}`.slice(-12000);
        const output = detectionBufferRef.current
          .replace(/\x1b\][^\x07]*(?:\x07|\x1b\\)/g, "")
          .replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "");
        const detected = /openai\s+codex|codex\s+\(v\d|codex\s+cli/i.test(output)
          ? "codex"
          : /claude\s+code|anthropic\s+claude/i.test(output)
            ? "claude"
            : null;
        if (detected) {
          detectedKindRef.current = detected;
          onDetectedKindRef.current?.(detected);
        }
      }
      // Raw PTY bytes: xterm owns escape-sequence interpretation. No splitting,
      // trimming, manual newlines or ANSI rewriting here.
      term.write(event.data);
    });

    const observer = new ResizeObserver(scheduleSyncSize);
    observer.observe(host);
    const notifyFocus = () => onFocusRef.current?.();
    host.addEventListener("focusin", notifyFocus);

    const start = async () => {
      try {
        await api.startTerminal(sessionKey);
        if (disposed) return;
        syncSize();
        if (initialCommand && !launchedCommands.has(sessionKey)) {
          launchedCommands.add(sessionKey);
          window.setTimeout(() => {
            if (!disposed) void api.terminalInput(sessionKey, `${initialCommand}\r`).catch(() => undefined);
          }, 150);
        }
        term.focus();
      } catch (error) {
        if (!disposed) term.write(`\r\n[terminal] failed to start: ${String(error)}\r\n`);
      }
    };
    void start();

    return () => {
      disposed = true;
      if (frame) cancelAnimationFrame(frame);
      observer.disconnect();
      host.removeEventListener("focusin", notifyFocus);
      off();
      inputSub.dispose();
      term.dispose();
      // The backend shell is deliberately left alive: it is owned by the
      // workspace and closed explicitly (pane close, restart, app shutdown).
      termRef.current = null;
    };
  }, [sessionKey, initialCommand, restartToken]);

  const copySelection = async () => {
    const selected = termRef.current?.getSelection() || "";
    if (selected) await navigator.clipboard.writeText(selected);
    else pushToast({ kind: "info", title: "Select terminal text to copy" });
  };
  const restart = async () => {
    launchedCommands.delete(sessionKey);
    await api.closeTerminal(sessionKey).catch(() => undefined);
    setRestartToken((token) => token + 1);
  };

  return (
    <div className="terminal">
      <div className="terminal__header">
        {kind === "codex" || kind === "claude" ? <ProviderLogo provider={kind} size={12} /> : <TerminalSquare size={12} />}
        <span className="terminal__title">{label}</span>
        <span style={{ flex: 1 }} />
        <button className="terminal__btn" title="Copy selection" onClick={() => void copySelection()}>
          <Copy size={11} />
        </button>
        <button className="terminal__btn" title="Restart terminal" onClick={() => void restart()}>
          <RotateCcw size={11} />
        </button>
        {onClose && (
          <button className="terminal__btn" title="Close terminal" onClick={onClose}>
            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        )}
      </div>
      <div
        className="terminal__native-host"
        ref={hostRef}
        onMouseDown={() => termRef.current?.focus()}
      />
    </div>
  );
});

export default TerminalView;
