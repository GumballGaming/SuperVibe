package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type Claude struct {
	opts    Options
	proc    *proc
	events  *eventChan
	done    chan struct{}
	mu      sync.Mutex
	stopped bool
}

func NewClaude(opts Options) *Claude {
	return &Claude{opts: opts, events: newEventChan(), done: make(chan struct{})}
}

func (c *Claude) Start(ctx context.Context) error {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--input-format", "stream-json",
	}
	mode := c.opts.ClaudePermissionMode
	if mode == "" {
		mode = "acceptEdits"
	}
	args = append(args, "--permission-mode", mode)
	if c.opts.Model != "" {
		args = append(args, "--model", c.opts.Model)
	}
	if c.opts.ReasoningEffort != "" {
		args = append(args, "--effort", c.opts.ReasoningEffort)
	}
	if c.opts.FastMode {
		args = append(args, "--settings", `{"fastMode":true}`)
	}
	if c.opts.ResumeProviderID != "" {
		args = append(args, "--resume", c.opts.ResumeProviderID)
	}
	p, err := startProc(ctx, c.opts.ClaudePath, args, c.opts.WorktreePath)
	if err != nil {
		return err
	}
	c.proc = p
	go func() {
		defer close(c.done)
		scanLines(p.stdout, func(line string) {
			for _, ev := range ParseClaudeLine(line) {
				c.events.emit(ev)
			}
			p.outRing.Write([]byte(line + "\n"))
		})
		_ = p.cmd.Wait()
	}()
	return nil
}

func (c *Claude) Send(prompt string) error {
	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]any{{"type": "text", "text": prompt}},
		},
		"parent_tool_use_id": nil,
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || c.proc == nil {
		return fmt.Errorf("session not running")
	}
	return c.proc.writeLine(string(b))
}

func (c *Claude) SendWithOptions(prompt string, _ TurnOptions) error {
	return c.Send(prompt)
}

func (c *Claude) Interrupt() error { return nil }

func (c *Claude) Stop() error {
	c.mu.Lock()
	c.stopped = true
	p := c.proc
	c.mu.Unlock()
	if p != nil {
		p.kill()
	}
	return nil
}

func (c *Claude) Events() <-chan AgentEvent { return c.events.receiver() }
func (c *Claude) Done() <-chan struct{}     { return c.done }

func (c *Claude) PID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != nil && c.proc.cmd.Process != nil {
		return c.proc.cmd.Process.Pid
	}
	return 0
}

func (c *Claude) OutputTail(n int) string {
	if c.proc == nil {
		return ""
	}
	return c.proc.outRing.Tail(n) + "\n-- stderr --\n" + c.proc.stderr.Tail(n)
}

type claudeStreamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Session string `json:"session_id"`
	Event   struct {
		Type  string `json:"type"`
		Delta struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	} `json:"event"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Result      string  `json:"result"`
	IsError     bool    `json:"is_error"`
	TotalCost   float64 `json:"total_cost_usd"`
	DurationMS  int64   `json:"duration_ms"`
	NumTurns    int64   `json:"num_turns"`
	ErrorString string  `json:"error"`
	Usage       struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

func ParseClaudeLine(line string) []AgentEvent {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return nil
	}
	var m claudeStreamEvent
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return []AgentEvent{{Type: EventError, Error: truncateStr("unparseable stream line: "+err.Error(), 200)}}
	}
	switch m.Type {
	case "system":
		if m.Subtype == "init" && m.Session != "" {
			return []AgentEvent{{Type: EventProviderID, ProviderSessionID: m.Session}}
		}
	case "stream_event":
		switch m.Event.Delta.Type {
		case "text_delta":
			if m.Event.Delta.Text != "" {
				return []AgentEvent{{Type: EventDelta, Role: "assistant", Text: m.Event.Delta.Text}}
			}
		case "thinking_delta":
			if m.Event.Delta.Thinking != "" {
				return []AgentEvent{{Type: EventThinkingDelta, Text: m.Event.Delta.Thinking}}
			}
		}
	case "assistant":
		var blocks []claudeBlock
		if err := json.Unmarshal(m.Message.Content, &blocks); err != nil {
			return nil
		}
		var out []AgentEvent
		for _, b := range blocks {
			switch b.Type {
			case "text":
				if b.Text != "" {
					out = append(out, AgentEvent{Type: EventMessage, Role: "assistant", Kind: "text", Text: b.Text})
				}
			case "thinking":
				out = append(out, AgentEvent{Type: EventMessage, Role: "assistant", Kind: "thinking", Text: b.Thinking})
			case "tool_use":
				out = append(out, AgentEvent{
					Type: EventToolStart, ToolCallID: b.ID, ToolName: b.Name,
					ToolInput: truncateStr(string(b.Input), 8192),
				})
			}
		}
		return out
	case "user":
		var blocks []claudeBlock
		if err := json.Unmarshal(m.Message.Content, &blocks); err != nil {
			return nil
		}
		var out []AgentEvent
		for _, b := range blocks {
			if b.Type == "tool_result" {
				out = append(out, AgentEvent{
					Type: EventToolEnd, ToolCallID: b.ToolUseID,
					ToolResult: truncateStr(claudeResultText(b.Content), 8192),
				})
			}
		}
		return out
	case "result":
		ev := AgentEvent{
			Type:       EventResult,
			Text:       m.Result,
			CostUSD:    m.TotalCost,
			TokensIn:   m.Usage.InputTokens,
			TokensOut:  m.Usage.OutputTokens,
			DurationMS: m.DurationMS,
		}
		if m.IsError {
			ev.Status = "error"
			ev.Error = firstNonEmpty(m.ErrorString, m.Result, "agent run failed")
		} else {
			ev.Status = "idle"
		}
		return []AgentEvent{ev}
	}
	return nil
}

func claudeResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []claudeBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	return string(raw)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
