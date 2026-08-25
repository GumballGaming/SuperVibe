package contextx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type Limits struct {
	TreeEntries  int
	MaxFileBytes int
	TotalBytes   int
}

func DefaultLimits() Limits {
	return Limits{TreeEntries: 300, MaxFileBytes: 24576, TotalBytes: 98304}
}

func normLimits(lim Limits) Limits {
	def := DefaultLimits()
	if lim.TreeEntries <= 0 {
		lim.TreeEntries = def.TreeEntries
	}
	if lim.MaxFileBytes <= 0 {
		lim.MaxFileBytes = def.MaxFileBytes
	}
	if lim.TotalBytes <= 0 {
		lim.TotalBytes = def.TotalBytes
	}
	return lim
}

const gitTimeout = 20 * time.Second

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func truncate(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func toSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func safeJoin(root, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("path escapes worktree: %s", rel)
	}
	base := filepath.Clean(root)
	full := filepath.Clean(filepath.Join(base, rel))
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes worktree: %s", rel)
	}
	return full, nil
}

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	"vendor":       true,
	"target":       true,
	"build":        true,
	"tmp":          true,
	".next":        true,
	"__pycache__":  true,
}

func walkRoot(root string, fn func(path, rel string, d fs.DirEntry, depth int) error) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if p == root {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if d.IsDir() && skipDirs[strings.ToLower(d.Name())] {
			return fs.SkipDir
		}
		if ferr := fn(p, rel, d, depth); ferr != nil {
			return ferr
		}
		if d.IsDir() && depth >= 3 {
			return fs.SkipDir
		}
		return nil
	})
}
