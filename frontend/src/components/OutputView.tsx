import { useEffect, useRef, useState } from "react";
import { api } from "../lib/backend";
import { useStore } from "../state/store";

export default function OutputView({ sessionId }: { sessionId: string }) {
  const [tail, setTail] = useState("");
  const tab = useStore((s) => s.tab);
  const procLines = useStore((s) => s.procLines[sessionId]);
  const viewRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    let alive = true;
    const load = () => {
      if (tab !== "output") return;
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

  return (
    <div className="output-view" ref={viewRef}>
      {procLines && procLines.length > 0 && (
        <pre className="code-chunk" style={{ maxHeight: "none", minHeight: 60 }}>
          {procLines.join("\n")}
        </pre>
      )}
      <div className="code-chunk code-chunk--label" style={{ marginTop: procLines?.length ? 8 : 0 }}>
        commands
      </div>
      <pre className="code-chunk" style={{ maxHeight: "none", minHeight: 120 }}>
        {tail || "No raw output captured for this session yet."}
      </pre>
    </div>
  );
}
