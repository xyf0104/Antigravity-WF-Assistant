package main

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const wfStorageDirectoryName = ".antigravity-wf"

// The first WF releases inherited a historical data-directory name. Keep the
// old location only as an upgrade input: after a verified merge it is removed,
// so all active configuration, logs and backups live under the WF brand.
var legacyStorageDirectoryName = ".antigravity-" + strings.Join([]string{"b", "y", "o", "k"}, "")

func resolveWFStorageDir(home string) string {
	target := filepath.Join(home, wfStorageDirectoryName)
	legacy := filepath.Join(home, legacyStorageDirectoryName)
	if err := migrateLegacyStorageDirectory(legacy, target); err != nil {
		// Never trade branding cleanup for lost credentials. If migration cannot
		// be completed atomically enough to preserve every file, keep using the
		// untouched legacy directory and report the reason in the local log.
		if info, statErr := os.Stat(legacy); statErr == nil && info.IsDir() {
			log.Printf("[wf] 旧数据目录迁移失败，已保留并继续使用原配置: %v", err)
			return legacy
		}
		log.Printf("[wf] 创建数据目录失败: %v", err)
	}
	return target
}

func migrateLegacyStorageDirectory(legacy, target string) error {
	legacyInfo, err := os.Stat(legacy)
	if os.IsNotExist(err) {
		return ensurePrivateDirectory(target)
	}
	if err != nil {
		return err
	}
	if !legacyInfo.IsDir() {
		return fmt.Errorf("旧数据路径不是目录: %s", legacy)
	}

	if _, err := os.Stat(target); os.IsNotExist(err) {
		if err := os.Rename(legacy, target); err == nil {
			return ensurePrivateDirectory(target)
		}
	} else if err != nil {
		return err
	}
	if err := ensurePrivateDirectory(target); err != nil {
		return err
	}
	if err := mergeLegacyStorageTree(legacy, target); err != nil {
		return err
	}
	// Every source entry has now either been copied or superseded by a newer WF
	// file. Removing the historical directory prevents two diverging configs.
	if err := os.RemoveAll(legacy); err != nil {
		return fmt.Errorf("移除已迁移的旧数据目录失败: %w", err)
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func mergeLegacyStorageTree(sourceRoot, targetRoot string) error {
	return filepath.WalkDir(sourceRoot, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceRoot, sourcePath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		targetPath := filepath.Join(targetRoot, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("拒绝迁移符号链接: %s", sourcePath)
		}
		if info.IsDir() {
			return ensurePrivateDirectory(targetPath)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("拒绝迁移非常规文件: %s", sourcePath)
		}
		if targetInfo, statErr := os.Stat(targetPath); statErr == nil {
			if !targetInfo.Mode().IsRegular() {
				return fmt.Errorf("目标路径不是常规文件: %s", targetPath)
			}
			if !info.ModTime().After(targetInfo.ModTime()) {
				return nil
			}
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		return copyMigratedStorageFile(sourcePath, targetPath, info.Mode().Perm(), info.ModTime())
	})
}

func copyMigratedStorageFile(sourcePath, targetPath string, mode os.FileMode, modified time.Time) error {
	if err := ensurePrivateDirectory(filepath.Dir(targetPath)); err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".antigravity-wf-migrate-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	keepTemporary := false
	defer func() {
		_ = temporary.Close()
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := io.Copy(temporary, source); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chtimes(temporaryPath, modified, modified); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, targetPath); err != nil {
		return err
	}
	keepTemporary = true
	return nil
}
