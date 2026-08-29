package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateLegacyStorageDirectoryRenamesSoleDirectory(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, legacyStorageDirectoryNames[1])
	target := filepath.Join(home, xiassToolsStorageDirectoryName)
	if err := os.MkdirAll(filepath.Join(legacy, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "custom_models.json"), []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyStorageDirectory(legacy, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy directory still exists: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(target, "custom_models.json")); err != nil || string(data) != "current" {
		t.Fatalf("migrated config = %q, %v", data, err)
	}
}

func TestMigrateLegacyStorageDirectoryMergesNewestFiles(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, legacyStorageDirectoryNames[0])
	target := filepath.Join(home, xiassToolsStorageDirectoryName)
	if err := os.MkdirAll(filepath.Join(legacy, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	newTime := time.Now()
	legacyConfig := filepath.Join(legacy, "custom_models.json")
	targetConfig := filepath.Join(target, "custom_models.json")
	if err := os.WriteFile(legacyConfig, []byte("latest-models"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(legacyConfig, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetConfig, []byte("stale-models"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(targetConfig, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyStorageDirectory(legacy, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(targetConfig)
	if err != nil || string(data) != "latest-models" {
		t.Fatalf("merged config = %q, %v", data, err)
	}
}
