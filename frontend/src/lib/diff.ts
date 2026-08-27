export type DiffLineKind = "add" | "del" | "ctx" | "meta";

export interface DiffLine {
  kind: DiffLineKind;
  oldNo: number | null;
  newNo: number | null;
  text: string;
}

export interface DiffHunk {
  header: string;
  lines: DiffLine[];
}

export interface DiffFile {
  path: string;
  additions: number;
  deletions: number;
  binary: boolean;
  status: "added" | "deleted" | "modified";
  hunks: DiffHunk[];
  raw: string;
}

export interface StatSummary {
  files: number;
  insertions: number;
  deletions: number;
}

export const TRUNCATED_MARKER = "… truncated";

export function parseStat(stat: string): StatSummary | null {
  const m = /(\d+)\s+files?\s+changed(?:,\s*(\d+)\s+insertions?\(\+\))?(?:,\s*(\d+)\s+deletions?\(-\))?/.exec(
    stat,
  );
  if (!m) return null;
  return {
    files: Number(m[1]),
    insertions: m[2] ? Number(m[2]) : 0,
    deletions: m[3] ? Number(m[3]) : 0,
  };
}

export function parsePatch(patch: string): DiffFile[] {
  if (!patch?.trim()) return [];
  const files: DiffFile[] = [];
  const lines = patch.split("\n");
  let current: DiffFile | null = null;
  let hunk: DiffHunk | null = null;
  let oldNo = 0;
  let newNo = 0;

  for (const line of lines) {
    if (line.startsWith("diff --git ")) {
      current = {
        path: parseFilePath(line),
        additions: 0,
        deletions: 0,
        binary: false,
        status: "modified",
        hunks: [],
        raw: line,
      };
      files.push(current);
      hunk = null;
      continue;
    }
    if (!current) continue;
    current.raw += "\n" + line;

    if (line.startsWith("Binary files ")) {
      current.binary = true;
      continue;
    }
    if (line.startsWith("new file mode")) {
      current.status = "added";
      continue;
    }
    if (line.startsWith("deleted file mode")) {
      current.status = "deleted";
      continue;
    }
    if (
      line.includes("No newline at end of file") ||
      /^(---|\+\+\+) /.test(line) ||
      /^(index|old file|copy from|copy to|rename from|rename to|similarity|dissimilarity|new mode|old mode|deleted mode) /.test(line)
    ) {
      continue;
    }
    if (line.startsWith("@@")) {
      const m = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/.exec(line);
      if (m) {
        hunk = { header: line, lines: [] };
        current.hunks.push(hunk);
        oldNo = Number(m[1]);
        newNo = Number(m[3]);
      }
      continue;
    }
    if (!hunk) continue;

    const prefix = line.charAt(0);
    if (prefix === "+") {
      current.additions += 1;
      hunk.lines.push({ kind: "add", oldNo: null, newNo: newNo++, text: line.slice(1) });
    } else if (prefix === "-") {
      current.deletions += 1;
      hunk.lines.push({ kind: "del", oldNo: oldNo++, newNo: null, text: line.slice(1) });
    } else if (prefix === " ") {
      hunk.lines.push({ kind: "ctx", oldNo: oldNo++, newNo: newNo++, text: line.slice(1) });
    } else if (line === "") {
      continue;
    } else {
      hunk.lines.push({ kind: "meta", oldNo: null, newNo: null, text: line });
    }
  }
  return files;
}

export function parseFilePath(header: string): string {
  const body = header.slice("diff --git ".length);
  const unquote = (p: string) =>
    p.startsWith('"') ? p.replace(/^"|"$/g, "").replace(/\\(.)/g, "$1") : p;
  const quoted = /^"a\/(.+)" "b\/(.+)"$/.exec(body);
  if (quoted) return unquote(quoted[2]);
  const idx = body.indexOf(" b/");
  if (idx >= 0) return unquote(body.slice(idx + 3));
  return unquote(body.slice(2));
}
