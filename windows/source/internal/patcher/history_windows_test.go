//go:build windows

package patcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeWindowsHistoryAtCombinesAllLegacyRootsWithoutOverwrite(t *testing.T) {
	home := t.TempDir()
	gemini := filepath.Join(home, ".gemini")
	target := filepath.Join(gemini, "antigravity", "conversations")
	legacyIDE := filepath.Join(gemini, "antigravity-ide", "conversations")
	legacyAgent := filepath.Join(gemini, "antigravity-agent", "conversations")
	for _, dir := range []string{target, legacyIDE, legacyAgent} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "shared.json"), []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyIDE, "shared.json"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyIDE, "ide.json"), []byte("ide"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyAgent, "agent.json"), []byte("agent"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := mergeWindowsHistoryAt(home); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shared.json", "ide.json", "agent.json"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Fatalf("merged conversation %s missing: %v", name, err)
		}
	}
	shared, err := os.ReadFile(filepath.Join(target, "shared.json"))
	if err != nil || string(shared) != "current" {
		t.Fatalf("current conversation was overwritten: %q, %v", shared, err)
	}
	for _, source := range []string{filepath.Dir(legacyIDE), filepath.Dir(legacyAgent)} {
		if _, err := os.Stat(source + ".antigravity-wf-backup"); err != nil {
			t.Fatalf("backup for %s missing: %v", source, err)
		}
	}
}

func TestMergeWindowsHistoryAtCreatesCanonicalTarget(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".gemini", "antigravity-ide", "conversations")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "old.json"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeWindowsHistoryAt(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "antigravity", "conversations", "old.json")); err != nil {
		t.Fatalf("canonical history was not created: %v", err)
	}
}
