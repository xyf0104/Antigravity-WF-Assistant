//go:build windows

package codexdesktop

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWindowsControllerRejectsBareLookalikeAndRedactsSelectedPath(t *testing.T) {
	filesystem := newWindowsFakeFileSystem(map[string]string{
		"LOCALAPPDATA": `C:\Users\alice\AppData\Local`,
	})
	bare := filepath.Join(`C:\Temp`, "ChatGPT.exe")
	filesystem.entries[filepath.Clean(bare)] = windowsFakeFileInfo{name: "ChatGPT.exe", mode: 0755}
	controller := newControllerForTest(New(Options{FileSystem: filesystem, Registry: fakeRegistry{err: fs.ErrNotExist}, Processes: &mutableWindowsProcessLister{}}), controlOperations{
		launch: func(context.Context, desktopTarget) error { return nil },
		stop:   func(context.Context, desktopTarget) error { return nil },
	})
	if _, err := controller.SelectPath(context.Background(), bare); !errors.Is(err, ErrSelectionRejected) {
		t.Fatalf("bare lookalike selection error = %v", err)
	}

	selected := filepath.Join(`C:\Tools\ChatGPT`, "ChatGPT.exe")
	addWindowsExecutable(filesystem, selected, `{"version":"26.825.32147"}`)
	status, err := controller.SelectPath(context.Background(), selected)
	if err != nil {
		t.Fatalf("select verified desktop app: %v", err)
	}
	if !status.Selected || !status.Discovered || status.Installation.Source != SourceManualSelection || !status.Installation.ExecutableVerified {
		t.Fatalf("selection status = %#v", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal control status: %v", err)
	}
	if strings.Contains(string(encoded), selected) || strings.Contains(string(encoded), `C:\Tools\ChatGPT`) {
		t.Fatalf("control status leaked selected path: %s", encoded)
	}
}

func TestWindowsControllerLifecycleRequiresConfirmation(t *testing.T) {
	filesystem := newWindowsFakeFileSystem(map[string]string{
		"LOCALAPPDATA": `C:\Users\alice\AppData\Local`,
	})
	executable := filepath.Join(`C:\Tools\Codex`, "Codex.exe")
	addWindowsExecutable(filesystem, executable, "")
	lister := &mutableWindowsProcessLister{processes: []Process{{Executable: executable}}}
	var stopCalls int
	var launchCalls int
	controller := newControllerForTest(New(Options{FileSystem: filesystem, Registry: fakeRegistry{err: fs.ErrNotExist}, Processes: lister}), controlOperations{
		stop: func(context.Context, desktopTarget) error {
			stopCalls++
			lister.Set(nil)
			return nil
		},
		launch: func(_ context.Context, target desktopTarget) error {
			launchCalls++
			lister.Set([]Process{{Executable: target.executable}})
			return nil
		},
	})
	if _, err := controller.SelectPath(context.Background(), executable); err != nil {
		t.Fatalf("select verified desktop app: %v", err)
	}
	if _, err := controller.Stop(context.Background(), "not-confirmed"); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("stop error = %v, want confirmation requirement", err)
	}
	if stopCalls != 0 {
		t.Fatalf("stop callback called without confirmation: %d", stopCalls)
	}
	status, err := controller.Restart(context.Background(), LifecycleConfirmation)
	if err != nil {
		t.Fatalf("restart verified desktop app: %v", err)
	}
	if stopCalls != 1 || launchCalls != 1 || !status.Running {
		t.Fatalf("restart calls=(stop %d, launch %d), status=%#v", stopCalls, launchCalls, status)
	}
}

func TestTrustedWindowsStoreLaunchTargetRejectsArbitraryValues(t *testing.T) {
	packageName := "OpenAI.Codex_1.5.0.0_x64__sample"
	if !trustedWindowsStoreLaunchTargetValue("OpenAI.Codex_sample!App", packageName) {
		t.Fatal("expected official Codex AppsFolder target to be accepted")
	}
	for _, value := range []string{
		"Other.Vendor_sample!App",
		"OpenAI.Codex_sample!App\\evil",
		"OpenAI.Codex_sample!App;evil",
		"OpenAI.Codex_sample",
		"C:\\Temp\\ChatGPT.exe",
	} {
		if trustedWindowsStoreLaunchTargetValue(value, packageName) {
			t.Fatalf("unsafe Store target accepted: %q", value)
		}
	}
}

func TestWindowsStoreRegistrationProvidesOnlyVerifiedLaunchCapability(t *testing.T) {
	filesystem := newWindowsFakeFileSystem(map[string]string{
		"ProgramFiles": `C:\Program Files`,
	})
	packageName := "OpenAI.ChatGPT_26.825.32147_x64__sample"
	controller := newControllerForTest(New(Options{
		FileSystem: filesystem,
		Registry:   fakeRegistryWithNames{names: []string{packageName}},
		Processes:  &mutableWindowsProcessLister{},
	}), controlOperations{
		launch: func(context.Context, desktopTarget) error { return nil },
		stop:   func(context.Context, desktopTarget) error { return nil },
	})
	status := controller.Status(context.Background())
	if !status.Discovered || status.Selected || status.Installation.Source != SourceWindowsStore {
		t.Fatalf("Store status = %#v", status)
	}
	if !status.CanLaunch || status.CanStop || status.CanRestart {
		t.Fatalf("Store lifecycle capabilities = %#v", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal Store status: %v", err)
	}
	if strings.Contains(string(encoded), packageName) || strings.Contains(string(encoded), `C:\Program Files`) {
		t.Fatalf("Store status leaked private target detail: %s", encoded)
	}
}

func TestWindowsControllerDoesNotRetargetLifecycleToSeparatelyRunningAppPathsTarget(t *testing.T) {
	filesystem := newWindowsFakeFileSystem(map[string]string{
		"LOCALAPPDATA": `C:\Users\alice\AppData\Local`,
	})
	selectedExecutable := filepath.Join(`C:\Users\alice\AppData\Local`, "Programs", "Codex", "Codex.exe")
	addWindowsExecutable(filesystem, selectedExecutable, `{"version":"26.8.1"}`)
	runningExecutable := filepath.Join(`D:\Managed\ChatGPT`, "ChatGPT.exe")
	addWindowsExecutable(filesystem, runningExecutable, `{"version":"26.8.2"}`)
	lister := &mutableWindowsProcessLister{processes: []Process{{Executable: runningExecutable}}}
	registry := &recordingAppPathRegistry{values: []string{`"` + runningExecutable + `"`}}
	var launches, stops int
	controller := newControllerForTest(New(Options{FileSystem: filesystem, Registry: registry, Processes: lister}), controlOperations{
		launch: func(context.Context, desktopTarget) error { launches++; return nil },
		stop:   func(context.Context, desktopTarget) error { stops++; return nil },
	})

	status := controller.Status(context.Background())
	if !status.Running || status.CanLaunch || status.CanStop || status.CanRestart {
		t.Fatalf("controller status = %#v, want global running with no retargetable lifecycle action", status)
	}
	if status.Installation.Source != SourceLocalAppData || status.Installation.Version != "26.8.1" {
		t.Fatalf("controller installation = %#v, want deterministic fixed target", status.Installation)
	}
	if _, err := controller.Launch(context.Background()); !errors.Is(err, ErrDesktopAlreadyRunning) {
		t.Fatalf("launch error = %v, want running protection", err)
	}
	if _, err := controller.Stop(context.Background(), LifecycleConfirmation); !errors.Is(err, ErrDesktopNotRunning) {
		t.Fatalf("stop error = %v, want lifecycle target mismatch protection", err)
	}
	if launches != 0 || stops != 0 {
		t.Fatalf("lifecycle operation retargeted a separately running desktop: launches=%d stops=%d", launches, stops)
	}
}

type mutableWindowsProcessLister struct {
	mu        sync.Mutex
	processes []Process
}

type fakeRegistryWithNames struct {
	names []string
}

func (registry fakeRegistryWithNames) Subkeys(string, int) ([]string, error) {
	return append([]string(nil), registry.names...), nil
}

func (lister *mutableWindowsProcessLister) List(context.Context) ([]Process, error) {
	lister.mu.Lock()
	defer lister.mu.Unlock()
	return append([]Process(nil), lister.processes...), nil
}

func (lister *mutableWindowsProcessLister) Set(processes []Process) {
	lister.mu.Lock()
	defer lister.mu.Unlock()
	lister.processes = append([]Process(nil), processes...)
}
