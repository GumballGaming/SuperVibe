// Geometry for the terminal deck: a binary split tree (tmux/iTerm style) laid
// out into flat, absolutely positioned panes.
//
// The tree *shape* is a pure function of the visible terminal ids plus a column
// budget, so it is deterministic and needs no persistence. The only mutable
// part is the ratio at each split — that is what the user drags, and it is
// stored per worktree.
//
// Panes are emitted as a flat rect map rather than a nested DOM tree so React
// never has to move a terminal between containers: a pane that changes
// position is only re-styled, never unmounted (unmounting would tear down
// xterm and reattach the shell).

export type SplitDir = "row" | "col";

export type LayoutNode =
  | { type: "leaf"; terminalId: string }
  | { type: "split"; dir: SplitDir; ratio: number; a: LayoutNode; b: LayoutNode };

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface GutterRect extends Rect {
  /** Store key for this split: `${dir}:${path}`. */
  key: string;
  dir: SplitDir;
  /**
   * The span this split divides, in surface coordinates — NOT the whole
   * surface. A nested split only owns its parent's slice of it, so a drag has
   * to be measured against this or the seam will not follow the pointer.
   */
  axisStart: number;
  axisLength: number;
}

/** Thickness of the draggable seam between two panes (also the pane gap). */
export const GUTTER = 8;

/** A pane is never squeezed below this many pixels on its split axis. */
export const MIN_PANE = 96;

const MIN_RATIO = 0.1;
const MAX_RATIO = 0.9;

export function splitKey(dir: SplitDir, path: string): string {
  return `${dir}:${path}`;
}

export function clampRatio(ratio: number): number {
  if (!Number.isFinite(ratio)) return 0.5;
  return Math.min(MAX_RATIO, Math.max(MIN_RATIO, ratio));
}

/**
 * Keeps both sides of a split at least `minPane` pixels wide, so a pane can be
 * dragged down to a sliver but never disappears or inverts.
 */
export function clampRatioToPixels(ratio: number, total: number, minPane = MIN_PANE): number {
  if (!Number.isFinite(ratio)) return 0.5;
  if (total <= minPane * 2) return 0.5;
  const min = minPane / total;
  return Math.min(1 - min, Math.max(min, ratio));
}

/** Split ids side by side, each taking an equal share. Right-nested. */
function band(ids: string[]): LayoutNode {
  if (ids.length === 1) return { type: "leaf", terminalId: ids[0] };
  const [first, ...rest] = ids;
  return {
    type: "split",
    dir: "row",
    ratio: 1 / ids.length,
    a: { type: "leaf", terminalId: first },
    b: band(rest),
  };
}

/** Deal ids into `rows` groups, front-loading the remainder (5 into 2 → 3, 2). */
function deal(ids: string[], rows: number): string[][] {
  const out: string[][] = [];
  const base = Math.floor(ids.length / rows);
  let extra = ids.length % rows;
  let cursor = 0;
  for (let row = 0; row < rows; row++) {
    const size = base + (extra > 0 ? 1 : 0);
    if (extra > 0) extra--;
    out.push(ids.slice(cursor, cursor + size));
    cursor += size;
  }
  return out;
}

/** Stack bands top to bottom, each taking an equal share of the height. */
function stack(rows: string[][]): LayoutNode {
  if (rows.length === 1) return band(rows[0]);
  return {
    type: "split",
    dir: "col",
    ratio: 1 / rows.length,
    a: band(rows[0]),
    b: stack(rows.slice(1)),
  };
}

/**
 * Default tiling for `ids` (ordered by slot). Reproduces the old static grid:
 * 1–3 panes in a single row, 4 as 2x2, 5 as 3-over-2, 6 as 3x2.
 */
export function defaultTree(ids: string[], maxColumns: number): LayoutNode | null {
  if (!ids.length) return null;
  const columns = Math.max(1, Math.min(maxColumns, ids.length));
  if (ids.length <= columns) return band(ids);
  return stack(deal(ids, Math.ceil(ids.length / columns)));
}

/**
 * Columns the deck can afford at a given width. Container-based rather than
 * viewport-based, so dragging the right rail re-tiles the deck too.
 */
export function columnsForWidth(width: number): number {
  if (width <= 0) return 3;
  if (width < 620) return 1;
  if (width < 1000) return 2;
  return 3;
}

/** Overlay saved ratios onto a tree, addressed by `${dir}:${path}`. */
export function applyRatios(node: LayoutNode, ratios: Record<string, number>, path = "0"): LayoutNode {
  if (node.type === "leaf") return node;
  const saved = ratios[splitKey(node.dir, path)];
  return {
    // Pixel clamping happens in place(), once the real span is known.
    ...node,
    ratio: saved === undefined ? node.ratio : clampRatio(saved),
    a: applyRatios(node.a, ratios, `${path}.0`),
    b: applyRatios(node.b, ratios, `${path}.1`),
  };
}

export function computeLayout(
  node: LayoutNode | null,
  width: number,
  height: number,
): { rects: Map<string, Rect>; gutters: GutterRect[] } {
  const rects = new Map<string, Rect>();
  const gutters: GutterRect[] = [];
  if (node) place(node, 0, 0, Math.max(0, width), Math.max(0, height), "0", rects, gutters);
  return { rects, gutters };
}

function place(
  node: LayoutNode,
  x: number,
  y: number,
  w: number,
  h: number,
  path: string,
  rects: Map<string, Rect>,
  gutters: GutterRect[],
): void {
  if (node.type === "leaf") {
    rects.set(node.terminalId, { x, y, w: Math.max(0, w), h: Math.max(0, h) });
    return;
  }

  const span = node.dir === "row" ? w : h;
  const ratio = span > MIN_PANE * 2 ? clampRatioToPixels(node.ratio, span) : 0.5;
  const offset = span * ratio;
  // The seam is centred on the split line, so each side gives up half of it.
  const half = Math.min(GUTTER / 2, span / 2);
  const first = Math.max(0, offset - half);
  const second = Math.max(0, span - offset - half);

  if (node.dir === "row") {
    gutters.push({ key: splitKey(node.dir, path), dir: "row", x: x + offset - half, y, w: GUTTER, h, axisStart: x, axisLength: w });
    place(node.a, x, y, first, h, `${path}.0`, rects, gutters);
    place(node.b, x + offset + half, y, second, h, `${path}.1`, rects, gutters);
  } else {
    gutters.push({ key: splitKey(node.dir, path), dir: "col", x, y: y + offset - half, w, h: GUTTER, axisStart: y, axisLength: h });
    place(node.a, x, y, w, first, `${path}.0`, rects, gutters);
    place(node.b, x, y + offset + half, w, second, `${path}.1`, rects, gutters);
  }
}
