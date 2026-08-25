package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type Provider string

const (
	ProviderClaude   Provider = "claude"
	ProviderCodex    Provider = "codex"
	ProviderOpencode Provider = "opencode"
)

type EventType string

const (
	EventStatus        EventType = "status"
	EventDelta         EventType = "delta"
	EventThinkingDelta EventType = "thinking_delta"
	EventMessage       EventType = "message"
	EventToolStart     EventType = "tool_start"
	EventToolEnd       EventType = "tool_end"
	EventFileChange    EventType = "file_change"
	EventResult        EventType = "result"
	EventError         EventType = "error"
	EventProviderID    EventType = "provider_session"
	EventPartUpsert    EventType = "part_upsert"
)

type AgentEvent struct {
	SessionID         string    `json:"sessionId,omitempty"`
	Type              EventType `json:"type"`
	Status            string    `json:"status,omitempty"`
	Role              string    `json:"role,omitempty"`
	Kind              string    `json:"kind,omitempty"`
	Text              string    `json:"text,omitempty"`
	PartID            string    `json:"partId,omitempty"`
	ToolCallID        string    `json:"toolCallId,omitempty"`
	ToolName          string    `json:"toolName,omitempty"`
	ToolInput         string    `json:"toolInput,omitempty"`
	ToolResult        string    `json:"toolResult,omitempty"`
	Paths             []string  `json:"paths,omitempty"`
	ProviderSessionID string    `json:"providerSessionId,omitempty"`
	Error             string    `json:"error,omitempty"`
	CostUSD           float64   `json:"costUsd,omitempty"`
	TokensIn          int64     `json:"tokensIn,omitempty"`
	TokensOut         int64     `json:"tokensOut,omitempty"`
	DurationMS        int64     `json:"durationMs,omitempty"`
	Ts                int64     `json:"ts"`
}

func stamp(ev *AgentEvent) {
	if ev.Ts == 0 {
		ev.Ts = time.Now().UnixMilli()
	}
}

type TurnOptions struct {
	Model           string
	ReasoningEffort string
	FastMode        bool
}

type Options struct {
	WorktreePath         string
	Model                string
	ReasoningEffort      string
	FastMode             bool
	ClaudePermissionMode string
	CodexSandbox         string
	ClaudePath           string
	CodexPath            string
	OpencodePath         string
	ResumeProviderID     string
	OpenCodeServer       *OpenCodeServer
	AppConfigDir         string
}

type TurnSender interface {
	SendWithOptions(prompt string, options TurnOptions) error
}

var ErrBusy = errors.New("agent is busy")

type Adapter interface {
	Start(ctx context.Context) error
	Send(prompt string) error
	Interrupt() error
	Stop() error
	Events() <-chan AgentEvent
	Done() <-chan struct{}
}

func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type eventChan struct {
	mu sync.Mutex
	ch chan AgentEvent
}

func newEventChan() *eventChan {
	return &eventChan{ch: make(chan AgentEvent, 1024)}
}

func (e *eventChan) emit(ev AgentEvent) {
	stamp(&ev)
	select {
	case e.ch <- ev:
	default:
		select {
		case <-e.ch:
		default:
		}
		select {
		case e.ch <- ev:
		default:
		}
	}
}

func (e *eventChan) receiver() <-chan AgentEvent { return e.ch }
