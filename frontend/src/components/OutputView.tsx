import { useEffect, useRef, useState } from "react";
import { Copy, TerminalSquare } from "lucide-react";
import { api } from "../lib/backend";
import { useStore } from "../state/store";

export default function OutputView({ sessionId }: { sessionId?: string }) {
  const [tail, setTail] = useState("");
  const [copied, setCopied] = useState(false);
  const tab = useStore((s) => s.tab);
  const procLines = useStore((s) => (sessionId ? s.procLines[sessionId] : undefined));
  const viewRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    let alive = true;
    const load = () => {
      if (tab !== "output" || !sessionId) return;
      api
        .getOutputTail(sessionId, 64 * 1024)
        .then((t) => alive && setTail(t))
        .catch(() => alive && setTail(""));
    };
    load();
    const t = setInterval(load, 2000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [sessionId, tab]);

  useEffect(() => {
    const el = viewRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [procLines, tail]);

  const full = [procLines?.length ? `[commands]\n${procLines.join("\n")}` : "", tail || "No raw output captured for this session yet."]
    .filter(Boolean)
    .join("\n");

  return (
    <div className="output-view">
      <div className="output-view__header">
        <TerminalSquare size={12.5} />
        <span className="output-view__title">Raw output</span>
        <span style={{ flex: 1 }} />
        <button
          className="icon-btn"
          style={{ width: 24, height: 24 }}
          title="Copy output"
          disabled={!sessionId}
          onClick={() => {
            if (!sessionId) return;
            void navigator.clipboard.writeText(full);
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
          }}
        >
          <Copy size={11.5} />
        </button>
        {copied && <span className="output-view__copied">Copied</span>}
      </div>
      {!sessionId ? (
        <div className="output-view__empty">Select a session to see its raw output.</div>
      ) : (
        <div className="output-view__body" ref={viewRef}>
          {procLines && procLines.length > 0 && (
            <>
              <div className="code-chunk code-chunk--label">commands</div>
              <pre className="code-chunk" style={{ maxHeight: "none", minHeight: 60 }}>
                {procLines.join("\n")}
              </pre>
            </>
          )}
          <div
            className="code-chunk code-chunk--label"
            style={{ marginTop: procLines?.length ? 8 : 0 }}
          >
            output tail
          </div>
          <pre className="code-chunk" style={{ maxHeight: "none", minHeight: 120 }}>
            {tail || "No raw output captured for this session yet."}
          </pre>
        </div>
      )}
    </div>
  );
}
