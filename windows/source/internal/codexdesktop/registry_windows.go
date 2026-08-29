//go:build windows

package codexdesktop

import (
	"errors"
	"io/fs"

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

func normalizeRegistryError(err error) error {
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return fs.ErrNotExist
	}
	return err
}
