export function formatCost(usd: number): string {
  if (!usd || usd <= 0) return "$0.00";
  if (usd < 0.01) return `$${usd.toFixed(4)}`;
  if (usd < 100) return `$${usd.toFixed(2)}`;
  return `$${Math.round(usd)}`;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return `${n}`;
}

export function formatDuration(fromMs: number, toMs: number): string {
  const secs = Math.max(0, Math.floor((toMs - fromMs) / 1000));
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  if (m < 60) return `${m}m ${s}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

export function relativeTime(ts: number): string {
  const diff = Date.now() - ts;
  if (diff < 30_000) return "just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

export function truncate(s: string, n: number): string {
  const clean = s.replace(/\s+/g, " ").trim();
  if (clean.length <= n) return clean;
  return clean.slice(0, n - 1) + "…";
}

export function providerLabel(p: string): string {
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

export function fmtCostKnown(cost: number, costKnown?: boolean): string {
  if (costKnown === false && (!cost || cost <= 0)) return "—";
  return formatCost(cost);
}

export function titleFromPrompt(text: string): string {
  const cleaned = text
    .replace(/(^|\s)@[^\s]+/g, " ")
    .replace(/\s+/g, " ")
    .replace(/[^\w\s-]/g, "")
    .trim();
  if (!cleaned) return "";
  return cleaned.split(" ").slice(0, 8).join(" ");
}

export function parseMentions(text: string): string[] {
  const out: string[] = [];
  const re = /(^|\s)@([^\s]+)/g;
  let m: RegExpExecArray | null = re.exec(text);
  while (m !== null) {
    out.push(m[2]);
    m = re.exec(text);
  }
  return out;
}
