package store

import (
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestProjectWorktreeSessionFlow(t *testing.T) {
	st := openTest(t)

	p, err := st.CreateProject("demo", `C:\code\demo`)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projects, err := st.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("list projects: %v %v", projects, err)
	}

	wt := &Worktree{ProjectID: p.ID, Name: "main", Branch: "main", Path: `C:\code\demo`, IsPrimary: true}
	if err := st.UpsertWorktree(wt); err != nil {
		t.Fatalf("upsert wt: %v", err)
	}
	wt2 := &Worktree{ProjectID: p.ID, Name: "feature-x", Branch: "feature/x", Path: `C:\code\demo-wt\feature-x`}
	if err := st.UpsertWorktree(wt2); err != nil {
		t.Fatalf("upsert wt2: %v", err)
	}
	wts, err := st.ListWorktrees(p.ID)
	if err != nil || len(wts) != 2 {
		t.Fatalf("list wts: %v %v", wts, err)
	}
	if !wts[0].IsPrimary {
		t.Fatalf("primary should sort first: %+v", wts[0])
	}

	sess := &Session{WorktreeID: wt.ID, Provider: "claude", Model: "sonnet"}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	got, err := st.GetSession(sess.ID)
	if err != nil || got.Status != "starting" {
		t.Fatalf("get session: %v %v", got, err)
	}
	if got.ID == "" || got.ID == sess.ID && false {
		t.Fatal("id should be generated")
	}

	empty, err := st.ListMessages(sess.ID, 0, 100)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty messages should be a non-nil slice: %v %v", empty, err)
	}

	if err := st.SetSessionProviderID(sess.ID, "ps-123"); err != nil {
		t.Fatalf("set psid: %v", err)
	}
	if err := st.UpdateSessionStatus(sess.ID, "running", ""); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := st.UpdateSessionProgress(sess.ID, "hello world", 0.5, 100, 200); err != nil {
		t.Fatalf("progress: %v", err)
	}
	got, _ = st.GetSession(sess.ID)
	if got.ProviderSessionID != "ps-123" || got.Status != "running" || got.Cost < 0.5 || got.TokensOut != 200 {
		t.Fatalf("session fields wrong: %+v", got)
	}

	if _, err := st.InsertMessage(&Message{SessionID: sess.ID, Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("insert msg: %v", err)
	}
	msgs, err := st.ListMessages(sess.ID, 0, 100)
	if err != nil || len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Fatalf("messages: %v %v", msgs, err)
	}
	after, err := st.ListMessages(sess.ID, msgs[0].ID, 100)
	if err != nil || len(after) != 0 {
		t.Fatalf("after: %v %v", after, err)
	}

	if err := st.UpdateSessionStatus(sess.ID, "idle", ""); err != nil {
		t.Fatalf("set idle: %v", err)
	}
	sess2 := &Session{WorktreeID: wt.ID, Provider: "codex", Status: "running"}
	_ = st.CreateSession(sess2)
	active, err := st.ActiveSessionInWorktree(wt.ID, "codex")
	if err != nil || active == nil {
		t.Fatalf("active codex missing: %v %v", active, err)
	}

	fleet, err := st.ListSessionsByStatus([]string{"running"})
	if err != nil || len(fleet) != 1 {
		t.Fatalf("fleet: %v %v", fleet, err)
	}
	if fleet[0].Provider != "codex" || fleet[0].ProjectName != "demo" || fleet[0].Branch != "main" {
		t.Fatalf("fleet join wrong: %+v", fleet[0])
	}

	if err := st.SetSetting("k", "v"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	v, ok, _ := st.GetSetting("k")
	if !ok || v != "v" {
		t.Fatalf("setting roundtrip: %q %v", v, ok)
	}
}

func TestCascadeDelete(t *testing.T) {
	st := openTest(t)
	p, _ := st.CreateProject("p1", "C:\\p1")
	wt := &Worktree{ProjectID: p.ID, Name: "n", Branch: "b", Path: "C:\\p1"}
	_ = st.UpsertWorktree(wt)
	sess := &Session{WorktreeID: wt.ID, Provider: "claude"}
	_ = st.CreateSession(sess)
	st.InsertMessage(&Message{SessionID: sess.ID, Role: "user", Content: "x"})

	if err := st.DeleteProject(p.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	msgs, _ := st.ListMessages(sess.ID, 0, 10)
	if len(msgs) != 0 {
		t.Fatalf("messages should cascade")
	}
	if s, err := st.GetSession(sess.ID); err == nil && s != nil {
		t.Fatalf("session should cascade")
	}
}
