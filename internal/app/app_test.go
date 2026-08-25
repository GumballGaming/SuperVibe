package app

import (
	"testing"

	"supervibe/internal/agent"
)

func TestDefaultModel(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		want     string
	}{
		{name: "codex empty", provider: string(agent.ProviderCodex), want: defaultCodexModel},
		{name: "codex whitespace", provider: string(agent.ProviderCodex), model: "  ", want: defaultCodexModel},
		{name: "codex explicit", provider: string(agent.ProviderCodex), model: "gpt-5.5", want: "gpt-5.5"},
		{name: "claude empty", provider: string(agent.ProviderClaude), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultModel(tt.provider, tt.model); got != tt.want {
				t.Fatalf("defaultModel(%q, %q) = %q, want %q", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}
