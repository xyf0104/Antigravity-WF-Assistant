//go:build windows

package main

import (
	"context"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// selectCodexDesktopNativeTarget keeps a chooser path on the Go side. The
// renderer receives only the post-validation redacted status, never the EXE
// path selected in this native panel.
func selectCodexDesktopNativeTarget(ctx context.Context) (string, bool, error) {
	selected, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "选择 Codex 或 ChatGPT Desktop 应用",
		Filters: []runtime.FileFilter{{
			DisplayName: "Codex / ChatGPT 应用 (*.exe)",
			Pattern:     "Codex.exe;ChatGPT.exe",
		}},
	})
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(selected) == "" {
		return "", true, nil
	}
	return selected, false, nil
}
