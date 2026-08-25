package procx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"
)

type Options struct {
	Dir     string
	Env     []string
	Timeout time.Duration
}

type OutputLine struct {
	Stream string
	Text   string
	Ts     int64
}

type waitResult struct {
	code int
	err  error
}

type Proc struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	out       chan OutputLine
	waitCh    chan waitResult
	res       waitResult
	waitOnce  sync.Once
	cancelMu  sync.Mutex
	canceled  bool
}

func environPlus(extra []string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	return append(os.Environ(), extra...)
}

func Start(ctx context.Context, name string, args []string, o Options) (*Proc, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p := &Proc{out: make(chan OutputLine, 1024), waitCh: make(chan waitResult, 1)}
	if o.Timeout > 0 {
		ctx, p.cancel = context.WithTimeout(ctx, o.Timeout)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = o.Dir
	cmd.Env = environPlus(o.Env)
	cmd.SysProcAttr = hideWindow()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		if p.cancel != nil {
			p.cancel()
		}
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	p.cmd = cmd

	var wg sync.WaitGroup
	pump := func(r io.Reader, stream string) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			select {
			case p.out <- OutputLine{Stream: stream, Text: line, Ts: time.Now().UnixMilli()}:
			default:
			}
		}
	}
	wg.Add(2)
	go pump(stdout, "stdout")
	go pump(stderr, "stderr")

	go func() {
		wg.Wait()
		waitErr := cmd.Wait()
		close(p.out)
		code := 0
		if waitErr != nil {
			var ee *exec.ExitError
			if errors.As(waitErr, &ee) {
				code = ee.ExitCode()
				waitErr = nil
			} else {
				code = -1
			}
		}
		p.waitCh <- waitResult{code: code, err: waitErr}
		if p.cancel != nil {
			p.cancel()
		}
	}()
	return p, nil
}

func Shell(ctx context.Context, dir, command string, o Options) (*Proc, error) {
	if runtime.GOOS == "windows" {
		return Start(ctx, "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", command}, o)
	}
	return Start(ctx, "sh", []string{"-c", command}, o)
}

func (p *Proc) Out() <-chan OutputLine { return p.out }

func (p *Proc) Wait() (int, error) {
	p.waitOnce.Do(func() {
		p.res = <-p.waitCh
	})
	return p.res.code, p.res.err
}

func (p *Proc) Cancel() error {
	p.cancelMu.Lock()
	defer p.cancelMu.Unlock()
	if p.canceled {
		return nil
	}
	p.canceled = true
	if runtime.GOOS == "windows" && p.cmd.Process != nil {
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.cmd.Process.Pid))
		kill.SysProcAttr = hideWindow()
		_ = kill.Run()
	}
	if p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}
