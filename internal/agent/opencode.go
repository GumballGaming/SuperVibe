package agent

import (
	"context"
	"encoding/json"
	"os"
	"sync"
)

func environ() []string { return os.Environ() }

type ocEnvelope struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

type ocPart struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionID"`
	MessageID string          `json:"messageID"`
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Tool      string          `json:"tool"`
	State     json.RawMessage `json:"state"`
}

type ocMessageInfo struct {
	ID        string  `json:"id"`
	SessionID string  `json:"sessionID"`
	Role      string  `json:"role"`
	Cost      float64 `json:"cost"`
	Tokens    struct {
		Input  int64 `json:"input"`
		Output int64 `json:"output"`
	} `json:"tokens"`
	Time struct {
		Completed float64 `json:"completed"`
	} `json:"time"`
}

func ParseOpenCodeEvent(raw []byte) []AgentEvent {
	var env ocEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	switch env.Type {
	case "message.part.updated":
		var props struct {
			Part  ocPart `json:"part"`
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(env.Properties, &props); err != nil {
			return nil
		}
		p := props.Part
		if p.SessionID == "" {
			return nil
		}
		switch p.Type {
		case "text":
			if props.Delta != "" {
				return []AgentEvent{{SessionID: p.SessionID, Type: EventDelta, Role: "assistant", Text: props.Delta}}
			}
			if p.Text == "" {
				return nil
			}
			return []AgentEvent{{SessionID: p.SessionID, Type: EventPartUpsert, PartID: p.ID, Role: "assistant", Kind: "text", Text: p.Text}}
		case "reasoning":
			if props.Delta != "" {
				return []AgentEvent{{SessionID: p.SessionID, Type: EventThinkingDelta, Text: props.Delta}}
			}
			return []AgentEvent{{SessionID: p.SessionID, Type: EventPartUpsert, PartID: p.ID, Role: "assistant", Kind: "thinking", Text: p.Text}}
		case "tool":
			ev := AgentEvent{
				SessionID: p.SessionID, Type: EventPartUpsert, PartID: p.ID,
				Role: "assistant", Kind: "tool",
				ToolName: firstNonEmpty(p.Tool, "tool"),
			}
			if len(p.State) > 0 {
				var st struct {
					Status string          `json:"status"`
					Input  json.RawMessage `json:"input"`
					Output string          `json:"output"`
				}
				if json.Unmarshal(p.State, &st) == nil {
					if len(st.Input) > 0 && string(st.Input) != "null" {
						ev.ToolInput = truncateStr(string(st.Input), 4096)
					}
					if st.Output != "" {
						ev.ToolResult = truncateStr(st.Output, 8192)
					}
				}
			}
			return []AgentEvent{ev}
		}
		return nil
	case "message.updated":
		var props struct {
			Info ocMessageInfo `json:"info"`
		}
		if err := json.Unmarshal(env.Properties, &props); err != nil {
			return nil
		}
		info := props.Info
		if info.SessionID == "" || info.Role != "assistant" {
			return nil
		}
		if info.Time.Completed > 0 {
			return []AgentEvent{{
				SessionID: info.SessionID, Type: EventResult, Status: "idle",
				CostUSD: info.Cost, TokensIn: info.Tokens.Input, TokensOut: info.Tokens.Output,
			}}
		}
		return []AgentEvent{{SessionID: info.SessionID, Type: EventStatus, Status: "running"}}
	case "permission.updated", "permission.asked":
		var props struct {
			SessionID string `json:"sessionID"`
		}
		_ = json.Unmarshal(env.Properties, &props)
		if props.SessionID == "" {
			return nil
		}
		return []AgentEvent{{SessionID: props.SessionID, Type: EventStatus, Status: "waiting"}}
	case "session.error":
		var props struct {
			SessionID string `json:"sessionID"`
			Error     struct {
				Name    string `json:"name"`
				Message string `json:"data"`
			} `json:"error"`
		}
		_ = json.Unmarshal(env.Properties, &props)
		if props.SessionID == "" {
			return nil
		}
		msg := firstNonEmpty(props.Error.Message, props.Error.Name, "unknown provider error")
		return []AgentEvent{{SessionID: props.SessionID, Type: EventError, Error: msg, Status: "error"}}
	}
	return nil
}

type Opencode struct {
	srv       *OpenCodeServer
	opts      Options
	events    *eventChan
	done      chan struct{}
	mu        sync.Mutex
	sid       string
	sub       <-chan AgentEvent
	unsub     func()
	cancelled bool
}

func NewOpencode(opts Options) *Opencode {
	return &Opencode{srv: opts.OpenCodeServer, opts: opts, events: newEventChan(), done: make(chan struct{})}
}

func (o *Opencode) Start(ctx context.Context) error {
	if o.srv == nil {
		return context.Canceled
	}
	sid, err := o.srv.CreateSession()
	if err != nil {
		return err
	}
	o.mu.Lock()
	o.sid = sid
	o.mu.Unlock()
	o.events.emit(AgentEvent{Type: EventProviderID, ProviderSessionID: sid})
	sub, unsub := o.srv.Subscribe(sid)
	o.mu.Lock()
	o.sub = sub
	o.unsub = unsub
	o.mu.Unlock()
	go func() {
		defer close(o.done)
		for ev := range sub {
			o.events.emit(ev)
		}
	}()
	return nil
}

func (o *Opencode) sessionID() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.sid
}

func (o *Opencode) Send(prompt string) error {
	return o.SendWithOptions(prompt, TurnOptions{Model: o.opts.Model, ReasoningEffort: o.opts.ReasoningEffort})
}

func (o *Opencode) SendWithOptions(prompt string, options TurnOptions) error {
	return o.srv.PromptAsyncWithOptions(o.sessionID(), prompt, options)
}

func (o *Opencode) Interrupt() error {
	return o.srv.Abort(o.sessionID())
}

func (o *Opencode) Stop() error {
	o.mu.Lock()
	if o.cancelled {
		o.mu.Unlock()
		return nil
	}
	o.cancelled = true
	unsub := o.unsub
	o.mu.Unlock()
	_ = o.Interrupt()
	if unsub != nil {
		unsub()
	}
	return nil
}

func (o *Opencode) Events() <-chan AgentEvent { return o.events.receiver() }
func (o *Opencode) Done() <-chan struct{}     { return o.done }
