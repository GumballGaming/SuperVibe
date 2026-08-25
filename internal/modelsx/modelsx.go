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
	Provider         string   `json:"provider"`
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	ContextWindow    int64    `json:"contextWindow,omitempty"`
	Suggested        bool     `json:"suggested,omitempty"`
	FastMode         bool     `json:"fastMode,omitempty"`
	ReasoningEfforts []string `json:"reasoningEfforts,omitempty"`
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

var gpt56ReasoningEfforts = []string{"none", "low", "medium", "high", "xhigh", "max"}
var gpt55ReasoningEfforts = []string{"none", "low", "medium", "high", "xhigh"}

var suggestedByProvider = map[string][]ModelInfo{
	"claude": {
		{Provider: "claude", ID: "claude-sonnet-5", Label: "Claude Sonnet 5", Suggested: true},
		{Provider: "claude", ID: "claude-opus-5", Label: "Claude Opus 5", Suggested: true, FastMode: true},
		{Provider: "claude", ID: "claude-fable-5", Label: "Claude Fable 5", Suggested: true},
		{Provider: "claude", ID: "claude-haiku-4-5", Label: "Claude Haiku 4.5", Suggested: true},
	},
	"codex": {
		{Provider: "codex", ID: "gpt-5.6-sol", Label: "GPT-5.6 Sol", Suggested: true, FastMode: true, ReasoningEfforts: gpt56ReasoningEfforts},
		{Provider: "codex", ID: "gpt-5.6-terra", Label: "GPT-5.6 Terra", Suggested: true, FastMode: true, ReasoningEfforts: gpt56ReasoningEfforts},
		{Provider: "codex", ID: "gpt-5.6-luna", Label: "GPT-5.6 Luna", Suggested: true, FastMode: true, ReasoningEfforts: gpt56ReasoningEfforts},
		{Provider: "codex", ID: "gpt-5.5", Label: "GPT-5.5", Suggested: true, ReasoningEfforts: gpt55ReasoningEfforts},
		{Provider: "codex", ID: "gpt-5.4", Label: "GPT-5.4", Suggested: true, ReasoningEfforts: gpt55ReasoningEfforts},
		{Provider: "codex", ID: "gpt-5.4-mini", Label: "GPT-5.4 Mini", Suggested: true, ReasoningEfforts: gpt55ReasoningEfforts},
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
	for i := range out {
		if list[i].ReasoningEfforts != nil {
			out[i].ReasoningEfforts = append([]string(nil), list[i].ReasoningEfforts...)
		}
	}
	return out
}

type claudeModelResponse struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

// DiscoverClaude asks Claude Code's non-interactive /model command for the
// model aliases available to the authenticated account.
func DiscoverClaude(ctx context.Context, bin string) ([]ModelInfo, error) {
	exe, err := resolveExecutable(bin)
	if err != nil {
		return nil, fmt.Errorf("claude model list: %w", err)
	}

	cctx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := claudeModelCommand(cctx, exe)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := truncate(combineOutput(stdout.String(), stderr.String()), detailMaxRunes)
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("claude model list: %s", detail)
	}

	var response claudeModelResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("claude model list: parse output: %w", err)
	}
	if response.IsError {
		return nil, fmt.Errorf("claude model list: %s", truncate(response.Result, detailMaxRunes))
	}
	return parseClaudeModels(response.Result)
}

func parseClaudeModels(result string) ([]ModelInfo, error) {
	const marker = "available:"
	lower := strings.ToLower(result)
	start := strings.Index(lower, marker)
	if start < 0 {
		return nil, fmt.Errorf("claude model list: output did not contain available models")
	}
	available := strings.TrimSpace(result[start+len(marker):])
	if end := strings.Index(strings.ToLower(available), " or a full model id"); end >= 0 {
		available = available[:end]
	}
	available = strings.TrimSpace(strings.TrimSuffix(available, "."))

	found := make(map[string]ModelInfo, 4)
	for _, rawID := range strings.Split(available, ",") {
		id := strings.Trim(strings.TrimSpace(rawID), "`")
		canonical, ok := canonicalClaudeModel(id)
		if !ok {
			continue
		}
		if _, ok := found[canonical]; ok {
			continue
		}
		found[canonical] = ModelInfo{
			Provider:  "claude",
			ID:        canonical,
			Label:     claudeModelLabel(canonical),
			Suggested: true,
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("claude model list: no supported models in output")
	}
	models := make([]ModelInfo, 0, len(found))
	for _, canonical := range []string{
		"claude-sonnet-5",
		"claude-opus-5",
		"claude-fable-5",
		"claude-haiku-4-5",
	} {
		if model, ok := found[canonical]; ok {
			models = append(models, model)
		}
	}
	return models, nil
}

func canonicalClaudeModel(id string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "sonnet", "sonnet[1m]", "claude-sonnet-5":
		return "claude-sonnet-5", true
	case "opus", "opus[1m]", "claude-opus-5":
		return "claude-opus-5", true
	case "fable", "fable[1m]", "claude-fable-5":
		return "claude-fable-5", true
	case "haiku", "claude-haiku-4-5", "claude-haiku-4-5-20251001":
		return "claude-haiku-4-5", true
	default:
		return "", false
	}
}

func claudeModelLabel(id string) string {
	switch strings.ToLower(id) {
	case "claude-sonnet-5":
		return "Claude Sonnet 5"
	case "claude-opus-5":
		return "Claude Opus 5"
	case "claude-fable-5":
		return "Claude Fable 5"
	case "claude-haiku-4-5":
		return "Claude Haiku 4.5"
	default:
		return id
	}
}

func resolveExecutable(bin string) (string, error) {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return "", errors.New("empty Claude Code path")
	}
	if strings.ContainsAny(bin, `/\`) {
		if fi, err := os.Stat(bin); err != nil || fi.IsDir() {
			return "", fmt.Errorf("Claude Code executable %q is unavailable", bin)
		}
		return bin, nil
	}
	exe, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("Claude Code executable %q is unavailable", bin)
	}
	return exe, nil
}

func claudeModelCommand(ctx context.Context, exe string) *exec.Cmd {
	args := []string{"-p", "/model", "--output-format", "json", "--no-session-persistence"}
	switch strings.ToLower(filepath.Ext(exe)) {
	case ".bat", ".cmd":
		return exec.CommandContext(ctx, "cmd", append([]string{"/c", exe}, args...)...)
	default:
		return exec.CommandContext(ctx, exe, args...)
	}
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
