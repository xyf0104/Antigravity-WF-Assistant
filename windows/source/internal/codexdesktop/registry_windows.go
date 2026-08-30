//go:build windows

package codexdesktop

import (
	"errors"
	"io/fs"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// systemRegistry reads public package registration under HKCU. It never opens
// a key with write access and never reads any user profile or credential data.
type systemRegistry struct{}

func (systemRegistry) Subkeys(path string, limit int) ([]string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if err != nil {
		return nil, normalizeRegistryError(err)
	}
	defer key.Close()
	names, err := key.ReadSubKeyNames(limit)
	if err != nil {
		return nil, normalizeRegistryError(err)
	}
	return names, nil
}

// AppPathValues reads only the default values of six fixed, public Windows
// App Paths keys: Codex.exe and ChatGPT.exe in the current-user, machine, and
// legacy WOW6432Node views. It does not enumerate the registry, inspect any
// account/configuration data, or return raw values outside codexdesktop.
//
// The caller still checks the executable name and public Electron layout, so a
// registry value alone is never enough to trust an arbitrary program.
func (systemRegistry) AppPathValues(limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	const appPathsRoot = `Software\Microsoft\Windows\CurrentVersion\App Paths`
	type appPathKey struct {
		root registry.Key
		path string
	}
	keys := []appPathKey{
		{root: registry.CURRENT_USER, path: appPathsRoot + `\Codex.exe`},
		{root: registry.CURRENT_USER, path: appPathsRoot + `\ChatGPT.exe`},
		{root: registry.LOCAL_MACHINE, path: appPathsRoot + `\Codex.exe`},
		{root: registry.LOCAL_MACHINE, path: appPathsRoot + `\ChatGPT.exe`},
		{root: registry.LOCAL_MACHINE, path: `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\App Paths\Codex.exe`},
		{root: registry.LOCAL_MACHINE, path: `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\App Paths\ChatGPT.exe`},
	}

	values := make([]string, 0, min(limit, len(keys)))
	var failures []error
	for _, candidate := range keys {
		if len(values) >= limit {
			break
		}
		key, err := registry.OpenKey(candidate.root, candidate.path, registry.READ)
		if err != nil {
			if isNotExist(normalizeRegistryError(err)) {
				continue
			}
			failures = append(failures, err)
			continue
		}
		value, _, valueErr := key.GetStringValue("")
		_ = key.Close()
		if valueErr != nil {
			if isNotExist(normalizeRegistryError(valueErr)) {
				continue
			}
			failures = append(failures, valueErr)
			continue
		}
		if strings.TrimSpace(value) != "" {
			values = append(values, value)
		}
	}
	if len(failures) > 0 {
		return values, errors.Join(failures...)
	}
	return values, nil
}

func normalizeRegistryError(err error) error {
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return fs.ErrNotExist
	}
	return err
}
