//go:build dev && windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const detachedProcess = 0x00000008

// scheduleSelfDelete starts a detached process that removes this executable
// after Wails and Air have released its file handle.
func scheduleSelfDelete() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}

	path := quotePowerShell(executable)
	script := fmt.Sprintf(
		"$path=%s; for ($i=0; $i -lt 20; $i++) { Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue; if (-not (Test-Path -LiteralPath $path)) { exit 0 }; Start-Sleep -Milliseconds 250 }",
		path,
	)
	cmd := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle",
		"Hidden",
		"-Command",
		script,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess,
	}
	return cmd.Start()
}

func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
