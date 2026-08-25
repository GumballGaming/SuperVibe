package agent

import (
	"strings"
	"testing"
)

func TestParseClaudeInitAndDelta(t *testing.T) {
	events := ParseClaudeLine(`{"type":"system","subtype":"init","session_id":"abc-123","model":"sonnet"}`)
	if len(events) != 1 || events[0].Type != EventProviderID || events[0].ProviderSessionID != "abc-123" {
		t.Fatalf("init parse: %+v", events)
	}

	events = ParseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}}`)
	if len(events) != 1 || events[0].Type != EventDelta || events[0].Text != "Hel" {
		t.Fatalf("text delta: %+v", events)
	}

	events = ParseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm"}}}`)
	if len(events) != 1 || events[0].Type != EventThinkingDelta || events[0].Text != "hmm" {
		t.Fatalf("thinking delta: %+v", events)
	}
}

func TestParseClaudeAssistantBlocks(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[
		{"type":"thinking","thinking":"plan"},
		{"type":"text","text":"doing it"},
		{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}
	]}}`
	events := ParseClaudeLine(line)
	if len(events) != 3 {
		t.Fatalf("expected 3 events got %d: %+v", len(events), events)
	}
	if events[0].Kind != "thinking" || events[0].Text != "plan" {
		t.Fatalf("thinking block: %+v", events[0])
	}
	if events[1].Type != EventMessage || events[1].Text != "doing it" {
		t.Fatalf("text block: %+v", events[1])
	}
	if events[2].Type != EventToolStart || events[2].ToolCallID != "toolu_1" || events[2].ToolName != "Bash" {
		t.Fatalf("tool block: %+v", events[2])
	}
}

func TestParseClaudeToolResultAndResult(t *testing.T) {
	events := ParseClaudeLine(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"file1\nfile2"}]}}`)
	if len(events) != 1 || events[0].Type != EventToolEnd || events[0].ToolCallID != "toolu_1" ||
		!strings.Contains(events[0].ToolResult, "file2") {
		t.Fatalf("tool result: %+v", events)
	}

	events = ParseClaudeLine(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"block text"}]}]}}`)
	if len(events) != 1 || events[0].ToolResult != "block text" {
		t.Fatalf("tool result blocks: %+v", events)
	}

	events = ParseClaudeLine(`{"type":"result","subtype":"success","result":"all done","is_error":false,"total_cost_usd":0.25,"duration_ms":1500,"usage":{"input_tokens":10,"output_tokens":20}}`)
	if len(events) != 1 || events[0].Type != EventResult || events[0].Text != "all done" ||
		events[0].Status != "idle" || events[0].CostUSD != 0.25 || events[0].TokensIn != 10 || events[0].TokensOut != 20 {
		t.Fatalf("result: %+v", events)
	}

	events = ParseClaudeLine(`{"type":"result","subtype":"error_during_execution","is_error":true,"error":"boom"}`)
	if len(events) != 1 || events[0].Status != "error" || events[0].Error != "boom" {
		t.Fatalf("error result: %+v", events)
	}
}

func TestParseClaudeGarbage(t *testing.T) {
	if ev := ParseClaudeLine(""); ev != nil {
		t.Fatalf("empty line should be nil")
	}
	if ev := ParseClaudeLine("not json"); ev != nil {
		t.Fatalf("non-json should be nil, got %+v", ev)
	}
	ev := ParseClaudeLine(`{invalid json}`)
	if len(ev) != 1 || ev[0].Type != EventError {
		t.Fatalf("bad json should produce error event: %+v", ev)
	}
}

func TestBuildUserMessage(t *testing.T) {
	c := NewClaude(Options{})
	if err := c.Send("hello"); err == nil {
		t.Fatal("send before start should fail")
	}
}
