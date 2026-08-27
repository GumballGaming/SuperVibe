//go:build !windows

package app

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// ptyConn wraps a Unix pty attached to the shell process, giving it a real
// controlling terminal (prompts, echo, interactive CLIs).
type ptyConn struct {
	ptmx *os.File
	cmd  *exec.Cmd
}

func openTerminalConn(dir, shell string) (ptySession, error) {
	cmd := exec.Command(shell, "-l")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: terminalCols, Rows: terminalRows})
	if err != nil {
		return nil, err
	}
	return &ptyConn{ptmx: ptmx, cmd: cmd}, nil
}

var _ ptySession = (*ptyConn)(nil)

func (p *ptyConn) Read(b []byte) (int, error) {
	return p.ptmx.Read(b)
}

func (p *ptyConn) Write(b []byte) (int, error) {
	return p.ptmx.Write(b)
}

func (p *ptyConn) Close() error {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return p.ptmx.Close()
}

func (p *ptyConn) Wait() error {
	return p.cmd.Wait()
}

func (p *ptyConn) Resize(cols, rows int) error {
	return pty.Setsize(p.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
