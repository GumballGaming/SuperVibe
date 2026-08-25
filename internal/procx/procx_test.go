package procx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func collect(p *Proc) (lines []OutputLine, code int, err error) {
	for l := range p.Out() {
		lines = append(lines, l)
	}
	code, err = p.Wait()
	return lines, code, err
}

func TestStreamsAndExit(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "s.cmd", "@echo off\r\necho hello-out\r\necho hello-err 1>&2\r\nexit /b 0\r\n")
	p, err := Start(t.Context(), "cmd", []string{"/C", script}, Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	lines, code, err := collect(p)
	if err != nil || code != 0 {
		t.Fatalf("exit: %d %v", code, err)
	}
	var sawOut, sawErr bool
	for _, l := range lines {
		trimmed := strings.TrimRight(l.Text, " \r")
		if l.Stream == "stdout" && trimmed == "hello-out" {
			sawOut = true
		}
		if l.Stream == "stderr" && trimmed == "hello-err" {
			sawErr = true
		}
	}
	if !sawOut || !sawErr {
		t.Fatalf("missing streams: %+v", lines)
	}
}

func TestNonzeroExitCode(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "f.cmd", "@echo off\r\nexit /b 7\r\n")
	p, err := Start(t.Context(), "cmd", []string{"/C", script}, Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	for range p.Out() {
	}
	code, err := p.Wait()
	if err != nil {
		t.Fatalf("wait err: %v", err)
	}
	if code != 7 {
		t.Fatalf("code=%d want 7", code)
	}
}

func TestCancelKillsTree(t *testing.T) {
	start := time.Now()
	p, err := Shell(t.Context(), "", "$host.UI.RawUI.KeyAvailable | Out-Null; Start-Sleep -Seconds 30; Write-Output done", Options{})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = p.Cancel()
	}()
	for range p.Out() {
	}
	_, err = p.Wait()
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("cancel took too long: %v", elapsed)
	}
	if err != nil && elapsed > 10*time.Second {
		t.Fatalf("unexpected hang: %v", err)
	}
}

func TestTimeout(t *testing.T) {
	start := time.Now()
	p, err := Shell(t.Context(), "", "Start-Sleep -Seconds 30", Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for range p.Out() {
	}
	_, _ = p.Wait()
	if time.Since(start) > 6*time.Second {
		t.Fatal("timeout did not kill process")
	}
}

func TestDirWithSpaces(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "space dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := writeScript(t, dir, "in space.cmd", "@echo off\r\necho spaced-ok\r\n")
	p, err := Start(t.Context(), "cmd", []string{"/C", script}, Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	lines, _, err := collect(p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range lines {
		if l.Text == "spaced-ok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("output missing: %+v", lines)
	}
}

func TestEnvInherited(t *testing.T) {
	t.Setenv("SUPERVIBE_PROCX_TEST", "yes42")
	p, err := Shell(t.Context(), "", "Write-Output $env:SUPERVIBE_PROCX_TEST", Options{})
	if err != nil {
		t.Fatal(err)
	}
	lines, _, err := collect(p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range lines {
		if l.Text == "yes42" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env not inherited: %+v", lines)
	}
}
