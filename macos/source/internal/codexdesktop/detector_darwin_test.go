//go:build darwin

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

func TestDiscoverAcceptsVerifiedChatGPTBundleAndReportsRunning(t *testing.T) {
	filesystem := newFakeFileSystem("/Users/alice")
	bundle := "/Applications/ChatGPT.app"
	executable := addMacBundle(filesystem, bundle, expectedBundleIdentifier, "ChatGPT", "1.24.3")
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))

	status := New(Options{
		FileSystem: filesystem,
		Processes:  fakeProcessLister{processes: []Process{{Executable: executable}}},
		Now:        func() time.Time { return now },
	}).Discover(context.Background())

	if status.State != StateRunning || !status.Running {
		t.Fatalf("state = %#v, want running", status)
	}
	if !status.Installation.Present || status.Installation.Source != SourceSystemApplications {
		t.Fatalf("installation = %#v, want verified system application", status.Installation)
	}
	if !status.Installation.ExecutableVerified || status.Installation.Version != "1.24.3" {
		t.Fatalf("installation = %#v, want executable and public version", status.Installation)
	}
	if !status.CheckedAt.Equal(now.UTC()) {
		t.Fatalf("checked at = %s, want %s", status.CheckedAt, now.UTC())
	}

	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if strings.Contains(string(encoded), "/Applications/") || strings.Contains(string(encoded), "/Users/alice") || strings.Contains(string(encoded), executable) {
		t.Fatalf("status leaked a local path: %s", encoded)
	}
}

func TestDiscoverRejectsWrongBundleIdentifier(t *testing.T) {
	filesystem := newFakeFileSystem("/Users/alice")
	addMacBundle(filesystem, "/Applications/Codex.app", "com.example.codex", "Codex", "1.0.0")
	lister := &recordingProcessLister{}

	status := New(Options{FileSystem: filesystem, Processes: lister}).Discover(context.Background())

	if status.State != StateDegraded {
		t.Fatalf("state = %q, want %q", status.State, StateDegraded)
	}
	if status.Installation.Present || status.Running {
		t.Fatalf("unexpected installation state: %#v", status)
	}
	if !containsWarning(status.Warnings, WarningInvalidInstallation) {
		t.Fatalf("warnings = %v, want invalid installation", status.Warnings)
	}
	if lister.calls != 0 {
		t.Fatalf("process lister called %d times for an unverified bundle", lister.calls)
	}
}

func TestDiscoverUsesUserApplicationsAndBoundsProcessList(t *testing.T) {
	filesystem := newFakeFileSystem("/Users/alice")
	bundle := filepath.Join("/Users/alice", "Applications", "Codex.app")
	addMacBundle(filesystem, bundle, expectedBundleIdentifier, "Codex", "2.0.0")
	lister := &recordingProcessLister{err: errors.New("unavailable")}

	status := New(Options{
		FileSystem:     filesystem,
		Processes:      lister,
		ProcessTimeout: 50 * time.Millisecond,
	}).Discover(context.Background())

	if status.State != StateDegraded {
		t.Fatalf("state = %q, want %q", status.State, StateDegraded)
	}
	if status.Installation.Source != SourceUserApplications || !status.Installation.ExecutableVerified {
		t.Fatalf("installation = %#v, want verified user application", status.Installation)
	}
	if !containsWarning(status.Warnings, WarningProcessListUnavailable) {
		t.Fatalf("warnings = %v, want process list warning", status.Warnings)
	}
	if !lister.sawDeadline {
		t.Fatal("process lister did not receive a timeout context")
	}
}

func containsWarning(warnings []Warning, wanted Warning) bool {
	for _, warning := range warnings {
		if warning == wanted {
			return true
		}
	}
	return false
}

type fakeFileSystem struct {
	home     string
	homeErr  error
	entries  map[string]fs.FileInfo
	contents map[string][]byte
}

func newFakeFileSystem(home string) *fakeFileSystem {
	return &fakeFileSystem{
		home:     home,
		entries:  make(map[string]fs.FileInfo),
		contents: make(map[string][]byte),
	}
}

func (filesystem *fakeFileSystem) Stat(path string) (fs.FileInfo, error) {
	if entry, ok := filesystem.entries[filepath.Clean(path)]; ok {
		return entry, nil
	}
	return nil, fs.ErrNotExist
}

func (filesystem *fakeFileSystem) ReadFile(path string) ([]byte, error) {
	if data, ok := filesystem.contents[filepath.Clean(path)]; ok {
		return append([]byte(nil), data...), nil
	}
	return nil, fs.ErrNotExist
}

func (filesystem *fakeFileSystem) UserHomeDir() (string, error) {
	return filesystem.home, filesystem.homeErr
}

func (*fakeFileSystem) Getenv(string) string { return "" }

func addMacBundle(filesystem *fakeFileSystem, bundle, identifier, executable, version string) string {
	bundle = filepath.Clean(bundle)
	filesystem.entries[bundle] = fakeFileInfo{name: filepath.Base(bundle), mode: fs.ModeDir | 0755}
	infoPath := filepath.Join(bundle, "Contents", "Info.plist")
	filesystem.contents[infoPath] = []byte("<?xml version=\"1.0\"?><plist><dict>" +
		"<key>CFBundleIdentifier</key><string>" + identifier + "</string>" +
		"<key>CFBundleExecutable</key><string>" + executable + "</string>" +
		"<key>CFBundleShortVersionString</key><string>" + version + "</string>" +
		"</dict></plist>")
	executablePath := filepath.Join(bundle, "Contents", "MacOS", executable)
	filesystem.entries[executablePath] = fakeFileInfo{name: executable, mode: 0755}
	return executablePath
}

type fakeFileInfo struct {
	name string
	mode fs.FileMode
}

func (info fakeFileInfo) Name() string       { return info.name }
func (info fakeFileInfo) Size() int64        { return 0 }
func (info fakeFileInfo) Mode() fs.FileMode  { return info.mode }
func (info fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (info fakeFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info fakeFileInfo) Sys() any           { return nil }

type fakeProcessLister struct {
	processes []Process
	err       error
}

func (lister fakeProcessLister) List(context.Context) ([]Process, error) {
	return lister.processes, lister.err
}

type recordingProcessLister struct {
	calls       int
	err         error
	sawDeadline bool
}

func (lister *recordingProcessLister) List(ctx context.Context) ([]Process, error) {
	lister.calls++
	_, lister.sawDeadline = ctx.Deadline()
	return nil, lister.err
}
