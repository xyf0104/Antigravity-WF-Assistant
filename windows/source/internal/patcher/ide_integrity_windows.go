//go:build windows

package patcher

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type windowsIDEProductIntegrity struct {
	Checksums map[string]string `json:"checksums"`
}

func windowsIDEProductPath(target windowsTarget) string {
	if target.kind != "ide" || target.root == "" {
		return ""
	}
	return filepath.Join(target.root, "resources", "app", "product.json")
}

func windowsIDEChecksum(data []byte) string {
	digest := sha256.Sum256(data)
	return base64.RawStdEncoding.EncodeToString(digest[:])
}

// prepareWindowsIDEProductChecksumPatch updates only checksum values for the
// two verified chat renderers. It preserves every other byte in product.json
// and refuses an unexpected pre-existing checksum.
func prepareWindowsIDEProductChecksumPatch(target windowsTarget, rendererPlans []*windowsPatchPlan) (*windowsPatchPlan, error) {
	productPath := windowsIDEProductPath(target)
	if productPath == "" || windowsExistingFile(productPath) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(productPath)
	if err != nil {
		return nil, err
	}
	var product windowsIDEProductIntegrity
	if err := json.Unmarshal(data, &product); err != nil {
		return nil, fmt.Errorf("解析 IDE product.json 完整性配置失败: %w", err)
	}
	if len(product.Checksums) == 0 {
		return nil, nil
	}

	appOut := filepath.Join(target.root, "resources", "app", "out")
	updated := string(data)
	changed := false
	for _, plan := range rendererPlans {
		if plan == nil || plan.path == "" {
			continue
		}
		relative, relErr := filepath.Rel(appOut, plan.path)
		if relErr != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		key := filepath.ToSlash(relative)
		current, tracked := product.Checksums[key]
		if !tracked {
			continue
		}
		desired := windowsIDEChecksum(plan.updated)
		if current == desired {
			continue
		}
		active, readErr := os.ReadFile(plan.path)
		if readErr != nil {
			return nil, readErr
		}
		activeChecksum := windowsIDEChecksum(active)
		originalChecksum := windowsIDEChecksum(plan.original)
		if current != activeChecksum && current != originalChecksum {
			return nil, fmt.Errorf("IDE checksum %s 与官方源文件和当前文件均不匹配；未修改任何文件", key)
		}
		pattern := regexp.MustCompile(`("` + regexp.QuoteMeta(key) + `"\s*:\s*")` + regexp.QuoteMeta(current) + `(")`)
		if len(pattern.FindAllStringIndex(updated, -1)) != 1 {
			return nil, fmt.Errorf("IDE checksum 字段 %s 的结构尚未验证；未修改任何文件", key)
		}
		updated = pattern.ReplaceAllString(updated, `${1}`+desired+`${2}`)
		product.Checksums[key] = desired
		changed = true
	}
	if !changed {
		return nil, nil
	}
	info, err := os.Stat(productPath)
	if err != nil {
		return nil, err
	}
	return &windowsPatchPlan{
		path: productPath, original: data, updated: []byte(updated), mode: info.Mode(), changed: true,
	}, nil
}

func verifyWindowsIDEProductChecksums(target windowsTarget, rendererPaths []string) error {
	productPath := windowsIDEProductPath(target)
	if productPath == "" || windowsExistingFile(productPath) == "" {
		return nil
	}
	data, err := os.ReadFile(productPath)
	if err != nil {
		return err
	}
	var product windowsIDEProductIntegrity
	if err := json.Unmarshal(data, &product); err != nil {
		return err
	}
	if len(product.Checksums) == 0 {
		return nil
	}
	appOut := filepath.Join(target.root, "resources", "app", "out")
	for _, path := range rendererPaths {
		relative, relErr := filepath.Rel(appOut, path)
		if relErr != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		key := filepath.ToSlash(relative)
		expected, tracked := product.Checksums[key]
		if !tracked {
			continue
		}
		actual, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if windowsIDEChecksum(actual) != expected {
			return fmt.Errorf("IDE renderer checksum 校验失败: %s", key)
		}
	}
	return nil
}
