package mcpconfig

import (
	"errors"
	"io/fs"
	"time"
)

const (
	// ManagedServerID is reserved for the one remote MCP entry XIASS Tools may
	// create or update. It never owns, removes, or rewrites other entries.
	ManagedServerID = "xiass-tools"

	maxConfigurationBytes = int64(1 << 20)
	backupDirectoryName   = "mcp-backups"
	lockFilename          = "mcp-config.operation.lock"
	backupManifestVersion = 1
)

// Target identifies one documented client MCP configuration file.
type Target string

const (
	TargetCursor   Target = "cursor"
	TargetWindsurf Target = "windsurf"
)

var (
	ErrUnsupportedTarget    = errors.New("MCP configuration target is unsupported")
	ErrUnavailable          = errors.New("MCP configuration is unavailable")
	ErrInvalidConfiguration = errors.New("MCP configuration is invalid")
	ErrUnsafeConfiguration  = errors.New("MCP configuration cannot be safely modified")
	ErrInvalidRemote        = errors.New("MCP remote endpoint is unsupported")
	ErrOperationBusy        = errors.New("another MCP configuration operation is already running")
	ErrOperation            = errors.New("MCP configuration operation failed")
)

// Snapshot is intentionally redacted. It reports only structural state and
// never contains a configuration path, URL, command, argument, header, env
// value, account, key, OAuth data, or other client-private state.
type Snapshot struct {
	Target                    Target `json:"target"`
	Exists                    bool   `json:"exists"`
	Valid                     bool   `json:"valid"`
	ServerCount               int    `json:"serverCount"`
	ManagedServerConfigured   bool   `json:"managedServerConfigured"`
	HasSensitiveConfiguration bool   `json:"hasSensitiveConfiguration"`
}

// ApplyInput contains the user-supplied remote endpoint for the reserved
// server entry. It is consumed only by ApplyRemote and is never echoed by a
// result, status, error, backup manifest, or diagnostic.
type ApplyInput struct {
	RemoteURL string
}

// ApplyResult reports only safe completion state. Backup identifiers and
// storage paths are intentionally not exposed to callers.
type ApplyResult struct {
	Snapshot      Snapshot `json:"snapshot"`
	BackupCreated bool     `json:"backupCreated"`
}

type backupManifest struct {
	Version         int       `json:"version"`
	ID              string    `json:"id"`
	Target          Target    `json:"target"`
	CreatedAt       time.Time `json:"createdAt"`
	Reason          string    `json:"reason"`
	OriginalExisted bool      `json:"originalExisted"`
	OriginalSHA256  string    `json:"originalSHA256,omitempty"`
	AppliedSHA256   string    `json:"appliedSHA256,omitempty"`
}

type manager struct {
	target     Target
	userHome   string
	appConfig  string
	configPath string
	backupRoot string
	lockPath   string

	// Tests may force a post-write failure to assert that rollback restores the
	// original configuration. Production managers never set this hook.
	afterAtomicWriteForTest func() error
}

func defaultMode(mode fs.FileMode) fs.FileMode {
	if mode == 0 {
		return 0o600
	}
	mode = mode.Perm() & 0o700
	if mode&0o600 != 0o600 {
		mode |= 0o600
	}
	return mode
}
