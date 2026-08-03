package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// StreamRecoverySettings control how the local proxy handles a transient
// upstream interruption after Antigravity has already started a response.
// They intentionally live outside model credentials, so a user can adjust
// reliability without rewriting any model configuration.
type StreamRecoverySettings struct {
	Enabled         bool `json:"enabled"`
	MaxAttempts     int  `json:"maxAttempts"`
	MaxDelaySeconds int  `json:"maxDelaySeconds"`
}

// UpdateSettings contains only local update preferences. The updater itself
// always verifies a release asset before it is allowed to start an installer.
type UpdateSettings struct {
	AutoCheck      bool   `json:"autoCheck"`
	SkippedVersion string `json:"skippedVersion"`
}

// AppSettings is persisted in the same private application directory as the
// model list. SchemaVersion lets newly added options retain safe defaults for
// installations created by earlier releases.
type AppSettings struct {
	SchemaVersion  int                    `json:"schemaVersion"`
	StreamRecovery StreamRecoverySettings `json:"streamRecovery"`
	Updates        UpdateSettings         `json:"updates"`
}

const appSettingsSchemaVersion = 1

var settingsMu sync.RWMutex

func DefaultAppSettings() AppSettings {
	return AppSettings{
		SchemaVersion: appSettingsSchemaVersion,
		StreamRecovery: StreamRecoverySettings{
			Enabled: true, MaxAttempts: 5, MaxDelaySeconds: 20,
		},
		Updates: UpdateSettings{AutoCheck: true},
	}
}

// NormalizeAppSettings prevents a corrupted or hand-edited settings file from
// disabling automatic recovery accidentally or producing an unbounded retry
// loop. Deliberately setting Enabled=false remains supported.
func NormalizeAppSettings(settings AppSettings) AppSettings {
	defaults := DefaultAppSettings()
	if settings.SchemaVersion <= 0 {
		return defaults
	}
	settings.SchemaVersion = appSettingsSchemaVersion
	if settings.StreamRecovery.MaxAttempts <= 0 {
		settings.StreamRecovery.MaxAttempts = defaults.StreamRecovery.MaxAttempts
	}
	if settings.StreamRecovery.MaxAttempts > 10 {
		settings.StreamRecovery.MaxAttempts = 10
	}
	if settings.StreamRecovery.MaxDelaySeconds <= 0 {
		settings.StreamRecovery.MaxDelaySeconds = defaults.StreamRecovery.MaxDelaySeconds
	}
	if settings.StreamRecovery.MaxDelaySeconds > 120 {
		settings.StreamRecovery.MaxDelaySeconds = 120
	}
	return settings
}

func appSettingsPath() string {
	return filepath.Join(storageDir, "settings.json")
}

func LoadAppSettings() (AppSettings, error) {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	return loadAppSettingsLocked()
}

func loadAppSettingsLocked() (AppSettings, error) {
	data, err := os.ReadFile(appSettingsPath())
	if os.IsNotExist(err) {
		return DefaultAppSettings(), nil
	}
	if err != nil {
		return DefaultAppSettings(), err
	}
	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return DefaultAppSettings(), err
	}
	return NormalizeAppSettings(settings), nil
}

func SaveAppSettings(settings AppSettings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	settings = NormalizeAppSettings(settings)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(storageDir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(storageDir, ".settings-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceStorageFile(tempPath, appSettingsPath())
}
