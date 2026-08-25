package contextx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func makeRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# proj demo\n\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "app.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "test@test")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init commit")
	return root
}

func makeUnbornRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-b", "main")
	return root
}

func TestDefaultLimits(t *testing.T) {
	got := DefaultLimits()
	if got.TreeEntries != 300 || got.MaxFileBytes != 24576 || got.TotalBytes != 98304 {
		t.Fatalf("defaults wrong: %+v", got)
	}
}

func TestRepoBrief(t *testing.T) {
	root := makeRepo(t)
	if err := os.WriteFile(filepath.Join(root, "src", "app.go"), []byte("package main\n\nfunc changed() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("scratch"), 0o644); err != nil {
		t.Fatal(err)
	}
	brief, err := RepoBrief(root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"proj", "branch: main", "0 staged, 1 unstaged, 1 untracked", "notes.txt", "src/app.go", "init commit", "== tree ==", "# proj demo"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief missing %q:\n%s", want, brief)
		}
	}
}

func TestRepoBriefWithoutGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "util.go"), []byte("package lib\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	brief, err := RepoBrief(root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(brief, "lib/util.go") || !strings.Contains(brief, filepath.Base(root)) {
		t.Fatalf("brief wrong:\n%s", brief)
	}
}

func TestParseMentions(t *testing.T) {
	text := "try @diff, then @tree! see @src/app.go and @https://example.com/x? dup @diff."
	clean, tokens := ParseMentions(text)
	want := []string{"@diff", "@tree", "@src/app.go", "@https://example.com/x"}
	if strings.Join(tokens, "|") != strings.Join(want, "|") {
		t.Fatalf("tokens: %#v", tokens)
	}
	if clean != "try then see and dup" {
		t.Fatalf("clean: %q", clean)
	}
	clean2, tokens2 := ParseMentions("no mentions here")
	if clean2 != "no mentions here" || len(tokens2) != 0 {
		t.Fatalf("plain text: %q %#v", clean2, tokens2)
	}
}

func TestResolveMentionDiff(t *testing.T) {
	root := makeRepo(t)
	if err := os.WriteFile(filepath.Join(root, "src", "app.go"), []byte("package main\n\nfunc edited() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	block, err := ResolveMention(context.Background(), root, "@diff", DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "diff --git") || !strings.Contains(block, "app.go") {
		t.Fatalf("diff block wrong:\n%s", block)
	}
}

func TestResolveMentionDiffWithoutHEAD(t *testing.T) {
	root := makeUnbornRepo(t)
	block, err := ResolveMention(context.Background(), root, "@diff", DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if block != "=== @diff ===\n" {
		t.Fatalf("unborn diff block wrong: %q", block)
	}
}

func TestResolveMentionGitTree(t *testing.T) {
	root := makeRepo(t)
	gitBlock, err := ResolveMention(context.Background(), root, "@git", DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"=== @git ===", "branch: main", "init commit"} {
		if !strings.Contains(gitBlock, want) {
			t.Fatalf("git block missing %q:\n%s", want, gitBlock)
		}
	}
	treeBlock, err := ResolveMention(context.Background(), root, "@tree", DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(treeBlock, "=== @tree ===") || !strings.Contains(treeBlock, "src/app.go") {
		t.Fatalf("tree block wrong:\n%s", treeBlock)
	}
}

func TestResolveMentionURL(t *testing.T) {
	root := makeRepo(t)
	called := ""
	fetcher := func(u string) (string, error) {
		called = u
		return "<html>hi</html>", nil
	}
	block, err := ResolveMention(context.Background(), root, "@https://example.com/a", DefaultLimits(), fetcher)
	if err != nil {
		t.Fatal(err)
	}
	if called != "https://example.com/a" || !strings.Contains(block, "<html>hi</html>") {
		t.Fatalf("url mention: called=%q block=\n%s", called, block)
	}
	if _, err := ResolveMention(context.Background(), root, "@https://example.com/a", DefaultLimits(), nil); err == nil {
		t.Fatal("expected error with nil fetcher")
	}
}

func TestResolveMentionPath(t *testing.T) {
	root := makeRepo(t)
	block, err := ResolveMention(context.Background(), root, "@src/app.go", DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"=== @src/app.go ===", "```go", "package main"} {
		if !strings.Contains(block, want) {
			t.Fatalf("missing %q:\n%s", want, block)
		}
	}
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveMention(context.Background(), root, "@../outside.txt", DefaultLimits(), nil); err == nil {
		t.Fatal("expected escape rejection")
	}
	if _, err := ResolveMention(context.Background(), root, "@C:/Windows/win.ini", DefaultLimits(), nil); err == nil {
		t.Fatal("expected absolute path rejection")
	}
}

func TestResolveMentionFileCap(t *testing.T) {
	root := makeRepo(t)
	lim := DefaultLimits()
	lim.MaxFileBytes = 5
	block, err := ResolveMention(context.Background(), root, "@src/app.go", lim, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "packa") || strings.Contains(block, "package main") {
		t.Fatalf("cap failed:\n%s", block)
	}
}

func TestFileSearch(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		"a_match.txt",
		filepath.Join("src", "deep", "x_match.go"),
		filepath.Join("node_modules", "match.js"),
		filepath.Join("vendor", "match.py"),
	}
	for _, p := range paths {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := FileSearch(root, "MATCH", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a_match.txt", "src/deep/x_match.go"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("matches: %#v", got)
	}
	all, err := FileSearch(root, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(all, "|") != strings.Join(want, "|") {
		t.Fatalf("all files: %#v", all)
	}
	limited, err := FileSearch(root, "match", 1)
	if err != nil || len(limited) != 1 || limited[0] != "a_match.txt" {
		t.Fatalf("limit: %#v %v", limited, err)
	}
}

func TestLimitsRespected(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		name := "f" + strconv.Itoa(i) + ".txt"
		if err := os.WriteFile(filepath.Join(root, name), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tree := treeLines(root, 5)
	lines := strings.Split(strings.TrimRight(tree, "\n"), "\n")
	if tree != "" && len(lines) != 5 {
		t.Fatalf("tree entries: %d\n%s", len(lines), tree)
	}
	lim := Limits{TreeEntries: 5, MaxFileBytes: 16, TotalBytes: 48}
	brief, err := RepoBrief(root, lim)
	if err != nil {
		t.Fatal(err)
	}
	if len(brief) > 48 {
		t.Fatalf("total cap exceeded: %d bytes", len(brief))
	}
}
