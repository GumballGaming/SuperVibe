import { useEffect } from "react";
import { AlertTriangle, CheckCircle2, Info, X } from "lucide-react";
import { useStore } from "../state/store";

export default function Toasts() {
  const toasts = useStore((s) => s.toasts);
  const dismissToast = useStore((s) => s.dismissToast);

  return (
    <div className="toasts">
      {toasts.map((t) => (
        <Toast key={t.id} id={t.id} kind={t.kind} title={t.title} detail={t.detail} onDismiss={dismissToast} />
      ))}
    </div>
  );
}

function Toast({
  id,
  kind,
  title,
  detail,
  onDismiss,
}: {
  id: number;
  kind: "error" | "info" | "success";
  title: string;
  detail?: string;
  onDismiss: (id: number) => void;
}) {
  useEffect(() => {
    const t = setTimeout(() => onDismiss(id), kind === "error" ? 7000 : 3500);
    return () => clearTimeout(t);
  }, [id, kind, onDismiss]);

  return (
    <div className={`toast toast--${kind}`}>
      <span style={{ marginTop: 1 }}>
        {kind === "error" ? (
          <AlertTriangle size={14} style={{ color: "var(--err)" }} />
        ) : kind === "success" ? (
          <CheckCircle2 size={14} style={{ color: "var(--ok)" }} />
        ) : (
          <Info size={14} style={{ color: "var(--text-muted)" }} />
        )}
      </span>
      <div style={{ flex: 1 }}>
        <div className="toast-title">{title}</div>
        {detail ? <div className="toast-detail">{detail}</div> : null}
      </div>
      <button className="icon-btn" style={{ width: 20, height: 20 }} onClick={() => onDismiss(id)}>
        <X size={12} />
      </button>
    </div>
  );
}
