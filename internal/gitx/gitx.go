package gitx

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Branch struct {
	Name      string `json:"name"`
	Sha       string `json:"sha"`
	IsCurrent bool   `json:"isCurrent"`
}

type WorktreeInfo struct {
	Path     string `json:"path"`
	Head     string `json:"head"`
	Branch   string `json:"branch"`
	Bare     bool   `json:"bare"`
	Detached bool   `json:"detached"`
}

type Status struct {
	Branch    string `json:"branch"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Staged    int    `json:"staged"`
	Unstaged  int    `json:"unstaged"`
	Untracked int    `json:"untracked"`
}

const cmdTimeout = 20 * time.Second

func run(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: timed out", strings.Join(args, " "))
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func IsRepo(dir string) bool {
	out, err := run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func RepoRoot(dir string) (string, error) {
	return run(dir, "rev-parse", "--show-toplevel")
}

func RepoName(path string) string {
	p := strings.TrimSuffix(strings.ReplaceAll(path, "\\", "/"), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func CurrentBranch(dir string) (string, error) {
	b, err := run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	if b == "HEAD" {
		return "(detached)", nil
	}
	return b, nil
}

func HeadSha(dir string) (string, error) {
	return run(dir, "rev-parse", "--short", "HEAD")
}

func Branches(dir string) ([]Branch, error) {
	cur, curErr := CurrentBranch(dir)
	out, err := run(dir, "for-each-ref", "refs/heads",
		"--format=%(refname:short)%09%(objectname:short)")
	if err != nil {
		return nil, err
	}
	var branches []Branch
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		b := Branch{Name: parts[0]}
		if len(parts) > 1 {
			b.Sha = parts[1]
		}
		if curErr == nil && b.Name == cur {
			b.IsCurrent = true
		}
		branches = append(branches, b)
	}
	return branches, nil
}

func Worktrees(repoRoot string) ([]WorktreeInfo, error) {
	out, err := run(repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var wts []WorktreeInfo
	var cur *WorktreeInfo
	flush := func() {
		if cur != nil {
			wts = append(wts, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &WorktreeInfo{Path: strings.TrimPrefix(line, "worktree ")}
		case cur == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "bare":
			cur.Bare = true
		case line == "detached":
			cur.Detached = true
		}
	}
	flush()
	return wts, nil
}

func AddWorktree(repoRoot, fullPath, branch, baseRef string) error {
	_, lookupErr := run(repoRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if lookupErr == nil {
		_, err := run(repoRoot, "worktree", "add", fullPath, branch)
		return err
	}
	args := []string{"worktree", "add", "-b", branch}
	if baseRef != "" {
		args = append(args, baseRef)
	}
	args = append(args, fullPath)
	_, err := run(repoRoot, args...)
	return err
}

func RemoveWorktree(repoRoot, fullPath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, fullPath)
	_, err := run(repoRoot, args...)
	return err
}

func StatusSummary(dir string) (*Status, error) {
	out, err := run(dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return nil, err
	}
	st := &Status{}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
			if st.Branch == "(detached)" {
				st.Branch = "(detached)"
			}
		case strings.HasPrefix(line, "# branch.ab "):
			fields := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			for _, f := range fields {
				n, _ := strconv.Atoi(f[1:])
				if strings.HasPrefix(f, "+") {
					st.Ahead = n
				} else if strings.HasPrefix(f, "-") {
					st.Behind = n
				}
			}
		case strings.HasPrefix(line, "1 "):
			if len(line) > 4 {
				xy := line[2:4]
				if xy[0] != '.' {
					st.Staged++
				}
				if xy[1] != '.' {
					st.Unstaged++
				}
			}
		case strings.HasPrefix(line, "2 "):
			st.Unstaged++
		case strings.HasPrefix(line, "? "):
			st.Untracked++
		}
	}
	return st, nil
}

func diffArgs(dir string) []string {
	args := []string{"--no-pager", "diff"}
	if _, err := run(dir, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		args = append(args, "HEAD")
	} else {
		args = append(args, "--root")
	}
	return args
}

func DiffStat(dir string) (string, error) {
	return run(dir, append(diffArgs(dir), "--stat")...)
}

func DiffPatch(dir string) (string, error) {
	return run(dir, diffArgs(dir)...)
}
