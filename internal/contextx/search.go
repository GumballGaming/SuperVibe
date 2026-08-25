package contextx

import (
	"io/fs"
	"os"
	"sort"
	"strings"
)

func FileSearch(root, query string, limit int) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	q := strings.ToLower(query)
	var matches []string
	err := walkRoot(root, func(_ string, rel string, d fs.DirEntry, _ int) error {
		if d.IsDir() {
			return nil
		}
		if q == "" || strings.Contains(strings.ToLower(toSlash(rel)), q) {
			matches = append(matches, toSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if len(matches[i]) != len(matches[j]) {
			return len(matches[i]) < len(matches[j])
		}
		return matches[i] < matches[j]
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}
