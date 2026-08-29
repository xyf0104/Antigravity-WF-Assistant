//go:build windows

package codexdesktop

import (
	"context"
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	maxProcessEntries = 8192
	maxProcessPath    = 32768
)

// systemProcessLister reads a Toolhelp snapshot. It opens individual process
// handles only with PROCESS_QUERY_LIMITED_INFORMATION, never asks for write or
// termination rights, and ignores processes whose public executable name is
// unrelated to the desktop app.
type systemProcessLister struct{}

func (systemProcessLister) List(ctx context.Context) ([]Process, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}

	processes := make([]Process, 0)
	for count := 0; count < maxProcessEntries; count++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(windows.UTF16ToString(entry.ExeFile[:]))
		if isDesktopExecutableName(name) {
			path, pathErr := processExecutablePath(entry.ProcessID)
			if pathErr != nil {
				// A matching executable name without a readable full path cannot
				// be safely classified as the desktop app or a CLI lookalike.
				return nil, pathErr
			}
			if path == "" {
				return nil, errors.New("candidate process has no executable path")
			}
			processes = append(processes, Process{Executable: path})
		}

		err := windows.Process32Next(snapshot, &entry)
		if err == nil {
			continue
		}
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return processes, nil
		}
		return nil, err
	}
	return nil, errors.New("process snapshot exceeded its fixed limit")
}

func processExecutablePath(processID uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	for size := uint32(1024); size <= maxProcessPath; size *= 2 {
		buffer := make([]uint16, size)
		length := size
		err = windows.QueryFullProcessImageName(handle, 0, &buffer[0], &length)
		if err == nil {
			return windows.UTF16ToString(buffer[:length]), nil
		}
		if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			return "", err
		}
	}
	return "", windows.ERROR_INSUFFICIENT_BUFFER
}
