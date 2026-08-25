package agent

import (
	"strings"
	"testing"
)

func TestParseCodexLifecycle(t *testing.T) {
	ev := ParseCodexLine(`{"type":"thread.started","thread_id":"0199a213"}`)
	if len(ev) != 1 || ev[0].Type != EventProviderID || ev[0].ProviderSessionID != "0199a213" {
		t.Fatalf("thread.started: %+v", ev)
	}
	ev = ParseCodexLine(`{"type":"turn.started"}`)
	if len(ev) != 1 || ev[0].Type != EventStatus || ev[0].Status != "running" {
		t.Fatalf("turn.started: %+v", ev)
	}
	ev = ParseCodexLine(`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"bash -lc ls","status":"in_progress"}}`)
	if len(ev) != 1 || ev[0].Type != EventToolStart || ev[0].ToolCallID != "item_1" ||
		ev[0].ToolName != "shell" || !strings.Contains(ev[0].ToolInput, "ls") {
		t.Fatalf("item started: %+v", ev)
	}
	ev = ParseCodexLine(`{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Repo contains docs."}}`)
	if len(ev) != 1 || ev[0].Type != EventMessage || ev[0].Text != "Repo contains docs." {
		t.Fatalf("agent message: %+v", ev)
	}
	ev = ParseCodexLine(`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","aggregated_output":"docs\nsdk"}}`)
	if len(ev) != 1 || ev[0].Type != EventToolEnd || ev[0].ToolCallID != "item_1" {
		t.Fatalf("tool end: %+v", ev)
	}
	ev = ParseCodexLine(`{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122}}`)
	if len(ev) != 1 || ev[0].Type != EventResult || ev[0].TokensIn != 24763 || ev[0].TokensOut != 122 || ev[0].Status != "idle" {
		t.Fatalf("turn completed: %+v", ev)
	}
}

func TestParseCodexFileChange(t *testing.T) {
	ev := ParseCodexLine(`{"type":"item.completed","item":{"id":"i5","type":"file_change","changes":[{"path":"src/a.go"},{"path":"src/b.go"}]}}`)
	if len(ev) != 1 || ev[0].Type != EventFileChange || len(ev[0].Paths) != 2 {
		t.Fatalf("file change: %+v", ev)
	}
}

func TestParseCodexFailures(t *testing.T) {
	ev := ParseCodexLine(`{"type":"turn.failed","message":"quota exceeded"}`)
	if len(ev) != 1 || ev[0].Type != EventError || ev[0].Error != "quota exceeded" {
		t.Fatalf("turn failed: %+v", ev)
	}
	ev = ParseCodexLine(`{"type":"error","message":"bad thing"}`)
	if len(ev) != 1 || ev[0].Error != "bad thing" {
		t.Fatalf("error: %+v", ev)
	}
	if ev := ParseCodexLine("garbage line"); ev != nil {
		t.Fatalf("non-json should be nil")
	}
}

func TestParseCodexReasoning(t *testing.T) {
	ev := ParseCodexLine(`{"type":"item.completed","item":{"id":"i7","type":"reasoning","title":"Scanning repo"}}`)
	if len(ev) != 1 || ev[0].Kind != "thinking" || ev[0].Text != "Scanning repo" {
		t.Fatalf("reasoning: %+v", ev)
	}
}
