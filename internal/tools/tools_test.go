package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return root
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func runTool(t *testing.T, r *Registry, tool string, args any) (string, []Event, error) {
	t.Helper()
	var events []Event
	sum, err := r.Execute(t.Context(), "c1", tool, mustJSON(t, args), func(e Event) {
		events = append(events, e)
	})
	return sum, events, err
}

func TestLifecycleEvents(t *testing.T) {
	r := New(Deps{WorktreePath: initRepo(t)})
	sum, events, err := runTool(t, r, "read_file", map[string]any{"path": "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 ||
		events[0].Type != EventStarted ||
		events[1].Type != EventOutput ||
		events[2].Type != EventCompleted {
		t.Fatalf("bad lifecycle: %+v", events)
	}
	if !strings.Contains(sum, "bytes") {
		t.Fatalf("summary: %q", sum)
	}
}

func TestPatchFile(t *testing.T) {
	root := initRepo(t)
	r := New(Deps{WorktreePath: root})
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha beta gamma"), 0o644)

	sum, _, err := runTool(t, r, "patch_file", map[string]any{"path": "a.txt", "old": "beta", "new": "delta"})
	if err != nil || sum != "1 replacement(s)" {
		t.Fatalf("patch single: %q %v", sum, err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if !strings.Contains(string(data), "delta") {
		t.Fatalf("content: %q", data)
	}

	os.WriteFile(filepath.Join(root, "b.txt"), []byte("x x x"), 0o644)
	sum, _, err = runTool(t, r, "patch_file", map[string]any{"path": "b.txt", "old": "x", "new": "y", "all": true})
	if err != nil || sum != "3 replacement(s)" {
		t.Fatalf("patch all: %q %v", sum, err)
	}

	if _, _, err := runTool(t, r, "patch_file", map[string]any{"path": "b.txt", "old": "nope", "new": "z"}); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestPathEscapeRejected(t *testing.T) {
	root := initRepo(t)
	r := New(Deps{WorktreePath: root})
	if _, _, err := runTool(t, r, "read_file", map[string]any{"path": "../../etc/passwd"}); err == nil {
		t.Fatal("escape should fail")
	}
	if _, _, err := runTool(t, r, "read_file", map[string]any{"path": `C:\Windows\win.ini`}); err == nil {
		t.Fatal("absolute outside should fail")
	}
}

func TestGrepAndSearchSkipDirs(t *testing.T) {
	root := initRepo(t)
	os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "node_modules", "pkg", "skip.js"), []byte("NEEDLE here\n"), 0o644)
	os.WriteFile(filepath.Join(root, "src.go"), []byte("package main // NEEDLE marker\n"), 0o644)

	r := New(Deps{WorktreePath: root})
	sum, events, err := runTool(t, r, "grep_files", map[string]any{"pattern": "NEEDLE"})
	if err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, e := range events {
		if e.Type == EventOutput {
			out = e.Text
		}
	}
	if strings.Contains(out, "node_modules") || !strings.Contains(out, "src.go") {
		t.Fatalf("grep wrong: %q (summary %q)", out, sum)
	}

	found := false
	_, events, err = runTool(t, r, "search_files", map[string]any{"query": "skip.js"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Type == EventOutput && strings.Contains(e.Text, "skip.js") {
			found = true
		}
	}
	if found {
		t.Fatal("search_files should skip node_modules")
	}
}

func TestGitTools(t *testing.T) {
	root := initRepo(t)
	r := New(Deps{WorktreePath: root})
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// changed\n"), 0o644)

	if _, _, err := runTool(t, r, "git_status", map[string]any{}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if _, _, err := runTool(t, r, "git_diff", map[string]any{}); err != nil {
		t.Fatalf("diff: %v", err)
	}
	sum, _, err := runTool(t, r, "git_commit", map[string]any{"message": "tool commit"})
	if err != nil || sum != "committed" {
		t.Fatalf("commit: %q %v", sum, err)
	}
	sum, events, err := runTool(t, r, "git_log", map[string]any{})
	logOut := ""
	for _, e := range events {
		if e.Type == EventOutput {
			logOut = e.Text
		}
	}
	if err != nil || !strings.Contains(logOut, "init") {
		t.Fatalf("log: %q / %q %v", sum, logOut, err)
	}
}

func TestRunCommandTailAndExit(t *testing.T) {
	root := initRepo(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "boom.cmd")
	os.WriteFile(script, []byte("@echo off\r\necho line-one\r\nexit /b 5\r\n"), 0o644)

	r := New(Deps{WorktreePath: root})
	sum, events, err := runTool(t, r, "run_command", map[string]any{
		"command":    script,
		"timeoutSec": 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sum, "exit 5") || !strings.Contains(sum, "line-one") {
		t.Fatalf("summary: %q", sum)
	}
	sawStream := false
	for _, e := range events {
		if e.Type == EventOutput && strings.Contains(e.Text, "stdout") {
			sawStream = true
		}
	}
	if !sawStream {
		t.Fatal("missing streamed output event")
	}
}

func TestPermDeny(t *testing.T) {
	root := initRepo(t)
	r := New(Deps{
		WorktreePath: root,
		Perm:         func(tool, action string) Decision { return Deny },
	})
	if _, _, err := runTool(t, r, "write_file", map[string]any{"path": "x.txt", "content": "hi"}); err == nil {
		t.Fatal("deny should block write_file")
	}
	if _, _, err := runTool(t, r, "run_command", map[string]any{"command": "echo hi"}); err == nil {
		t.Fatal("deny should block command")
	}
}

func TestWebUnavailable(t *testing.T) {
	r := New(Deps{WorktreePath: initRepo(t)})
	if _, _, err := runTool(t, r, "web_search", map[string]any{"query": "x"}); err == nil {
		t.Fatal("nil browse should error")
	}
}

func readOutput(events []Event) string {
	for _, e := range events {
		if e.Type == EventOutput {
			return e.Text
		}
	}
	return ""
}

func eventsOf(events []Event) []Event { return events }
