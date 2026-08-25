package contextx

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"supervibe/internal/gitx"
)

type changeLine struct {
	xy   string
	path string
}

func RepoBrief(root string, lim Limits) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("contextx: %s is not a directory", root)
	}
	lim = normLimits(lim)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", filepath.Base(filepath.Clean(root)))
	if st, serr := gitx.StatusSummary(root); serr == nil {
		b.WriteString(branchLine(st.Branch, st.Ahead, st.Behind))
		b.WriteByte('\n')
		fmt.Fprintf(&b, "dirty: %d staged, %d unstaged, %d untracked\n", st.Staged, st.Unstaged, st.Untracked)
		for i, ch := range changedPaths(root) {
			if i >= 25 {
				break
			}
			fmt.Fprintf(&b, "  %s %s\n", ch.xy, ch.path)
		}
		b.WriteByte('\n')
	}
	if log, lerr := gitOut(context.Background(), root, "log", "--oneline", "-n", "10"); lerr == nil && log != "" {
		b.WriteString("== commits ==\n")
		b.WriteString(log)
		b.WriteString("\n\n")
	}
	b.WriteString("== tree ==\n")
	b.WriteString(treeLines(root, lim.TreeEntries))
	b.WriteByte('\n')
	if head := readmeHead(root); head != "" {
		b.WriteString("== readme ==\n")
		b.WriteString(head)
		b.WriteByte('\n')
	}
	return truncate(b.String(), lim.TotalBytes), nil
}

func branchLine(branch string, ahead, behind int) string {
	line := "branch: " + branch
	var extra []string
	if ahead > 0 {
		extra = append(extra, fmt.Sprintf("ahead %d", ahead))
	}
	if behind > 0 {
		extra = append(extra, fmt.Sprintf("behind %d", behind))
	}
	if len(extra) > 0 {
		line += " (" + strings.Join(extra, ", ") + ")"
	}
	return line
}

func treeLines(root string, max int) string {
	var b strings.Builder
	n := 0
	_ = walkRoot(root, func(_ string, rel string, d fs.DirEntry, depth int) error {
		if n >= max {
			return fs.SkipAll
		}
		display := toSlash(rel)
		if d.IsDir() {
			display += "/"
		}
		b.WriteString(strings.Repeat("  ", depth-1))
		b.WriteString(display)
		b.WriteByte('\n')
		n++
		return nil
	})
	return b.String()
}

func changedPaths(dir string) []changeLine {
	out, err := gitOut(context.Background(), dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return nil
	}
	var changes []changeLine
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			fields := strings.Fields(line)
			parts := strings.SplitN(line, "\t", 2)
			if len(fields) < 2 || len(parts) < 2 {
				continue
			}
			changes = append(changes, changeLine{xy: fields[1], path: toSlash(strings.TrimSpace(parts[1]))})
		case strings.HasPrefix(line, "? "):
			changes = append(changes, changeLine{xy: "??", path: toSlash(strings.TrimSpace(line[2:]))})
		}
	}
	return changes
}

func readmeHead(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return ""
	}
	if len(data) > 4096 {
		data = data[:4096]
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 60 {
		lines = lines[:60]
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
