//go:build !windows

package app

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func openDirectoryDialog(ctx context.Context, title string) (string, error) {
	return runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: title})
}

func openMultipleFilesDialog(ctx context.Context, title string) ([]string, error) {
	return runtime.OpenMultipleFilesDialog(ctx, runtime.OpenDialogOptions{Title: title})
}
