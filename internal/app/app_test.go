package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"supervibe/internal/agent"
	"supervibe/internal/contextx"
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

func TestAttachmentBlock(t *testing.T) {
	lim := contextx.DefaultLimits()
	dir := t.TempDir()

	img := filepath.Join(dir, "a.png")
	if err := os.WriteFile(img, []byte{0x89, 'P', 'N', 'G'}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := attachmentBlock(img, lim)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "[Attached image:") {
		t.Fatalf("image block = %q", got)
	}

	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("hello\nworld"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = attachmentBlock(txt, lim)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "```txt") {
		t.Fatalf("text block = %q", got)
	}

	bin := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(bin, []byte{0x00, 0x01, 0x02, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = attachmentBlock(bin, lim)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Attachment") || strings.Contains(got, "```") {
		t.Fatalf("binary block = %q", got)
	}

	if _, err := attachmentBlock(filepath.Join(dir, "missing.png"), lim); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestBuildExtendedPrompt(t *testing.T) {
	var ctx context.Context
	dir := t.TempDir()
	file := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(file, []byte("attached content"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, err := buildExtendedPrompt(ctx, dir, "fix it", nil, []string{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prompt, "fix it") || !strings.Contains(prompt, "attached content") {
		t.Fatalf("prompt = %q", prompt)
	}
}
