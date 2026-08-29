import { describe, expect, test } from "bun:test";
import {
  GUTTER,
  MIN_PANE,
  applyRatios,
  clampRatioToPixels,
  columnsForWidth,
  computeLayout,
  defaultTree,
  splitKey,
} from "./terminalLayout";
import type { GutterRect, LayoutNode } from "./terminalLayout";

const ids = (count: number) => Array.from({ length: count }, (_, i) => `t${i + 1}`);

/** Read a tree back as a shape string, e.g. "row(t1,row(t2,t3))". */
function shape(node: LayoutNode | null): string {
  if (!node) return "empty";
  if (node.type === "leaf") return node.terminalId;
  return `${node.dir}(${shape(node.a)},${shape(node.b)})`;
}

/** Ratio of the top-level split; NaN when the tree is a single leaf. */
function rootRatio(node: LayoutNode): number {
  return node.type === "split" ? node.ratio : Number.NaN;
}

/** Width/height of every leaf, in the order the ids were given. */
function boxes(node: LayoutNode | null, width = 1200, height = 800) {
  const { rects } = computeLayout(node, width, height);
  return [...rects.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([, rect]) => `${Math.round(rect.w)}x${Math.round(rect.h)}`);
}

/**
 * Nested seams each carve GUTTER/2 off both neighbours, so panes come out
 * within a pixel or two of perfectly even (the same rounding tmux has).
 */
function expectEven(node: LayoutNode | null, expected: string[]) {
  const actual = boxes(node);
  expect(actual.length).toBe(expected.length);
  for (let i = 0; i < actual.length; i++) {
    const [aw, ah] = actual[i].split("x").map(Number);
    const [ew, eh] = expected[i].split("x").map(Number);
    expect(Math.abs(aw - ew)).toBeLessThanOrEqual(3);
    expect(Math.abs(ah - eh)).toBeLessThanOrEqual(3);
  }
}

describe("defaultTree", () => {
  test("tiles 1-3 panes into a single row", () => {
    expect(shape(defaultTree(ids(1), 3))).toBe("t1");
    expect(shape(defaultTree(ids(2), 3))).toBe("row(t1,t2)");
    expect(shape(defaultTree(ids(3), 3))).toBe("row(t1,row(t2,t3))");
  });

  test("tiles 4-6 panes into two rows, matching the old grid", () => {
    expect(shape(defaultTree(ids(4), 3))).toBe("col(row(t1,t2),row(t3,t4))");
    expect(shape(defaultTree(ids(5), 3))).toBe("col(row(t1,row(t2,t3)),row(t4,t5))");
    expect(shape(defaultTree(ids(6), 3))).toBe("col(row(t1,row(t2,t3)),row(t4,row(t5,t6)))");
  });

  test("every pane gets an equal share by default", () => {
    expectEven(defaultTree(ids(3), 3), ["395x800", "395x800", "395x800"]);
    expectEven(defaultTree(ids(4), 3), ["596x396", "596x396", "596x396", "596x396"]);
  });

  test("reflows to the column budget instead of squeezing panes", () => {
    expect(shape(defaultTree(ids(6), 2))).toContain("col(");
    expectEven(defaultTree(ids(6), 2), Array(6).fill("596x262"));
    expectEven(defaultTree(ids(3), 1), Array(3).fill("1200x262"));
  });

  test("an empty deck has no layout", () => {
    expect(defaultTree([], 3)).toBeNull();
    expect(computeLayout(null, 800, 600).rects.size).toBe(0);
  });
});

describe("columnsForWidth", () => {
  test("drops columns on narrow surfaces but never below one", () => {
    expect(columnsForWidth(1400)).toBe(3);
    expect(columnsForWidth(1000)).toBe(3);
    expect(columnsForWidth(999)).toBe(2);
    expect(columnsForWidth(620)).toBe(2);
    expect(columnsForWidth(619)).toBe(1);
    expect(columnsForWidth(0)).toBe(3);
  });
});

describe("computeLayout", () => {
  test("one gutter per split, sized to the seam", () => {
    const { gutters } = computeLayout(defaultTree(ids(4), 3), 1200, 800);
    expect(gutters.map((g) => g.dir).sort()).toEqual(["col", "row", "row"]);
    for (const gutter of gutters) {
      expect(gutter.dir === "row" ? gutter.w : gutter.h).toBe(GUTTER);
    }
  });

  test("panes and gutters tile the surface without overlapping", () => {
    const { rects, gutters } = computeLayout(defaultTree(ids(6), 3), 1200, 800);
    const boxes = [...rects.values(), ...gutters];
    for (const box of boxes) {
      expect(box.x).toBeGreaterThanOrEqual(-0.001);
      expect(box.y).toBeGreaterThanOrEqual(-0.001);
      expect(box.x + box.w).toBeLessThanOrEqual(1200.001);
      expect(box.y + box.h).toBeLessThanOrEqual(800.001);
    }
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i];
        const b = boxes[j];
        const overlaps = a.x < b.x + b.w - 0.001 && b.x < a.x + a.w - 0.001
          && a.y < b.y + b.h - 0.001 && b.y < a.y + a.h - 0.001;
        expect(overlaps).toBe(false);
      }
    }
  });

  test("a dragged gutter moves the seam between its two neighbours only", () => {
    const tree = applyRatios(defaultTree(ids(3), 3)!, { [splitKey("row", "0")]: 0.5 });
    const { rects } = computeLayout(tree, 1200, 800);
    // Splitting the root in half gives t1 half the width and t2/t3 a quarter each.
    expect(Math.round(rects.get("t1")!.w)).toBe(596);
    expect(Math.round(rects.get("t2")!.w)).toBe(294);
    expect(Math.round(rects.get("t3")!.w)).toBe(294);
    // The untouched inner split still divides its own band evenly.
    expect(rects.get("t2")!.h).toBe(800);
  });
});

describe("gutter drag tracks", () => {
  test("a nested gutter is measured against its own band, not the surface", () => {
    const tree = applyRatios(defaultTree(ids(3), 3)!, { [splitKey("row", "0.1")]: 0.5 });
    const { gutters } = computeLayout(tree, 1200, 800);
    const inner = gutters.find((g) => g.key === splitKey("row", "0.1")) as GutterRect;
    expect(inner.axisStart).toBeGreaterThan(0);
    expect(inner.axisLength).toBeLessThan(1200);
    // Feeding the dragged ratio back through the track lands on the seam, so
    // the seam follows the pointer instead of drifting toward the centre.
    expect(inner.axisStart + inner.axisLength * 0.5).toBeCloseTo(inner.x + inner.w / 2, 1);
  });

  test("the root gutter spans the whole surface", () => {
    const { gutters } = computeLayout(defaultTree(ids(2), 3)!, 1200, 800);
    expect(gutters[0].axisStart).toBe(0);
    expect(gutters[0].axisLength).toBe(1200);
    expect(gutters[0].x + gutters[0].w / 2).toBeCloseTo(600, 1);
  });

  test("a col gutter tracks the vertical axis", () => {
    const { gutters } = computeLayout(defaultTree(ids(4), 3)!, 1200, 800);
    const seam = gutters.find((g) => g.dir === "col") as GutterRect;
    expect(seam.axisStart).toBe(0);
    expect(seam.axisLength).toBe(800);
    expect(seam.axisStart + seam.axisLength * 0.5).toBeCloseTo(seam.y + seam.h / 2, 1);
  });
});

describe("applyRatios", () => {
  test("keeps the default ratio when nothing was dragged", () => {
    expect(rootRatio(applyRatios(defaultTree(ids(2), 3)!, {}))).toBe(0.5);
  });

  test("is keyed by direction so a narrow reflow does not reuse a wide drag", () => {
    const wide = splitKey("row", "0");
    const tree = applyRatios(defaultTree(ids(2), 3)!, { [wide]: 0.75 });
    expect(tree.type).toBe("split");
    expect(rootRatio(tree)).toBe(0.75);
    // The same path under the other direction is a different split.
    expect(rootRatio(applyRatios(defaultTree(ids(2), 3)!, { [splitKey("col", "0")]: 0.75 }))).toBe(0.5);
  });

  test("clamps absurd saved ratios instead of collapsing a pane", () => {
    const tree = applyRatios(defaultTree(ids(2), 3)!, { [splitKey("row", "0")]: 5 });
    expect(rootRatio(tree)).toBeLessThanOrEqual(0.9);
    const clamped = applyRatios(defaultTree(ids(2), 3)!, { [splitKey("row", "0")]: Number.NaN });
    expect(Number.isFinite(rootRatio(clamped))).toBe(true);
  });
});

describe("clampRatioToPixels", () => {
  test("keeps both sides of a split at least MIN_PANE wide", () => {
    const ratio = clampRatioToPixels(0.01, 1000);
    expect(ratio * 1000).toBeGreaterThanOrEqual(MIN_PANE);
    expect((1 - ratio) * 1000).toBeGreaterThanOrEqual(MIN_PANE);
  });

  test("gives up and halves when the surface is too small to honour the minimum", () => {
    expect(clampRatioToPixels(0.2, MIN_PANE)).toBe(0.5);
    expect(clampRatioToPixels(Number.NaN, 1000)).toBe(0.5);
  });
});
