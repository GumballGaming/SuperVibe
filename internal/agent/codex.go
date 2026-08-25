package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type Codex struct {
	opts    Options
	proc    *proc
	events  *eventChan
	done    chan struct{}
	mu      sync.Mutex
	stopped bool
	running bool
}

func NewCodex(opts Options) *Codex {
	return &Codex{opts: opts, events: newEventChan(), done: make(chan struct{})}
}

func (c *Codex) Start(ctx context.Context) error { return nil }

func (c *Codex) Send(prompt string) error {
	return c.SendWithOptions(prompt, TurnOptions{Model: c.opts.Model, ReasoningEffort: c.opts.ReasoningEffort})
}

func (c *Codex) SendWithOptions(prompt string, options TurnOptions) error {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return fmt.Errorf("session stopped")
	}
	if c.running {
		c.mu.Unlock()
		return ErrBusy
	}
	c.running = true
	c.mu.Unlock()

	args := []string{"exec", "--json", "--skip-git-repo-check"}
	if options.Model != "" {
		args = append(args, "--model", options.Model)
	}
	if options.ReasoningEffort != "" {
		args = append(args, "--config", "model_reasoning_effort="+options.ReasoningEffort)
	}
	if options.FastMode {
		args = append(args, "--config", `service_tier="fast"`)
	}
	sandbox := c.opts.CodexSandbox
	if sandbox == "" {
		sandbox = "workspace-write"
	}
	args = append(args, "--sandbox", sandbox)
	if c.opts.ResumeProviderID != "" {
		args = append(args, "resume", "--last")
	}
	args = append(args, prompt)

	p, err := startProc(context.Background(), c.opts.CodexPath, args, c.opts.WorktreePath)
	if err != nil {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.proc = p
	done := c.done
	c.mu.Unlock()

	go func() {
		defer func() {
			_ = p.cmd.Wait()
			c.events.emit(AgentEvent{Type: EventStatus, Status: "turn_end"})
			c.mu.Lock()
			c.running = false
			close(done)
			newDone := make(chan struct{})
			c.done = newDone
			c.mu.Unlock()
		}()
		scanLines(p.stdout, func(line string) {
			for _, ev := range ParseCodexLine(line) {
				c.events.emit(ev)
			}
			p.outRing.Write([]byte(line + "\n"))
		})
	}()
	return nil
}

func (c *Codex) Interrupt() error {
	c.mu.Lock()
	p := c.proc
	c.mu.Unlock()
	if p != nil {
		p.kill()
	}
	return nil
}

func (c *Codex) Stop() error {
	c.mu.Lock()
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
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != nil && c.proc.cmd.Process != nil {
		return c.proc.cmd.Process.Pid
	}
	return 0
}

func (c *Codex) OutputTail(n int) string {
	if c.proc == nil {
		return ""
	}
	return c.proc.outRing.Tail(n) + "\n-- stderr --\n" + c.proc.stderr.Tail(n)
}

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
