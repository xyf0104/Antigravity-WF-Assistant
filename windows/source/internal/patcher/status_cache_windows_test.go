//go:build windows

package patcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsCachedTargetSupportRequiresUnchangedFingerprint(t *testing.T) {
	invalidateWindowsStatusCache()
	t.Cleanup(invalidateWindowsStatusCache)
	root := t.TempDir()
	executable := filepath.Join(root, "Antigravity.exe")
	if err := os.WriteFile(executable, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := windowsTarget{kind: "ide", root: root, executable: executable}
	cacheWindowsStatus(Status{Targets: []TargetStatus{{
		Kind: "ide", AppPath: root, ExecutablePath: executable,
		Supported: true, ConnectionMode: "user-settings",
	}}})
	if supported, mode, _, ok := windowsCachedTargetSupport(target); !ok || !supported || mode != "user-settings" {
		t.Fatalf("unchanged cached support = supported:%t mode:%q ok:%t", supported, mode, ok)
	}
	if err := os.WriteFile(executable, []byte("a different executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := windowsCachedTargetSupport(target); ok {
		t.Fatal("changed executable incorrectly reused cached compatibility")
	}
}
