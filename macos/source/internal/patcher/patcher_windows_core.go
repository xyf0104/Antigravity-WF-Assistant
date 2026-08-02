package patcher

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	windowsProductionEndpoint    = "https://daily-cloudcode-pa.googleapis.com"
	windowsSandboxEndpoint       = "https://daily-cloudcode-pa.sandbox.googleapis.com"
	windowsBaseProxyEndpoint     = "http://127.0.0.1:50999"
	windowsTextProxyEndpoint     = "http://127.0.0.1:50999/v1internal/antigravity-byok"
	windowsBinaryProxyEndpoint   = "http://127.0.0.1:50999/v1internal/byokxxx"
	windowsBinarySandboxEndpoint = "http://127.0.0.1:50999/v1internal/byokxxx-sandbox"
	windowsExtensionMarker       = "antigravity-byok:windows-extension-endpoint"
	windowsMainMarker            = "antigravity-byok:windows-main-endpoint"
	windowsASARMarker            = "antigravity-byok:windows-asar-endpoint"
	windowsLegacyASARMarker      = "antigravity-byok:proxy-hook"
	windowsLegacyExtensionMarker = "antigravity-byok:ide-proxy-hook"
	windowsLegacyMainMarker      = "antigravity-byok:ide-main-proxy-hook"
	windowsSharedDataArgument    = `"--app_data_dir","antigravity"`
	windowsIDECloudCodeSetting   = `this._configurationService.getValue("jetski.cloudCodeUrl")`
)

var windowsCloudCodeCallPattern = regexp.MustCompile(`await [A-Za-z_$][\w$]*\.getCloudCodeUrl\(\)`)
var windowsExtensionDataPattern = regexp.MustCompile(`"--app_data_dir",[A-Za-z_$][\w$]*\.getInstance\(\)\.appDataDirectoryName`)
var windowsMainDataPattern = regexp.MustCompile(`"--app_data_dir",[A-Za-z_$][\w$]*\.ideName`)

// windowsTarget describes both the packaged Agent/2.x layout and the unpacked
// IDE layout. Keeping the shape independent from discovery makes the patch
// algorithms testable on macOS before the Windows executable is shipped.
type windowsTarget struct {
	root           string
	name           string
	kind           string
	version        string
	executable     string
	main           string
	asar           string
	extensionEntry string
	language       string
}

type windowsPatchPlan struct {
	path     string
	original []byte
	updated  []byte
	mode     os.FileMode
	changed  bool
}

func windowsExistingFile(path string) string {
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path
	}
	return ""
}

// windowsLanguagePatchState deliberately treats a Language Server without an
// embedded Cloud Code URL as valid. Newer Windows builds receive the endpoint
// entirely through languageServer.js; requiring a binary string caused the
// old "原地址计数=0、补丁地址计数=0" failure.
func windowsLanguagePatchState(path string) (patched, hasEmbeddedEndpoint bool) {
	if path == "" {
		return true, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	hasOriginal := bytes.Contains(data, []byte(windowsProductionEndpoint)) ||
		bytes.Contains(data, []byte(windowsSandboxEndpoint))
	hasPatched := bytes.Contains(data, []byte(windowsBinaryProxyEndpoint)) ||
		bytes.Contains(data, []byte(windowsBinarySandboxEndpoint))
	if !hasOriginal && !hasPatched {
		return true, false
	}
	return !hasOriginal && hasPatched, true
}

func windowsTargetPatchState(target windowsTarget) (main, extension, language, fully bool) {
	language, _ = windowsLanguagePatchState(target.language)
	if target.kind == "agent" {
		main = windowsASARPatched(target.asar)
		return main, true, language, main && language
	}
	main = windowsMainPatched(target.main)
	extension = target.extensionEntry == "" || windowsExtensionPatched(target.extensionEntry)
	return main, extension, language, main && extension && language
}

func windowsMainPatched(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	source := string(data)
	endpointPatched := strings.Contains(source, windowsBaseProxyEndpoint) &&
		!strings.Contains(source, windowsProductionEndpoint) &&
		!strings.Contains(source, windowsIDECloudCodeSetting)
	return endpointPatched && strings.Contains(source, windowsMainMarker) &&
		!strings.Contains(source, authEligibilityOriginal) &&
		!windowsMainDataPattern.MatchString(source)
}

func windowsExtensionPatched(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	source := string(data)
	return strings.Contains(source, windowsExtensionMarker) &&
		strings.Contains(source, windowsBaseProxyEndpoint) &&
		!windowsCloudCodeCallPattern.MatchString(source) &&
		!windowsExtensionDataPattern.MatchString(source)
}

func windowsASARPatched(path string) bool {
	if path == "" {
		return false
	}
	archive, err := readASAR(path)
	if err != nil {
		return false
	}
	launcher, err := archive.readFile("dist/languageServer.js")
	if err != nil {
		return false
	}
	main, err := archive.readFile("dist/main.js")
	if err != nil {
		return false
	}
	return bytes.Contains(launcher, []byte(windowsBaseProxyEndpoint)) &&
		!bytes.Contains(launcher, []byte(windowsProductionEndpoint)) &&
		bytes.Contains(main, []byte(windowsASARMarker)) &&
		!bytes.Contains(main, []byte(authEligibilityOriginal))
}

func prepareWindowsLanguagePatch(path string) (*windowsPatchPlan, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	replacements := [][2]string{
		{windowsProductionEndpoint, windowsBinaryProxyEndpoint},
		{windowsSandboxEndpoint, windowsBinarySandboxEndpoint},
	}
	updated := append([]byte(nil), data...)
	changed, embedded := false, false
	for _, replacement := range replacements {
		oldValue, newValue := []byte(replacement[0]), []byte(replacement[1])
		if len(oldValue) != len(newValue) {
			return nil, false, fmt.Errorf("Language Server 补丁长度不一致: %d != %d", len(oldValue), len(newValue))
		}
		if bytes.Contains(updated, oldValue) {
			embedded = true
			updated = bytes.ReplaceAll(updated, oldValue, newValue)
			changed = true
		}
		if bytes.Contains(updated, newValue) {
			embedded = true
		}
	}
	return &windowsPatchPlan{
		path: path, original: data, updated: updated, mode: info.Mode(), changed: changed,
	}, embedded, nil
}

func prepareWindowsMainPatch(path string) (*windowsPatchPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	source := string(data)
	source = strings.ReplaceAll(source, windowsProductionEndpoint, windowsTextProxyEndpoint)
	source = strings.ReplaceAll(source, windowsIDECloudCodeSetting, `"`+windowsBaseProxyEndpoint+`"`)
	source = strings.ReplaceAll(source, authEligibilityOriginal, authEligibilityPatched)
	if windowsMainDataPattern.MatchString(source) {
		source = windowsMainDataPattern.ReplaceAllString(source, windowsSharedDataArgument)
	}
	if !strings.Contains(source, windowsBaseProxyEndpoint) {
		return nil, fmt.Errorf("%s 中未找到受支持的 Cloud Code URL 设置", path)
	}
	if !strings.Contains(source, windowsMainMarker) {
		source = addWindowsSourceMarker(source, windowsMainMarker)
	}
	updated := []byte(source)
	return &windowsPatchPlan{
		path: path, original: data, updated: updated, mode: info.Mode(), changed: !bytes.Equal(data, updated),
	}, nil
}

func prepareWindowsExtensionPatch(path string) (*windowsPatchPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	source := string(data)
	if windowsCloudCodeCallPattern.MatchString(source) {
		source = windowsCloudCodeCallPattern.ReplaceAllString(source, `"`+windowsBaseProxyEndpoint+`"`)
	}
	source = strings.ReplaceAll(source, windowsProductionEndpoint, windowsBaseProxyEndpoint)
	if windowsExtensionDataPattern.MatchString(source) {
		source = windowsExtensionDataPattern.ReplaceAllString(source, windowsSharedDataArgument)
	}
	if !strings.Contains(source, windowsBaseProxyEndpoint) {
		return nil, fmt.Errorf("%s 中未找到受支持的 getCloudCodeUrl 调用", path)
	}
	if !strings.Contains(source, windowsExtensionMarker) {
		source = addWindowsSourceMarker(source, windowsExtensionMarker)
	}
	updated := []byte(source)
	return &windowsPatchPlan{
		path: path, original: data, updated: updated, mode: info.Mode(), changed: !bytes.Equal(data, updated),
	}, nil
}

func addWindowsSourceMarker(source, marker string) string {
	line := "// " + marker
	license := "/*! For license information please see extension.js.LICENSE.txt */"
	if strings.HasPrefix(source, license) {
		return strings.Replace(source, license, license+"\n"+line, 1)
	}
	strict := `"use strict";`
	if strings.HasPrefix(source, strict) {
		return strings.Replace(source, strict, strict+"\n"+line, 1)
	}
	return line + "\n" + source
}

func prepareWindowsASARCandidate(path string) (string, error) {
	archive, err := readASAR(path)
	if err != nil {
		return "", err
	}
	mainData, err := archive.readFile("dist/main.js")
	if err != nil {
		return "", err
	}
	launcherData, err := archive.readFile("dist/languageServer.js")
	if err != nil {
		return "", err
	}
	mainSource := string(mainData)
	mainSource = strings.ReplaceAll(mainSource, windowsProductionEndpoint, windowsTextProxyEndpoint)
	mainSource = strings.ReplaceAll(mainSource, authEligibilityOriginal, authEligibilityPatched)
	if !strings.Contains(mainSource, windowsASARMarker) {
		mainSource = addWindowsSourceMarker(mainSource, windowsASARMarker)
	}
	launcherSource := string(launcherData)
	launcherSource = strings.ReplaceAll(launcherSource, windowsProductionEndpoint, windowsBaseProxyEndpoint)
	launcherSource = strings.ReplaceAll(launcherSource, windowsSandboxEndpoint, windowsBaseProxyEndpoint)
	if !strings.Contains(launcherSource, windowsBaseProxyEndpoint) {
		return "", fmt.Errorf("app.asar 中的 cloud_code_endpoint 结构已变化")
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".antigravity-byok-windows-asar-*")
	if err != nil {
		return "", err
	}
	candidate := temp.Name()
	if err := temp.Close(); err != nil {
		return "", err
	}
	_ = os.Remove(candidate)
	if err := archive.write(candidate, map[string][]byte{
		"dist/main.js": []byte(mainSource), "dist/languageServer.js": []byte(launcherSource),
	}); err != nil {
		return "", err
	}
	if !windowsASARPatched(candidate) {
		_ = os.Remove(candidate)
		return "", fmt.Errorf("app.asar 补丁候选未通过完整性校验")
	}
	return candidate, nil
}

func windowsContainsKnownPatch(data []byte) bool {
	markers := []string{
		windowsBaseProxyEndpoint, windowsTextProxyEndpoint, windowsBinaryProxyEndpoint,
		windowsBinarySandboxEndpoint, authEligibilityPatched,
		windowsExtensionMarker, windowsMainMarker, windowsASARMarker,
		windowsLegacyASARMarker, windowsLegacyExtensionMarker, windowsLegacyMainMarker,
	}
	for _, marker := range markers {
		if bytes.Contains(data, []byte(marker)) {
			return true
		}
	}
	return false
}

func windowsBackupPath(sourcePath string) string {
	dir := strings.TrimSpace(os.Getenv("ANTIGRAVITY_BYOK_BACKUP_DIR"))
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".antigravity-byok", "backups")
	}
	digest := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(sourcePath))))
	return filepath.Join(dir, fmt.Sprintf("%s-%x.windows.bak", filepath.Base(sourcePath), digest[:8]))
}

func windowsLegacyBackupPaths(sourcePath string) []string {
	return []string{
		sourcePath + ".orig",
		sourcePath + ".antigravity-byok.orig",
	}
}

func windowsVersionFromTarget(target windowsTarget) string {
	var data []byte
	if target.kind == "ide" {
		data, _ = os.ReadFile(filepath.Join(target.root, "resources", "app", "package.json"))
	} else if target.asar != "" {
		if archive, err := readASAR(target.asar); err == nil {
			data, _ = archive.readFile("package.json")
		}
	}
	var value struct {
		Version string `json:"version"`
	}
	if len(data) > 0 && json.Unmarshal(data, &value) == nil {
		return strings.TrimSpace(value.Version)
	}
	return ""
}
