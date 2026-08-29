// Package codexconfig manages the local Codex configuration without starting,
// stopping, or otherwise controlling the Codex application.
//
// It intentionally has no UI or process-management dependencies so the same
// implementation can be reused by Windows and macOS front ends.
package codexconfig

import (
	"io/fs"
	"time"
)

const (
	// DefaultProviderID is the stable provider entry written by XIASS Tools.
	// It is deliberately separate from provider IDs used by earlier helpers so
	// applying a new configuration never overwrites an unrelated provider.
	DefaultProviderID   = "xiass_tools"
	DefaultProviderName = "XIASS Tools"
	DefaultModel        = "gpt-5.6-sol"
	DefaultWireAPI      = "responses"

	DefaultContextWindow         int64 = 372000
	DefaultAutoCompactTokenLimit int64 = DefaultContextWindow * 9 / 10
	MinimumContextWindow         int64 = 64000
	MaximumContextWindow         int64 = 1050000
	MinimumAutoCompactTokenLimit int64 = 16000
)

var defaultLegacyProviderIDs = []string{"codex_local_access", "xiass"}

// ApplyConfig describes the Codex provider entry to write. BaseURL accepts a
// bare hostname, an API root, or a normal OpenAI-compatible /v1 endpoint.
// APIKey is never written to a manifest or returned by inspection APIs.
type ApplyConfig struct {
	BaseURL                    string `json:"base_url"`
	APIKey                     string `json:"api_key"`
	KeyName                    string `json:"key_name,omitempty"`
	ProviderID                 string `json:"provider_id,omitempty"`
	ProviderName               string `json:"provider_name,omitempty"`
	Model                      string `json:"model,omitempty"`
	ReviewModel                string `json:"review_model,omitempty"`
	WireAPI                    string `json:"wire_api,omitempty"`
	WebSearch                  string `json:"web_search,omitempty"`
	ModelContextWindow         int64  `json:"model_context_window,omitempty"`
	ModelAutoCompactTokenLimit int64  `json:"model_auto_compact_token_limit,omitempty"`
}

// ContextSettings are the two Codex context settings that XIASS Tools owns.
type ContextSettings struct {
	ModelContextWindow         int64 `json:"model_context_window"`
	ModelAutoCompactTokenLimit int64 `json:"model_auto_compact_token_limit"`
}

// ConfigLocation identifies a discovered Codex config file.
type ConfigLocation struct {
	CodexHome  string `json:"codex_home"`
	ConfigPath string `json:"config_path"`
	Exists     bool   `json:"exists"`
}

// Provider is the non-secret portion of a configured Codex provider.
type Provider struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name,omitempty"`
	BaseURL               string   `json:"base_url,omitempty"`
	WireAPI               string   `json:"wire_api,omitempty"`
	RequiresOpenAIAuth    bool     `json:"requires_openai_auth,omitempty"`
	SupportsWebSockets    bool     `json:"supports_websockets,omitempty"`
	HasExperimentalBearer bool     `json:"has_experimental_bearer_token"`
	HeaderNames           []string `json:"header_names,omitempty"`
}

// ConfigSnapshot is a verified, redacted view of config.toml.
type ConfigSnapshot struct {
	Location         ConfigLocation  `json:"location"`
	SHA256           string          `json:"sha256,omitempty"`
	Mode             fs.FileMode     `json:"mode,omitempty"`
	Valid            bool            `json:"valid"`
	ModelProvider    string          `json:"model_provider,omitempty"`
	Model            string          `json:"model,omitempty"`
	ReviewModel      string          `json:"review_model,omitempty"`
	WebSearch        string          `json:"web_search,omitempty"`
	Context          ContextSettings `json:"context"`
	Providers        []Provider      `json:"providers,omitempty"`
	ConfiguredModels []string        `json:"configured_models,omitempty"`
}

// BackupManifest describes one exact, checksum-protected config backup.
// It deliberately contains no secret data; secrets only live in the protected
// config.toml backup file.
type BackupManifest struct {
	Version         int       `json:"version"`
	Tool            string    `json:"tool"`
	ID              string    `json:"id"`
	Reason          string    `json:"reason"`
	CreatedAt       time.Time `json:"created_at"`
	ConfigPath      string    `json:"config_path"`
	OriginalExisted bool      `json:"original_existed"`
	OriginalMode    uint32    `json:"original_mode,omitempty"`
	OriginalSHA256  string    `json:"original_sha256,omitempty"`
	AppliedSHA256   string    `json:"applied_sha256,omitempty"`
	// ImportSource fields record safe provenance for a backup copied from a
	// first-party predecessor. They never contain a path or backup contents.
	ImportSource   string     `json:"import_source,omitempty"`
	ImportSourceID string     `json:"import_source_id,omitempty"`
	ImportedAt     *time.Time `json:"imported_at,omitempty"`
}

// BackupInfo is the safe list view of a stored backup.
type BackupInfo struct {
	ID              string    `json:"id"`
	Reason          string    `json:"reason"`
	CreatedAt       time.Time `json:"created_at"`
	OriginalExisted bool      `json:"original_existed"`
}

type ApplyResult struct {
	BackupID   string `json:"backup_id"`
	ConfigSHA  string `json:"config_sha256"`
	ProviderID string `json:"provider_id"`
}

type RestoreResult struct {
	RestoredBackupID string `json:"restored_backup_id"`
	SafetyBackupID   string `json:"safety_backup_id"`
}

// LegacyBackupKind identifies a first-party XIASS Codex Helper backup format
// that can be copied into the current XIASS Tools backup store. It is only a
// staging/import operation; it never restores active Codex data.
type LegacyBackupKind string

const (
	LegacyBackupConfig  LegacyBackupKind = "config"
	LegacyBackupHistory LegacyBackupKind = "history"
)

// LegacyBackupInfo is a redacted discovery result. No source paths, config
// contents, session lines, database content, API keys, or headers are exposed.
type LegacyBackupInfo struct {
	Kind           LegacyBackupKind `json:"kind"`
	SourceID       string           `json:"source_id"`
	CreatedAt      time.Time        `json:"created_at"`
	Reason         string           `json:"reason,omitempty"`
	TargetProvider string           `json:"target_provider,omitempty"`
	Valid          bool             `json:"valid"`
	Importable     bool             `json:"importable"`
	Message        string           `json:"message,omitempty"`
}

// LegacyImportResult reports the new current-format backup ID after a
// validated, copy-only import. It deliberately omits all secret data.
type LegacyImportResult struct {
	Kind             LegacyBackupKind `json:"kind"`
	SourceID         string           `json:"source_id"`
	ImportedBackupID string           `json:"imported_backup_id"`
	CreatedAt        time.Time        `json:"created_at"`
	Message          string           `json:"message"`
}

// MutationError reports an operation that failed after the target could have
// changed. Callers can use errors.As to surface the rollback state accurately.
type MutationError struct {
	Cause       error
	RollbackErr error
}

func (e *MutationError) Error() string {
	if e.RollbackErr != nil {
		return "Codex configuration mutation failed: " + e.Cause.Error() + "; rollback also failed: " + e.RollbackErr.Error()
	}
	return "Codex configuration mutation failed and was rolled back safely: " + e.Cause.Error()
}

func (e *MutationError) Unwrap() error { return e.Cause }

// ManagerOptions allow a platform UI to use its own application data root or
// explicitly migrate provider IDs created by an earlier first-party helper.
type ManagerOptions struct {
	DataDirectoryName string
	ProviderID        string
	LegacyProviderIDs []string
	// HistoryWriteGuard is intentionally only a narrow, read-only safety
	// predicate. Production uses the platform detector; tests may inject a
	// deterministic guard without starting, stopping, or inspecting secrets.
	HistoryWriteGuard historyWriteGuard
}

// Manager owns a single Codex config.toml lifecycle. Its paths are exported so
// the UI can show only the config location, while callers should avoid exposing
// backup internals or secret file contents.
type Manager struct {
	CodexHome         string
	ConfigPath        string
	BackupRoot        string
	LockPath          string
	ProviderID        string
	LegacyProviderIDs []string
	historyWriteGuard historyWriteGuard
}
