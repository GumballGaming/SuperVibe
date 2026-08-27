import { describe, expect, test } from "bun:test";
import { parseFilePath, parsePatch, parseStat } from "./diff";

describe("diff parse", () => {
  test("parseStat extracts change counts", () => {
    expect(parseStat("README.md | 2 ++\n 1 file changed, 2 insertions(+)")).toEqual({
      files: 1,
      insertions: 2,
      deletions: 0,
    });
    expect(parseStat("a | 4 +-\nb | 2 -+\n 2 files changed, 4 insertions(+), 2 deletions(-)")).toEqual({
      files: 2,
      insertions: 4,
      deletions: 2,
    });
    expect(parseStat("1 file changed")).toEqual({ files: 1, insertions: 0, deletions: 0 });
    expect(parseStat("")).toBeNull();
  });

  test("parseFilePath handles quoted and unquoted paths", () => {
    expect(parseFilePath("diff --git a/README.md b/README.md")).toBe("README.md");
    expect(parseFilePath('diff --git "a/my file.txt" "b/my file.txt"')).toBe("my file.txt");
  });

  test("parsePatch numbers lines from hunk headers", () => {
    const patch = [
      "diff --git a/hello.txt b/hello.txt",
      "index 346d2da..23d37cb 100644",
      "--- a/hello.txt",
      "+++ b/hello.txt",
      "@@ -1,3 +1,5 @@",
      " hello",
      "-old line",
      "+new line a",
      "+new line b",
      " last",
      "",
    ].join("\n");

    const files = parsePatch(patch);
    expect(files).toHaveLength(1);
    const line = files[0];
    expect(line.path).toBe("hello.txt");
    expect(line.additions).toBe(2);
    expect(line.deletions).toBe(1);
    expect(line.hunks).toHaveLength(1);
    const rows = line.hunks[0].lines;
    expect(rows[0]).toEqual({ kind: "ctx", oldNo: 1, newNo: 1, text: "hello" });
    expect(rows[1]).toEqual({ kind: "del", oldNo: 2, newNo: null, text: "old line" });
    expect(rows[2]).toEqual({ kind: "add", oldNo: null, newNo: 2, text: "new line a" });
    expect(rows[3]).toEqual({ kind: "add", oldNo: null, newNo: 3, text: "new line b" });
    expect(rows[4]).toEqual({ kind: "ctx", oldNo: 3, newNo: 4, text: "last" });
  });

  test("parsePatch detects added/deleted/binary files", () => {
    const added = "diff --git a/new.go b/new.go\nnew file mode 100644\n@@ -0,0 +1,1 @@\n+package main\n";
    expect(parsePatch(added)[0].status).toBe("added");

    const deleted = "diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\n@@ -1,1 +0,0 @@\n-gone\n";
    expect(parsePatch(deleted)[0].status).toBe("deleted");

    const binary = "diff --git a/icon.png b/icon.png\nBinary files a/icon.png and b/icon.png differ\n";
    const file = parsePatch(binary)[0];
    expect(file.binary).toBe(true);
    expect(file.hunks).toHaveLength(0);
  });

  test("parsePatch splits multiple files", () => {
    const patch = [
      "diff --git a/one.ts b/one.ts",
      "@@ -1 +1 @@",
      "-a",
      "+b",
      "diff --git a/two.ts b/two.ts",
      "@@ -1 +1 @@",
      "-c",
      "+d",
    ].join("\n");
    const files = parsePatch(patch);
    expect(files.map((f) => f.path)).toEqual(["one.ts", "two.ts"]);
    expect(files[0].raw.startsWith("diff --git")).toBe(true);
    expect(files[0].hunks[0].header).toBe("@@ -1 +1 @@");
  });

  test("parsePatch ignores git stderr noise", () => {
    const patch = "warning: in the working copy of 'x', LF will be replaced by CRLF\n\ndiff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b\n";
    const files = parsePatch(patch);
    expect(files).toHaveLength(1);
    expect(files[0].path).toBe("x");
  });
});
