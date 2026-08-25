import type { PermissionRequest } from "../lib/types";
import { useStore } from "../state/store";

export default function PermCard({ req }: { req: PermissionRequest }) {
  const approve = useStore((s) => s.approve);
  return (
    <div className="msg-system">
      <div style={{ fontWeight: 500 }}>
        {req.tool} · {req.action}
      </div>
      {req.detail ? <div style={{ whiteSpace: "pre-wrap" }}>{req.detail}</div> : null}
      <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
        <button className="btn btn--danger" onClick={() => void approve(req, false)}>
          Deny
        </button>
        <button className="btn btn--primary" onClick={() => void approve(req, true)}>
          Allow
        </button>
      </div>
    </div>
  );
}
