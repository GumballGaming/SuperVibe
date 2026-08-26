//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func openDirectoryDialog(_ context.Context, title string) (string, error) {
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'; Add-Type -AssemblyName System.Windows.Forms; $dialog = New-Object System.Windows.Forms.FolderBrowserDialog; $dialog.Description = %s; $dialog.ShowNewFolderButton = $true; if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($dialog.SelectedPath) }`, quotePowerShellLiteral(title))
	cmd := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-STA",
		"-WindowStyle",
		"Hidden",
		"-Command",
		script,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("directory picker failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
func openMultipleFilesDialog(_ context.Context, title string) ([]string, error) {
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'; Add-Type -AssemblyName System.Windows.Forms; $dialog = New-Object System.Windows.Forms.OpenFileDialog; $dialog.Title = %s; $dialog.Multiselect = $true; if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { ConvertTo-Json -InputObject @($dialog.FileNames) -Compress } else { '[]' }`, quotePowerShellLiteral(title))
	cmd := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-STA",
		"-WindowStyle",
		"Hidden",
		"-Command",
		script,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("file picker failed: %w", err)
	}
	var paths []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(output))), &paths); err != nil {
		return nil, fmt.Errorf("file picker response invalid: %w", err)
	}
	return paths, nil
}

func quotePowerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
