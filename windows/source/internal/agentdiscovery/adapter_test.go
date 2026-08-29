package agentdiscovery

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"antigravity-byok/internal/agent"
)

func TestClaudeCodeDetectsCLIAndConfiguration(t *testing.T) {
	home := filepath.Join("C:", "Users", "fixture")
	config := joinClaudeConfigPath(home)
	fixture := newFilesystemFixture(home)
	fixture.addDirectory(config)
	runner := commandFixture{
		paths:  map[string]string{"claude": filepath.Join("C:", "Tools", "claude.exe")},
		output: map[string][]byte{filepath.Join("C:", "Tools", "claude.exe"): []byte("2.1.8 (Claude Code)\n")},
	}
	adapter := NewClaudeCodeAdapter(Options{FileSystem: fixture, Commands: runner, Now: fixedNow})

	status, err := adapter.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != agent.StateReady {
		t.Fatalf("state = %q, want %q (%s)", status.State, agent.StateReady, status.Message)
	}
	if status.Installation.Root != config || status.Installation.ExecutablePath == "" {
		t.Fatalf("installation = %+v, want config and CLI", status.Installation)
	}
	if status.Installation.Version != "2.1.8" {
		t.Fatalf("version = %q, want 2.1.8", status.Installation.Version)
	}
	assertDiscoveryOnly(t, status)
}

func TestClaudeCodePartialAndVersionFailureBoundaries(t *testing.T) {
	home := filepath.Join("C:", "Users", "fixture")
	config := joinClaudeConfigPath(home)
	cli := filepath.Join("C:", "Tools", "claude.exe")

	t.Run("configuration-only-is-detected", func(t *testing.T) {
		fixture := newFilesystemFixture(home)
		fixture.addDirectory(config)
		status, err := NewClaudeCodeAdapter(Options{FileSystem: fixture, Commands: commandFixture{}, Now: fixedNow}).Detect(context.Background())
		if err != nil || status.State != agent.StateDetected {
			t.Fatalf("status = %+v, err = %v; want detected without error", status, err)
		}
	})

	t.Run("failed-version-probe-is-degraded-not-fatal", func(t *testing.T) {
		fixture := newFilesystemFixture(home)
		fixture.addDirectory(config)
		status, err := NewClaudeCodeAdapter(Options{
			FileSystem: fixture,
			Commands: commandFixture{
				paths:     map[string]string{"claude": cli},
				outputErr: map[string]error{cli: errors.New("temporary command failure")},
			},
			Now: fixedNow,
		}).Detect(context.Background())
		if err != nil {
			t.Fatalf("expected non-fatal detector result, got %v", err)
		}
		if status.State != agent.StateDegraded {
			t.Fatalf("state = %q, want %q", status.State, agent.StateDegraded)
		}
	})
}

func TestWindowsDesktopDiscoveryStateBoundaries(t *testing.T) {
	local := filepath.Join("C:", "Users", "fixture", "AppData", "Local")
	roaming := filepath.Join("C:", "Users", "fixture", "AppData", "Roaming")
	home := filepath.Join("C:", "Users", "fixture")
	executable := filepath.Join(local, "Programs", "Cursor", "Cursor.exe")
	installation := filepath.Dir(executable)
	dataPath := filepath.Join(roaming, "Cursor")
	productMetadata := filepath.Join(installation, "resources", "app", "product.json")

	t.Run("installation-and-data-is-ready", func(t *testing.T) {
		fixture := newWindowsFixture(home, local, roaming)
		fixture.addDirectory(installation)
		fixture.addFile(executable)
		fixture.addDirectory(dataPath)
		fixture.files[productMetadata] = []byte(`{"version":"0.48.7"}`)
		status, err := NewCursorAdapter(Options{FileSystem: fixture, Now: fixedNow}).Detect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if status.State != agent.StateReady {
			t.Fatalf("state = %q, want ready (%s)", status.State, status.Message)
		}
		if status.Installation.Version != "0.48.7" || status.Details["dataDirectory"] != dataPath {
			t.Fatalf("status = %+v, want version and data path", status)
		}
		assertDiscoveryOnly(t, status)
	})

	t.Run("local-data-only-is-detected", func(t *testing.T) {
		fixture := newWindowsFixture(home, local, roaming)
		fixture.addDirectory(dataPath)
		status, err := NewCursorAdapter(Options{FileSystem: fixture, Now: fixedNow}).Detect(context.Background())
		if err != nil || status.State != agent.StateDetected {
			t.Fatalf("status = %+v, err = %v; want detected", status, err)
		}
	})

	t.Run("corrupt-installation-is-degraded", func(t *testing.T) {
		fixture := newWindowsFixture(home, local, roaming)
		fixture.addDirectory(installation)
		fixture.addDirectory(dataPath)
		status, err := NewCursorAdapter(Options{FileSystem: fixture, Now: fixedNow}).Detect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if status.State != agent.StateDegraded {
			t.Fatalf("state = %q, want degraded (%s)", status.State, status.Message)
		}
	})

	t.Run("no-artifacts-is-not-installed", func(t *testing.T) {
		fixture := newWindowsFixture(home, local, roaming)
		status, err := NewWindsurfAdapter(Options{FileSystem: fixture, Now: fixedNow}).Detect(context.Background())
		if err != nil || status.State != agent.StateNotInstalled {
			t.Fatalf("status = %+v, err = %v; want not-installed", status, err)
		}
	})
}

func TestWindowsUnreadableLocalDataIsDegraded(t *testing.T) {
	home := filepath.Join("C:", "Users", "fixture")
	local := filepath.Join(home, "AppData", "Local")
	roaming := filepath.Join(home, "AppData", "Roaming")
	executable := filepath.Join(local, "Programs", "Windsurf", "Windsurf.exe")
	fixture := newWindowsFixture(home, local, roaming)
	fixture.addDirectory(filepath.Dir(executable))
	fixture.addFile(executable)
	fixture.errors[filepath.Join(roaming, "Windsurf")] = fs.ErrPermission

	status, err := NewWindsurfAdapter(Options{FileSystem: fixture, Now: fixedNow}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != agent.StateDegraded {
		t.Fatalf("state = %q, want degraded for unreadable local data", status.State)
	}
}

func TestFailedClaudeProbeDoesNotPreventOtherRegistryAdapters(t *testing.T) {
	home := filepath.Join("C:", "Users", "fixture")
	local := filepath.Join(home, "AppData", "Local")
	roaming := filepath.Join(home, "AppData", "Roaming")
	cursorFS := newWindowsFixture(home, local, roaming)
	cursorExecutable := filepath.Join(local, "Programs", "Cursor", "Cursor.exe")
	cursorFS.addDirectory(filepath.Dir(cursorExecutable))
	cursorFS.addFile(cursorExecutable)
	cursorFS.addDirectory(filepath.Join(roaming, "Cursor"))
	windsurfFS := newWindowsFixture(home, local, roaming)
	claudeFS := newFilesystemFixture(home)
	claudeFS.addDirectory(joinClaudeConfigPath(home))
	claudeCLI := filepath.Join("C:", "Tools", "claude.exe")

	registry := agent.NewDefaultRegistry()
	if err := registry.Bind(NewClaudeCodeAdapter(Options{
		FileSystem: claudeFS,
		Commands: commandFixture{
			paths: map[string]string{"claude": claudeCLI},
			block: true,
		},
		VersionTimeout: time.Millisecond,
		Now:            fixedNow,
	})); err != nil {
		t.Fatal(err)
	}
	if err := registry.Bind(NewCursorAdapter(Options{FileSystem: cursorFS, Now: fixedNow})); err != nil {
		t.Fatal(err)
	}
	if err := registry.Bind(NewWindsurfAdapter(Options{FileSystem: windsurfFS, Now: fixedNow})); err != nil {
		t.Fatal(err)
	}

	aggregate := registry.DetectAll(context.Background())
	states := make(map[agent.ID]agent.State, len(aggregate.Agents))
	for _, status := range aggregate.Agents {
		states[status.AgentID] = status.State
	}
	if states[agent.ClaudeCodeID] != agent.StateDegraded {
		t.Fatalf("Claude state = %q, want degraded", states[agent.ClaudeCodeID])
	}
	if states[agent.CursorID] != agent.StateReady {
		t.Fatalf("Cursor state = %q, want ready after Claude failure", states[agent.CursorID])
	}
}

func TestClaudeVersionTimeoutIsBoundedAndDegraded(t *testing.T) {
	home := filepath.Join("C:", "Users", "fixture")
	fixture := newFilesystemFixture(home)
	fixture.addDirectory(joinClaudeConfigPath(home))
	cli := filepath.Join("C:", "Tools", "claude.exe")
	adapter := NewClaudeCodeAdapter(Options{
		FileSystem: fixture,
		Commands: commandFixture{
			paths: map[string]string{"claude": cli},
			block: true,
		},
		VersionTimeout: time.Millisecond,
		Now:            fixedNow,
	})
	status, err := adapter.Detect(context.Background())
	if err != nil || status.State != agent.StateDegraded {
		t.Fatalf("status = %+v, err = %v; want bounded degraded result", status, err)
	}
}

func assertDiscoveryOnly(t *testing.T, status agent.Status) {
	t.Helper()
	for _, capability := range status.Capabilities {
		if capability.Capability == agent.CapabilityDiscovery {
			if !capability.Available || capability.Availability != agent.CapabilityAvailable {
				t.Fatalf("discovery capability = %+v, want available", capability)
			}
			continue
		}
		if capability.Available {
			t.Fatalf("non-discovery capability unexpectedly available: %+v", capability)
		}
	}
}

var fixedNow = func() time.Time { return time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC) }

type filesystemFixture struct {
	home    string
	homeErr error
	env     map[string]string
	entries map[string]fixtureFileInfo
	files   map[string][]byte
	errors  map[string]error
}

func newFilesystemFixture(home string) *filesystemFixture {
	return &filesystemFixture{
		home:    home,
		env:     map[string]string{},
		entries: map[string]fixtureFileInfo{},
		files:   map[string][]byte{},
		errors:  map[string]error{},
	}
}

func newWindowsFixture(home, local, roaming string) *filesystemFixture {
	fixture := newFilesystemFixture(home)
	fixture.env["LOCALAPPDATA"] = local
	fixture.env["APPDATA"] = roaming
	fixture.env["ProgramFiles"] = filepath.Join("C:", "Program Files")
	fixture.env["ProgramFiles(x86)"] = filepath.Join("C:", "Program Files (x86)")
	return fixture
}

func (fixture *filesystemFixture) addDirectory(path string) {
	fixture.entries[filepath.Clean(path)] = fixtureFileInfo{name: filepath.Base(path), directory: true}
}

func (fixture *filesystemFixture) addFile(path string) {
	fixture.entries[filepath.Clean(path)] = fixtureFileInfo{name: filepath.Base(path)}
}

func (fixture *filesystemFixture) Stat(path string) (fs.FileInfo, error) {
	path = filepath.Clean(path)
	if err, exists := fixture.errors[path]; exists {
		return nil, err
	}
	if info, exists := fixture.entries[path]; exists {
		return info, nil
	}
	return nil, fs.ErrNotExist
}

func (fixture *filesystemFixture) ReadFile(path string) ([]byte, error) {
	path = filepath.Clean(path)
	if err, exists := fixture.errors[path]; exists {
		return nil, err
	}
	data, exists := fixture.files[path]
	if !exists {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (fixture *filesystemFixture) UserHomeDir() (string, error) { return fixture.home, fixture.homeErr }
func (fixture *filesystemFixture) Getenv(key string) string     { return fixture.env[key] }

type fixtureFileInfo struct {
	name      string
	directory bool
}

func (info fixtureFileInfo) Name() string { return info.name }
func (info fixtureFileInfo) Size() int64  { return 0 }
func (info fixtureFileInfo) Mode() fs.FileMode {
	if info.directory {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (info fixtureFileInfo) ModTime() time.Time { return time.Time{} }
func (info fixtureFileInfo) IsDir() bool        { return info.directory }
func (info fixtureFileInfo) Sys() any           { return nil }

type commandFixture struct {
	paths     map[string]string
	lookErr   map[string]error
	output    map[string][]byte
	outputErr map[string]error
	block     bool
}

func (fixture commandFixture) LookPath(name string) (string, error) {
	if err := fixture.lookErr[name]; err != nil {
		return "", err
	}
	if path, exists := fixture.paths[name]; exists {
		return path, nil
	}
	return "", exec.ErrNotFound
}

func (fixture commandFixture) Output(ctx context.Context, name string, _ ...string) ([]byte, error) {
	if fixture.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := fixture.outputErr[name]; err != nil {
		return nil, err
	}
	return append([]byte(nil), fixture.output[name]...), nil
}
