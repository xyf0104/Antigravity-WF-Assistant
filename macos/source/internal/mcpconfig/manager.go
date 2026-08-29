package mcpconfig

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// NewDefaultManager resolves exactly one documented global MCP location. It
// never scans an application-data directory, project directory, database, or
// account store for alternative configuration files.
func NewDefaultManager(target Target) (*manager, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, ErrUnavailable
	}
	appConfig, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(appConfig) == "" {
		return nil, ErrUnavailable
	}
	manager := newManager(target, home, appConfig)
	if err := manager.validatePaths(); err != nil {
		return nil, err
	}
	return manager, nil
}

// newManager is intentionally package-private so production callers cannot
// redirect this package at arbitrary client files. Tests use temporary user
// homes to exercise the same fixed relative paths.
func newManager(target Target, home, appConfig string) *manager {
	home = cleanAbsolutePath(home)
	appConfig = cleanAbsolutePath(appConfig)
	configuration := configurationPath(target, home)
	backupRoot := ""
	if appConfig != "" && target.valid() {
		backupRoot = filepath.Join(appConfig, "XIASS Tools", backupDirectoryName, string(target))
	}
	return &manager{
		target:     target,
		userHome:   home,
		appConfig:  appConfig,
		configPath: configuration,
		backupRoot: backupRoot,
		lockPath:   filepath.Join(backupRoot, lockFilename),
	}
}

func (target Target) valid() bool {
	return target == TargetCursor || target == TargetWindsurf
}

func configurationPath(target Target, home string) string {
	if home == "" {
		return ""
	}
	switch target {
	case TargetCursor:
		return filepath.Join(home, ".cursor", "mcp.json")
	case TargetWindsurf:
		return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
	default:
		return ""
	}
}

func (m *manager) validatePaths() error {
	if m == nil || !m.target.valid() {
		return ErrUnsupportedTarget
	}
	if m.userHome == "" || m.appConfig == "" || m.configPath == "" || m.backupRoot == "" || m.lockPath == "" {
		return ErrUnavailable
	}
	if m.configPath != configurationPath(m.target, m.userHome) || m.backupRoot != filepath.Join(m.appConfig, "XIASS Tools", backupDirectoryName, string(m.target)) || m.lockPath != filepath.Join(m.backupRoot, lockFilename) {
		return ErrUnsafeConfiguration
	}
	if !pathWithin(m.userHome, m.configPath) || !pathWithin(m.appConfig, m.backupRoot) || !pathWithin(m.backupRoot, m.lockPath) {
		return ErrUnsafeConfiguration
	}
	return nil
}

func cleanAbsolutePath(path string) string {
	if strings.TrimSpace(path) == "" || containsControl(path) {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	abs = filepath.Clean(abs)
	if !filepath.IsAbs(abs) {
		return ""
	}
	// macOS exposes some system-owned roots through compatibility symlinks
	// (for example /var -> /private/var). Canonicalize only the trusted user
	// roots supplied by the operating system; later target and child paths are
	// still checked component-by-component and reject symlinks.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(resolved)
	}
	return abs
}

func containsControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func pathWithin(root, candidate string) bool {
	if root == "" || candidate == "" {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

// Inspect reads only the fixed MCP file and returns a redacted structural
// snapshot. A malformed or unsafe file is never partially parsed or exposed.
func (m *manager) Inspect() (Snapshot, error) {
	snapshot := Snapshot{Target: m.target}
	if err := m.validatePaths(); err != nil {
		return snapshot, err
	}
	document, data, exists, _, err := m.readDocument()
	defer zeroBytes(data)
	if err != nil {
		snapshot.Exists = exists
		return snapshot, err
	}
	return document.snapshot(m.target, exists), nil
}

// ListBackups returns only checksum-verified, redacted recovery metadata. It
// is read-only: absent backup directories remain absent, and malformed or
// tampered entries are never returned as usable recovery points.
func (m *manager) ListBackups() ([]BackupInfo, error) {
	if err := m.validatePaths(); err != nil {
		return nil, err
	}
	if err := ensureExistingPathNoSymlink(m.backupRoot); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(m.backupRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	type backupListing struct {
		info      BackupInfo
		createdAt time.Time
	}
	listings := make([]backupListing, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !validBackupID(entry.Name()) {
			continue
		}
		backup, err := m.readVerifiedBackup(entry.Name())
		if err != nil {
			continue
		}
		zeroBytes(backup.data)
		listings = append(listings, backupListing{
			info:      backupInfo(backup.manifest),
			createdAt: backup.manifest.CreatedAt,
		})
	}
	sort.Slice(listings, func(left, right int) bool {
		return listings[left].createdAt.After(listings[right].createdAt)
	})
	backups := make([]BackupInfo, 0, len(listings))
	for _, listing := range listings {
		backups = append(backups, listing.info)
	}
	return backups, nil
}

// ApplyRemote atomically creates or updates the reserved XIASS Tools remote
// MCP entry. It accepts only HTTPS or loopback HTTP without credentials,
// headers, query values, OAuth data, or environment values.
func (m *manager) ApplyRemote(input ApplyInput) (ApplyResult, error) {
	endpoint, err := normalizeRemoteEndpoint(input.RemoteURL)
	if err != nil {
		return ApplyResult{Snapshot: Snapshot{Target: m.target}}, ErrInvalidRemote
	}
	if err := m.validatePaths(); err != nil {
		return ApplyResult{Snapshot: Snapshot{Target: m.target}}, err
	}

	var result ApplyResult
	err = m.withLock(func() error {
		document, original, existed, mode, err := m.readDocument()
		defer zeroBytes(original)
		if err != nil {
			return err
		}
		if document.hasSensitiveConfiguration {
			return ErrUnsafeConfiguration
		}
		updated, err := document.withManagedRemote(m.target, endpoint)
		if err != nil {
			return ErrOperation
		}
		defer zeroBytes(updated)

		manifest, err := m.createBackup(original, existed, mode, "apply")
		if err != nil {
			return err
		}
		if err := m.ensureConfigurationUnchanged(original, existed); err != nil {
			return err
		}
		if err := writeFileAtomic(m.configPath, updated, defaultMode(mode)); err != nil {
			return m.rollback(original, existed, mode)
		}
		if m.afterAtomicWriteForTest != nil && m.afterAtomicWriteForTest() != nil {
			return m.rollback(original, existed, mode)
		}

		verified, written, writtenExists, _, err := m.readDocument()
		defer zeroBytes(written)
		if err != nil || !writtenExists || !verified.matchesManagedRemote(m.target, endpoint) {
			return m.rollback(original, existed, mode)
		}
		manifest.AppliedSHA256 = sha256Hex(written)
		if err := m.writeBackupManifest(manifest); err != nil {
			return m.rollback(original, existed, mode)
		}
		result = ApplyResult{
			Snapshot:      verified.snapshot(m.target, true),
			BackupCreated: true,
		}
		return nil
	})
	if err != nil {
		return ApplyResult{Snapshot: Snapshot{Target: m.target}}, safeOperationError(err)
	}
	return result, nil
}

// Restore replaces the target global MCP file with one checksum-verified
// XIASS Tools backup. Before changing anything it creates a new verified
// safety backup of the current, non-sensitive configuration. It never restores
// a backup containing sensitive account, credential, OAuth, header, command,
// or environment data.
func (m *manager) Restore(backupID string) (RestoreResult, error) {
	empty := RestoreResult{Snapshot: Snapshot{Target: m.target}}
	if err := m.validatePaths(); err != nil {
		return empty, err
	}
	if !validBackupID(backupID) {
		return empty, ErrOperation
	}

	var result RestoreResult
	err := m.withLock(func() error {
		target, err := m.readVerifiedBackup(backupID)
		if err != nil {
			return err
		}
		defer zeroBytes(target.data)

		current, original, existed, mode, err := m.readDocument()
		defer zeroBytes(original)
		if err != nil {
			return err
		}
		if current.hasSensitiveConfiguration {
			return ErrUnsafeConfiguration
		}

		safety, err := m.createBackup(original, existed, mode, "restore")
		if err != nil {
			return err
		}
		if err := m.ensureConfigurationUnchanged(original, existed); err != nil {
			return err
		}

		restoredMode := fs.FileMode(target.manifest.OriginalMode)
		if err := restoreOriginal(m.configPath, target.data, target.manifest.OriginalExisted, restoredMode); err != nil {
			return m.rollback(original, existed, mode)
		}

		restored, written, writtenExists, _, err := m.readDocument()
		defer zeroBytes(written)
		if err != nil || writtenExists != target.manifest.OriginalExisted || (writtenExists && !bytes.Equal(written, target.data)) || restored.hasSensitiveConfiguration {
			return m.rollback(original, existed, mode)
		}
		if writtenExists {
			safety.AppliedSHA256 = sha256Hex(written)
		}
		if err := m.writeBackupManifest(safety); err != nil {
			return m.rollback(original, existed, mode)
		}
		result = RestoreResult{
			Snapshot:      restored.snapshot(m.target, writtenExists),
			BackupCreated: true,
		}
		return nil
	})
	if err != nil {
		return empty, safeOperationError(err)
	}
	return result, nil
}

// DeleteBackup removes only a complete, checksum-verified XIASS Tools backup.
// Tampered, unexpected, and non-owned directories are rejected rather than
// recursively deleted.
func (m *manager) DeleteBackup(backupID string) error {
	if err := m.validatePaths(); err != nil {
		return err
	}
	if !validBackupID(backupID) {
		return ErrOperation
	}
	err := m.withLock(func() error {
		backup, err := m.readVerifiedBackup(backupID)
		if err != nil {
			return err
		}
		zeroBytes(backup.data)
		return m.removeVerifiedBackup(backupID)
	})
	return safeOperationError(err)
}

func safeOperationError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrUnsupportedTarget), errors.Is(err, ErrUnavailable), errors.Is(err, ErrInvalidConfiguration), errors.Is(err, ErrUnsafeConfiguration), errors.Is(err, ErrInvalidRemote), errors.Is(err, ErrOperationBusy):
		return err
	default:
		return ErrOperation
	}
}

func (m *manager) rollback(original []byte, existed bool, mode fs.FileMode) error {
	if err := restoreOriginal(m.configPath, original, existed, mode); err != nil {
		return ErrOperation
	}
	return ErrOperation
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newBackupID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", ErrOperation
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), nil
}
