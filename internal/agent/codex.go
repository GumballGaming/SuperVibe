package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	appServerTimeout     = 120 * time.Second
	appServerStartWindow = 60 * time.Second
)

// Codex talks to `codex app-server` over stdio (JSONL) so that agent text and
// reasoning stream into the UI in real time via item delta notifications.
type Codex struct {
	opts    Options
	events  *eventChan
	done    chan struct{}
	mu      sync.Mutex
	stopped bool

	proc  *proc
	pid   int32
	readY chan struct{}

	inFlight bool
	threadID string
	turnID   string

	reqSeq int64
	pend   sync.Map // id -> chan *jsonrpcEnvelope

	usageMu sync.Mutex
	usage   *codexUsage
}

type codexUsage struct {
	InputTokens    int64 `json:"inputTokens"`
	CachedInput    int64 `json:"cachedInputTokens"`
	CacheWrite     int64 `json:"cacheWriteInputTokens"`
	OutputTokens   int64 `json:"outputTokens"`
	ReasonTokens   int64 `json:"reasoningOutputTokens"`
	TotalTokens    int64 `json:"totalTokens"`
}

type jsonrpcEnvelope struct {
	ID     int64           `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *jsonrpcErr     `json:"error,omitempty"`
}

type jsonrpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewCodex(opts Options) *Codex {
	return &Codex{
		opts:   opts,
		events: newEventChan(),
		done:   make(chan struct{}),
		pid:    -1,
		readY:  make(chan struct{}),
	}
}

func (c *Codex) Start(ctx context.Context) error {
	p, err := startProc(ctx, c.opts.CodexPath, []string{"app-server"}, c.opts.WorktreePath)
	if err != nil {
		return err
	}
	if p.cmd.Process != nil {
		c.pid = int32(p.cmd.Process.Pid)
	}
	c.mu.Lock()
	c.proc = p
	c.mu.Unlock()

	go func() {
		scanLines(p.stdout, c.handleLine)
		close(c.done)
	}()

	if err := c.handshake(); err != nil {
		p.kill()
		return err
	}
	return nil
}

func (c *Codex) handshake() error {
	if err := c.rpcCall("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "SuperVibe",
			"title":   "SuperVibe",
			"version": "1.0.0",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	}, nil); err != nil {
		return fmt.Errorf("codex initialize: %w", err)
	}
	c.notify("initialized", map[string]any{})

	var thread *struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if c.opts.ResumeProviderID != "" {
		err := c.rpcCall("thread/resume", map[string]any{"threadId": c.opts.ResumeProviderID}, &thread)
		if err != nil {
			return fmt.Errorf("codex resume: %w", err)
		}
	} else {
		params := map[string]any{
			"model":          c.opts.Model,
			"cwd":            c.opts.WorktreePath,
			"approvalPolicy": "never",
			"sandbox":        codexSandbox(c.opts.CodexSandbox),
		}
		err := c.rpcCall("thread/start", params, &thread)
		if err != nil {
			return fmt.Errorf("codex thread/start: %w", err)
		}
	}
	c.mu.Lock()
	c.threadID = thread.Thread.ID
	c.mu.Unlock()
	c.events.emit(AgentEvent{Type: EventProviderID, ProviderSessionID: thread.Thread.ID})
	return nil
}

func codexSandbox(s string) string {
	switch s {
	case "read-only":
		return "read-only"
	case "danger-full-access":
		return "danger-full-access"
	default:
		return "workspace-write"
	}
}

func (c *Codex) notify(method string, params any) {
	c.writeMsg(jsonrpcEnvelope{Method: method, Params: mustJSON(params)})
}

func (c *Codex) rpcCall(method string, params any, out any) error {
	id := atomic.AddInt64(&c.reqSeq, 1)
	ch := make(chan *jsonrpcEnvelope, 1)
	c.pend.Store(id, ch)
	defer c.pend.Delete(id)
	if err := c.writeMsg(jsonrpcEnvelope{ID: id, Method: method, Params: mustJSON(params)}); err != nil {
		return err
	}
	timer := time.NewTimer(appServerTimeout)
	defer timer.Stop()
	select {
	case m := <-ch:
		if m.Error != nil {
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		if out != nil && len(m.Result) > 0 {
			if err := json.Unmarshal(m.Result, out); err != nil {
				return fmt.Errorf("%s: decode result: %w", method, err)
			}
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("%s: timeout", method)
	}
}

func (c *Codex) writeMsg(env jsonrpcEnvelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.mu.Lock()
	p := c.proc
	c.mu.Unlock()
	if p == nil {
		return fmt.Errorf("codex app-server not running")
	}
	return p.writeLine(string(b))
}

func mustJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func (c *Codex) handleLine(line string) {
	if line == "" {
		return
	}
	if p := c.proc; p != nil {
		p.outRing.Write([]byte(line + "\n"))
	}
	if !strings.HasPrefix(line, "{") {
		return
	}
	var env jsonrpcEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return
	}
	if env.ID != 0 {
		if ch, ok := c.pend.LoadAndDelete(env.ID); ok {
			ch.(chan *jsonrpcEnvelope) <- &env
		}
		return
	}
	c.dispatchNotify(env.Method, env.Params)
}

func (c *Codex) dispatchNotify(method string, params json.RawMessage) {
	switch method {
	case "turn/started":
		var p struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(params, &p)
		c.mu.Lock()
		c.turnID = p.Turn.ID
		c.threadID = firstNonEmpty(c.threadID, p.Turn.ID)
		c.mu.Unlock()
		c.events.emit(AgentEvent{Type: EventStatus, Status: "running"})

	case "turn/completed":
		var p struct {
			Turn struct {
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(params, &p)
		c.mu.Lock()
		c.inFlight = false
		c.turnID = ""
		c.mu.Unlock()
		switch p.Turn.Status {
		case "failed":
			c.events.emit(AgentEvent{Type: EventError, Error: firstNonEmpty(p.Turn.Error.Message, "codex turn failed"), Status: "error"})
		case "interrupted":
			c.events.emit(AgentEvent{Type: EventResult, Status: "idle"})
		default:
			c.events.emit(AgentEvent{Type: EventResult, Status: "idle", TokensIn: c.usageIn(), TokensOut: c.usageOut(), CachedTokens: c.usageCached(), ReasoningTokens: c.usageReason()})
		}

	case "turn/failed":
		var p struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(params, &p)
		c.mu.Lock()
		c.inFlight = false
		c.turnID = ""
		c.mu.Unlock()
		c.events.emit(AgentEvent{Type: EventError, Error: firstNonEmpty(p.Error.Message, "codex turn failed"), Status: "error"})

	case "thread/status/changed":
		var p struct {
			Status struct {
				Type string   `json:"type"`
				Tags []string `json:"activeFlags"`
			} `json:"status"`
		}
		_ = json.Unmarshal(params, &p)
		switch p.Status.Type {
		case "active":
			status := "running"
			for _, f := range p.Status.Tags {
				if f == "waitingOnApproval" {
					status = "waiting"
				}
			}
			c.events.emit(AgentEvent{Type: EventStatus, Status: status})
		case "idle":
			c.events.emit(AgentEvent{Type: EventStatus, Status: "idle"})
		}

	case "thread/tokenUsage/updated":
		var p struct {
			TokenUsage struct {
				Last codexUsage `json:"last"`
			} `json:"tokenUsage"`
		}
		if json.Unmarshal(params, &p) == nil {
			c.usageMu.Lock()
			c.usage = &p.TokenUsage.Last
			c.usageMu.Unlock()
		}

	case "item/started":
		var p struct {
			Item appItem `json:"item"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		c.emitItemLifecycle(p.Item, true)

	case "item/completed":
		var p struct {
			Item appItem `json:"item"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		c.emitItemLifecycle(p.Item, false)

	case "item/agentMessage/delta":
		var p struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(params, &p) != nil || p.Delta == "" {
			return
		}
		c.events.emit(AgentEvent{Type: EventDelta, Role: "assistant", Text: p.Delta})

	case "item/reasoning/summaryTextDelta":
		var p struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(params, &p) != nil || p.Delta == "" {
			return
		}
		c.events.emit(AgentEvent{Type: EventThinkingDelta, Text: p.Delta})

	case "item/reasoning/textDelta":
		var p struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(params, &p) != nil || p.Delta == "" {
			return
		}
		c.events.emit(AgentEvent{Type: EventThinkingDelta, Text: p.Delta})

	case "error":
		var p struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(params, &p)
		msg := ""
		if p.Error != nil {
			msg = p.Error.Message
		}
		c.events.emit(AgentEvent{Type: EventError, Error: firstNonEmpty(msg, "codex error"), Status: "error"})
	}
}

type appItem struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	Text             string          `json:"text"`
	Summary          string          `json:"summary"`
	Content          string          `json:"content"`
	Command          string          `json:"command"`
	AggregatedOutput string          `json:"aggregatedOutput"`
	Status           string          `json:"status"`
	Error            string          `json:"error"`
	Result           string          `json:"result"`
	Query            string          `json:"query"`
	Tool             string          `json:"tool"`
	ToolName         string          `json:"toolName"`
	Server           string          `json:"server"`
	Name             string          `json:"name"`
	Arguments        json.RawMessage `json:"arguments"`
	Changes          []struct {
		Path string `json:"path"`
	} `json:"changes"`
}

func (c *Codex) emitItemLifecycle(item appItem, started bool) {
	switch item.Type {
	case "agentMessage":
		if !started && item.Text != "" {
			c.events.emit(AgentEvent{Type: EventMessage, Role: "assistant", Kind: "text", Text: item.Text})
		}
	case "reasoning":
		if !started {
			text := firstNonEmpty(item.Summary, item.Text, item.Content)
			if text != "" {
				c.events.emit(AgentEvent{Type: EventMessage, Role: "assistant", Kind: "thinking", Text: text})
			}
		}
	case "commandExecution":
		if started {
			c.events.emit(AgentEvent{Type: EventToolStart, ToolCallID: item.ID, ToolName: "shell", ToolInput: truncateStr(item.Command, 4096)})
		} else {
			c.events.emit(AgentEvent{Type: EventToolEnd, ToolCallID: item.ID, ToolResult: truncateStr(item.AggregatedOutput, 8192)})
		}
	case "mcpToolCall":
		if started {
			input := ""
			if len(item.Arguments) > 0 {
				input = truncateStr(string(item.Arguments), 4096)
			}
			c.events.emit(AgentEvent{Type: EventToolStart, ToolCallID: item.ID, ToolName: firstNonEmpty(item.Tool, item.ToolName, item.Server, "mcp tool"), ToolInput: input})
		} else {
			c.events.emit(AgentEvent{Type: EventToolEnd, ToolCallID: item.ID, ToolResult: truncateStr(firstNonEmpty(item.Result, item.Error, item.AggregatedOutput), 8192)})
		}
	case "dynamicToolCall":
		if started {
			c.events.emit(AgentEvent{Type: EventToolStart, ToolCallID: item.ID, ToolName: firstNonEmpty(item.Tool, item.ToolName, "dynamic tool"), ToolInput: string(item.Arguments)})
		} else {
			c.events.emit(AgentEvent{Type: EventToolEnd, ToolCallID: item.ID, ToolResult: truncateStr(firstNonEmpty(item.Result, item.Error), 8192)})
		}
	case "webSearch":
		if started {
			c.events.emit(AgentEvent{Type: EventToolStart, ToolCallID: item.ID, ToolName: "web_search", ToolInput: truncateStr(item.Query, 2048)})
		} else {
			c.events.emit(AgentEvent{Type: EventToolEnd, ToolCallID: item.ID, ToolResult: ""})
		}
	case "fileChange":
		if !started {
			var paths []string
			for _, ch := range item.Changes {
				if ch.Path != "" {
					paths = append(paths, ch.Path)
				}
			}
			if len(paths) > 0 {
				c.events.emit(AgentEvent{Type: EventFileChange, Paths: paths})
			}
		}
	case "error":
		if !started {
			text := firstNonEmpty(item.Text, item.Error, "codex error")
			c.events.emit(AgentEvent{Type: EventMessage, Role: "assistant", Kind: "meta", Text: text})
		}
	}
}

func (c *Codex) usageIn() int64    { c.usageMu.Lock(); defer c.usageMu.Unlock(); if c.usage == nil { return 0 }; return c.usage.InputTokens + c.usage.CachedInput + c.usage.CacheWrite }
func (c *Codex) usageOut() int64   { c.usageMu.Lock(); defer c.usageMu.Unlock(); if c.usage == nil { return 0 }; return c.usage.OutputTokens }
func (c *Codex) usageCached() int64 { c.usageMu.Lock(); defer c.usageMu.Unlock(); if c.usage == nil { return 0 }; return c.usage.CachedInput }
func (c *Codex) usageReason() int64 { c.usageMu.Lock(); defer c.usageMu.Unlock(); if c.usage == nil { return 0 }; return c.usage.ReasonTokens }

func (c *Codex) Send(prompt string) error {
	return c.SendWithOptions(prompt, TurnOptions{Model: c.opts.Model, ReasoningEffort: c.opts.ReasoningEffort})
}

func (c *Codex) SendWithOptions(prompt string, options TurnOptions) error {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return fmt.Errorf("session stopped")
	}
	if c.inFlight {
		c.mu.Unlock()
		return ErrBusy
	}
	c.inFlight = true
	threadID := c.threadID
	c.mu.Unlock()

	input := []map[string]any{{"type": "text", "text": prompt}}
	params := map[string]any{"threadId": threadID, "input": input}
	if options.Model != "" {
		params["model"] = options.Model
	}
	if options.ReasoningEffort != "" {
		params["effort"] = options.ReasoningEffort
	}
	var res struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := c.rpcCall("turn/start", params, &res); err != nil {
		c.mu.Lock()
		c.inFlight = false
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.turnID = res.Turn.ID
	c.mu.Unlock()
	return nil
}

func (c *Codex) Interrupt() error {
	c.mu.Lock()
	threadID := c.threadID
	turnID := c.turnID
	c.mu.Unlock()
	if turnID == "" {
		return nil
	}
	return c.rpcCall("turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID}, nil)
}

func (c *Codex) Stop() error {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return nil
	}
	c.stopped = true
	p := c.proc
	c.mu.Unlock()
	if p != nil {
		p.kill()
	}
	return nil
}

func (c *Codex) Events() <-chan AgentEvent { return c.events.receiver() }
func (c *Codex) Done() <-chan struct{}     { return c.done }

func (c *Codex) PID() int {
	return int(atomic.LoadInt32(&c.pid))
}

func (c *Codex) OutputTail(n int) string {
	c.mu.Lock()
	p := c.proc
	c.mu.Unlock()
	if p == nil {
		return ""
	}
	return p.outRing.Tail(n) + "\n-- stderr --\n" + p.stderr.Tail(n)
}

// ---------- legacy exec JSONL parsing (kept for tests) ----------

type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Command   string          `json:"command"`
		Status    string          `json:"status"`
		Path      string          `json:"path"`
		Changes   json.RawMessage `json:"changes"`
		Output    string          `json:"aggregated_output"`
		Title     string          `json:"title"`
		Query     string          `json:"query"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"item"`
	Usage struct {
		InputTokens  int64 `json:"input_tokens"`
		CachedInput  int64 `json:"cached_input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
	Message string `json:"message"`
}

func ParseCodexLine(line string) []AgentEvent {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return nil
	}
	var e codexEvent
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		return []AgentEvent{{Type: EventError, Error: truncateStr("unparseable codex line: "+err.Error(), 200)}}
	}
	switch e.Type {
	case "thread.started":
		if e.ThreadID != "" {
			return []AgentEvent{{Type: EventProviderID, ProviderSessionID: e.ThreadID}}
		}
	case "turn.started":
		return []AgentEvent{{Type: EventStatus, Status: "running"}}
	case "turn.completed":
		return []AgentEvent{{
			Type: EventResult, Status: "idle",
			TokensIn: e.Usage.InputTokens, TokensOut: e.Usage.OutputTokens,
		}}
	case "turn.failed":
		msg := firstNonEmpty(e.Message, "turn failed")
		return []AgentEvent{{Type: EventError, Error: msg, Status: "error"}}
	case "error":
		return []AgentEvent{{Type: EventError, Error: firstNonEmpty(e.Message, "unknown error")}}
	case "item.started":
		switch e.Item.Type {
		case "command_execution":
			return []AgentEvent{{
				Type: EventToolStart, ToolCallID: e.Item.ID, ToolName: "shell",
				ToolInput: truncateStr(e.Item.Command, 4096),
			}}
		case "mcp_tool_call":
			input := ""
			if len(e.Item.Arguments) > 0 {
				input = truncateStr(string(e.Item.Arguments), 4096)
			}
			return []AgentEvent{{
				Type: EventToolStart, ToolCallID: e.Item.ID,
				ToolName: firstNonEmpty(e.Item.Name, "mcp tool"), ToolInput: input,
			}}
		case "web_search":
			return []AgentEvent{{
				Type: EventToolStart, ToolCallID: e.Item.ID, ToolName: "web_search",
				ToolInput: truncateStr(e.Item.Query, 2048),
			}}
		}
	case "item.completed":
		switch e.Item.Type {
		case "agent_message":
			if e.Item.Text != "" {
				return []AgentEvent{{Type: EventMessage, Role: "assistant", Kind: "text", Text: e.Item.Text}}
			}
		case "reasoning":
			title := firstNonEmpty(e.Item.Title, e.Item.Text, "reasoning")
			return []AgentEvent{{Type: EventMessage, Role: "assistant", Kind: "thinking", Text: title}}
		case "command_execution":
			return []AgentEvent{{
				Type: EventToolEnd, ToolCallID: e.Item.ID,
				ToolResult: truncateStr(e.Item.Output, 8192),
			}}
		case "file_change":
			var paths []string
			if len(e.Item.Changes) > 0 {
				var changes []map[string]any
				if err := json.Unmarshal(e.Item.Changes, &changes); err == nil {
					for _, ch := range changes {
						if p, ok := ch["path"].(string); ok {
							paths = append(paths, p)
						}
					}
				}
			}
			if e.Item.Path != "" {
				paths = append(paths, e.Item.Path)
			}
			if len(paths) > 0 {
				return []AgentEvent{{Type: EventFileChange, Paths: paths}}
			}
		case "mcp_tool_call":
			return []AgentEvent{{
				Type: EventToolEnd, ToolCallID: e.Item.ID,
				ToolResult: truncateStr(firstNonEmpty(e.Item.Output, e.Item.Text), 8192),
			}}
		default:
			text := firstNonEmpty(e.Item.Text, e.Item.Title, e.Item.Query)
			if text != "" && e.Item.Type != "todo_list" {
				return []AgentEvent{{Type: EventMessage, Role: "assistant", Kind: "meta", Text: text}}
			}
		}
	}
	return nil
}
