package claudeconfig

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func writeFileAtomic(path string, data []byte, mode fs.FileMode) (err error) {
	directory := filepath.Dir(path)
	if err := ensureDirectoryNoSymlink(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".xiass-tools-claude-settings-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(secureMode(mode)); err != nil {
		return err
	}
	if written, err := temporary.Write(data); err != nil || written != len(data) {
		if err != nil {
			return err
		}
		return io.ErrShortWrite
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFileAtomic(temporaryPath, path); err != nil {
		return fmt.Errorf("replace Claude user settings atomically: %w", err)
	}
	return nil
}
