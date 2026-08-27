package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitInit(t *testing.T) (root string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	run2 := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run2("init", "-b", "main")
	run2("config", "user.email", "test@test")
	run2("config", "user.name", "test")
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644)
	run2("add", ".")
	run2("commit", "-m", "init")
	return root
}

func gitInitUnborn(t *testing.T) (root string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return root
}

func TestRepoDetectAndBranches(t *testing.T) {
	root := gitInit(t)
	if !IsRepo(root) {
		t.Fatal("should be a repo")
	}
	got, err := RepoRoot(root)
	if err != nil || filepath.Clean(got) != filepath.Clean(root) {
		t.Fatalf("root mismatch: %v %v", got, err)
	}
	branch, err := CurrentBranch(root)
	if err != nil || branch != "main" {
		t.Fatalf("branch: %q %v", branch, err)
	}
	branches, err := Branches(root)
	if err != nil || len(branches) == 0 || !branches[0].IsCurrent || branches[0].Name != "main" {
		t.Fatalf("branches: %+v %v", branches, err)
	}
}

func TestStatusAndDiff(t *testing.T) {
	root := gitInit(t)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# changed\n"), 0o644)
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("hi"), 0o644)

	st, err := StatusSummary(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.Unstaged < 1 || st.Untracked < 1 {
		t.Fatalf("status wrong: %+v", st)
	}

	patch, err := DiffPatch(root)
	if err != nil || !strings.Contains(patch, "# changed") {
		t.Fatalf("patch: %v %v", patch, err)
	}
	stat, err := DiffStat(root)
	if err != nil || stat == "" {
		t.Fatalf("stat empty: %q %v", stat, err)
	}
}

func TestStageUnstageCommit(t *testing.T) {
	root := gitInit(t)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("b\n"), 0o644)

	if err := Stage(root, nil); err != nil {
		t.Fatal(err)
	}
	st, err := StatusSummary(root)
	if err != nil || st.Staged < 2 {
		t.Fatalf("stage all: %+v %v", st, err)
	}
	if err := Unstage(root, nil); err != nil {
		t.Fatal(err)
	}
	st, err = StatusSummary(root)
	if err != nil || st.Staged != 0 {
		t.Fatalf("unstage all: %+v %v", st, err)
	}
	if err := Stage(root, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	st, _ = StatusSummary(root)
	if st.Staged != 1 {
		t.Fatalf("stage one: %+v", st)
	}
	if err := Commit(root, "added a"); err != nil {
		t.Fatal(err)
	}
	st, err = StatusSummary(root)
	if err != nil || st.Staged != 0 || st.Untracked != 1 {
		t.Fatalf("after commit: %+v %v", st, err)
	}
}

func TestDiffWithoutHEAD(t *testing.T) {
	root := gitInitUnborn(t)

	if stat, err := DiffStat(root); err != nil || stat != "" {
		t.Fatalf("unborn diff stat: %q %v", stat, err)
	}
	if patch, err := DiffPatch(root); err != nil || patch != "" {
		t.Fatalf("unborn diff patch: %q %v", patch, err)
	}
}

func TestWorktreeLifecycle(t *testing.T) {
	root := gitInit(t)
	wtPath := filepath.Join(t.TempDir(), "feature-wt")

	wts0, _ := Worktrees(root)
	if len(wts0) != 1 {
		t.Fatalf("expected 1 worktree got %d", len(wts0))
	}

	if err := AddWorktree(root, wtPath, "feature/ui", ""); err != nil {
		t.Fatalf("add worktree: %v", err)
	}
	if !IsRepo(wtPath) {
		t.Fatal("new worktree should be a repo checkout")
	}
	wts, err := Worktrees(root)
	if err != nil || len(wts) != 2 {
		t.Fatalf("worktrees after add: %+v %v", wts, err)
	}
	var found bool
	for _, w := range wts {
		if filepath.Clean(w.Path) == filepath.Clean(wtPath) && w.Branch == "feature/ui" {
			found = true
		}
	}
	if !found {
		t.Fatalf("added worktree not listed: %+v", wts)
	}

	st, err := StatusSummary(wtPath)
	if err != nil || st.Branch != "feature/ui" {
		t.Fatalf("worktree status: %+v %v", st, err)
	}

	os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("x"), 0o644)
	if err := RemoveWorktree(root, wtPath, true); err != nil {
		t.Fatalf("remove: %v", err)
	}
	wts, _ = Worktrees(root)
	if len(wts) != 1 {
		t.Fatalf("worktrees after remove: %d", len(wts))
	}
}

func TestAddWorktreeExistingBranch(t *testing.T) {
	root := gitInit(t)
	c := exec.Command("git", "branch", "existing")
	c.Dir = root
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("branch: %v %s", err, out)
	}
	wtPath := filepath.Join(t.TempDir(), "existing-wt")
	if err := AddWorktree(root, wtPath, "existing", ""); err != nil {
		t.Fatalf("add existing: %v", err)
	}
	st, _ := StatusSummary(wtPath)
	if st.Branch != "existing" {
		t.Fatalf("branch wrong: %+v", st)
	}
}
