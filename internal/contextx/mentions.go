package contextx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"supervibe/internal/gitx"
)

const mentionPunct = ",.;:!?'\"()[]{}<>"

func ParseMentions(text string) (string, []string) {
	var tokens []string
	var spans [][2]int
	seen := make(map[string]bool)
	i := nextNonSpace(text, 0)
	for i < len(text) {
		end := wordEnd(text, i)
		tok := strings.Trim(text[i:end], mentionPunct)
		if isMentionToken(tok) {
			spans = append(spans, [2]int{i, end})
			if !seen[tok] {
				seen[tok] = true
				tokens = append(tokens, tok)
			}
		}
		i = nextNonSpace(text, end)
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(text[last:sp[0]])
		last = sp[1]
	}
	b.WriteString(text[last:])
	return squeezeSpaces(b.String()), tokens
}

func isMentionToken(tok string) bool {
	return len(tok) > 1 && tok[0] == '@'
}

func nextNonSpace(text string, start int) int {
	i := start
	for i < len(text) {
		r, sz := utf8.DecodeRuneInString(text[i:])
		if !unicode.IsSpace(r) {
			return i
		}
		i += sz
	}
	return i
}

func wordEnd(text string, start int) int {
	i := start
	for i < len(text) {
		r, sz := utf8.DecodeRuneInString(text[i:])
		if unicode.IsSpace(r) {
			return i
		}
		i += sz
	}
	return i
}

func squeezeSpaces(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if inSpace {
				continue
			}
			b.WriteByte(' ')
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func ResolveMention(ctx context.Context, root, token string, lim Limits, fetcher func(url string) (string, error)) (string, error) {
	tok := strings.TrimPrefix(strings.TrimSpace(token), "@")
	lim = normLimits(lim)
	switch {
	case tok == "":
		return "", errors.New("empty mention")
	case tok == "diff":
		return resolveDiff(ctx, root)
	case tok == "git":
		return resolveGit(ctx, root)
	case tok == "tree":
		return fmt.Sprintf("=== @%s ===\n%s", tok, treeLines(root, lim.TreeEntries)), nil
	case strings.HasPrefix(tok, "http://"), strings.HasPrefix(tok, "https://"):
		if fetcher == nil {
			return "", errors.New("no fetcher configured")
		}
		content, err := fetcher(tok)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("=== @%s ===\n%s", tok, truncate(content, lim.MaxFileBytes)), nil
	default:
		return resolvePath(root, tok, lim)
	}
}

func resolveDiff(ctx context.Context, root string) (string, error) {
	args := []string{"--no-pager", "diff"}
	if _, err := gitOut(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		args = append(args, "HEAD")
	} else {
		args = append(args, "--root")
	}

	statArgs := append(append([]string{}, args...), "--stat")
	stat, _ := gitOut(ctx, root, statArgs...)
	patch, perr := gitOut(ctx, root, args...)
	if perr != nil && stat == "" && patch == "" {
		return "", perr
	}
	body := stat
	if stat != "" && patch != "" {
		body += "\n\n"
	}
	body += patch
	return truncate(fmt.Sprintf("=== @diff ===\n%s", body), 16*1024), nil
}

func resolveGit(ctx context.Context, root string) (string, error) {
	var b strings.Builder
	b.WriteString("=== @git ===\n")
	if st, err := gitx.StatusSummary(root); err == nil {
		b.WriteString(branchLine(st.Branch, st.Ahead, st.Behind))
		b.WriteByte('\n')
		fmt.Fprintf(&b, "dirty: %d staged, %d unstaged, %d untracked\n", st.Staged, st.Unstaged, st.Untracked)
		for i, ch := range changedPaths(root) {
			if i >= 25 {
				break
			}
			fmt.Fprintf(&b, "  %s %s\n", ch.xy, ch.path)
		}
	}
	if log, err := gitOut(ctx, root, "log", "--oneline", "-n", "15"); err == nil && log != "" {
		b.WriteString("\n== log ==\n")
		b.WriteString(log)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func resolvePath(root, tok string, lim Limits) (string, error) {
	full, err := safeJoin(root, filepath.FromSlash(tok))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("@%s: %w", tok, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("@%s: is a directory", tok)
	}
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, lim.MaxFileBytes)
	n, rerr := io.ReadFull(f, buf)
	if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
		return "", rerr
	}
	content := strings.ToValidUTF8(truncate(string(buf[:n]), lim.MaxFileBytes), "\uFFFD")
	ext := strings.TrimPrefix(filepath.Ext(tok), ".")
	return fmt.Sprintf("=== @%s ===\n%s\n```%s\n%s\n```", tok, toSlash(tok), ext, content), nil
}
