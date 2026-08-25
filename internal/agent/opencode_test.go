package agent

import (
	"strings"
	"testing"
)

func TestParseOpenCodeDelta(t *testing.T) {
	raw := []byte(`{"type":"message.part.updated","properties":{"part":{"id":"prt_1","sessionID":"ses_1","type":"text"},"delta":"Hello "}}`)
	ev := ParseOpenCodeEvent(raw)
	if len(ev) != 1 || ev[0].Type != EventDelta || ev[0].SessionID != "ses_1" || ev[0].Text != "Hello " {
		t.Fatalf("delta: %+v", ev)
	}
}

func TestParseOpenCodePartUpsert(t *testing.T) {
	raw := []byte(`{"type":"message.part.updated","properties":{"part":{"id":"prt_1","sessionID":"ses_1","type":"text","text":"full text"}}}`)
	ev := ParseOpenCodeEvent(raw)
	if len(ev) != 1 || ev[0].Type != EventPartUpsert || ev[0].PartID != "prt_1" || ev[0].Text != "full text" {
		t.Fatalf("upsert: %+v", ev)
	}

	raw = []byte(`{"type":"message.part.updated","properties":{"part":{"id":"prt_2","sessionID":"ses_1","type":"reasoning","text":"thought"}}}`)
	ev = ParseOpenCodeEvent(raw)
	if len(ev) != 1 || ev[0].Kind != "thinking" || ev[0].Text != "thought" {
		t.Fatalf("thinking upsert: %+v", ev)
	}
}

func TestParseOpenCodeTool(t *testing.T) {
	raw := []byte(`{"type":"message.part.updated","properties":{"part":{"id":"prt_3","sessionID":"ses_1","type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"ls"},"output":"a.go\nb.go"}}}}`)
	ev := ParseOpenCodeEvent(raw)
	if len(ev) != 1 || ev[0].Type != EventPartUpsert || ev[0].Kind != "tool" ||
		ev[0].ToolName != "bash" || !strings.Contains(ev[0].ToolResult, "b.go") {
		t.Fatalf("tool: %+v", ev)
	}
}

func TestParseOpenCodeMessageCompleted(t *testing.T) {
	raw := []byte(`{"type":"message.updated","properties":{"info":{"id":"msg_1","sessionID":"ses_1","role":"assistant","cost":0.02,"tokens":{"input":100,"output":50},"time":{"completed":1720000000000}}}}`)
	ev := ParseOpenCodeEvent(raw)
	if len(ev) != 1 || ev[0].Type != EventResult || ev[0].Status != "idle" ||
		ev[0].CostUSD != 0.02 || ev[0].TokensIn != 100 || ev[0].TokensOut != 50 {
		t.Fatalf("completed: %+v", ev)
	}

	raw = []byte(`{"type":"message.updated","properties":{"info":{"id":"msg_2","sessionID":"ses_1","role":"assistant","time":{}}}}`)
	ev = ParseOpenCodeEvent(raw)
	if len(ev) != 1 || ev[0].Type != EventStatus || ev[0].Status != "running" {
		t.Fatalf("running: %+v", ev)
	}
}

func TestParseOpenCodePermissionAndError(t *testing.T) {
	raw := []byte(`{"type":"permission.updated","properties":{"sessionID":"ses_1"}}`)
	ev := ParseOpenCodeEvent(raw)
	if len(ev) != 1 || ev[0].Status != "waiting" {
		t.Fatalf("permission: %+v", ev)
	}
	raw = []byte(`{"type":"session.error","properties":{"sessionID":"ses_1","error":{"name":"ProviderAuthError","data":"bad key"}}}`)
	ev = ParseOpenCodeEvent(raw)
	if len(ev) != 1 || ev[0].Type != EventError || !strings.Contains(ev[0].Error, "bad key") {
		t.Fatalf("error: %+v", ev)
	}
}

func TestParseOpenCodeIgnoresUnknown(t *testing.T) {
	for _, raw := range []string{
		`{"type":"server.connected","properties":{}}`,
		`{"type":"server.heartbeat","properties":{}}`,
		`not json at all`,
		`{"type":"storage.write","properties":{"blob":"x"}}`,
	} {
		if ev := ParseOpenCodeEvent([]byte(raw)); len(ev) != 0 {
			t.Fatalf("%s should be ignored, got %+v", raw, ev)
		}
	}
}
