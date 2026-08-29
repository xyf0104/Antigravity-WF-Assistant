package mcpconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (m *manager) readDocument() (configurationDocument, []byte, bool, fs.FileMode, error) {
	data, exists, mode, err := readRegularFile(m.configPath, maxConfigurationBytes)
	if err != nil {
		return configurationDocument{}, data, exists, mode, err
	}
	if !exists {
		return configurationDocument{
			root:    make(map[string]json.RawMessage),
			servers: make(map[string]json.RawMessage),
		}, nil, false, mode, nil
	}
	document, err := parseConfiguration(data)
	if err != nil {
		return configurationDocument{}, data, true, mode, err
	}
	return document, data, true, mode, nil
}

func (m *manager) ensureConfigurationUnchanged(expected []byte, expectedExists bool) error {
	current, exists, _, err := readRegularFile(m.configPath, maxConfigurationBytes)
	defer zeroBytes(current)
	if err != nil || exists != expectedExists || !bytes.Equal(current, expected) {
		return ErrOperation
	}
	return nil
}

func (m *manager) createBackup(original []byte, existed bool, mode fs.FileMode, reason string) (backupManifest, error) {
	id, err := newBackupID()
	if err != nil {
		return backupManifest{}, err
	}
	directory := filepath.Join(m.backupRoot, id)
	if !pathWithin(m.backupRoot, directory) {
		return backupManifest{}, ErrUnsafeConfiguration
	}
	if err := createPrivateDirectory(directory); err != nil {
		return backupManifest{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(directory)
		}
	}()

	manifest := backupManifest{
		Version:         backupManifestVersion,
		ID:              id,
		Target:          m.target,
		CreatedAt:       nowUTC(),
		Reason:          reason,
		OriginalExisted: existed,
	}
	if existed {
		manifest.OriginalSHA256 = sha256Hex(original)
		if err := writeFileAtomic(filepath.Join(directory, "configuration.json"), original, defaultMode(mode)); err != nil {
			return backupManifest{}, err
		}
	}
	if err := writeBackupManifestAt(directory, manifest); err != nil {
		return backupManifest{}, err
	}
	published = true
	return manifest, nil
}

func (m *manager) writeBackupManifest(manifest backupManifest) error {
	if manifest.ID == "" || manifest.Target != m.target {
		return ErrOperation
	}
	directory := filepath.Join(m.backupRoot, manifest.ID)
	if !pathWithin(m.backupRoot, directory) {
		return ErrUnsafeConfiguration
	}
	return writeBackupManifestAt(directory, manifest)
}

func writeBackupManifestAt(directory string, manifest backupManifest) error {
	if err := ensureExistingPathNoSymlink(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrUnsafeConfiguration
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ErrOperation
	}
	defer zeroBytes(data)
	data = append(data, '\n')
	return writeFileAtomic(filepath.Join(directory, "manifest.json"), data, 0o600)
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

// readRegularFile follows no symlinks and verifies the opened file still
// matches the lstat result. It returns only controlled, non-path-bearing
// errors so callers cannot leak local layout in UI or diagnostics.
func readRegularFile(path string, limit int64) ([]byte, bool, fs.FileMode, error) {
	if limit <= 0 || ensureExistingPathNoSymlink(filepath.Dir(path)) != nil {
		return nil, false, 0, ErrUnsafeConfiguration
	}
	initial, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, 0o600, nil
	}
	if err != nil {
		return nil, false, 0, ErrUnavailable
	}
	if initial.Mode()&os.ModeSymlink != 0 || !initial.Mode().IsRegular() {
		return nil, false, 0, ErrUnsafeConfiguration
	}
	if initial.Size() < 0 || initial.Size() > limit {
		return nil, false, 0, ErrInvalidConfiguration
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, 0, ErrUnavailable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(initial, opened) {
		return nil, false, 0, ErrUnsafeConfiguration
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		zeroBytes(data)
		return nil, false, 0, ErrInvalidConfiguration
	}
	final, err := os.Lstat(path)
	if err != nil || final.Mode()&os.ModeSymlink != 0 || !final.Mode().IsRegular() || !os.SameFile(initial, final) {
		zeroBytes(data)
		return nil, false, 0, ErrUnsafeConfiguration
	}
	return data, true, initial.Mode().Perm(), nil
}

func ensureExistingPathNoSymlink(path string) error {
	root, components, err := absolutePathComponents(path)
	if err != nil {
		return ErrUnsafeConfiguration
	}
	current := root
	for _, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrUnsafeConfiguration
		}
	}
	return nil
}

func ensureDirectoryNoSymlink(path string) error {
	root, components, err := absolutePathComponents(path)
	if err != nil {
		return ErrUnsafeConfiguration
	}
	current := root
	for _, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return ErrOperation
			}
			info, err = os.Lstat(current)
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrUnsafeConfiguration
		}
	}
	return nil
}

func createPrivateDirectory(path string) error {
	if err := ensureDirectoryNoSymlink(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrUnsafeConfiguration
		}
		return ErrOperation
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return ErrUnavailable
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return ErrOperation
	}
	info, err = os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrUnsafeConfiguration
	}
	return nil
}

func absolutePathComponents(path string) (string, []string, error) {
	if path == "" || containsControl(path) {
		return "", nil, ErrUnsafeConfiguration
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, ErrUnsafeConfiguration
	}
	abs = filepath.Clean(abs)
	if !filepath.IsAbs(abs) {
		return "", nil, ErrUnsafeConfiguration
	}
	volume := filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs, volume)
	separator := string(os.PathSeparator)
	if !strings.HasPrefix(rest, separator) {
		return "", nil, ErrUnsafeConfiguration
	}
	root := volume + separator
	rest = strings.TrimLeft(rest, separator)
	if rest == "" || rest == "." {
		return root, nil, nil
	}
	parts := strings.Split(rest, separator)
	components := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", nil, ErrUnsafeConfiguration
		}
		components = append(components, part)
	}
	return root, components, nil
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	directory := filepath.Dir(path)
	if err := ensureDirectoryNoSymlink(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".xiass-tools-mcp-*")
	if err != nil {
		return ErrOperation
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(defaultMode(mode)); err != nil {
		return ErrOperation
	}
	if written, err := temporary.Write(data); err != nil || written != len(data) {
		return ErrOperation
	}
	if err := temporary.Sync(); err != nil {
		return ErrOperation
	}
	if err := temporary.Close(); err != nil {
		return ErrOperation
	}
	if err := replaceFileAtomic(temporaryPath, path); err != nil {
		return ErrOperation
	}
	return nil
}

func restoreOriginal(path string, data []byte, existed bool, mode fs.FileMode) error {
	if existed {
		if err := writeFileAtomic(path, data, defaultMode(mode)); err != nil {
			return err
		}
		restored, restoredExists, _, err := readRegularFile(path, maxConfigurationBytes)
		defer zeroBytes(restored)
		if err != nil || !restoredExists || !bytes.Equal(restored, data) {
			return ErrOperation
		}
		return nil
	}
	if err := ensureExistingPathNoSymlink(filepath.Dir(path)); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafeConfiguration
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ErrUnavailable
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ErrOperation
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return ErrOperation
	}
	return nil
}

func normalizeRemoteEndpoint(value string) (string, error) {
	if strings.TrimSpace(value) != value || value == "" || len(value) > 2048 || containsControl(value) || strings.ContainsAny(value, "?#") {
		return "", ErrInvalidRemote
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrInvalidRemote
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	switch parsed.Scheme {
	case "https":
		if parsed.Hostname() == "" {
			return "", ErrInvalidRemote
		}
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return "", ErrInvalidRemote
		}
	default:
		return "", ErrInvalidRemote
	}
	return parsed.String(), nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func rawContainsSensitiveEndpoint(raw json.RawMessage) bool {
	entry, err := strictJSONObject(raw)
	if err != nil {
		return true
	}
	for _, field := range []string{"url", "serverUrl"} {
		value, exists := entry[field]
		if !exists {
			continue
		}
		var endpoint string
		if json.Unmarshal(value, &endpoint) != nil {
			return true
		}
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return true
		}
	}
	return false
}

func zeroBytes(data []byte) {
	for index := range data {
		data[index] = 0
	}
}
