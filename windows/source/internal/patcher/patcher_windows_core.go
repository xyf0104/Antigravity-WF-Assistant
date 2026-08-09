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
	windowsPublicEndpoint        = "https://cloudcode-pa.googleapis.com"
	windowsAutopushEndpoint      = "https://autopush-cloudcode-pa.sandbox.googleapis.com"
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
var windowsFlexibleCloudCodeCallPattern = regexp.MustCompile(`await\s+[A-Za-z_$][\w$]*(?:\??\.[A-Za-z_$][\w$]*)*\??\.getCloudCodeUrl\(\)`)
var windowsCloudCodeSettingPattern = regexp.MustCompile(`this\.[A-Za-z_$][\w$]*\.getValue\(["']jetski\.cloudCodeUrl["']\)`)
var windowsCloudCodeURLPattern = regexp.MustCompile(`https://[A-Za-z0-9.-]*cloudcode-pa(?:\.sandbox)?\.googleapis\.com`)
var windowsCloudCodeFlagPattern = regexp.MustCompile(`["']--(?:cloud_code_endpoint|api_server_url)["']`)
var windowsExtensionDataPattern = regexp.MustCompile(`"--app_data_dir",[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*\.getInstance\(\)\.appDataDirectoryName`)
var windowsMainDataPattern = regexp.MustCompile(`"--app_data_dir",[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*\.ideName`)

// windowsTarget describes both the packaged Agent/2.x layout and the unpacked
// IDE layout. Keeping the shape independent from discovery makes the patch
// algorithms independently testable before the executable is shipped.
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
	hasOriginal := windowsCloudCodeURLPattern.Find(data) != nil
	hasPatched := bytes.Contains(data, []byte(windowsBaseProxyEndpoint+"/v1internal/"))
	if !hasOriginal && !hasPatched {
		return true, false
	}
	return !hasOriginal && hasPatched, true
}

func windowsTargetPatchState(target windowsTarget) (main, extension, language, fully bool) {
	language, _ = windowsLanguagePatchState(target.language)
	imagePreviewPatched := !windowsImagePreviewNeedsPatch(target)
	if target.kind == "agent" {
		main = windowsASARPatched(target.asar)
		return main, true, language, main && language && imagePreviewPatched
	}
	main = windowsMainPatched(target.main)
	extension = target.extensionEntry == "" || windowsExtensionPatched(target.extensionEntry)
	return main, extension, language, main && extension && language && imagePreviewPatched
}

func windowsImagePreviewNeedsPatch(target windowsTarget) bool {
	if target.kind == "agent" {
		return imagePreviewASARNeedsPatch(target.asar)
	}
	return imagePreviewRenderersNeedPatch(windowsImagePreviewRendererPaths(target))
}

func windowsMainPatched(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	source := string(data)
	endpointPatched := strings.Contains(source, windowsBaseProxyEndpoint) &&
		!windowsCloudCodeURLPattern.MatchString(source) &&
		!windowsCloudCodeSettingPattern.MatchString(source)
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
		windowsLauncherHasProxyEndpoint(source) &&
		!windowsFlexibleCloudCodeCallPattern.MatchString(source) &&
		!windowsCloudCodeURLPattern.MatchString(source) &&
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
	return windowsLauncherHasProxyEndpoint(string(launcher)) &&
		!windowsCloudCodeURLPattern.Match(launcher) &&
		bytes.Contains(main, []byte(windowsASARMarker)) &&
		!bytes.Contains(main, []byte(authEligibilityOriginal))
}

func windowsLauncherHasProxyEndpoint(source string) bool {
	flagLocations := windowsCloudCodeFlagPattern.FindAllStringIndex(source, -1)
	if len(flagLocations) == 0 {
		return false
	}
	for _, location := range flagLocations {
		start := location[0] - 1024
		if start < 0 {
			start = 0
		}
		end := location[1] + 1024
		if end > len(source) {
			end = len(source)
		}
		if strings.Contains(source[start:end], windowsBaseProxyEndpoint) {
			return true
		}
	}
	return false
}

func patchWindowsCloudCodeSource(source string) string {
	source = windowsCloudCodeURLPattern.ReplaceAllString(source, windowsBaseProxyEndpoint)
	source = windowsCloudCodeSettingPattern.ReplaceAllString(source, `"`+windowsBaseProxyEndpoint+`"`)
	source = windowsFlexibleCloudCodeCallPattern.ReplaceAllString(source, `"`+windowsBaseProxyEndpoint+`"`)
	return source
}

func windowsBinaryEndpointFor(original string) (string, error) {
	switch original {
	case windowsProductionEndpoint:
		return windowsBinaryProxyEndpoint, nil
	case windowsSandboxEndpoint:
		return windowsBinarySandboxEndpoint, nil
	}
	prefix := windowsBaseProxyEndpoint + "/v1internal/"
	if len(prefix) > len(original) {
		return "", fmt.Errorf("Language Server 地址过短，无法安全替换: %s", original)
	}
	return prefix + strings.Repeat("x", len(original)-len(prefix)), nil
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
	updated := append([]byte(nil), data...)
	changed, embedded := false, false
	seen := make(map[string]bool)
	for _, match := range windowsCloudCodeURLPattern.FindAll(data, -1) {
		original := string(match)
		if seen[original] {
			continue
		}
		seen[original] = true
		replacement, replacementErr := windowsBinaryEndpointFor(original)
		if replacementErr != nil {
			return nil, false, replacementErr
		}
		oldValue, newValue := []byte(original), []byte(replacement)
		if len(oldValue) != len(newValue) {
			return nil, false, fmt.Errorf("Language Server 补丁长度不一致: %d != %d", len(oldValue), len(newValue))
		}
		if bytes.Contains(updated, oldValue) {
			embedded = true
			updated = bytes.ReplaceAll(updated, oldValue, newValue)
			changed = true
		}
	}
	if bytes.Contains(updated, []byte(windowsBaseProxyEndpoint+"/v1internal/")) {
		embedded = true
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
	source := patchWindowsCloudCodeSource(string(data))
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
	if updated, result := patchImagePreviewRenderer(source); result.Changed {
		source = updated
	}
	updated := []byte(source)
	return &windowsPatchPlan{
		path: path, original: data, updated: updated, mode: info.Mode(), changed: !bytes.Equal(data, updated),
	}, nil
}

func windowsImagePreviewRendererPaths(target windowsTarget) []string {
	if target.kind != "ide" || target.root == "" {
		return nil
	}
	return imagePreviewRendererPaths(filepath.Join(target.root, "resources", "app"))
}

func windowsASARUnpackedImagePreviewRendererPaths(target windowsTarget) []string {
	if target.kind != "agent" || target.asar == "" {
		return nil
	}
	return imagePreviewASARUnpackedRendererPathsForPath(target.asar)
}

func prepareWindowsImagePreviewPatch(path string) (*windowsPatchPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取图片预览渲染器 %s 失败: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	updated, result := patchImagePreviewRenderer(string(data))
	return &windowsPatchPlan{
		path: path, original: data, updated: []byte(updated), mode: info.Mode(), changed: result.Changed,
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
	source := patchWindowsCloudCodeSource(string(data))
	if windowsExtensionDataPattern.MatchString(source) {
		source = windowsExtensionDataPattern.ReplaceAllString(source, windowsSharedDataArgument)
	}
	if !windowsLauncherHasProxyEndpoint(source) {
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

func prepareWindowsASARCandidate(sourcePath, destinationPath string) (string, error) {
	archive, err := readASAR(sourcePath)
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
	mainSource := patchWindowsCloudCodeSource(string(mainData))
	mainSource = strings.ReplaceAll(mainSource, authEligibilityOriginal, authEligibilityPatched)
	if !strings.Contains(mainSource, windowsASARMarker) {
		mainSource = addWindowsSourceMarker(mainSource, windowsASARMarker)
	}
	if updated, result := patchImagePreviewRenderer(mainSource); result.Changed {
		mainSource = updated
	}
	launcherSource := patchWindowsCloudCodeSource(string(launcherData))
	if !windowsLauncherHasProxyEndpoint(launcherSource) {
		return "", fmt.Errorf("app.asar 中的 cloud_code_endpoint 结构已变化")
	}
	// A clean backup may live on another volume. Create the candidate beside
	// the destination so replacement remains an atomic same-volume operation.
	temp, err := os.CreateTemp(filepath.Dir(destinationPath), ".antigravity-byok-windows-asar-*")
	if err != nil {
		return "", err
	}
	candidate := temp.Name()
	if err := temp.Close(); err != nil {
		return "", err
	}
	_ = os.Remove(candidate)
	replacements := map[string][]byte{
		"dist/main.js": []byte(mainSource), "dist/languageServer.js": []byte(launcherSource),
	}
	patchImagePreviewASARRenderers(archive, replacements)
	if err := archive.write(candidate, replacements); err != nil {
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
		imagePreviewPatchMarker, imagePreviewPatchV3Marker, imagePreviewPatchV2Marker,
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
