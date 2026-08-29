package mcpconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFixedGlobalPathsOnly(t *testing.T) {
	for _, target := range []Target{TargetCursor, TargetWindsurf} {
		t.Run(string(target), func(t *testing.T) {
			manager := newTestManager(t, target)
			want := filepath.Join(manager.userHome, ".cursor", "mcp.json")
			if target == TargetWindsurf {
				want = filepath.Join(manager.userHome, ".codeium", "windsurf", "mcp_config.json")
			}
			if manager.configPath != want {
				t.Fatalf("configuration path did not use the documented fixed location")
			}
			if err := manager.validatePaths(); err != nil {
				t.Fatalf("fixed manager was not valid: %v", err)
			}
		})
	}
}

func TestInspectRedactsAllConfigurationValues(t *testing.T) {
	manager := newTestManager(t, TargetCursor)
	writeTestConfiguration(t, manager, []byte(`{
  "futureMetadata": {"safe": true},
  "mcpServers": {
    "foreign": {"url": "https://foreign.example/mcp"},
    "xiass-tools": {"url": "https://old.example/mcp"}
  }
}`))

	snapshot, err := manager.Inspect()
	if err != nil {
		t.Fatalf("inspect failed: %v", err)
	}
	if !snapshot.Exists || !snapshot.Valid || !snapshot.ManagedServerConfigured || snapshot.ServerCount != 2 || snapshot.HasSensitiveConfiguration {
		t.Fatalf("unexpected redacted snapshot: %+v", snapshot)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	assertDoesNotContain(t, encoded, "foreign.example", "old.example", "mcp.json", "futureMetadata", "url")
}

func TestApplyPreservesForeignAndUnknownEntriesAndCreatesRedactedBackup(t *testing.T) {
	for _, target := range []Target{TargetCursor, TargetWindsurf} {
		t.Run(string(target), func(t *testing.T) {
			manager := newTestManager(t, target)
			foreignRaw := []byte(`{"url":"https://foreign.example/mcp"}`)
			unknownRaw := []byte(`{"enabled":true,"revision":7}`)
			writeTestConfiguration(t, manager, []byte(`{"futureMetadata":{"enabled":true,"revision":7},"mcpServers":{"foreign":{"url":"https://foreign.example/mcp"}}}`))

			result, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://xiass.example/mcp"})
			if err != nil {
				t.Fatalf("apply failed: %v", err)
			}
			if !result.BackupCreated || !result.Snapshot.Valid || !result.Snapshot.ManagedServerConfigured || result.Snapshot.ServerCount != 2 {
				t.Fatalf("unexpected apply result: %+v", result)
			}
			encodedResult, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			assertDoesNotContain(t, encodedResult, "xiass.example", "foreign.example", "mcp.json", "serverUrl", "url")

			root := readConfigurationObject(t, manager.configPath)
			if !jsonSemanticallyEqual(root["futureMetadata"], unknownRaw) {
				t.Fatal("unknown top-level value changed during apply")
			}
			servers := readRawObject(t, root["mcpServers"])
			if !jsonSemanticallyEqual(servers["foreign"], foreignRaw) {
				t.Fatal("foreign MCP entry changed during apply")
			}
			managed := readRawObject(t, servers[ManagedServerID])
			field := "url"
			if target == TargetWindsurf {
				field = "serverUrl"
			}
			var endpoint string
			if err := json.Unmarshal(managed[field], &endpoint); err != nil || endpoint != "https://xiass.example/mcp" || len(managed) != 1 {
				t.Fatal("managed entry was not the restricted target-specific remote shape")
			}

			backupDirectory := onlyBackupDirectory(t, manager)
			manifest, err := os.ReadFile(filepath.Join(backupDirectory, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			assertDoesNotContain(t, manifest, "xiass.example", "foreign.example", "mcp.json", "url", "headers", "env")
			backup, err := os.Stat(filepath.Join(backupDirectory, "configuration.json"))
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && backup.Mode().Perm()&0o077 != 0 {
				t.Fatalf("backup is not private: %o", backup.Mode().Perm())
			}
		})
	}
}

func TestSensitiveConfigurationsAreReadOnlyAndRedacted(t *testing.T) {
	cases := map[string][]byte{
		"env":     []byte(`{"mcpServers":{"foreign":{"env":{"API_KEY":"direct-secret"}}}}`),
		"headers": []byte(`{"mcpServers":{"foreign":{"headers":{"Authorization":"Bearer direct-secret"}}}}`),
		"oauth":   []byte(`{"mcpServers":{"foreign":{"auth":{"CLIENT_SECRET":"direct-secret"}}}}`),
		"stdio":   []byte(`{"mcpServers":{"foreign":{"command":"npx","args":["private-argument"]}}}`),
	}
	for name, configuration := range cases {
		t.Run(name, func(t *testing.T) {
			manager := newTestManager(t, TargetCursor)
			writeTestConfiguration(t, manager, configuration)
			snapshot, err := manager.Inspect()
			if err != nil || !snapshot.Valid || !snapshot.HasSensitiveConfiguration {
				t.Fatalf("sensitive inspection was not a safe read-only result: %+v / %v", snapshot, err)
			}
			encodedSnapshot, _ := json.Marshal(snapshot)
			assertDoesNotContain(t, encodedSnapshot, "direct-secret", "private-argument", "Authorization", "API_KEY")
			before := readTestFile(t, manager.configPath)
			result, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://xiass.example/mcp"})
			if !errors.Is(err, ErrUnsafeConfiguration) {
				t.Fatalf("sensitive configuration apply error = %v", err)
			}
			assertDoesNotContain(t, []byte(fmt.Sprint(err)), "direct-secret", "private-argument", "Authorization", "API_KEY")
			encodedResult, _ := json.Marshal(result)
			assertDoesNotContain(t, encodedResult, "xiass.example", "direct-secret", "private-argument")
			if after := readTestFile(t, manager.configPath); !bytes.Equal(after, before) {
				t.Fatal("sensitive configuration changed despite write rejection")
			}
			if backupDirectories(t, manager) != nil {
				t.Fatal("sensitive configuration produced a recoverable plaintext backup")
			}
		})
	}
}

func TestApplyRejectsUnsafeRemoteInputs(t *testing.T) {
	manager := newTestManager(t, TargetCursor)
	for _, endpoint := range []string{
		"http://remote.example/mcp",
		"ftp://remote.example/mcp",
		"https://user:secret@remote.example/mcp",
		"https://remote.example/mcp?token=secret",
		"https://remote.example/mcp#secret",
		" https://remote.example/mcp",
	} {
		result, err := manager.ApplyRemote(ApplyInput{RemoteURL: endpoint})
		if !errors.Is(err, ErrInvalidRemote) {
			t.Fatalf("unsafe endpoint error = %v", err)
		}
		assertDoesNotContain(t, []byte(fmt.Sprint(err)), endpoint, "secret")
		encodedResult, _ := json.Marshal(result)
		assertDoesNotContain(t, encodedResult, endpoint, "secret")
		if _, statErr := os.Lstat(manager.configPath); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatal("invalid endpoint unexpectedly created a configuration file")
		}
	}
	for _, endpoint := range []string{"https://remote.example/mcp", "http://localhost:50999/mcp", "http://127.0.0.1:50999/mcp", "http://[::1]:50999/mcp"} {
		if _, err := normalizeRemoteEndpoint(endpoint); err != nil {
			t.Fatalf("valid endpoint was rejected: %v", err)
		}
	}
}

func TestRejectsMalformedDuplicateOversizedAndSpecialFiles(t *testing.T) {
	t.Run("duplicate nested key", func(t *testing.T) {
		manager := newTestManager(t, TargetCursor)
		writeTestConfiguration(t, manager, []byte(`{"mcpServers":{"foreign":{"url":"https://one.example/mcp","url":"https://two.example/mcp"}}}`))
		snapshot, err := manager.Inspect()
		if !errors.Is(err, ErrInvalidConfiguration) || snapshot.Valid {
			t.Fatalf("duplicate key inspection = %+v / %v", snapshot, err)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		manager := newTestManager(t, TargetCursor)
		writeTestConfiguration(t, manager, bytes.Repeat([]byte(" "), int(maxConfigurationBytes)+1))
		if _, err := manager.Inspect(); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("oversized inspection error = %v", err)
		}
	})

	t.Run("directory instead of file", func(t *testing.T) {
		manager := newTestManager(t, TargetCursor)
		if err := os.MkdirAll(manager.configPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Inspect(); !errors.Is(err, ErrUnsafeConfiguration) {
			t.Fatalf("special file inspection error = %v", err)
		}
	})

	if runtime.GOOS != "windows" {
		t.Run("symbolic link", func(t *testing.T) {
			manager := newTestManager(t, TargetCursor)
			if err := os.MkdirAll(filepath.Dir(manager.configPath), 0o700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside.json")
			if err := os.WriteFile(outside, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, manager.configPath); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Inspect(); !errors.Is(err, ErrUnsafeConfiguration) {
				t.Fatalf("symlink inspection error = %v", err)
			}
		})
	}
}

func TestApplyRollsBackAfterPostWriteFailure(t *testing.T) {
	manager := newTestManager(t, TargetCursor)
	original := []byte(`{"mcpServers":{"foreign":{"url":"https://foreign.example/mcp"}}}`)
	writeTestConfiguration(t, manager, original)
	manager.afterAtomicWriteForTest = func() error { return errors.New("forced failure") }
	result, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://xiass.example/mcp"})
	if !errors.Is(err, ErrOperation) {
		t.Fatalf("rollback error = %v", err)
	}
	encodedResult, _ := json.Marshal(result)
	assertDoesNotContain(t, encodedResult, "xiass.example", "foreign.example")
	if restored := readTestFile(t, manager.configPath); !bytes.Equal(restored, original) {
		t.Fatal("post-write failure did not restore the original configuration")
	}
	if backupDirectories(t, manager) == nil {
		t.Fatal("rollback did not retain the pre-write recovery point")
	}
}

func TestVerifiedBackupsCanBeListedRestoredAndDeletedWithoutExposingConfiguration(t *testing.T) {
	for _, target := range []Target{TargetCursor, TargetWindsurf} {
		t.Run(string(target), func(t *testing.T) {
			manager := newTestManager(t, target)
			original := []byte(`{"futureMetadata":{"enabled":true},"mcpServers":{"foreign":{"url":"https://foreign.example/mcp"}}}`)
			writeTestConfiguration(t, manager, original)

			if _, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://first.example/mcp"}); err != nil {
				t.Fatalf("first apply failed: %v", err)
			}
			backups, err := manager.ListBackups()
			if err != nil || len(backups) != 1 || !backups[0].OriginalExisted || backups[0].Reason != "apply" {
				t.Fatalf("first verified backup list = %#v / %v", backups, err)
			}
			originalBackupID := backups[0].ID
			encoded, err := json.Marshal(backups)
			if err != nil {
				t.Fatal(err)
			}
			assertDoesNotContain(t, encoded, "first.example", "foreign.example", "mcp.json", "mcp_config.json", "configuration.json", "url")

			if _, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://second.example/mcp"}); err != nil {
				t.Fatalf("second apply failed: %v", err)
			}
			restored, err := manager.Restore(originalBackupID)
			if err != nil {
				t.Fatalf("restore failed: %v", err)
			}
			if !restored.BackupCreated || !restored.Snapshot.Valid || restored.Snapshot.ManagedServerConfigured {
				t.Fatalf("unexpected redacted restore result: %#v", restored)
			}
			if after := readTestFile(t, manager.configPath); !bytes.Equal(after, original) {
				t.Fatal("restore did not recover the exact original global MCP configuration")
			}
			backups, err = manager.ListBackups()
			if err != nil || len(backups) != 3 {
				t.Fatalf("restore did not retain verified recovery points: %#v / %v", backups, err)
			}

			if err := manager.DeleteBackup(originalBackupID); err != nil {
				t.Fatalf("delete verified backup failed: %v", err)
			}
			backups, err = manager.ListBackups()
			if err != nil || len(backups) != 2 || containsBackupID(backups, originalBackupID) {
				t.Fatalf("deleted backup remained visible: %#v / %v", backups, err)
			}
			directory, err := manager.backupDirectory(originalBackupID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(directory); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("deleted backup directory remains: %v", err)
			}
		})
	}
}

func TestRestoreOfAbsentOriginalRemovesGeneratedConfiguration(t *testing.T) {
	for _, target := range []Target{TargetCursor, TargetWindsurf} {
		t.Run(string(target), func(t *testing.T) {
			manager := newTestManager(t, target)
			if _, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://xiass.example/mcp"}); err != nil {
				t.Fatal(err)
			}
			backups, err := manager.ListBackups()
			if err != nil || len(backups) != 1 || backups[0].OriginalExisted {
				t.Fatalf("missing-file backup = %#v / %v", backups, err)
			}
			result, err := manager.Restore(backups[0].ID)
			if err != nil {
				t.Fatalf("restore absent original failed: %v", err)
			}
			if !result.BackupCreated || !result.Snapshot.Valid || result.Snapshot.Exists || result.Snapshot.ServerCount != 0 {
				t.Fatalf("restore of absent original result = %#v", result)
			}
			if _, err := os.Lstat(manager.configPath); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("generated configuration was not removed: %v", err)
			}
		})
	}
}

func TestListedBackupUsesRendererSafeRFC3339Timestamp(t *testing.T) {
	manager := newTestManager(t, TargetCursor)
	if _, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://xiass.example/mcp"}); err != nil {
		t.Fatal(err)
	}
	backup := onlyBackupInfo(t, manager)
	if _, err := time.Parse(time.RFC3339Nano, backup.CreatedAt); err != nil {
		t.Fatalf("backup timestamp is not RFC3339Nano: %q / %v", backup.CreatedAt, err)
	}
	encoded, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["createdAt"].(string); !ok {
		t.Fatalf("renderer timestamp must be a JSON string: %s", encoded)
	}
}

func TestTamperedOrSensitiveBackupCannotBeRestoredOrDeleted(t *testing.T) {
	manager := newTestManager(t, TargetCursor)
	original := []byte(`{"mcpServers":{"foreign":{"url":"https://foreign.example/mcp"}}}`)
	writeTestConfiguration(t, manager, original)
	if _, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://xiass.example/mcp"}); err != nil {
		t.Fatal(err)
	}
	backup := onlyBackupInfo(t, manager)
	directory, err := manager.backupDirectory(backup.ID)
	if err != nil {
		t.Fatal(err)
	}

	// A checksum mismatch is rejected before any active configuration mutation
	// or recursive deletion is attempted.
	if err := os.WriteFile(filepath.Join(directory, "configuration.json"), []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	activeBefore := readTestFile(t, manager.configPath)
	if _, err := manager.Restore(backup.ID); !errors.Is(err, ErrUnsafeConfiguration) {
		t.Fatalf("tampered restore error = %v", err)
	}
	if err := manager.DeleteBackup(backup.ID); !errors.Is(err, ErrUnsafeConfiguration) {
		t.Fatalf("tampered delete error = %v", err)
	}
	if activeAfter := readTestFile(t, manager.configPath); !bytes.Equal(activeAfter, activeBefore) {
		t.Fatal("tampered backup changed active configuration")
	}
	if _, err := os.Lstat(directory); err != nil {
		t.Fatalf("tampered backup was unexpectedly removed: %v", err)
	}

	// Even an attacker who updates the checksum cannot use a backup containing
	// account-adjacent values to overwrite a client configuration.
	sensitive := []byte(`{"mcpServers":{"foreign":{"headers":{"Authorization":"Bearer direct-secret"}}}}`)
	if err := os.WriteFile(filepath.Join(directory, "configuration.json"), sensitive, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := readBackupManifestForTest(t, directory)
	manifest.OriginalSHA256 = sha256Hex(sensitive)
	writeBackupManifestForTest(t, directory, manifest)
	if _, err := manager.Restore(backup.ID); !errors.Is(err, ErrUnsafeConfiguration) {
		t.Fatalf("sensitive restore error = %v", err)
	}
	if err := manager.DeleteBackup(backup.ID); !errors.Is(err, ErrUnsafeConfiguration) {
		t.Fatalf("sensitive delete error = %v", err)
	}
	listed, err := manager.ListBackups()
	if err != nil || len(listed) != 0 {
		t.Fatalf("sensitive backup was presented as a recovery point: %#v / %v", listed, err)
	}
	encodedListed, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	assertDoesNotContain(t, encodedListed, "direct-secret", "Authorization", "headers")
	if activeAfter := readTestFile(t, manager.configPath); !bytes.Equal(activeAfter, activeBefore) {
		t.Fatal("sensitive backup changed active configuration")
	}
}

func TestRestoreSupportsPreModeVerifiedBackupManifest(t *testing.T) {
	manager := newTestManager(t, TargetCursor)
	original := []byte(`{"mcpServers":{"foreign":{"url":"https://foreign.example/mcp"}}}`)
	writeTestConfiguration(t, manager, original)
	if _, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://xiass.example/mcp"}); err != nil {
		t.Fatal(err)
	}
	backup := onlyBackupInfo(t, manager)
	directory, err := manager.backupDirectory(backup.ID)
	if err != nil {
		t.Fatal(err)
	}
	manifest := readBackupManifestForTest(t, directory)
	manifest.OriginalMode = 0 // manifests created before OriginalMode was added
	writeBackupManifestForTest(t, directory, manifest)

	if _, err := manager.Restore(backup.ID); err != nil {
		t.Fatalf("pre-mode backup restore failed: %v", err)
	}
	if restored := readTestFile(t, manager.configPath); !bytes.Equal(restored, original) {
		t.Fatal("pre-mode backup did not restore original configuration")
	}
}

func TestBackupRootSymlinkIsRejectedWithoutMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link fixture is platform-specific")
	}
	manager := newTestManager(t, TargetCursor)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(manager.backupRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, manager.backupRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ListBackups(); !errors.Is(err, ErrUnsafeConfiguration) {
		t.Fatalf("backup-root list error = %v", err)
	}
	if _, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://xiass.example/mcp"}); !errors.Is(err, ErrUnsafeConfiguration) {
		t.Fatalf("backup-root apply error = %v", err)
	}
	if _, err := os.Lstat(manager.configPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unsafe backup root unexpectedly created configuration: %v", err)
	}
}

func TestOperationLockRejectsConcurrentApply(t *testing.T) {
	manager := newTestManager(t, TargetCursor)
	if err := ensureDirectoryNoSymlink(manager.backupRoot); err != nil {
		t.Fatal(err)
	}
	release, err := acquireOperationLock(manager.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := manager.ApplyRemote(ApplyInput{RemoteURL: "https://xiass.example/mcp"}); !errors.Is(err, ErrOperationBusy) {
		t.Fatalf("concurrent operation error = %v", err)
	}
}

func newTestManager(t *testing.T, target Target) *manager {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	appConfig := filepath.Join(root, "app-config")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := newManager(target, home, appConfig)
	if err := manager.validatePaths(); err != nil {
		t.Fatalf("test manager invalid: %v", err)
	}
	return manager
}

func writeTestConfiguration(t *testing.T, manager *manager, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(manager.configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readConfigurationObject(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	return readRawObject(t, readTestFile(t, path))
}

func readRawObject(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func backupDirectories(t *testing.T, manager *manager) []string {
	t.Helper()
	entries, err := os.ReadDir(manager.backupRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	directories := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, filepath.Join(manager.backupRoot, entry.Name()))
		}
	}
	if len(directories) == 0 {
		return nil
	}
	return directories
}

func onlyBackupDirectory(t *testing.T, manager *manager) string {
	t.Helper()
	directories := backupDirectories(t, manager)
	if len(directories) != 1 {
		t.Fatalf("expected one backup directory, got %d", len(directories))
	}
	return directories[0]
}

func onlyBackupInfo(t *testing.T, manager *manager) BackupInfo {
	t.Helper()
	backups, err := manager.ListBackups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one verified backup, got %#v / %v", backups, err)
	}
	return backups[0]
}

func containsBackupID(backups []BackupInfo, backupID string) bool {
	for _, backup := range backups {
		if backup.ID == backupID {
			return true
		}
	}
	return false
}

func readBackupManifestForTest(t *testing.T, directory string) backupManifest {
	t.Helper()
	data := readTestFile(t, filepath.Join(directory, "manifest.json"))
	defer zeroBytes(data)
	var manifest backupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeBackupManifestForTest(t *testing.T, directory string, manifest backupManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(data)
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertDoesNotContain(t *testing.T, data []byte, values ...string) {
	t.Helper()
	for _, value := range values {
		if value != "" && strings.Contains(string(data), value) {
			t.Fatalf("unsafe value leaked into a public result")
		}
	}
}

func jsonSemanticallyEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && reflect.DeepEqual(leftValue, rightValue)
}
