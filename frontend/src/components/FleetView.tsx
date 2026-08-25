import { useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Search, Square } from "lucide-react";
import { useStore } from "../state/store";
import { api } from "../lib/backend";
import type { FleetRow, Session } from "../lib/types";
import { fmtCostKnown, formatTokens, relativeTime, truncate } from "../lib/format";

type StatusFilter = "all" | "active" | "idle" | "error";

const FILTERS: { key: StatusFilter; label: string; statuses: string[] | null }[] = [
  { key: "all", label: "All", statuses: null },
  {
    key: "active",
    label: "Active",
    statuses: ["running", "waiting", "starting"],
  },
  { key: "idle", label: "Idle", statuses: ["idle"] },
  { key: "error", label: "Errors", statuses: ["error"] },
];

export default function FleetView() {
  const sessions = useStore((s) => s.sessions);
  const sessionInfo = useStore((s) => s.sessionInfo);
  const refreshSessionLists = useStore((s) => s.refreshSessionLists);
  const openSession = useStore((s) => s.openSession);
  const pushToast = useStore((s) => s.pushToast);
  const [filter, setFilter] = useState<StatusFilter>("active");
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [serverRows, setServerRows] = useState<FleetRow[] | null>(null);
  const [hitCount, setHitCount] = useState(0);

  useEffect(() => {
    void refreshSessionLists();
    const t = setInterval(() => void refreshSessionLists(), 5000);
    return () => clearInterval(t);
  }, [refreshSessionLists]);

  useEffect(() => {
    const t = setTimeout(() => setDebouncedQuery(query), 300);
    return () => clearTimeout(t);
  }, [query]);

  useEffect(() => {
    const q = debouncedQuery.trim();
    if (q.length < 2) {
      setServerRows(null);
      return;
    }
    let alive = true;
    api
      .searchSessions(q)
      .then((res) => {
        if (alive) {
          setServerRows(res.fleet || []);
          setHitCount((res.hits || []).length);
        }
      })
      .catch(() => alive && setServerRows(null));
    return () => {
      alive = false;
    };
  }, [debouncedQuery]);

  const rows = useMemo(() => {
    const f = FILTERS.find((x) => x.key === filter)!;
    let list: Session[] =
      serverRows !== null ? serverRows : Object.values(sessions);
    if (f.statuses) list = list.filter((s) => f.statuses!.includes(s.status));
    if (serverRows === null && query.trim()) {
      const q = query.toLowerCase();
      list = list.filter((s) => {
        const info = sessionInfo[s.id];
        return (
          info?.projectName.toLowerCase().includes(q) ||
          info?.branch.toLowerCase().includes(q) ||
          s.lastMessage.toLowerCase().includes(q)
        );
      });
    }
    return list.sort((a, b) => b.updatedAt - a.updatedAt);
  }, [sessions, sessionInfo, filter, query, serverRows]);

  const parentRef = useRef<HTMLDivElement | null>(null);
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 44,
    overscan: 20,
  });

  return (
    <div className="fleet">
      <div className="fleet__toolbar">
        <div className="search-wrap">
          <Search size={13} />
          <input
            className="fleet__search"
            placeholder="Filter by project, branch, message"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
        <div className="filter-pills">
          {FILTERS.map((f) => (
            <button
              key={f.key}
              className={`pill ${filter === f.key ? "pill--active" : ""}`}
              onClick={() => setFilter(f.key)}
            >
              {f.label}
            </button>
          ))}
        </div>
        <span style={{ flex: 1 }} />
        {serverRows !== null && <span className="pill pill--active">{hitCount} hits</span>}
        <span style={{ color: "var(--text-muted)", fontSize: 11.5 }}>{rows.length} sessions</span>
      </div>

      <div className="fleet__head">
        <span>Status</span>
        <span>Provider</span>
        <span>Target</span>
        <span>Last activity</span>
        <span style={{ textAlign: "right" }}>Cost</span>
        <span style={{ textAlign: "right" }}>Actions</span>
      </div>

      <div className="fleet__list" ref={parentRef}>
        <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
          {virtualizer.getVirtualItems().map((vi) => {
            const s = rows[vi.index];
            const info = sessionInfo[s.id];
            return (
              <div
                key={s.id}
                className="fleet__row"
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: "100%",
                  height: vi.size,
                  transform: `translateY(${vi.start}px)`,
                }}
                onClick={() => void openSession(s.id)}
              >
                <span style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <span
                    className={`pulse-dot ${
                      s.status === "running" || s.status === "starting" || s.status === "waiting"
                        ? ""
                        : s.status === "error"
                          ? "pulse-dot--error"
                          : "pulse-dot--idle"
                    }`}
                    title={s.status}
                  />
                  <span style={{ fontSize: 12, color: "var(--text-secondary)" }}>{s.status}</span>
                </span>
                <span className="fleet-provider" style={{ flexDirection: "column", alignItems: "flex-start", gap: 1 }}>
                  <span>{shortProvider(s.provider)}</span>
                  {s.model ? (
                    <span className="fleet-branch" style={{ maxWidth: "100%" }}>
                      {s.model}
                    </span>
                  ) : null}
                </span>
                <span className="fleet-target">
                  <div className="fleet-project">{truncate(s.title || s.lastMessage || s.error, 110)}</div>
                  <div className="fleet-last">
                    {info ? `${info.projectName} · ` : ""}
                    <span className="fleet-branch" style={{ display: "inline" }}>
                      {info?.branch}
                    </span>
                  </div>
                </span>
                <span style={{ fontSize: 11.5, color: "var(--text-muted)" }}>
                  {relativeTime(s.updatedAt)} · {formatTokens(s.tokensOut)} out
                </span>
                <span className="fleet-cost">{fmtCostKnown(s.cost, s.costKnown)}</span>
                <span style={{ display: "flex", justifyContent: "flex-end" }}>
                  {s.status === "running" || s.status === "waiting" || s.status === "starting" ? (
                    <button
                      className="icon-btn"
                      title="Stop session"
                      onClick={(e) => {
                        e.stopPropagation();
                        api.stopSession(s.id).catch((err) =>
                          pushToast({ kind: "error", title: "Stop failed", detail: String(err) })
                        );
                      }}
                    >
                      <Square size={12} fill="currentColor" />
                    </button>
                  ) : null}
                </span>
              </div>
            );
          })}
        </div>
        {rows.length === 0 && (
          <div className="empty-state" style={{ padding: 60 }}>
            <div className="empty-state__title">No sessions</div>
            <div className="empty-state__detail">
              Start an agent from any worktree and it will appear here.
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function shortProvider(p: string): string {
  switch (p) {
    case "claude":
      return "Claude Code";
    case "codex":
      return "Codex";
    case "opencode":
      return "opencode";
    default:
      return p;
  }
}
