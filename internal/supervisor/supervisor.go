package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"supervibe/internal/agent"
	"supervibe/internal/store"
)

type EmitFunc func(sessionID string, ev agent.AgentEvent)

const flushInterval = 50 * time.Millisecond

type Supervisor struct {
	store    *store.Store
	emit     EmitFunc
	mu       sync.Mutex
	sessions map[string]*live
}

type live struct {
	id        string
	provider  agent.Provider
	ad        agent.Adapter
	seq       chan seqItem
	quit      chan struct{}
	quitOnce  sync.Once
	wg        sync.WaitGroup
	stopping  atomic.Bool
	lastState atomic.Value
}

type seqKind int

const (
	seqEv seqKind = iota
	seqFlush
)

type seqItem struct {
	kind seqKind
	ev   agent.AgentEvent
}

var ErrUnknownSession = errors.New("unknown or stopped session")

func New(st *store.Store, emit EmitFunc) *Supervisor {
	return &Supervisor{store: st, emit: emit, sessions: map[string]*live{}}
}

func (s *Supervisor) StartSession(ctx context.Context, worktreeID, provider, model string, opts agent.Options) (*store.Session, error) {
	p := agent.Provider(provider)
	var ad agent.Adapter
	switch p {
	case agent.ProviderClaude:
		ad = agent.NewClaude(opts)
	case agent.ProviderCodex:
		if existing, err := s.store.ActiveSessionInWorktree(worktreeID, provider); err == nil && existing != nil {
			return nil, fmt.Errorf("a codex session is already active in this worktree (%s)", existing.ID)
		}
		ad = agent.NewCodex(opts)
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	return s.register(ctx, worktreeID, provider, model, ad)
}

func (s *Supervisor) register(ctx context.Context, worktreeID, provider, model string, ad agent.Adapter) (*store.Session, error) {
	sess := &store.Session{WorktreeID: worktreeID, Provider: provider, Model: model, Status: "starting"}
	if err := s.store.CreateSession(sess); err != nil {
		return nil, err
	}
	if err := ad.Start(ctx); err != nil {
		_ = s.store.UpdateSessionStatus(sess.ID, "error", err.Error())
		s.emit(sess.ID, agent.AgentEvent{Type: agent.EventStatus, Status: "error", Error: err.Error()})
		return nil, err
	}
	if pidder, ok := ad.(interface{ PID() int }); ok {
		_ = s.store.SetSessionPID(sess.ID, pidder.PID())
	}
	lv := newLive(sess.ID, agent.Provider(provider), ad)
	s.mu.Lock()
	s.sessions[sess.ID] = lv
	s.mu.Unlock()

	lv.attach(s)
	_ = s.store.UpdateSessionStatus(sess.ID, "idle", "")
	s.emit(sess.ID, agent.AgentEvent{Type: agent.EventStatus, Status: "idle"})
	return sess, nil
}

// ReattachCodex restores a persisted Codex session after the supervisor has
// been recreated, resuming the stored Codex thread when one was recorded.
func (s *Supervisor) ReattachCodex(ctx context.Context, sess *store.Session, opts agent.Options) error {
	if sess == nil || sess.Provider != string(agent.ProviderCodex) {
		return fmt.Errorf("session is not a Codex session")
	}
	opts.ResumeProviderID = sess.ProviderSessionID
	ad := agent.NewCodex(opts)
	if err := ad.Start(ctx); err != nil {
		return err
	}
	lv := newLive(sess.ID, agent.ProviderCodex, ad)
	s.mu.Lock()
	if _, exists := s.sessions[sess.ID]; exists {
		s.mu.Unlock()
		return nil
	}
	s.sessions[sess.ID] = lv
	s.mu.Unlock()
	lv.attach(s)
	_ = s.store.UpdateSessionStatus(sess.ID, "idle", "")
	s.emit(sess.ID, agent.AgentEvent{Type: agent.EventStatus, Status: "idle"})
	return nil
}

func newLive(id string, p agent.Provider, ad agent.Adapter) *live {
	lv := &live{
		id:       id,
		provider: p,
		ad:       ad,
		seq:      make(chan seqItem, 4096),
		quit:     make(chan struct{}),
	}
	lv.lastState.Store("starting")
	return lv
}

func (lv *live) attach(s *Supervisor) {
	lv.wg.Add(2)
	go func() { defer lv.wg.Done(); s.pump(lv) }()
	go func() { defer lv.wg.Done(); s.sequencer(lv) }()
	go func() {
		<-lv.ad.Done()
		s.onUnexpectedExit(lv)
	}()
}

func (s *Supervisor) pump(lv *live) {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-lv.quit:
			return
		case ev, ok := <-lv.ad.Events():
			if !ok {
				s.push(lv, seqItem{kind: seqFlush})
				return
			}
			s.push(lv, seqItem{ev: ev})
		case <-ticker.C:
			s.push(lv, seqItem{kind: seqFlush})
		}
	}
}

func (s *Supervisor) push(lv *live, it seqItem) {
	select {
	case <-lv.quit:
	case lv.seq <- it:
	}
}

type streamBuf struct {
	text     strings.Builder
	thinking strings.Builder
}

func (s *Supervisor) sequencer(lv *live) {
	var buf streamBuf
	flushText := func() {
		if buf.text.Len() > 0 {
			s.forward(lv, agent.AgentEvent{Type: agent.EventDelta, Role: "assistant", Text: buf.text.String()})
			buf.text.Reset()
		}
		if buf.thinking.Len() > 0 {
			s.forward(lv, agent.AgentEvent{Type: agent.EventThinkingDelta, Text: buf.thinking.String()})
			buf.thinking.Reset()
		}
	}
	for {
		select {
		case <-lv.quit:
			flushText()
			return
		case it := <-lv.seq:
			if it.kind == seqFlush {
				flushText()
				continue
			}
			switch it.ev.Type {
			case agent.EventDelta:
				buf.text.WriteString(it.ev.Text)
			case agent.EventThinkingDelta:
				buf.thinking.WriteString(it.ev.Text)
			default:
				flushText()
				s.handleEvent(lv, it.ev)
			}
		}
	}
}

func (s *Supervisor) handleEvent(lv *live, ev agent.AgentEvent) {
	switch ev.Type {
	case agent.EventProviderID:
		_ = s.store.SetSessionProviderID(lv.id, ev.ProviderSessionID)

	case agent.EventStatus:
		if ev.Status == "" || ev.Status == "turn_end" {
			return
		}
		_ = s.store.UpdateSessionStatus(lv.id, ev.Status, "")
		lv.lastState.Store(ev.Status)

	case agent.EventMessage:
		_, _ = s.store.InsertMessage(&store.Message{
			SessionID: lv.id,
			Role:      firstNonEmpty(ev.Role, "assistant"),
			Kind:      firstNonEmpty(ev.Kind, "text"),
			Content:   ev.Text,
		})
		if ev.Kind == "" || ev.Kind == "text" {
			_ = s.store.UpdateSessionProgress(lv.id, ev.Text, 0, 0, 0)
		}

	case agent.EventPartUpsert:
		_, _ = s.store.InsertMessage(&store.Message{
			SessionID: lv.id,
			Role:      firstNonEmpty(ev.Role, "assistant"),
			Kind:      firstNonEmpty(ev.Kind, "text"),
			Content:   ev.Text,
			Meta:      marshalMeta(map[string]string{"partId": ev.PartID}),
		})
		if ev.Kind == "" || ev.Kind == "text" {
			_ = s.store.UpdateSessionProgress(lv.id, ev.Text, 0, 0, 0)
		}

	case agent.EventToolStart:
		_, _ = s.store.InsertMessage(&store.Message{
			SessionID: lv.id, Role: "tool", Kind: "tool_start",
			Content: ev.ToolName,
			Meta:    marshalMeta(map[string]string{"id": ev.ToolCallID, "input": ev.ToolInput}),
		})

	case agent.EventToolEnd:
		_, _ = s.store.InsertMessage(&store.Message{
			SessionID: lv.id, Role: "tool", Kind: "tool_end",
			Meta: marshalMeta(map[string]string{"id": ev.ToolCallID, "result": ev.ToolResult}),
		})

	case agent.EventFileChange:
		_, _ = s.store.InsertMessage(&store.Message{
			SessionID: lv.id, Role: "system", Kind: "files",
			Content: strings.Join(ev.Paths, "\n"),
		})

	case agent.EventResult:
		_ = s.store.UpdateSessionProgress(lv.id, "", ev.CostUSD, ev.TokensIn, ev.TokensOut)
		status := firstNonEmpty(ev.Status, "idle")
		_ = s.store.UpdateSessionStatus(lv.id, status, ev.Error)
		lv.lastState.Store(status)

	case agent.EventError:
		_ = s.store.UpdateSessionStatus(lv.id, "error", ev.Error)
		lv.lastState.Store("error")
		_, _ = s.store.InsertMessage(&store.Message{
			SessionID: lv.id, Role: "system", Kind: "error", Content: ev.Error,
		})
	}
	s.forward(lv, ev)
}

func (s *Supervisor) forward(lv *live, ev agent.AgentEvent) {
	ev.SessionID = lv.id
	s.emit(lv.id, ev)
}

func marshalMeta(m map[string]string) string {
	b, _ := json.Marshal(m)
	return string(b)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Supervisor) SendMessage(id, prompt string) error {
	return s.SendMessageWithOptions(id, prompt, agent.TurnOptions{})
}

func (s *Supervisor) SendMessageWithOptions(id, prompt string, options agent.TurnOptions) error {
	s.mu.Lock()
	lv := s.sessions[id]
	s.mu.Unlock()
	if lv == nil {
		return ErrUnknownSession
	}
	if _, err := s.store.InsertMessage(&store.Message{
		SessionID: id, Role: "user", Kind: "text", Content: prompt,
	}); err != nil {
		return err
	}
	var err error
	if sender, ok := lv.ad.(agent.TurnSender); ok {
		err = sender.SendWithOptions(prompt, options)
	} else {
		err = lv.ad.Send(prompt)
	}
	if err != nil {
		_ = s.store.UpdateSessionStatus(id, "error", err.Error())
		lv.lastState.Store("error")
		s.emit(id, agent.AgentEvent{Type: agent.EventStatus, Status: "error", Error: err.Error()})
		return err
	}
	_ = s.store.UpdateSessionStatus(id, "running", "")
	lv.lastState.Store("running")
	s.emit(id, agent.AgentEvent{Type: agent.EventStatus, Status: "running"})
	return nil
}

func (s *Supervisor) Interrupt(id string) error {
	s.mu.Lock()
	lv := s.sessions[id]
	s.mu.Unlock()
	if lv == nil {
		return ErrUnknownSession
	}
	return lv.ad.Interrupt()
}

func (s *Supervisor) StopSession(id string) error {
	s.mu.Lock()
	lv := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()
	if lv == nil {
		return nil
	}
	lv.stopping.Store(true)
	lv.quitOnce.Do(func() { close(lv.quit) })
	_ = lv.ad.Stop()
	done := make(chan struct{})
	go func() { lv.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
	}
	if cur, _ := lv.lastState.Load().(string); cur == "running" || cur == "waiting" || cur == "starting" {
		_ = s.store.UpdateSessionStatus(id, "idle", "")
		s.emit(id, agent.AgentEvent{Type: agent.EventStatus, Status: "idle"})
	}
	return nil
}

func (s *Supervisor) RestartClaude(id string, opts agent.Options) error {
	sess, err := s.store.GetSession(id)
	if err != nil {
		return err
	}
	if sess.Provider != string(agent.ProviderClaude) || sess.ProviderSessionID == "" {
		return errors.New("session cannot be resumed")
	}
	_ = s.StopSession(id)
	opts.ResumeProviderID = sess.ProviderSessionID
	var ad agent.Adapter = agent.NewClaude(opts)
	if err := ad.Start(context.Background()); err != nil {
		_ = s.store.UpdateSessionStatus(id, "error", err.Error())
		s.emit(id, agent.AgentEvent{Type: agent.EventStatus, Status: "error", Error: err.Error()})
		return err
	}
	if pidder, ok := ad.(interface{ PID() int }); ok {
		_ = s.store.SetSessionPID(id, pidder.PID())
	}
	lv := newLive(id, agent.ProviderClaude, ad)
	lv.lastState.Store("idle")
	s.mu.Lock()
	s.sessions[id] = lv
	s.mu.Unlock()
	lv.attach(s)
	_ = s.store.UpdateSessionStatus(id, "idle", "")
	s.emit(id, agent.AgentEvent{Type: agent.EventStatus, Status: "idle"})
	return nil
}

func (s *Supervisor) OutputTail(id string, maxBytes int) string {
	s.mu.Lock()
	lv := s.sessions[id]
	s.mu.Unlock()
	if lv == nil {
		return ""
	}
	type tailer interface{ OutputTail(n int) string }
	if t, ok := lv.ad.(tailer); ok {
		return t.OutputTail(maxBytes / 2)
	}
	return ""
}

func (s *Supervisor) StopAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.StopSession(id)
	}
}

func (s *Supervisor) onUnexpectedExit(lv *live) {
	s.mu.Lock()
	stillLive := s.sessions[lv.id] == lv
	s.mu.Unlock()
	if !stillLive || lv.stopping.Load() {
		return
	}
	state, _ := lv.lastState.Load().(string)
	if state == "running" || state == "waiting" || state == "starting" {
		msg := "agent process exited unexpectedly"
		if t := s.OutputTail(lv.id, 1200); t != "" {
			msg += "\n" + t
		}
		_ = s.store.UpdateSessionStatus(lv.id, "error", msg)
		_, _ = s.store.InsertMessage(&store.Message{
			SessionID: lv.id, Role: "system", Kind: "error", Content: msg,
		})
		s.emit(lv.id, agent.AgentEvent{Type: agent.EventStatus, Status: "error", Error: msg})
	}
}
