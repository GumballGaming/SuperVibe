package app

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	terminalEventTopic = "terminal:event"
	terminalBufferMax  = 768 * 1024
	terminalReadChunk  = 4096
	terminalCols       = 120
	terminalRows       = 40
	defaultShellOnUnix = "bash"
)

var (
	errAppNotStarted   = fmt.Errorf("app not started")
	terminalNotRunning = fmt.Errorf("terminal not running")
)

// TerminalEvent is streamed to the frontend on terminal:event.
type TerminalEvent struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "started" | "output" | "exit"
	Data string `json:"data"`
}

// ptySession is a real terminal connection: a PTY on Unix, a ConPTY on
// Windows. It is both the input path and the output path.
type ptySession interface {
	io.Reader
	io.Writer
	io.Closer
	// Wait blocks until the attached process exits.
	Wait() error
	// Resize changes the console size in characters.
	Resize(cols, rows int) error
}

// TerminalSession is one persistent shell running inside a worktree.
type TerminalSession struct {
	id    string
	dir   string
	shell string
	conn  ptySession
	mu    sync.Mutex
	buf   []byte
	dead  bool
}

func (t *TerminalSession) append(data string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf)+len(data) > terminalBufferMax {
		keep := t.buf[len(t.buf)-terminalBufferMax/2:]
		t.buf = append([]byte(nil), keep...)
	}
	t.buf = append(t.buf, data...)
}

func (t *TerminalSession) snapshot() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

func (t *TerminalSession) isDead() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dead
}

func (t *TerminalSession) markDead() {
	t.mu.Lock()
	t.dead = true
	t.mu.Unlock()
}

// StartTerminal starts (or reuses) a persistent shell for a worktree and
// returns the session id (which is the worktree id). The shell is attached
// to a real pseudo-console (ConPTY on Windows, PTY elsewhere), so interactive
// CLIs work. Output is streamed via terminal:event; an "exit" event fires
// when the shell ends.
func (a *App) StartTerminal(worktreeID string) (string, error) {
	if a.ctx == nil {
		return "", errAppNotStarted
	}
	a.termMu.Lock()
	defer a.termMu.Unlock()

	if existing := a.terms[worktreeID]; existing != nil && !existing.isDead() {
		return existing.id, nil
	}

	// A pane may provide a unique key (`worktree::pane`) so multiple shells
	// can run in the same worktree without sharing a PTY.
	wt, err := a.store.GetWorktree(baseWorktreeID(worktreeID))
	if err != nil {
		return "", err
	}

	shell := terminalShell()
	conn, err := openTerminalConn(wt.Path, shell)
	if err != nil {
		return "", err
	}

	term := &TerminalSession{
		id:    worktreeID,
		dir:   wt.Path,
		shell: shell,
		conn:  conn,
	}
	a.terms[worktreeID] = term

	a.emitTerminal(worktreeID, "started", wt.Path)

	go a.pumpTerminal(term, conn)
	go func() {
		_ = conn.Wait()
		term.markDead()
		a.emitTerminal(worktreeID, "exit", "")
		a.termMu.Lock()
		if a.terms[worktreeID] == term {
			delete(a.terms, worktreeID)
		}
		a.termMu.Unlock()
	}()

	wailsruntime.LogInfo(a.ctx, "terminal started: "+worktreeID+" @ "+wt.Path)
	return worktreeID, nil
}

// GetTerminalOutput returns the full scrollback buffer of a terminal session.
func (a *App) GetTerminalOutput(worktreeID string) (string, error) {
	a.termMu.Lock()
	defer a.termMu.Unlock()
	term := a.terms[worktreeID]
	if term == nil {
		return "", terminalNotRunning
	}
	return term.snapshot(), nil
}

// TerminalInput writes user keystrokes (a command line) into the shell.
func (a *App) TerminalInput(worktreeID, data string) error {
	a.termMu.Lock()
	term := a.terms[worktreeID]
	a.termMu.Unlock()
	if term == nil || term.isDead() {
		return terminalNotRunning
	}
	text := data
	// ConPTY and Unix line disciplines expect bare CR / bare LF respectively.
	if runtime.GOOS == "windows" {
		text = strings.ReplaceAll(text, "\n", "\r")
	}
	_, err := term.conn.Write([]byte(text))
	return err
}

// CloseTerminal kills a terminal session if one is running.
func (a *App) CloseTerminal(worktreeID string) error {
	a.termMu.Lock()
	term := a.terms[worktreeID]
	delete(a.terms, worktreeID)
	a.termMu.Unlock()
	if term == nil {
		return nil
	}
	return term.conn.Close()
}

// TerminalResize adjusts the pseudo-console size (in character columns/rows).
func (a *App) TerminalResize(worktreeID string, cols, rows int) error {
	a.termMu.Lock()
	term := a.terms[worktreeID]
	a.termMu.Unlock()
	if term == nil || term.isDead() {
		return terminalNotRunning
	}
	if cols < 20 {
		cols = 20
	}
	if cols > 500 {
		cols = 500
	}
	if rows < 5 {
		rows = 5
	}
	if rows > 200 {
		rows = 200
	}
	return term.conn.Resize(cols, rows)
}

func (a *App) stopAllTerminals() {
	a.termMu.Lock()
	defer a.termMu.Unlock()
	for id, term := range a.terms {
		_ = term.conn.Close()
		delete(a.terms, id)
	}
}

func (a *App) emitTerminal(id, kind, data string) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, terminalEventTopic, TerminalEvent{ID: id, Kind: kind, Data: data})
}

// pumpTerminal forwards PTY output verbatim. xterm owns escape sequence
// interpretation: dropping or rewriting cursor movement and erase sequences
// here is what makes interactive TUIs render as duplicated garbage.
func (a *App) pumpTerminal(term *TerminalSession, r io.Reader) {
	buf := make([]byte, terminalReadChunk)
	var pending []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if len(pending) > 0 {
				chunk = append(append([]byte(nil), pending...), chunk...)
				pending = nil
			}
			// A read can end in the middle of a multi-byte rune. Hold that tail
			// back so it travels with the rest of the sequence instead of being
			// split into invalid halves by JSON encoding.
			if tail := incompleteUTFTail(chunk); len(tail) > 0 {
				pending = append(pending, tail...)
				chunk = chunk[:len(chunk)-len(tail)]
			}
			if len(chunk) > 0 {
				data := string(chunk)
				term.append(data)
				a.emitTerminal(term.id, "output", data)
			}
		}
		if err != nil {
			return
		}
	}
}

// incompleteUTFTail returns the trailing bytes of b that begin a rune the
// chunk does not finish, so the caller can re-emit them with the next read.
func incompleteUTFTail(b []byte) []byte {
	max := 3
	if len(b) < max {
		max = len(b)
	}
	for i := 1; i <= max; i++ {
		tail := b[len(b)-i:]
		if !utf8.RuneStart(tail[0]) {
			continue
		}
		if !utf8.FullRune(tail) {
			return tail
		}
		return nil
	}
	return nil
}

// baseWorktreeID resolves the worktree a terminal id belongs to. Terminal ids
// are `worktreeID` or `worktreeID::paneID`, where the pane suffix lets one
// worktree run up to six independent shells.
func baseWorktreeID(terminalID string) string {
	return strings.SplitN(terminalID, "::", 2)[0]
}

// terminalShell picks the default shell for the platform.
func terminalShell() string {
	if runtime.GOOS == "windows" {
		return "cmd.exe"
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return defaultShellOnUnix
}
