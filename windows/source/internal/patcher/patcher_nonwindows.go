//go:build !windows

package patcher

import "fmt"

func runWindows(string) (string, error) {
	return "", fmt.Errorf("Windows 补丁器只能在 Windows 上运行")
}

func getWindowsStatus() Status { return Status{} }

func getWindowsQuickStatus() Status { return Status{} }

func refreshWindowsStatus() Status { return Status{} }

func mergeWindowsHistoryOnStartup() error { return nil }
