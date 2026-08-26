import { describe, expect, test } from "bun:test";
import { formatCost, formatTokens, formatDuration, truncate, providerLabel } from "./format";

describe("format", () => {
  test("formatCost", () => {
    expect(formatCost(0)).toBe("$0.00");
    expect(formatCost(0.001)).toBe("$0.0010");
    expect(formatCost(1.5)).toBe("$1.50");
    expect(formatCost(250)).toBe("$250");
  });

  test("formatTokens", () => {
    expect(formatTokens(500)).toBe("500");
    expect(formatTokens(1500)).toBe("1.5k");
    expect(formatTokens(2_300_000)).toBe("2.3M");
  });

  test("formatDuration", () => {
    expect(formatDuration(0, 30_000)).toBe("30s");
    expect(formatDuration(0, 90_000)).toBe("1m 30s");
    expect(formatDuration(0, 3_600_000 + 120_000)).toBe("1h 2m");
  });

  test("truncate collapses whitespace", () => {
    expect(truncate("a\n\nb   c", 10)).toBe("a b c");
    expect(truncate("x".repeat(20), 5)).toBe("xxxx…");
  });

  test("providerLabel", () => {
    expect(providerLabel("claude")).toBe("Claude Code");
    expect(providerLabel("codex")).toBe("Codex");
  });
});
