package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"antigravity-byok/internal/claudeconfig"
)

func TestClaudeCodeBridgeWritesOnlySettingsAndNeverSerializesToken(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, "selected-claude")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	credentialsPath := filepath.Join(configDir, ".credentials.json")
	legacyPath := filepath.Join(home, ".claude.json")
	credentials := []byte(`{"credential":"must-stay-private"}`)
	legacy := []byte(`{"legacy":"must-stay-private"}`)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath, credentials, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	secret := "sk-claude-bridge-secret-must-not-escape"
	app := &App{}
	status := app.ApplyClaudeCodeConfiguration(ClaudeCodeApplyInput{
		BaseURL:   "https://api.example.test/v1",
		AuthToken: secret,
		Model:     "claude-sonnet-4-5",
	})
	if !status.OK || !status.Snapshot.Valid || !status.Snapshot.Managed || !status.Snapshot.AuthTokenConfigured {
		t.Fatalf("unexpected apply status: %#v", status)
	}
	if len(status.Backups) != 1 || status.Backups[0].ID == "" {
		t.Fatalf("apply did not expose a verified redacted backup: %#v", status.Backups)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "must-stay-private") {
		t.Fatalf("bridge result leaked secret material: %s", encoded)
	}
	if strings.Contains(string(encoded), configDir) || strings.Contains(string(encoded), "settingsPath") || strings.Contains(string(encoded), "configDir") {
		t.Fatalf("bridge result leaked Claude settings path metadata: %s", encoded)
	}

	manager, err := claudeconfig.NewDefaultManager()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := os.ReadFile(manager.SettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), secret) {
		t.Fatal("managed Claude settings did not receive the request-local token")
	}
	if got, err := os.ReadFile(credentialsPath); err != nil || string(got) != string(credentials) {
		t.Fatalf("credentials file was changed: %q, %v", got, err)
	}
	if got, err := os.ReadFile(legacyPath); err != nil || string(got) != string(legacy) {
		t.Fatalf("legacy Claude file was changed: %q, %v", got, err)
	}

	invalid := app.ApplyClaudeCodeConfiguration(ClaudeCodeApplyInput{
		BaseURL:   "https://api.example.test/v1",
		AuthToken: "Bearer " + secret,
		Model:     "claude-sonnet-4-5",
	})
	if invalid.OK {
		t.Fatalf("invalid token was accepted: %#v", invalid)
	}
	encodedInvalid, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedInvalid), secret) {
		t.Fatalf("bridge error leaked request token: %s", encodedInvalid)
	}
}

func TestClaudeCodeBridgeUsesEmptySerializedBackupArrays(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "empty-claude"))

	status := (&App{}).GetClaudeCodeConfiguration()
	if !status.OK || status.Backups == nil || status.LegacyBackups == nil {
		t.Fatalf("empty bridge state = %#v", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"backups":[]`) || !strings.Contains(string(encoded), `"legacyBackups":[]`) {
		t.Fatalf("empty backup arrays were not serialized: %s", encoded)
	}
}
