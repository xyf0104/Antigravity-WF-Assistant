//go:build windows

package patcher

import (
	"path/filepath"
	"reflect"
	"testing"

	winapi "golang.org/x/sys/windows"
)

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
