package modelsx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	discoverTimeout = 10 * time.Second
	probeTimeout    = 12 * time.Second
	detailMaxRunes  = 200
)

type ModelInfo struct {
	Provider      string `json:"provider"`
	ID            string `json:"id"`
	Label         string `json:"label"`
	ContextWindow int64  `json:"contextWindow,omitempty"`
	Suggested     bool   `json:"suggested,omitempty"`
	FastMode      bool   `json:"fastMode,omitempty"`
}

type ModelSelection string

const (
	selectionNone     ModelSelection = "none"
	selectionFreeform ModelSelection = "freeform"
	selectionDynamic  ModelSelection = "dynamic"
)

type HealthState string

const (
	stateReady         HealthState = "ready"
	stateNotInstalled  HealthState = "not_installed"
	stateAuthRequired  HealthState = "auth_required"
	stateMisconfigured HealthState = "misconfigured"
	stateError         HealthState = "error"
)

type Capabilities struct {
	Streaming         bool           `json:"streaming"`
	Tools             bool           `json:"tools"`
	FileEdit          bool           `json:"fileEdit"`
	Shell             bool           `json:"shell"`
	Images            bool           `json:"images"`
	MCP               bool           `json:"mcp"`
	Subagents         bool           `json:"subagents"`
	Resume            bool           `json:"resume"`
	Usage             bool           `json:"usage"`
	CostReport        bool           `json:"costReport"`
	ReasoningControls bool           `json:"reasoningControls"`
	NativeWebBrowse   bool           `json:"nativeWebBrowse"`
	ModelSelection    ModelSelection `json:"modelSelection"`
}

type Health struct {
	Provider string      `json:"provider"`
	State    HealthState `json:"state"`
	Version  string      `json:"version,omitempty"`
	Detail   string      `json:"detail,omitempty"`
}

var suggestedByProvider = map[string][]ModelInfo{
	"claude": {
		{Provider: "claude", ID: "claude-sonnet-4-5", Label: "Claude Sonnet 4.5", Suggested: true},
		{Provider: "claude", ID: "claude-opus-4-6", Label: "Claude Opus 4.6", Suggested: true},
		{Provider: "claude", ID: "claude-opus-4-8", Label: "Claude Opus 4.8", Suggested: true, FastMode: true},
		{Provider: "claude", ID: "claude-opus-5", Label: "Claude Opus 5", Suggested: true, FastMode: true},
		{Provider: "claude", ID: "claude-haiku-4-5", Label: "Claude Haiku 4.5", Suggested: true},
	},
	"codex": {
		{Provider: "codex", ID: "gpt-5-codex", Label: "GPT-5 Codex", Suggested: true},
		{Provider: "codex", ID: "gpt-5.6-sol", Label: "GPT-5.6 Sol", Suggested: true, FastMode: true},
		{Provider: "codex", ID: "gpt-5.6-terra", Label: "GPT-5.6 Terra", Suggested: true, FastMode: true},
		{Provider: "codex", ID: "o4-mini", Label: "o4-mini", Suggested: true},
	},
}

var authMarkers = []string{
	"api key",
	"unauthorized",
	"login required",
	"not authenticated",
	"invalid api key",
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func CapabilitiesFor(provider string) Capabilities {
	switch normalizeProvider(provider) {
	case "claude":
		return Capabilities{
			Streaming:         true,
			Tools:             true,
			FileEdit:          true,
			Shell:             true,
			Images:            true,
			MCP:               true,
			Subagents:         true,
			Resume:            true,
			Usage:             true,
			CostReport:        true,
			ReasoningControls: true,
			NativeWebBrowse:   true,
			ModelSelection:    selectionFreeform,
		}
	case "codex":
		return Capabilities{
			Streaming:         true,
			Tools:             true,
			FileEdit:          true,
			Shell:             true,
			Images:            true,
			MCP:               true,
			Resume:            true,
			Usage:             true,
			ReasoningControls: true,
			NativeWebBrowse:   true,
			ModelSelection:    selectionFreeform,
		}
	case "opencode":
		return Capabilities{
			Streaming:         true,
			Tools:             true,
			FileEdit:          true,
			Shell:             true,
			MCP:               true,
			Resume:            true,
			Usage:             true,
			CostReport:        true,
			ReasoningControls: true,
			NativeWebBrowse:   true,
			ModelSelection:    selectionDynamic,
		}
	default:
		return Capabilities{ModelSelection: selectionNone}
	}
}

func SuggestionsFor(provider string) []ModelInfo {
	list, ok := suggestedByProvider[normalizeProvider(provider)]
	if !ok {
		return nil
	}
	out := make([]ModelInfo, len(list))
	copy(out, list)
	return out
}

type ocModelEntry struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	ContextLength json.RawMessage `json:"context_length"`
	Variants      json.RawMessage `json:"variants"`
}

func DiscoverOpencode(ctx context.Context, baseURL string) ([]ModelInfo, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("opencode provider list: empty base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/provider", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: discoverTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode provider list: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("opencode provider list: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("opencode provider list: %s: %s", resp.Status, truncate(string(body), detailMaxRunes))
	}
	return parseOpencodeProviders(body)
}

func parseOpencodeProviders(body []byte) ([]ModelInfo, error) {
	items, err := decodeOpencodeEnvelope(body)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(items))
	for _, rawProvider := range items {
		var prov struct {
			ID     string                     `json:"id"`
			Models map[string]json.RawMessage `json:"models"`
		}
		if err := json.Unmarshal(rawProvider, &prov); err != nil || prov.ID == "" {
			continue
		}
		for modelID, rawModel := range prov.Models {
			info := ModelInfo{Provider: prov.ID, ID: prov.ID + "/" + modelID}
			if len(rawModel) == 0 || bytes.Equal(bytes.TrimSpace(rawModel), []byte("null")) {
				info.Label = modelID
				out = append(out, info)
				continue
			}
			var entry ocModelEntry
			if err := json.Unmarshal(rawModel, &entry); err != nil {
				var s string
				if err := json.Unmarshal(rawModel, &s); err != nil || s == "" {
					continue
				}
				entry.ID, entry.Name = s, s
			}
			info.Label = firstNonEmpty(entry.Name, entry.ID, modelID)
			if hasFastVariant(entry.Variants) {
				info.FastMode = true
			}
			if cw := parseLenientInt64(entry.ContextLength); cw > 0 {
				info.ContextWindow = cw
			}
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func hasFastVariant(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	var named map[string]json.RawMessage
	if json.Unmarshal(raw, &named) == nil {
		if fast, ok := named["fast"]; ok {
			return variantEnabled(fast)
		}
	}
	var listed []struct {
		ID       string `json:"id"`
		Disabled bool   `json:"disabled"`
	}
	if json.Unmarshal(raw, &listed) == nil {
		for _, variant := range listed {
			if variant.ID == "fast" {
				return !variant.Disabled
			}
		}
	}
	return false
}

func variantEnabled(raw json.RawMessage) bool {
	var variant struct {
		Disabled bool `json:"disabled"`
	}
	if json.Unmarshal(raw, &variant) == nil {
		return !variant.Disabled
	}
	return true
}

func decodeOpencodeEnvelope(body []byte) ([]json.RawMessage, error) {
	var envelope struct {
		Providers []json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Providers != nil {
		return envelope.Providers, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("parse opencode providers: %w", err)
	}
	return list, nil
}

func ProbeHealth(ctx context.Context, provider, bin string) Health {
	h := Health{Provider: provider, State: stateNotInstalled}
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return h
	}
	exe := bin
	if strings.ContainsAny(exe, `/\`) {
		if fi, err := os.Stat(exe); err != nil || fi.IsDir() {
			return h
		}
	} else {
		resolved, err := exec.LookPath(exe)
		if err != nil {
			return h
		}
		exe = resolved
	}
	cctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := versionCommand(cctx, exe)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || cctx.Err() != nil {
			h.State = stateError
			h.Detail = truncate("probe timed out", detailMaxRunes)
			return h
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return h
		}
		output := combineOutput(stdout.String(), stderr.String())
		h.Detail = truncate(output, detailMaxRunes)
		if hasAuthMarker(output) {
			h.State = stateAuthRequired
		} else {
			h.State = stateError
		}
		return h
	}
	h.State = stateReady
	h.Version = firstLine(firstNonEmpty(stdout.String(), stderr.String()))
	return h
}

func versionCommand(ctx context.Context, exe string) *exec.Cmd {
	switch strings.ToLower(filepath.Ext(exe)) {
	case ".bat", ".cmd":
		return exec.CommandContext(ctx, "cmd", "/c", exe, "--version")
	default:
		return exec.CommandContext(ctx, exe, "--version")
	}
}

func combineOutput(stdout, stderr string) string {
	switch {
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		return stdout + "\n" + stderr
	}
}

func hasAuthMarker(output string) bool {
	lower := strings.ToLower(output)
	for _, marker := range authMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func parseLenientInt64(raw json.RawMessage) int64 {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if s == "" || s == "null" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f)
	}
	return 0
}
