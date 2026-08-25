package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"supervibe/internal/agent"
	"supervibe/internal/contextx"
	"supervibe/internal/gitx"
	"supervibe/internal/modelsx"
	"supervibe/internal/store"
	"supervibe/internal/supervisor"
)

type ProjectTree struct {
	Project   store.Project    `json:"project"`
	Worktrees []store.Worktree `json:"worktrees"`
}

type SessionDetail struct {
	Session  *store.Session  `json:"session"`
	Messages []store.Message `json:"messages"`
}

type SessionEvent struct {
	SessionID string           `json:"sessionId"`
	Event     agent.AgentEvent `json:"event"`
}

const eventTopic = "agent:event"

var defaultSettings = map[string]string{
	"paths.claude":          "",
	"paths.codex":           "",
	"paths.opencode":        "",
	"claude.permissionMode": "acceptEdits",
	"codex.sandbox":         "workspace-write",
	"opencode.autoApprove":  "true",
	"appearance.theme":      "dark",
	"appearance.accent":     "orange",
}

type App struct {
	ctx         context.Context
	store       *store.Store
	sup         *supervisor.Supervisor
	mu          sync.Mutex
	ocServers   map[string]*agent.OpenCodeServer
	turnOptions map[string]agent.TurnOptions
	cfgDir      string
}

func NewApp() *App {
	return &App{
		ocServers:   map[string]*agent.OpenCodeServer{},
		turnOptions: map[string]agent.TurnOptions{},
	}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	a.cfgDir = filepath.Join(base, "SuperVibe")
	st, err := store.Open(filepath.Join(a.cfgDir, "supervibe.db"))
	if err != nil {
		runtime.LogErrorf(ctx, "store open failed: %v", err)
		return
	}
	a.store = st
	if err := a.writeOpencodeConfig(); err != nil {
		runtime.LogWarningf(ctx, "opencode config: %v", err)
	}
	a.sup = supervisor.New(st, func(sessionID string, ev agent.AgentEvent) {
		runtime.EventsEmit(ctx, eventTopic, SessionEvent{SessionID: sessionID, Event: ev})
	})
	runtime.LogInfo(ctx, "SuperVibe backend started")
}

func (a *App) Shutdown(ctx context.Context) {
	if a.sup != nil {
		a.sup.StopAll()
	}
	a.mu.Lock()
	servers := make([]*agent.OpenCodeServer, 0, len(a.ocServers))
	for _, s := range a.ocServers {
		servers = append(servers, s)
	}
	a.mu.Unlock()
	for _, s := range servers {
		s.Shutdown()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
	if err := scheduleSelfDelete(); err != nil {
		runtime.LogWarningf(ctx, "dev executable cleanup: %v", err)
	}
}

// OpenDirectoryDialog opens the native directory picker.
func (a *App) OpenDirectoryDialog(title string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app not started")
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

// OpenMultipleFilesDialog opens the native multi-file picker.
func (a *App) OpenMultipleFilesDialog(title string) ([]string, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("app not started")
	}
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

func (a *App) writeOpencodeConfig() error {
	auto, ok, err := a.store.GetSetting("opencode.autoApprove")
	if err == nil && ok && auto == "false" {
		return nil
	}
	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"permission": map[string]any{
			"edit":     "allow",
			"bash":     "allow",
			"webfetch": "allow",
		},
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(filepath.Join(a.cfgDir, "opencode-allow.json"), b, 0o644)
}

func (a *App) ListProjects() ([]ProjectTree, error) {
	projects, err := a.store.ListProjects()
	if err != nil {
		return nil, err
	}
	out := make([]ProjectTree, 0, len(projects))
	for _, p := range projects {
		wts, err := a.store.ListWorktrees(p.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, ProjectTree{Project: p, Worktrees: wts})
	}
	return out, nil
}

func (a *App) AddProject(path string) (*ProjectTree, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	root := path
	if !gitx.IsRepo(root) {
		found, err := gitx.RepoRoot(root)
		if err != nil {
			return nil, fmt.Errorf("%q is not inside a git repository", path)
		}
		root = found
	}
	name := gitx.RepoName(root)
	proj, err := a.store.CreateProject(name, root)
	if err != nil {
		return nil, err
	}
	wtInfos, _ := gitx.Worktrees(root)
	if len(wtInfos) == 0 {
		branch, _ := gitx.CurrentBranch(root)
		wtInfos = append(wtInfos, gitx.WorktreeInfo{Path: root, Branch: branch})
	}
	tree := &ProjectTree{Project: *proj}
	primarySeen := false
	for i, wi := range wtInfos {
		isPrimary := strings.EqualFold(strings.ToLower(wi.Path), strings.ToLower(root))
		branch := wi.Branch
		if branch == "" || branch == "(detached)" {
			branch = "(detached)"
		}
		wt := &store.Worktree{
			ProjectID: proj.ID,
			Name:      gitx.RepoName(wi.Path),
			Branch:    branch,
			Path:      wi.Path,
			IsPrimary: isPrimary && !primarySeen,
		}
		if wt.IsPrimary {
			primarySeen = true
			wt.Name = name
		}
		if err := a.store.UpsertWorktree(wt); err != nil {
			return nil, err
		}
		tree.Worktrees = append(tree.Worktrees, *wt)
		_ = i
	}
	return tree, nil
}

func (a *App) RemoveProject(id string) error {
	wts, err := a.store.ListWorktrees(id)
	if err != nil {
		return err
	}
	for _, wt := range wts {
		sessions, err := a.store.ListSessionsByWorktree(wt.ID)
		if err != nil {
			continue
		}
		for _, sess := range sessions {
			_ = a.sup.StopSession(sess.ID)
		}
	}
	return a.store.DeleteProject(id)
}

func sanitizeBranch(b string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', ' ', '*', '?', '"', '<', '>', '|', '~':
			return '-'
		}
		return r
	}
	return strings.Map(repl, b)
}

func (a *App) CreateWorktree(projectID, branch, baseRef string) (*store.Worktree, error) {
	proj, err := a.getProject(projectID)
	if err != nil {
		return nil, err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil, fmt.Errorf("branch name is required")
	}
	dir := filepath.Join(filepath.Dir(proj.Path), filepath.Base(proj.Path)+"-wt", sanitizeBranch(branch))
	if err := gitx.AddWorktree(proj.Path, dir, branch, baseRef); err != nil {
		return nil, err
	}
	wt := &store.Worktree{
		ProjectID: proj.ID,
		Name:      branch,
		Branch:    branch,
		Path:      dir,
	}
	if err := a.store.UpsertWorktree(wt); err != nil {
		return nil, err
	}
	return wt, nil
}

func (a *App) DeleteWorktree(worktreeID string, force bool) error {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return err
	}
	if wt.IsPrimary {
		return fmt.Errorf("cannot remove the primary worktree")
	}
	sessions, _ := a.store.ListSessionsByWorktree(worktreeID)
	for _, sess := range sessions {
		_ = a.sup.StopSession(sess.ID)
	}
	proj, err := a.getProject(wt.ProjectID)
	if err == nil {
		_ = gitx.RemoveWorktree(proj.Path, wt.Path, force)
	}
	return a.store.DeleteWorktree(worktreeID)
}

func (a *App) getProject(id string) (*store.Project, error) {
	projects, err := a.store.ListProjects()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("project not found")
}

type WorktreeInfo struct {
	*store.Worktree
	GitStatus *gitx.Status `json:"gitStatus"`
	Head      string       `json:"head"`
}

func (a *App) GetWorktreeInfo(worktreeID string) (*WorktreeInfo, error) {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return nil, err
	}
	info := &WorktreeInfo{Worktree: wt}
	st, err := gitx.StatusSummary(wt.Path)
	if err == nil {
		info.GitStatus = st
		if st.Branch != "" && st.Branch != wt.Branch && st.Branch != "(detached)" {
			wt.Branch = st.Branch
			_ = a.store.UpsertWorktree(wt)
		}
	}
	if sha, err := gitx.HeadSha(wt.Path); err == nil {
		info.Head = sha
	}
	return info, nil
}

func (a *App) ListBranches(worktreeID string) ([]gitx.Branch, error) {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return nil, err
	}
	return gitx.Branches(wt.Path)
}

func (a *App) GetSettings() (map[string]string, error) {
	out := map[string]string{}
	for k, v := range defaultSettings {
		out[k] = v
	}
	for k := range defaultSettings {
		if v, ok, err := a.store.GetSetting(k); err == nil && ok {
			out[k] = v
		}
	}
	return out, nil
}

func (a *App) SetSettings(values map[string]string) error {
	for k, v := range values {
		if err := a.store.SetSetting(k, v); err != nil {
			return err
		}
	}
	return a.writeOpencodeConfig()
}

func (a *App) DetectCLIs() (map[string]bool, error) {
	settings, _ := a.GetSettings()
	return map[string]bool{
		"git":      lookPath("git"),
		"claude":   lookPath(firstNonEmpty(settings["paths.claude"], "claude")),
		"codex":    lookPath(firstNonEmpty(settings["paths.codex"], "codex")),
		"opencode": lookPath(firstNonEmpty(settings["paths.opencode"], "opencode")),
	}, nil
}

func (a *App) ListModels(provider string) ([]modelsx.ModelInfo, error) {
	if provider != string(agent.ProviderOpencode) {
		return modelsx.SuggestionsFor(provider), nil
	}
	a.mu.Lock()
	servers := make([]*agent.OpenCodeServer, 0, len(a.ocServers))
	for _, srv := range a.ocServers {
		servers = append(servers, srv)
	}
	a.mu.Unlock()
	for _, srv := range servers {
		models, err := modelsx.DiscoverOpencode(context.Background(), srv.BaseURL)
		if err == nil && len(models) > 0 {
			return models, nil
		}
	}
	return []modelsx.ModelInfo{}, nil
}

func (a *App) RefreshModels(provider string) ([]modelsx.ModelInfo, error) {
	return a.ListModels(provider)
}

func (a *App) GetCapabilities(provider string) (modelsx.Capabilities, error) {
	return modelsx.CapabilitiesFor(provider), nil
}

func lookPath(name string) bool {
	if name == "" {
		return false
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		_, err := os.Stat(name)
		return err == nil
	}
	_, err := exec.LookPath(name)
	return err == nil
}

func (a *App) StartSession(worktreeID, provider, model string) (*store.Session, error) {
	opts, err := a.buildOptions(worktreeID, provider)
	if err != nil {
		return nil, err
	}
	opts.Model = model
	return a.sup.StartSession(a.ctx, worktreeID, provider, model, opts)
}

func (a *App) buildOptions(worktreeID, provider string) (agent.Options, error) {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return agent.Options{}, err
	}
	settings, _ := a.GetSettings()
	opts := agent.Options{
		WorktreePath:         wt.Path,
		ClaudePermissionMode: settings["claude.permissionMode"],
		CodexSandbox:         settings["codex.sandbox"],
		ClaudePath:           firstNonEmpty(settings["paths.claude"], "claude"),
		CodexPath:            firstNonEmpty(settings["paths.codex"], "codex"),
		OpencodePath:         firstNonEmpty(settings["paths.opencode"], "opencode"),
		AppConfigDir:         a.cfgDir,
	}
	if provider == string(agent.ProviderOpencode) {
		srv, err := a.ensureOpencodeServer(wt.ProjectID, opts.OpencodePath)
		if err != nil {
			return agent.Options{}, err
		}
		opts.OpenCodeServer = srv
	}
	return opts, nil
}

func (a *App) ensureOpencodeServer(projectID, binPath string) (*agent.OpenCodeServer, error) {
	proj, err := a.getProject(projectID)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if srv, ok := a.ocServers[proj.Path]; ok {
		return srv, nil
	}
	var cfgFile string
	auto := true
	if v, ok, err := a.store.GetSetting("opencode.autoApprove"); err == nil && ok {
		auto = v != "false"
	}
	if auto {
		cfgFile = filepath.Join(a.cfgDir, "opencode-allow.json")
	}
	srv, err := agent.StartOpenCodeServer(binPath, proj.Path, cfgFile)
	if err != nil {
		return nil, err
	}
	a.ocServers[proj.Path] = srv
	return srv, nil
}

func (a *App) SendMessage(sessionID, prompt string) error {
	return a.sup.SendMessage(sessionID, prompt)
}

func (a *App) SendMessageConfigured(sessionID, prompt, model, reasoningEffort string, fastMode bool) error {
	sess, err := a.store.GetSession(sessionID)
	if err != nil {
		return err
	}
	if reasoningEffort == "off" {
		reasoningEffort = ""
	}
	options := agent.TurnOptions{Model: model, ReasoningEffort: reasoningEffort, FastMode: fastMode}
	a.mu.Lock()
	previous, known := a.turnOptions[sessionID]
	changed := !known || previous != options
	a.mu.Unlock()
	if sess.Provider == string(agent.ProviderClaude) && changed && sess.ProviderSessionID != "" {
		if sess.Status == "running" || sess.Status == "waiting" || sess.Status == "starting" {
			return fmt.Errorf("wait for the current Claude turn to finish before changing model or thinking")
		}
		opts, err := a.buildOptions(sess.WorktreeID, sess.Provider)
		if err != nil {
			return err
		}
		opts.Model = model
		opts.ReasoningEffort = reasoningEffort
		opts.FastMode = fastMode
		if err := a.sup.RestartClaude(sessionID, opts); err != nil {
			return err
		}
	}
	if changed {
		a.mu.Lock()
		a.turnOptions[sessionID] = options
		a.mu.Unlock()
	}
	return a.sup.SendMessageWithOptions(sessionID, prompt, options)
}

func (a *App) InterruptSession(sessionID string) error {
	return a.sup.Interrupt(sessionID)
}

func (a *App) StopSession(sessionID string) error {
	return a.sup.StopSession(sessionID)
}
func (a *App) RenameSession(sessionID, title string) error {
	if a.store == nil {
		return fmt.Errorf("backend unavailable")
	}
	return a.store.RenameSession(sessionID, title)
}

func (a *App) DeleteSession(sessionID string) error {
	if a.sup != nil {
		if err := a.sup.StopSession(sessionID); err != nil {
			return err
		}
	}
	if a.store == nil {
		return fmt.Errorf("backend unavailable")
	}
	if err := a.store.DeleteSessionByID(sessionID); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.turnOptions, sessionID)
	a.mu.Unlock()
	return nil
}

func (a *App) ResumeSession(sessionID string) error {
	opts, err := a.resumeOptions(sessionID)
	if err != nil {
		return err
	}
	return a.sup.RestartClaude(sessionID, opts)
}

func (a *App) resumeOptions(sessionID string) (agent.Options, error) {
	sess, err := a.store.GetSession(sessionID)
	if err != nil {
		return agent.Options{}, err
	}
	return a.buildOptions(sess.WorktreeID, sess.Provider)
}

func (a *App) GetSessionDetail(sessionID string) (*SessionDetail, error) {
	sess, err := a.store.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	msgs, err := a.store.ListMessages(sessionID, 0, 1000)
	if err != nil {
		return nil, err
	}
	return &SessionDetail{Session: sess, Messages: msgs}, nil
}

func (a *App) GetMessagesAfter(sessionID string, afterID int64) ([]store.Message, error) {
	return a.store.ListMessages(sessionID, afterID, 500)
}

func (a *App) ListFleet(statuses []string) ([]store.FleetRow, error) {
	return a.store.ListSessionsByStatus(statuses)
}

type DiffResult struct {
	Stat  string `json:"stat"`
	Patch string `json:"patch"`
}

func (a *App) GetDiff(worktreeID string) (*DiffResult, error) {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return nil, err
	}
	stat, err := gitx.DiffStat(wt.Path)
	if err != nil {
		return nil, err
	}
	patch, err := gitx.DiffPatch(wt.Path)
	if err != nil {
		return nil, err
	}
	const maxPatch = 300 * 1024
	if len(patch) > maxPatch {
		patch = patch[:maxPatch] + "\n… truncated"
	}
	return &DiffResult{Stat: stat, Patch: patch}, nil
}

func (a *App) GetOutputTail(sessionID string, maxBytes int) (string, error) {
	if maxBytes <= 0 || maxBytes > 256*1024 {
		maxBytes = 64 * 1024
	}
	return a.sup.OutputTail(sessionID, maxBytes), nil
}

func (a *App) SearchRepo(worktreeID, query string, limit int) ([]string, error) {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return nil, err
	}
	return contextx.FileSearch(wt.Path, query, limit)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
