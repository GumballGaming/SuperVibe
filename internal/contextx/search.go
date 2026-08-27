package contextx

import (
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type indexPath struct {
	path  string
	lower string
}

type cachedIndex struct {
	ts    time.Time
	files []indexPath
}

var (
	indexMu sync.Mutex
	indexes = map[string]cachedIndex{}
)

// fileIndexTTL keeps the GUI browse index fresh enough while turning every
// keystroke of a mention query or search into a pure in-memory filter instead
// of a full directory walk (walks are the slow part, especially on OneDrive).
const fileIndexTTL = 2 * time.Second

func fileIndex(root string) []indexPath {
	indexMu.Lock()
	defer indexMu.Unlock()
	if c, ok := indexes[root]; ok && time.Since(c.ts) < fileIndexTTL {
		return c.files
	}
	if len(indexes) > 16 {
		for k, v := range indexes {
			if time.Since(v.ts) >= fileIndexTTL {
				delete(indexes, k)
			}
		}
	}
	files := buildFileIndex(root)
	indexes[root] = cachedIndex{ts: time.Now(), files: files}
	return files
}

func buildFileIndex(root string) []indexPath {
	var files []indexPath
	_ = walkRoot(root, func(_ string, rel string, d fs.DirEntry, _ int) error {
		if d.IsDir() {
			return nil
		}
		display := toSlash(rel)
		files = append(files, indexPath{path: display, lower: strings.ToLower(display)})
		return nil
	})
	return files
}

func FileSearch(root, query string, limit int) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	q := strings.ToLower(query)
	var matches []string
	for _, f := range fileIndex(root) {
		if q == "" || strings.Contains(f.lower, q) {
			matches = append(matches, f.path)
		}
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
