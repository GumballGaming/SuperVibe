package modelsx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCapabilitiesFor(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     Capabilities
	}{
		{
			name:     "claude",
			provider: "claude",
			want: Capabilities{
				Streaming: true, Tools: true, FileEdit: true, Shell: true,
				Images: true, MCP: true, Subagents: true, Resume: true,
				Usage: true, CostReport: true, ReasoningControls: true,
				NativeWebBrowse: true, ModelSelection: selectionFreeform,
			},
		},
		{
			name:     "codex",
			provider: "codex",
			want: Capabilities{
				Streaming: true, Tools: true, FileEdit: true, Shell: true,
				Images: true, MCP: true, Resume: true, Usage: true,
				ReasoningControls: true, NativeWebBrowse: true, ModelSelection: selectionFreeform,
			},
		},
		{
			name:     "opencode",
			provider: "opencode",
			want: Capabilities{
				Streaming: true, Tools: true, FileEdit: true, Shell: true,
				MCP: true, Resume: true, Usage: true, CostReport: true,
				ReasoningControls: true, NativeWebBrowse: true, ModelSelection: selectionDynamic,
			},
		},
		{
			name:     "unknown",
			provider: "mystery-cli",
			want:     Capabilities{ModelSelection: selectionNone},
		},
		{
			name:     "empty",
			provider: "",
			want:     Capabilities{ModelSelection: selectionNone},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CapabilitiesFor(tt.provider)
			if got != tt.want {
				t.Fatalf("CapabilitiesFor(%q) = %+v, want %+v", tt.provider, got, tt.want)
			}
		})
	}
}
func TestHasFastVariant(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"object", `{"fast":{"reasoningEffort":"low"}}`, true},
		{"array", `[{"id":"fast"}]`, true},
		{"disabled", `{"fast":{"disabled":true}}`, false},
		{"other", `{"deep":{"reasoningEffort":"high"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasFastVariant([]byte(tt.raw)); got != tt.want {
				t.Fatalf("hasFastVariant(%s) = %t, want %t", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSuggestionsFor(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantIDs  []string
	}{
		{"claude", "claude", []string{"claude-sonnet-5", "claude-opus-5", "claude-fable-5", "claude-haiku-4-5"}},
		{"codex", "codex", []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "gpt-5.4", "gpt-5.4-mini"}},
		{"opencode", "opencode", nil},
		{"unknown", "nope", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuggestionsFor(tt.provider)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("SuggestionsFor(%q) len = %d, want %d (%+v)", tt.provider, len(got), len(tt.wantIDs), got)
			}
			for i, m := range got {
				if m.ID != tt.wantIDs[i] {
					t.Fatalf("SuggestionsFor(%q)[%d].ID = %q, want %q", tt.provider, i, m.ID, tt.wantIDs[i])
				}
				if !m.Suggested {
					t.Fatalf("SuggestionsFor(%q)[%d] not marked Suggested", tt.provider, i)
				}
				if m.Provider != tt.provider {
					t.Fatalf("SuggestionsFor(%q)[%d].Provider = %q", tt.provider, i, m.Provider)
				}
			}
			if got := SuggestionsFor("never-heard-of-it"); got != nil {
				t.Fatalf("unknown provider suggestions = %+v, want nil", got)
			}
		})
	}
}
func TestCodexSuggestionLabels(t *testing.T) {
	want := []string{
		"GPT-5.6 Sol",
		"GPT-5.6 Terra",
		"GPT-5.6 Luna",
		"GPT-5.5",
		"GPT-5.4",
		"GPT-5.4 Mini",
	}
	got := SuggestionsFor("codex")
	if len(got) != len(want) {
		t.Fatalf("codex suggestion count = %d, want %d", len(got), len(want))
	}
	for i, model := range got {
		if model.Label != want[i] {
			t.Errorf("codex suggestion %d label = %q, want %q", i, model.Label, want[i])
		}
		if strings.Contains(strings.ToLower(model.Label), "fast") {
			t.Errorf("codex suggestion %d exposes Fast in label %q", i, model.Label)
		}
	}
}

func TestParseClaudeModels(t *testing.T) {
	got, err := parseClaudeModels("Current model: Opus 5 (default)\nUsage: /model <name>. Available: sonnet, opus, haiku, fable, best, sonnet[1m], opus[1m], opusplan, default, or a full model ID.")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude-sonnet-5", "claude-opus-5", "claude-fable-5", "claude-haiku-4-5"}
	if len(got) != len(want) {
		t.Fatalf("got %d models, want %d (%+v)", len(got), len(want), got)
	}
	for i, model := range got {
		if model.Provider != "claude" || model.ID != want[i] {
			t.Errorf("model %d = %+v, want provider claude and id %q", i, model, want[i])
		}
	}
	if got[0].Label != "Claude Sonnet 5" || got[2].Label != "Claude Fable 5" {
		t.Errorf("unexpected Claude labels: %+v", got)
	}
}

func TestDiscoverClaude(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Claude Code command fixture uses a Windows cmd script")
	}
	dir := t.TempDir()
	script := writeCmdScript(t, dir, "claude-models.cmd",
		"@echo off",
		`echo {"result":"Usage: /model. Available: sonnet, opus, default, or a full model ID.","is_error":false}`,
	)
	got, err := DiscoverClaude(context.Background(), script)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "claude-sonnet-5" || got[1].ID != "claude-opus-5" {
		t.Fatalf("got %+v, want claude-sonnet-5/claude-opus-5", got)
	}
}
func TestCodexReasoningEfforts(t *testing.T) {
	want56 := []string{"none", "low", "medium", "high", "xhigh", "max"}
	want55 := []string{"none", "low", "medium", "high", "xhigh"}
	want := map[string][]string{
		"gpt-5.6-sol":   want56,
		"gpt-5.6-terra": want56,
		"gpt-5.6-luna":  want56,
		"gpt-5.5":       want55,
		"gpt-5.4":       want55,
		"gpt-5.4-mini":  want55,
	}
	for _, model := range SuggestionsFor("codex") {
		if !reflect.DeepEqual(model.ReasoningEfforts, want[model.ID]) {
			t.Errorf("%s reasoning efforts = %#v, want %#v", model.ID, model.ReasoningEfforts, want[model.ID])
		}
	}
}

const ocProvidersArrayFixture = `[
	{"id":"anthropic","name":"Anthropic","models":{
		"claude-sonnet-4-5":{"id":"claude-sonnet-4-5","name":"Claude Sonnet 4.5","context_length":200000},
		"claude-opus-4-6":{"id":"claude-opus-4-6","name":"Claude Opus 4.6","context_length":"200000"}
	}},
	{"id":"openai","name":"OpenAI","models":{
		"gpt-5.6":{"id":"gpt-5.6","name":"GPT-5.6","context_length":400000,"variants":{"fast":{"reasoningEffort":"low"}}},
		"bare-model":"Bare Model Name"
	}},
	{"id":"broken","models":"not-an-object"},
	{"id":"emptymodels","models":null}
]`

var ocWantModels = []ModelInfo{
	{Provider: "anthropic", ID: "anthropic/claude-opus-4-6", Label: "Claude Opus 4.6", ContextWindow: 200000},
	{Provider: "anthropic", ID: "anthropic/claude-sonnet-4-5", Label: "Claude Sonnet 4.5", ContextWindow: 200000},
	{Provider: "openai", ID: "openai/bare-model", Label: "Bare Model Name"},
	{Provider: "openai", ID: "openai/gpt-5.6", Label: "GPT-5.6", ContextWindow: 400000, FastMode: true},
}

func serveFixture(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/provider" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverOpencodeArrayShape(t *testing.T) {
	srv := serveFixture(t, ocProvidersArrayFixture, http.StatusOK)
	got, err := DiscoverOpencode(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, ocWantModels) {
		t.Fatalf("got %+v, want %+v", got, ocWantModels)
	}
}

func TestDiscoverOpencodeEnvelopeShape(t *testing.T) {
	envelope := `{"providers":` + ocProvidersArrayFixture + `}`
	srv := serveFixture(t, envelope, http.StatusOK)
	got, err := DiscoverOpencode(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, ocWantModels) {
		t.Fatalf("got %+v, want %+v", got, ocWantModels)
	}
}

func TestDiscoverOpencodeErrors(t *testing.T) {
	ctx := context.Background()
	srv := serveFixture(t, `{"error":"boom"}`, http.StatusInternalServerError)
	if _, err := DiscoverOpencode(ctx, srv.URL); err == nil {
		t.Fatal("expected error on HTTP 500")
	}

	bad := serveFixture(t, `this is not json {`, http.StatusOK)
	if _, err := DiscoverOpencode(ctx, bad.URL); err == nil {
		t.Fatal("expected error on malformed body")
	}
}

func writeCmdScript(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := strings.Join(lines, "\r\n") + "\r\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProbeHealthReady(t *testing.T) {
	dir := t.TempDir()
	script := writeCmdScript(t, dir, "sb-ready.cmd", "@echo off", "echo sbfake-cli 1.2.7")
	h := ProbeHealth(context.Background(), "claude", script)
	if h.Provider != "claude" {
		t.Fatalf("Provider = %q", h.Provider)
	}
	if h.State != stateReady {
		t.Fatalf("State = %q detail %q, want ready", h.State, h.Detail)
	}
	if h.Version != "sbfake-cli 1.2.7" {
		t.Fatalf("Version = %q", h.Version)
	}
	if h.Detail != "" {
		t.Fatalf("Detail = %q, want empty", h.Detail)
	}
}

func TestProbeHealthAuthRequired(t *testing.T) {
	dir := t.TempDir()
	script := writeCmdScript(t, dir, "sb-auth.cmd", "@echo off", "echo Invalid API key provided 1>&2", "exit /b 1")
	h := ProbeHealth(context.Background(), "claude", script)
	if h.State != stateAuthRequired {
		t.Fatalf("State = %q detail %q, want auth_required", h.State, h.Detail)
	}
	if !strings.Contains(strings.ToLower(h.Detail), "api key") {
		t.Fatalf("Detail = %q, want auth marker", h.Detail)
	}
	if len(h.Detail) > 200 {
		t.Fatalf("Detail length = %d, want <= 200", len(h.Detail))
	}
	if h.Version != "" {
		t.Fatalf("Version = %q, want empty", h.Version)
	}
}

func TestProbeHealthError(t *testing.T) {
	dir := t.TempDir()
	script := writeCmdScript(t, dir, "sb-error.cmd", "@echo off", "echo something exploded", "exit /b 3")
	h := ProbeHealth(context.Background(), "codex", script)
	if h.State != stateError {
		t.Fatalf("State = %q detail %q, want error", h.State, h.Detail)
	}
	if !strings.Contains(h.Detail, "something exploded") {
		t.Fatalf("Detail = %q, want failure output", h.Detail)
	}
}

func TestProbeHealthNotInstalled(t *testing.T) {
	h := ProbeHealth(context.Background(), "opencode", "definitely-not-a-real-cli-xyz")
	if h.State != stateNotInstalled {
		t.Fatalf("State = %q, want not_installed", h.State)
	}
	if h.Version != "" || h.Detail != "" {
		t.Fatalf("Version/Detail = %q/%q, want empty", h.Version, h.Detail)
	}

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing-binary.cmd")
	h = ProbeHealth(context.Background(), "opencode", missing)
	if h.State != stateNotInstalled {
		t.Fatalf("State = %q for missing path, want not_installed", h.State)
	}
}
