package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"supervibe/internal/agent"
	"supervibe/internal/browser"
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
const defaultCodexModel = "gpt-5.6-sol"

var defaultSettings = map[string]string{
	"paths.claude":          "",
	"paths.codex":           "",
	"claude.permissionMode": "acceptEdits",
	"codex.sandbox":         "workspace-write",
	"appearance.theme":      "dark",
	"appearance.accent":     "orange",
	"last.agent.provider":   "",
	"last.agent.model":      "",
	"last.agent.target":     "",
	"last.agent.branch":     "",
	"last.agent.base":       "",
	"last.agent.worktree":   "",
}

type App struct {
	ctx         context.Context
	store       *store.Store
	sup         *supervisor.Supervisor
	mu          sync.Mutex
	turnOptions map[string]agent.TurnOptions
	cfgDir      string
	termMu      sync.Mutex
	terms       map[string]*TerminalSession
}

func NewApp() *App {
	return &App{
		turnOptions: map[string]agent.TurnOptions{},
		terms:       map[string]*TerminalSession{},
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
	a.sup = supervisor.New(st, func(sessionID string, ev agent.AgentEvent) {
		runtime.EventsEmit(ctx, eventTopic, SessionEvent{SessionID: sessionID, Event: ev})
	})
	runtime.LogInfo(ctx, "SuperVibe backend started")
}

func (a *App) Shutdown(ctx context.Context) {
	a.stopAllTerminals()
	if a.sup != nil {
		a.sup.StopAll()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
	if err := scheduleSelfDelete(); err != nil {
		runtime.LogWarningf(ctx, "dev executable cleanup: %v", err)
	}
}

func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
	}
}

// OpenDirectoryDialog opens the native directory picker.
func (a *App) OpenDirectoryDialog(title string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app not started")
	}
	return openDirectoryDialog(a.ctx, title)
}

// OpenMultipleFilesDialog opens the native multi-file picker.
func (a *App) OpenMultipleFilesDialog(title string) ([]string, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("app not started")
	}
	return openMultipleFilesDialog(a.ctx, title)
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
	return nil
}

func (a *App) DetectCLIs() (map[string]bool, error) {
	settings, _ := a.GetSettings()
	return map[string]bool{
		"git":    lookPath("git"),
		"claude": lookPath(firstNonEmpty(settings["paths.claude"], "claude")),
		"codex":  lookPath(firstNonEmpty(settings["paths.codex"], "codex")),
	}, nil
}

func (a *App) ListModels(provider string) ([]modelsx.ModelInfo, error) {
	switch provider {
	case string(agent.ProviderClaude):
		settings, _ := a.GetSettings()
		bin := firstNonEmpty(settings["paths.claude"], "claude")
		models, err := modelsx.DiscoverClaude(context.Background(), bin)
		if err == nil && len(models) > 0 {
			return models, nil
		}
		return modelsx.SuggestionsFor(provider), nil
	default:
		return modelsx.SuggestionsFor(provider), nil
	}
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
	if strings.ContainsAny(name, `/\`) {
		_, err := os.Stat(name)
		return err == nil
	}
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	for _, dir := range commonBinDirs() {
		for _, ext := range binExts {
			p := filepath.Join(dir, name+ext)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return true
			}
		}
	}
	return false
}

// resolveBin returns a path that can be handed to exec.Command: the absolute
// location when found on PATH or in a common shim dir, otherwise the original
// name ("claude" etc. so the normal error path still shows something useful).
func resolveBin(name string) string {
	if name == "" {
		return ""
	}
	if strings.ContainsAny(name, `/\`) {
		if _, err := os.Stat(name); err == nil {
			return name
		}
		return name
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range commonBinDirs() {
		for _, ext := range binExts {
			p := filepath.Join(dir, name+ext)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	return name
}

var binExts = []string{".exe", ".cmd", ".bat", ""}

func commonBinDirs() []string {
	var dirs []string
	if appData := os.Getenv("APPDATA"); appData != "" {
		dirs = append(dirs, filepath.Join(appData, "npm"))
		dirs = append(dirs, filepath.Join(appData, "pnpm"))
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		dirs = append(dirs, filepath.Join(local, "pnpm"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
		)
	}
	return dirs
}

func (a *App) StartSession(worktreeID, provider, model string) (*store.Session, error) {
	model = defaultModel(provider, model)
	opts, err := a.buildOptions(worktreeID, provider)
	if err != nil {
		return nil, err
	}
	opts.Model = model
	sess, err := a.sup.StartSession(a.ctx, worktreeID, provider, model, opts)
	if err != nil {
		return nil, err
	}
	_ = a.store.SetSetting("last.agent.provider", provider)
	_ = a.store.SetSetting("last.agent.model", model)
	return sess, nil
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
		ClaudePath:           resolveBin(firstNonEmpty(settings["paths.claude"], "claude")),
		CodexPath:            resolveBin(firstNonEmpty(settings["paths.codex"], "codex")),
	}
	return opts, nil
}

func (a *App) SendMessage(sessionID, prompt string) error {
	sess, err := a.store.GetSession(sessionID)
	if err != nil {
		return err
	}
	return a.sendWithRecovery(sess, prompt, agent.TurnOptions{
		Model: defaultModel(sess.Provider, sess.Model),
	})
}

func (a *App) SendMessageConfigured(sessionID, prompt, model, reasoningEffort string, fastMode bool) error {
	sess, err := a.store.GetSession(sessionID)
	if err != nil {
		return err
	}
	model = defaultModel(sess.Provider, model)
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
	return a.sendWithRecovery(sess, prompt, options)
}

func (a *App) SendMessageExtended(sessionID, text string, mentions, attachmentPaths []string) error {
	sess, err := a.store.GetSession(sessionID)
	if err != nil {
		return err
	}
	wt, err := a.store.GetWorktree(sess.WorktreeID)
	if err != nil {
		return err
	}
	prompt, err := buildExtendedPrompt(a.ctx, wt.Path, text, mentions, attachmentPaths, a.urlFetcher())
	if err != nil {
		return err
	}
	return a.sendWithRecovery(sess, prompt, agent.TurnOptions{
		Model: defaultModel(sess.Provider, sess.Model),
	})
}

func buildExtendedPrompt(ctx context.Context, worktreePath, text string, mentions, attachments []string, fetcher func(string) (string, error)) (string, error) {
	lim := contextx.DefaultLimits()
	var blocks []string
	for _, tok := range mentions {
		block, err := contextx.ResolveMention(ctx, worktreePath, tok, lim, fetcher)
		if err != nil {
			return "", fmt.Errorf("mention @%s: %w", tok, err)
		}
		blocks = append(blocks, block)
	}
	for _, p := range attachments {
		block, err := attachmentBlock(p, lim)
		if err != nil {
			return "", err
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return text, nil
	}
	main := strings.TrimSpace(text)
	if main == "" {
		return strings.Join(blocks, "\n\n"), nil
	}
	return main + "\n\n" + strings.Join(blocks, "\n\n"), nil
}

func (a *App) urlFetcher() func(string) (string, error) {
	return func(raw string) (string, error) {
		page, err := browser.Fetch(a.ctx, raw, 256*1024)
		if err != nil {
			return "", err
		}
		return page.Text, nil
	}
}

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".bmp": true, ".svg": true, ".avif": true, ".ico": true,
}

func attachmentBlock(p string, lim contextx.Limits) (string, error) {
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("attachment %s: %w", p, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("attachment %s: is a directory", p)
	}
	name := filepath.Base(p)
	ext := strings.ToLower(filepath.Ext(name))
	if imageExts[ext] {
		return fmt.Sprintf("[Attached image: %s]", filepath.ToSlash(p)), nil
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, lim.MaxFileBytes)
	n, rerr := io.ReadFull(f, buf)
	if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
		return "", rerr
	}
	content := string(buf[:n])
	if !utf8.ValidString(content) || strings.ContainsRune(content, '\x00') {
		return fmt.Sprintf("[Attachment: %s (%d bytes)]", filepath.ToSlash(p), info.Size()), nil
	}
	content = strings.TrimRight(content, " \t\r\n")
	return fmt.Sprintf("=== %s ===\n```%s\n%s\n```", filepath.ToSlash(p), strings.TrimPrefix(ext, "."), content), nil
}

func (a *App) sendWithRecovery(sess *store.Session, prompt string, options agent.TurnOptions) error {
	err := a.sup.SendMessageWithOptions(sess.ID, prompt, options)
	if !errors.Is(err, supervisor.ErrUnknownSession) {
		return err
	}
	opts, err := a.buildOptions(sess.WorktreeID, sess.Provider)
	if err != nil {
		return err
	}
	opts.Model = options.Model
	opts.ReasoningEffort = options.ReasoningEffort
	opts.FastMode = options.FastMode
	switch sess.Provider {
	case string(agent.ProviderCodex):
		err = a.sup.ReattachCodex(a.ctx, sess, opts)
	case string(agent.ProviderClaude):
		err = a.sup.RestartClaude(sess.ID, opts)
	default:
		err = fmt.Errorf("session cannot be reattached for provider %q", sess.Provider)
	}
	if err != nil {
		return err
	}
	return a.sup.SendMessageWithOptions(sess.ID, prompt, options)
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
	Stat        string `json:"stat"`
	Patch       string `json:"patch"`
	StagedStat  string `json:"stagedStat"`
	StagedPatch string `json:"stagedPatch"`
}

const maxDiffPatchBytes = 300 * 1024

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
	stagedStat, err := gitx.DiffCachedStat(wt.Path)
	if err != nil {
		return nil, err
	}
	stagedPatch, err := gitx.DiffCachedPatch(wt.Path)
	if err != nil {
		return nil, err
	}
	return &DiffResult{
		Stat:        stat,
		Patch:       truncatePatch(patch),
		StagedStat:  stagedStat,
		StagedPatch: truncatePatch(stagedPatch),
	}, nil
}

// GetSessionDiff returns the diff of everything that changed since the
// session's baseline (agent commits plus uncommitted worktree edits).
func (a *App) GetSessionDiff(sessionID string) (*DiffResult, error) {
	sess, err := a.store.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	wt, err := a.store.GetWorktree(sess.WorktreeID)
	if err != nil {
		return nil, err
	}
	from := sess.BaselineHead
	if from == "" {
		from = "HEAD"
	}
	stat, err := gitx.DiffRangeStat(wt.Path, from)
	if err != nil {
		return nil, err
	}
	patch, err := gitx.DiffRange(wt.Path, from)
	if err != nil {
		return nil, err
	}
	return &DiffResult{Stat: stat, Patch: truncatePatch(patch)}, nil
}

func truncatePatch(patch string) string {
	if len(patch) > maxDiffPatchBytes {
		return patch[:maxDiffPatchBytes] + "\n… truncated"
	}
	return patch
}

func (a *App) GitStage(worktreeID string, paths []string) error {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return err
	}
	return gitx.Stage(wt.Path, paths)
}

func (a *App) GitUnstage(worktreeID string, paths []string) error {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return err
	}
	return gitx.Unstage(wt.Path, paths)
}

func (a *App) GitCommit(worktreeID, message string, amend bool) error {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("commit message is empty")
	}
	if amend {
		return gitx.AmendCommit(wt.Path, message)
	}
	return gitx.Commit(wt.Path, message)
}

func (a *App) GetRecentCommits(worktreeID string, limit int) ([]gitx.CommitInfo, error) {
	wt, err := a.store.GetWorktree(worktreeID)
	if err != nil {
		return nil, err
	}
	return gitx.RecentCommits(wt.Path, limit)
}

func (a *App) ForkSession(sessionID string, upToMessageID int64) (*store.Session, error) {
	return a.store.ForkSession(sessionID, upToMessageID)
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

func defaultModel(provider, model string) string {
	if provider == string(agent.ProviderCodex) && strings.TrimSpace(model) == "" {
		return defaultCodexModel
	}
	return model
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
