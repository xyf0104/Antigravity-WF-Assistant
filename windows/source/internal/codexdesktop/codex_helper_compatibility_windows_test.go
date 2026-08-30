//go:build windows

package codexdesktop

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// This fixture verifies the bounded public Desktop-shape contract used by the
// former XIASS Codex Helper. It uses fake public installation metadata and a
// fake process snapshot; no registry, installed app, or user data is read.
func TestCodexHelperCompatibilityDiscoversVerifiedWindowsDesktopFixture(t *testing.T) {
	filesystem := newWindowsFakeFileSystem(map[string]string{
		"LOCALAPPDATA": `C:\Users\compatibility\AppData\Local`,
		"ProgramFiles": `C:\Program Files`,
	})
	executable := filepath.Join(`C:\Users\compatibility\AppData\Local`, "Programs", "Codex", "Codex.exe")
	addWindowsExecutable(filesystem, executable, `{"version":"26.8.1"}`)

	status := New(Options{
		FileSystem: filesystem,
		Registry:   fakeRegistry{},
		Processes:  fakeWindowsProcessLister{processes: []Process{{Executable: executable}}},
	}).Discover(context.Background())

	if status.State != StateRunning || !status.Running {
		t.Fatalf("fixture state = %#v, want verified running desktop", status)
	}
	if !status.Installation.Present || status.Installation.Source != SourceLocalAppData || !status.Installation.ExecutableVerified || status.Installation.Version != "26.8.1" {
		t.Fatalf("fixture installation = %#v, want verified LocalAppData Codex app", status.Installation)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal redacted discovery result: %v", err)
	}
	for _, forbidden := range []string{executable, `C:\Users\compatibility`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("desktop discovery leaked fixture path %q: %s", forbidden, encoded)
		}
	}
}
