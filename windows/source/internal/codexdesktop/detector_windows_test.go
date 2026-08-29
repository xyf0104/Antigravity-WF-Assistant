//go:build windows

package codexdesktop

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoverWindowsFixedInstallUsesExactProcessPath(t *testing.T) {
	filesystem := newWindowsFakeFileSystem(map[string]string{
		"LOCALAPPDATA": `C:\Users\alice\AppData\Local`,
		"ProgramFiles": `C:\Program Files`,
	})
	executable := filepath.Join(`C:\Users\alice\AppData\Local`, "Programs", "Codex", "Codex.exe")
	addWindowsExecutable(filesystem, executable, `{"version":"2.3.4"}`)

	status := New(Options{
		FileSystem: filesystem,
		Registry:   fakeRegistry{err: fs.ErrNotExist},
		Processes: fakeWindowsProcessLister{processes: []Process{
			{Executable: `C:\Users\alice\AppData\Roaming\npm\codex.exe`},
			{Executable: executable},
		}},
	}).Discover(context.Background())

	if status.State != StateRunning || !status.Running {
		t.Fatalf("state = %#v, want running", status)
	}
	if status.Installation.Source != SourceLocalAppData || !status.Installation.ExecutableVerified {
		t.Fatalf("installation = %#v, want verified local app data install", status.Installation)
	}
	if status.Installation.Version != "2.3.4" {
		t.Fatalf("version = %q, want 2.3.4", status.Installation.Version)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if strings.Contains(string(encoded), `C:\Users\alice`) || strings.Contains(string(encoded), executable) {
		t.Fatalf("status leaked a local path: %s", encoded)
	}
}

func TestDiscoverWindowsRejectsPseudoCodexCLIs(t *testing.T) {
	filesystem := newWindowsFakeFileSystem(map[string]string{
		"LOCALAPPDATA": `C:\Users\alice\AppData\Local`,
		"ProgramFiles": `C:\Program Files`,
	})

	status := New(Options{
		FileSystem: filesystem,
		Registry:   fakeRegistry{err: fs.ErrNotExist},
		Processes: fakeWindowsProcessLister{processes: []Process{
			{Executable: `C:\Users\alice\AppData\Roaming\npm\codex.exe`},
			{Executable: `C:\Users\alice\.cargo\bin\codex.exe`},
			{Executable: `C:\Users\alice\scoop\apps\codex\current\codex.exe`},
			{Executable: `C:\ProgramData\chocolatey\bin\codex.exe`},
			{Executable: `C:\Users\alice\AppData\Local\Antigravity\resources\codex.exe`},
		}},
	}).Discover(context.Background())

	if status.State != StateNotInstalled || status.Running || status.Installation.Present {
		t.Fatalf("pseudo CLIs produced a desktop detection: %#v", status)
	}
}

func TestDiscoverWindowsStoreRegistrationAndProtectedRunningPath(t *testing.T) {
	filesystem := newWindowsFakeFileSystem(map[string]string{
		"LOCALAPPDATA": `C:\Users\alice\AppData\Local`,
		"ProgramFiles": `C:\Program Files`,
	})
	packageName := "OpenAI.Codex_1.5.0.0_x64__sample"
	storeExecutable := filepath.Join(`C:\Program Files`, "WindowsApps", packageName, "Codex.exe")
	registry := &recordingRegistry{names: []string{packageName}}

	status := New(Options{
		FileSystem: filesystem,
		Registry:   registry,
		Processes:  fakeWindowsProcessLister{processes: []Process{{Executable: storeExecutable}}},
	}).Discover(context.Background())

	if status.State != StateRunning || !status.Running {
		t.Fatalf("state = %#v, want running Store app", status)
	}
	if status.Installation.Source != SourceWindowsStore || !status.Installation.ExecutableVerified {
		t.Fatalf("installation = %#v, want verified Store application", status.Installation)
	}
	if status.Installation.Version != "1.5.0.0" {
		t.Fatalf("version = %q, want 1.5.0.0", status.Installation.Version)
	}
	if registry.path != storePackagesRegistryPath || registry.limit != maxStorePackageKeys {
		t.Fatalf("registry request = (%q, %d), want public Store package key", registry.path, registry.limit)
	}
}

func TestDiscoverWindowsDegradesWhenProcessListFailsWithVerifiedInstall(t *testing.T) {
	filesystem := newWindowsFakeFileSystem(map[string]string{
		"LOCALAPPDATA": `C:\Users\alice\AppData\Local`,
	})
	executable := filepath.Join(`C:\Users\alice\AppData\Local`, "Programs", "ChatGPT", "ChatGPT.exe")
	addWindowsExecutable(filesystem, executable, "")
	lister := &recordingWindowsProcessLister{err: errors.New("not available")}

	status := New(Options{
		FileSystem:     filesystem,
		Registry:       fakeRegistry{err: fs.ErrNotExist},
		Processes:      lister,
		ProcessTimeout: 50 * time.Millisecond,
	}).Discover(context.Background())

	if status.State != StateDegraded || status.Running {
		t.Fatalf("state = %#v, want degraded non-running result", status)
	}
	if !status.Installation.Present || status.Installation.Source != SourceLocalAppData {
		t.Fatalf("installation = %#v, want preserved verified installation", status.Installation)
	}
	if !containsWindowsWarning(status.Warnings, WarningProcessListUnavailable) {
		t.Fatalf("warnings = %v, want process list warning", status.Warnings)
	}
	if !lister.sawDeadline {
		t.Fatal("process lister did not receive a timeout context")
	}
}

func containsWindowsWarning(warnings []Warning, wanted Warning) bool {
	for _, warning := range warnings {
		if warning == wanted {
			return true
		}
	}
	return false
}

type windowsFakeFileSystem struct {
	env      map[string]string
	home     string
	homeErr  error
	entries  map[string]fs.FileInfo
	contents map[string][]byte
}

func newWindowsFakeFileSystem(env map[string]string) *windowsFakeFileSystem {
	return &windowsFakeFileSystem{
		env:      env,
		home:     `C:\Users\alice`,
		entries:  make(map[string]fs.FileInfo),
		contents: make(map[string][]byte),
	}
}

func (filesystem *windowsFakeFileSystem) Stat(path string) (fs.FileInfo, error) {
	if entry, ok := filesystem.entries[filepath.Clean(path)]; ok {
		return entry, nil
	}
	return nil, fs.ErrNotExist
}

func (filesystem *windowsFakeFileSystem) ReadFile(path string) ([]byte, error) {
	if data, ok := filesystem.contents[filepath.Clean(path)]; ok {
		return append([]byte(nil), data...), nil
	}
	return nil, fs.ErrNotExist
}

func (filesystem *windowsFakeFileSystem) UserHomeDir() (string, error) {
	return filesystem.home, filesystem.homeErr
}

func (filesystem *windowsFakeFileSystem) Getenv(key string) string {
	return filesystem.env[key]
}

func addWindowsExecutable(filesystem *windowsFakeFileSystem, executable, metadata string) {
	executable = filepath.Clean(executable)
	filesystem.entries[executable] = windowsFakeFileInfo{name: filepath.Base(executable), mode: 0755}
	if metadata != "" {
		metadataPath := filepath.Join(filepath.Dir(executable), "resources", "app", "product.json")
		filesystem.contents[metadataPath] = []byte(metadata)
	}
}

type windowsFakeFileInfo struct {
	name string
	mode fs.FileMode
}

func (info windowsFakeFileInfo) Name() string       { return info.name }
func (info windowsFakeFileInfo) Size() int64        { return 0 }
func (info windowsFakeFileInfo) Mode() fs.FileMode  { return info.mode }
func (info windowsFakeFileInfo) ModTime() time.Time { return time.Time{} }
func (info windowsFakeFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info windowsFakeFileInfo) Sys() any           { return nil }

type fakeRegistry struct {
	err error
}

func (registry fakeRegistry) Subkeys(string, int) ([]string, error) {
	return nil, registry.err
}

type recordingRegistry struct {
	names []string
	path  string
	limit int
}

func (registry *recordingRegistry) Subkeys(path string, limit int) ([]string, error) {
	registry.path = path
	registry.limit = limit
	return registry.names, nil
}

type fakeWindowsProcessLister struct {
	processes []Process
	err       error
}

func (lister fakeWindowsProcessLister) List(context.Context) ([]Process, error) {
	return lister.processes, lister.err
}

type recordingWindowsProcessLister struct {
	err         error
	sawDeadline bool
}

func (lister *recordingWindowsProcessLister) List(ctx context.Context) ([]Process, error) {
	_, lister.sawDeadline = ctx.Deadline()
	return nil, lister.err
}
