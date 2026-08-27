//go:build windows

package app

import (
	"context"

	"github.com/UserExistsError/conpty"
)

// PtyConn wraps a Windows ConPTY pseudo-console attached to the shell
// process. Programs see a real console (stdin is a terminal), so interactive
// CLIs such as codex work.
type ptyConn struct {
	c *conpty.ConPty
}

func openTerminalConn(dir, shell string) (ptySession, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, terminalNotRunning
	}
	c, err := conpty.Start(
		shell,
		conpty.ConPtyWorkDir(dir),
		conpty.ConPtyDimensions(terminalCols, terminalRows),
	)
	if err != nil {
		return nil, err
	}
	return &ptyConn{c: c}, nil
}

var _ ptySession = (*ptyConn)(nil)

func (p *ptyConn) Read(b []byte) (int, error) {
	return p.c.Read(b)
}

func (p *ptyConn) Write(b []byte) (int, error) {
	return p.c.Write(b)
}

// Close closes the pseudo console; ConPTY terminates the attached process.
func (p *ptyConn) Close() error {
	return p.c.Close()
}

func (p *ptyConn) Wait() error {
	_, err := p.c.Wait(context.Background())
	return err
}

func (p *ptyConn) Resize(cols, rows int) error {
	return p.c.Resize(cols, rows)
}
