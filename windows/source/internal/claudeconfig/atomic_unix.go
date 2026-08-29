//go:build !windows

package claudeconfig

import (
	"os"
	"path/filepath"
)

func replaceFileAtomic(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(destination))
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
