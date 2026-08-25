package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

const ringCap = 256 * 1024

type Ring struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func NewRing() *Ring { return &Ring{buf: make([]byte, ringCap)} }

func (r *Ring) Write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(p) > 0 {
		n := copy(r.buf[min(r.size, ringCap):], p)
		if n == 0 {
			copy(r.buf, r.buf[ringCap/2:])
			r.size -= ringCap / 2
			continue
		}
		p = p[n:]
		r.size += n
		if r.size > ringCap {
			r.size = ringCap
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (r *Ring) Tail(n int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n > r.size {
		n = r.size
	}
	return string(r.buf[r.size-n : r.size])
}

type proc struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    *Ring
	outRing   *Ring
	mu        sync.Mutex
	doneCh    chan struct{}
	closeOnce sync.Once
}

func startProc(ctx context.Context, name string, args []string, dir string) (*proc, error) {
	return startProcEnv(ctx, name, args, dir, nil)
}

func startProcEnv(ctx context.Context, name string, args []string, dir string, extraEnv []string) (*proc, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(environ(), extraEnv...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	p := &proc{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  NewRing(),
		outRing: NewRing(),
		doneCh:  make(chan struct{}),
	}
	go func() {
		sc := bufio.NewScanner(stderrPipe)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			p.stderr.Write(sc.Bytes())
			p.stderr.Write([]byte("\n"))
		}
	}()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	return p, nil
}

func (p *proc) writeLine(line string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := io.WriteString(p.stdin, line+"\n")
	return err
}

func (p *proc) closeStdin() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stdin.Close()
}

func (p *proc) kill() {
	p.closeOnce.Do(func() {
		p.closeStdin()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	})
}

func scanLines(r io.Reader, fn func(line string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
	for sc.Scan() {
		fn(sc.Text())
	}
}
