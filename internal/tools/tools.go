package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"supervibe/internal/procx"
)

type EventType string

const (
	EventStarted   EventType = "started"
	EventOutput    EventType = "output"
	EventCompleted EventType = "completed"
	EventFailed    EventType = "failed"
)

type Event struct {
	CallID string            `json:"callId"`
	Tool   string            `json:"tool"`
	Type   EventType         `json:"type"`
	Text   string            `json:"text,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
	Ts     int64             `json:"ts"`
}

type Decision string

const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
)

type PermFunc func(tool, action string) Decision

type BResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type Browser interface {
	Search(ctx context.Context, query string, limit int) ([]BResult, error)
	Fetch(ctx context.Context, url string) (title string, text string, err error)
}

type Deps struct {
	WorktreePath   string
	Perm           PermFunc
	Browse         func() Browser
	CommandTimeout time.Duration
}

type Registry struct {
	deps Deps
}

func New(d Deps) *Registry {
	if d.CommandTimeout <= 0 {
		d.CommandTimeout = 120 * time.Second
	}
	return &Registry{deps: d}
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "vendor": true,
	"target": true, "build": true, "tmp": true, ".next": true, "__pycache__": true,
}

const (
	maxToolOutput = 32 * 1024
	maxDiffOutput = 64 * 1024
)

func (r *Registry) resolve(p string) (string, error) {
	root := filepath.Clean(r.deps.WorktreePath)
	if p == "" {
		return root, nil
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	ap := filepath.Clean(p)
	rel, err := filepath.Rel(root, ap)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes worktree: %s", p)
	}
	return ap, nil
}

func (r *Registry) allowed(tool, action string) error {
	if r.deps.Perm == nil {
		return nil
	}
	if r.deps.Perm(tool, action) == Deny {
		return fmt.Errorf("permission denied: %s", action)
	}
	return nil
}

func (r *Registry) Execute(ctx context.Context, callID, tool string, argsJSON []byte, emit func(Event)) (string, error) {
	em := func(t EventType, text string, fields map[string]string) {
		e := Event{CallID: callID, Tool: tool, Type: t, Text: text, Fields: fields, Ts: time.Now().UnixMilli()}
		if e.Fields == nil {
			e.Fields = map[string]string{}
		}
		emit(e)
	}
	em(EventStarted, "", nil)
	sum, err := r.dispatch(ctx, tool, argsJSON, em)
	if err != nil {
		em(EventFailed, err.Error(), nil)
		return "", err
	}
	em(EventCompleted, sum, nil)
	return sum, nil
}

func (r *Registry) dispatch(ctx context.Context, tool string, argsJSON []byte, em func(EventType, string, map[string]string)) (string, error) {
	var args map[string]any
	_ = json.Unmarshal(argsJSON, &args)
	getStr := func(key string) string {
		if v, ok := args[key].(string); ok {
			return v
		}
		return ""
	}
	getInt := func(key string, def int) int {
		if v, ok := args[key].(float64); ok && v > 0 {
			return int(v)
		}
		return def
	}
	switch tool {
	case "read_file":
		p, err := r.resolve(getStr("path"))
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		content := string(data)
		if len(content) > maxToolOutput {
			content = content[:maxToolOutput] + "\n… truncated"
		}
		em(EventOutput, content, nil)
		return fmt.Sprintf("%d bytes", len(data)), nil

	case "write_file":
		if err := r.allowed(tool, "file_write"); err != nil {
			return "", err
		}
		p, err := r.resolve(getStr("path"))
		if err != nil {
			return "", err
		}
		overwrite, _ := args["overwrite"].(bool)
		content, _ := args["content"].(string)
		if _, statErr := os.Stat(p); statErr == nil && !overwrite {
			return "", fmt.Errorf("exists; pass overwrite:true to replace")
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d bytes written", len(content)), nil

	case "patch_file":
		if err := r.allowed(tool, "file_write"); err != nil {
			return "", err
		}
		p, err := r.resolve(getStr("path"))
		if err != nil {
			return "", err
		}
		oldS, _ := args["old"].(string)
		newS, _ := args["new"].(string)
		all, _ := args["all"].(bool)
		data, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		src := string(data)
		n := strings.Count(src, oldS)
		if n == 0 {
			return "", fmt.Errorf("pattern not found in %s", getStr("path"))
		}
		if all {
			src = strings.ReplaceAll(src, oldS, newS)
		} else {
			src = strings.Replace(src, oldS, newS, 1)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			return "", err
		}
		replaced := 1
		if all {
			replaced = n
		}
		return fmt.Sprintf("%d replacement(s)", replaced), nil

	case "list_dir":
		p, err := r.resolve(getStr("path"))
		if err != nil {
			return "", err
		}
		depth := getInt("depth", 2)
		limit := getInt("limit", 400)
		lines, count, err := listTree(p, depth, limit)
		if err != nil {
			return "", err
		}
		em(EventOutput, lines, nil)
		return fmt.Sprintf("%d entries", count), nil

	case "grep_files":
		pattern := getStr("pattern")
		if pattern == "" {
			return "", fmt.Errorf("pattern required")
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("bad pattern: %w", err)
		}
		glob := getStr("glob")
		limit := getInt("limit", 200)
		var hits []string
		err = walkFiles(r.deps.WorktreePath, func(rel, abs string) bool {
			if len(hits) >= limit {
				return false
			}
			if glob != "" {
				ok, _ := filepath.Match(glob, filepath.Base(rel))
				if !ok {
					return true
				}
			}
			data, readErr := os.ReadFile(abs)
			if readErr != nil || len(data) > 2*1024*1024 {
				return true
			}
			for i, line := range strings.Split(string(data), "\n") {
				if re.MatchString(line) {
					hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
					if len(hits) >= limit {
						break
					}
				}
			}
			return true
		})
		if err != nil {
			return "", err
		}
		out := strings.Join(hits, "\n")
		if len(out) > maxToolOutput {
			out = out[:maxToolOutput]
		}
		em(EventOutput, out, nil)
		return fmt.Sprintf("%d match(es)", len(hits)), nil

	case "search_files":
		query := strings.ToLower(getStr("query"))
		limit := getInt("limit", 100)
		var found []string
		_ = walkFiles(r.deps.WorktreePath, func(rel, abs string) bool {
			if len(found) >= limit {
				return false
			}
			if query == "" || strings.Contains(strings.ToLower(rel), query) {
				found = append(found, rel)
			}
			return true
		})
		em(EventOutput, strings.Join(found, "\n"), nil)
		return fmt.Sprintf("%d file(s)", len(found)), nil

	case "git_status":
		out, err := gitOut(ctx, r.deps.WorktreePath, "status", "--porcelain=v2", "--branch")
		if err != nil {
			return "", err
		}
		em(EventOutput, out, nil)
		return "status retrieved", nil

	case "git_diff":
		out, err := gitOut(ctx, r.deps.WorktreePath, "diff", "HEAD")
		if err != nil {
			return "", err
		}
		if len(out) > maxDiffOutput {
			out = out[:maxDiffOutput] + "\n… truncated"
		}
		em(EventOutput, out, nil)
		return "diff retrieved", nil

	case "git_log":
		out, err := gitOut(ctx, r.deps.WorktreePath, "log", "--oneline", "-n", "20")
		if err != nil {
			return "", err
		}
		em(EventOutput, out, nil)
		return "log retrieved", nil

	case "git_stage":
		if err := r.allowed(tool, "git_stage"); err != nil {
			return "", err
		}
		paths, _ := args["paths"].([]any)
		gitArgs := []string{"add", "--"}
		for _, p := range paths {
			if s, ok := p.(string); ok {
				gitArgs = append(gitArgs, s)
			}
		}
		if len(gitArgs) == 2 {
			gitArgs = []string{"add", "-A"}
		}
		out, err := gitOut(ctx, r.deps.WorktreePath, gitArgs...)
		return summarizeGit(out, err)

	case "git_unstage":
		if err := r.allowed(tool, "git_unstage"); err != nil {
			return "", err
		}
		paths, _ := args["paths"].([]any)
		gitArgs := []string{"restore", "--staged", "--"}
		for _, p := range paths {
			if s, ok := p.(string); ok {
				gitArgs = append(gitArgs, s)
			}
		}
		out, err := gitOut(ctx, r.deps.WorktreePath, gitArgs...)
		return summarizeGit(out, err)

	case "git_commit":
		if err := r.allowed(tool, "git_commit"); err != nil {
			return "", err
		}
		msg := getStr("message")
		if msg == "" {
			return "", fmt.Errorf("message required")
		}
		if _, err := gitOut(ctx, r.deps.WorktreePath, "add", "-A"); err != nil {
			return "", err
		}
		out, err := gitOut(ctx, r.deps.WorktreePath, "commit", "-m", msg)
		if err != nil {
			if strings.Contains(out, "nothing to commit") {
				return "nothing to commit", nil
			}
			return "", fmt.Errorf("%s: %w", strings.TrimSpace(out), err)
		}
		return "committed", nil

	case "run_command":
		if err := r.allowed(tool, "command"); err != nil {
			return "", err
		}
		command := getStr("command")
		if command == "" {
			return "", fmt.Errorf("command required")
		}
		timeout := r.deps.CommandTimeout
		if s := getInt("timeoutSec", 0); s > 0 {
			timeout = time.Duration(s) * time.Second
		}
		full := command
		if runtime.GOOS == "windows" {
			full = command + "\r\nexit $LASTEXITCODE"
		}
		p, err := procx.Shell(ctx, r.deps.WorktreePath, full, procx.Options{Timeout: timeout})
		if err != nil {
			return "", err
		}
		var tail strings.Builder
		for line := range p.Out() {
			formatted := line.Stream + ": " + line.Text
			em(EventOutput, formatted, nil)
			tail.WriteString(line.Text + "\n")
			if tail.Len() > 8192 {
				tail.Reset()
				tail.WriteString(line.Text + "\n")
			}
		}
		code, waitErr := p.Wait()
		fields := map[string]string{"exit": fmt.Sprintf("%d", code)}
		em(EventOutput, "", fields)
		if ctx.Err() != nil {
			return "", fmt.Errorf("command timed out after %s", timeout)
		}
		_ = waitErr
		return fmt.Sprintf("exit %d\n%s", code, strings.TrimRight(tail.String(), "\n")), nil

	case "web_search":
		if err := r.allowed(tool, "network"); err != nil {
			return "", err
		}
		b := r.browse()
		if b == nil {
			return "", fmt.Errorf("browsing unavailable")
		}
		results, err := b.Search(ctx, getStr("query"), getInt("limit", 8))
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		for i, res := range results {
			fmt.Fprintf(&sb, "[%d] %s\n%s\n%s\n\n", i+1, res.Title, res.URL, res.Snippet)
		}
		em(EventOutput, sb.String(), nil)
		return fmt.Sprintf("%d result(s)", len(results)), nil

	case "web_fetch":
		if err := r.allowed(tool, "network"); err != nil {
			return "", err
		}
		b := r.browse()
		if b == nil {
			return "", fmt.Errorf("browsing unavailable")
		}
		title, text, err := b.Fetch(ctx, getStr("url"))
		if err != nil {
			return "", err
		}
		if len(text) > 24576 {
			text = text[:24576]
		}
		em(EventOutput, text, nil)
		return title, nil

	case "project_meta":
		meta := map[string]any{
			"worktree": r.deps.WorktreePath,
		}
		if branch, err := gitOut(ctx, r.deps.WorktreePath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			meta["branch"] = branch
		}
		if head, err := gitOut(ctx, r.deps.WorktreePath, "rev-parse", "--short", "HEAD"); err == nil {
			meta["head"] = head
		}
		if entries, err := os.ReadDir(r.deps.WorktreePath); err == nil {
			meta["topLevelEntries"] = len(entries)
		}
		b, _ := json.MarshalIndent(meta, "", "  ")
		em(EventOutput, string(b), nil)
		return "metadata", nil

	default:
		return "", fmt.Errorf("unknown tool %q", tool)
	}
}

func (r *Registry) browse() Browser {
	if r.deps.Browse == nil {
		return nil
	}
	return r.deps.Browse()
}

func summarizeGit(out string, err error) (string, error) {
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(out), err)
	}
	if strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	return "ok", nil
}

func listTree(root string, depth, limit int) (string, int, error) {
	var lines []string
	count := 0
	var build func(dir, rel string, level int) error
	build = func(dir, rel string, level int) error {
		if count >= limit || level > depth {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if count >= limit {
				return nil
			}
			name := e.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") && name != "." {
				continue
			}
			childRel := name
			if rel != "" {
				childRel = rel + "/" + name
			}
			indent := strings.Repeat("  ", level-1)
			if e.IsDir() {
				lines = append(lines, indent+childRel+"/")
				count++
				if err := build(filepath.Join(dir, name), childRel, level+1); err != nil {
					return err
				}
			} else {
				lines = append(lines, indent+childRel)
				count++
			}
		}
		return nil
	}
	err := build(root, "", 1)
	out := strings.Join(lines, "\n")
	if len(out) > maxToolOutput {
		out = out[:maxToolOutput]
	}
	return out, count, err
}

func walkFiles(root string, fn func(rel, abs string) bool) error {
	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			name := e.Name()
			if skipDirs[name] {
				continue
			}
			childAbs := filepath.Join(dir, name)
			childRel := name
			if rel != "" {
				childRel = rel + "/" + name
			}
			if e.IsDir() {
				if err := walk(childAbs, childRel); err != nil {
					return err
				}
			} else {
				if !fn(childRel, childAbs) {
					return nil
				}
			}
		}
		return nil
	}
	return walk(root, "")
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.SysProcAttr = procx.SysProcAttr()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
