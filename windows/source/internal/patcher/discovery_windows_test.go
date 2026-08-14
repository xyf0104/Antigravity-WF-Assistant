//go:build windows

package patcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	winapi "golang.org/x/sys/windows"
)

func TestWindowsSavedInstallPathsFromDataKeepsUniqueNonSecretPaths(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"schema": 1,
		"targets": []map[string]any{
			{"appPath": `C:\Apps\Antigravity IDE`, "version": "2.5.5"},
			{"appPath": `C:\Apps\Antigravity IDE`},
			{"appPath": `D:\Portable\Antigravity 2.0`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`C:\Apps\Antigravity IDE`, `D:\Portable\Antigravity 2.0`}
	if got := windowsSavedInstallPathsFromData(data); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved install paths = %#v, want %#v", got, want)
	}
	if got := windowsSavedInstallPathsFromData([]byte(`{"schema":2,"targets":[]}`)); got != nil {
		t.Fatalf("unsupported schema returned %#v", got)
	}
}

func TestNormalizeWindowsInstallRootParsesQuotedDisplayIconAndUninstallCommand(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Antigravity 2.0")
	exe := filepath.Join(root, "Antigravity.exe")
	for _, input := range []string{
		`"` + exe + `",0`,
		`"` + exe + `" --uninstall`,
		exe + ` /S`,
	} {
		if got, want := normalizeWindowsInstallRoot(input), filepath.Clean(root); got != want {
			t.Fatalf("normalizeWindowsInstallRoot(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeWindowsInstallRootHandlesResourcesSubpath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Google Antigravity")
	input := filepath.Join(root, "resources", "app", "out", "main.js")
	if got, want := normalizeWindowsInstallRoot(input), filepath.Clean(root); got != want {
		t.Fatalf("resources path normalized to %q, want %q", got, want)
	}
}

func TestWindowsDriveRootsForMaskSkipsRemoteAndRemovableLetters(t *testing.T) {
	mask := uint32(0)
	for _, index := range []int{1, 2, 4, 5} { // B, C, E, F
		mask |= uint32(1) << uint(index)
	}
	driveTypes := map[string]uint32{
		"B:\\": winapi.DRIVE_REMOVABLE,
		"C:\\": winapi.DRIVE_FIXED,
		"E:\\": winapi.DRIVE_REMOTE,
		"F:\\": winapi.DRIVE_FIXED,
	}
	got := windowsDriveRootsForMask(mask, func(root string) uint32 { return driveTypes[root] })
	want := []string{"C:\\", "F:\\"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fixed roots = %#v, want %#v", got, want)
	}
}

func TestWindowsShellDiscoveryPathsHandlesRegistryOutput(t *testing.T) {
	output := []byte("  C:\\Program Files\\Antigravity\\Antigravity.exe\r\n\r\n  C:\\Apps\\Antigravity 2.0  \n")
	want := []string{
		`C:\Program Files\Antigravity\Antigravity.exe`,
		`C:\Apps\Antigravity 2.0`,
	}
	if got := windowsShellDiscoveryPaths(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("registry/process output paths = %#v, want %#v", got, want)
	}
}

func TestWindowsFormatVersionWordsUsesProductVersionShape(t *testing.T) {
	ms := uint32(2<<16 | 5)
	ls := uint32(5 << 16)
	if got, want := windowsFormatVersionWords(ms, ls), "2.5.5"; got != want {
		t.Fatalf("formatted version = %q, want %q", got, want)
	}
	if got, want := windowsFormatVersionWords(1<<16|23, 2<<16|7), "1.23.2.7"; got != want {
		t.Fatalf("formatted four-part version = %q, want %q", got, want)
	}
}

func TestWindowsExecutableProductVersionReadsInstalledIDE(t *testing.T) {
	path := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Antigravity IDE", "Antigravity IDE.exe")
	if windowsExistingFile(path) == "" {
		t.Skip("official Antigravity IDE is not installed in the standard per-user path")
	}
	if version := windowsExecutableProductVersion(path); version == "" {
		t.Fatalf("failed to read the product version from %s", path)
	} else {
		t.Logf("installed Antigravity IDE product version: %s", version)
	}
}
