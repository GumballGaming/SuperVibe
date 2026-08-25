package supervisor

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"supervibe/internal/agent"
	"supervibe/internal/store"
)

type fakeAdapter struct {
	mu      sync.Mutex
	events  chan agent.AgentEvent
	done    chan struct{}
	sent    []string
	options []agent.TurnOptions
	stopped bool
}

func newFake() *fakeAdapter {
	return &fakeAdapter{events: make(chan agent.AgentEvent, 64), done: make(chan struct{})}
}

func (f *fakeAdapter) Start(_ context.Context) error { return nil }
func (f *fakeAdapter) Send(p string) error {
	return f.SendWithOptions(p, agent.TurnOptions{})
}
func (f *fakeAdapter) SendWithOptions(p string, options agent.TurnOptions) error {
	f.mu.Lock()
	f.sent = append(f.sent, p)
	f.options = append(f.options, options)
	f.mu.Unlock()
	return nil
}
func (f *fakeAdapter) Interrupt() error { return nil }
func (f *fakeAdapter) Stop() error {
	f.mu.Lock()
	if !f.stopped {
		f.stopped = true
		close(f.done)
	}
	f.mu.Unlock()
	return nil
}
func (f *fakeAdapter) Events() <-chan agent.AgentEvent { return f.events }
func (f *fakeAdapter) Done() <-chan struct{}           { return f.done }

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

func newTestSupervisor(t *testing.T, st *store.Store) (*Supervisor, *[]agent.AgentEvent, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	emitted := &[]agent.AgentEvent{}
	s := New(st, func(id string, ev agent.AgentEvent) {
		mu.Lock()
		*emitted = append(*emitted, ev)
		mu.Unlock()
	})
	return s, emitted, &mu
}

func TestSequencerCoalescesAndOrders(t *testing.T) {
	st := openStore(t)
	lv := &live{id: "s1", seq: make(chan seqItem, 4096), quit: make(chan struct{})}
	lv.lastState.Store("idle")

	var mu sync.Mutex
	var deltaCount int
	deltaText := 0
	messageSeen := make(chan struct{}, 1)

	s := &Supervisor{store: st, emit: func(id string, ev agent.AgentEvent) {
		mu.Lock()
		defer mu.Unlock()
		switch ev.Type {
		case agent.EventDelta:
			deltaCount++
			deltaText += len(ev.Text)
		case agent.EventMessage:
			select {
			case messageSeen <- struct{}{}:
			default:
			}
		}
	}}

	go s.sequencer(lv)

	const n = 200
	for i := 0; i < n; i++ {
		lv.seq <- seqItem{ev: agent.AgentEvent{Type: agent.EventDelta, Text: "x"}}
	}
	lv.seq <- seqItem{ev: agent.AgentEvent{Type: agent.EventMessage, Role: "assistant", Kind: "text", Text: "final"}}

	select {
	case <-messageSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}

	mu.Lock()
	defer mu.Unlock()
	if deltaCount >= n {
		t.Fatalf("deltas were not coalesced: %d emissions for %d deltas", deltaCount, n)
	}
	if deltaText != n {
		t.Fatalf("delta text lost: have %d want %d", deltaText, n)
	}
	lv.quitOnce.Do(func() { close(lv.quit) })
}

func TestSessionPipelinePersistence(t *testing.T) {
	st := openStore(t)
	s, _, _ := newTestSupervisor(t, st)

	p, _ := st.CreateProject("proj", "C:\\proj")
	wt := &store.Worktree{ProjectID: p.ID, Name: "main", Branch: "main", Path: "C:\\proj"}
	_ = st.UpsertWorktree(wt)

	fake := newFake()
	sess, err := s.register(context.Background(), wt.ID, "claude", "test-model", fake)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := s.SendMessageWithOptions(sess.ID, "do work", agent.TurnOptions{
		Model:           "gpt-5-codex",
		ReasoningEffort: "high",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	fake.mu.Lock()
	if len(fake.sent) != 1 || fake.sent[0] != "do work" {
		fake.mu.Unlock()
		t.Fatalf("adapter did not receive prompt: %v", fake.sent)
	}
	if len(fake.options) != 1 || fake.options[0].Model != "gpt-5-codex" ||
		fake.options[0].ReasoningEffort != "high" {
		fake.mu.Unlock()
		t.Fatalf("adapter did not receive turn options: %v", fake.options)
	}
	fake.mu.Unlock()

	fake.events <- agent.AgentEvent{Type: agent.EventProviderID, ProviderSessionID: "ps-9"}
	fake.events <- agent.AgentEvent{Type: agent.EventToolStart, ToolCallID: "t1", ToolName: "Bash", ToolInput: "{}"}
	fake.events <- agent.AgentEvent{Type: agent.EventMessage, Role: "assistant", Kind: "text", Text: "working on it"}
	fake.events <- agent.AgentEvent{Type: agent.EventToolEnd, ToolCallID: "t1", ToolResult: "ok"}
	fake.events <- agent.AgentEvent{Type: agent.EventResult, Status: "idle", CostUSD: 0.1, TokensIn: 11, TokensOut: 22}

	waitFor(t, 3*time.Second, func() bool {
		got, err := st.GetSession(sess.ID)
		return err == nil && got.Status == "idle" && got.ProviderSessionID == "ps-9" && got.Cost >= 0.1
	}, "session never reached idle with provider id and cost")

	msgs, _ := st.ListMessages(sess.ID, 0, 100)
	var roles []string
	for _, m := range msgs {
		roles = append(roles, m.Role+":"+m.Kind)
	}
	joined := ""
	for _, r := range roles {
		joined += r + ","
	}
	for _, want := range []string{"user:text", "tool:tool_start", "assistant:text", "tool:tool_end"} {
		if !contains(joined, want) {
			t.Fatalf("missing %q in %v", want, roles)
		}
	}
}

func TestUnexpectedExitMarksError(t *testing.T) {
	st := openStore(t)
	s, _, _ := newTestSupervisor(t, st)
	p, _ := st.CreateProject("p2", "C:\\p2")
	wt := &store.Worktree{ProjectID: p.ID, Name: "n", Branch: "b", Path: "C:\\p2"}
	_ = st.UpsertWorktree(wt)

	fake := newFake()
	sess, err := s.register(context.Background(), wt.ID, "claude", "", fake)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	_ = s.SendMessage(sess.ID, "go")

	close(fake.done)
	waitFor(t, 3*time.Second, func() bool {
		got, err := st.GetSession(sess.ID)
		return err == nil && got.Status == "error"
	}, "session should be marked error after unexpected exit")

	got, _ := st.GetSession(sess.ID)
	if !contains(got.Error, "exited unexpectedly") {
		t.Fatalf("error message wrong: %q", got.Error)
	}
}

func TestStopSessionWhileRunning(t *testing.T) {
	st := openStore(t)
	s, _, _ := newTestSupervisor(t, st)
	p, _ := st.CreateProject("p3", "C:\\p3")
	wt := &store.Worktree{ProjectID: p.ID, Name: "n", Branch: "b", Path: "C:\\p3"}
	_ = st.UpsertWorktree(wt)

	fake := newFake()
	sess, _ := s.register(context.Background(), wt.ID, "claude", "", fake)
	_ = s.SendMessage(sess.ID, "long task")

	if err := s.StopSession(sess.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		got, err := st.GetSession(sess.ID)
		return err == nil && got.Status == "idle"
	}, "stopped session should end idle")

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !fake.stopped {
		t.Fatal("adapter should be stopped")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && stringsContains(haystack, needle)
}

func stringsContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
