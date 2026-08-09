//go:build darwin

package patcher

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	productionEndpoint = "https://daily-cloudcode-pa.googleapis.com"
	sandboxEndpoint    = "https://daily-cloudcode-pa.sandbox.googleapis.com"

	baseProxyEndpoint          = "http://127.0.0.1:50999"
	textProxyEndpoint          = "http://127.0.0.1:50999/v1internal/antigravity-byok"
	binaryProxyEndpoint        = "http://127.0.0.1:50999/v1internal/byokxxx"
	binarySandboxProxyEndpoint = "http://127.0.0.1:50999/v1internal/byokxxx-sandbox"

	darwinExtensionMarker = "antigravity-byok:mac-extension-endpoint"
	darwinASARMarker      = "antigravity-byok:mac-asar-endpoint"
)

var darwinCloudCodeCallPattern = regexp.MustCompile(`await [A-Za-z_$][\w$]*\.getCloudCodeUrl\(\)`)
var darwinExtensionDataPattern = regexp.MustCompile(`"--app_data_dir",[A-Za-z_$][\w$]*\.getInstance\(\)\.appDataDirectoryName`)
var darwinMainDataPattern = regexp.MustCompile(`"--app_data_dir",[A-Za-z_$][\w$]*\.ideName`)

// signDarwinLanguageServer is kept as a narrow seam for transaction tests.
// Production always calls signPatchedDarwinLanguageServer; tests can induce a
// post-ASAR-write failure without depending on the host machine's codesign
// implementation or certificate configuration.
var signDarwinLanguageServer = signPatchedDarwinLanguageServer

const darwinSharedDataArgument = `"--app_data_dir","antigravity"`

type darwinTargets struct {
	app            string
	name           string
	kind           string
	version        string
	main           string
	asar           string
	extension      string
	extensionEntry string
	language       string
}

type byteReplacement struct {
	old []byte
	new []byte
}

type patchPlan struct {
	path     string
	original []byte
	updated  []byte
	mode     os.FileMode
	changed  bool
}

func runDarwin(action string) (string, error) {
	if action == "sync-history" {
		summary, err := syncDarwinHistory()
		if err != nil {
			return "", err
		}
		return summary.message(), nil
	}

	targets := locateDarwinInstallations()
	if action == "status" {
		return darwinStatusAll(targets), nil
	}

	if len(targets) == 0 {
		return "", fmt.Errorf("未找到可支持的 Antigravity 安装（已检查环境变量、/Applications、~/Applications、正在运行的应用和 Spotlight）")
	}

	switch action {
	case "apply", "apply-ide":
		return applyDarwinPatches(targets, action == "apply-ide")
	case "restore":
		return restoreDarwinPatches(targets)
	default:
		return "", fmt.Errorf("未知补丁操作: %s", action)
	}
}

func locateDarwinTargets() darwinTargets {
	targets := locateDarwinInstallations()
	if len(targets) == 0 {
		return darwinTargets{}
	}
	return targets[0]
}

func existingFile(path string) string {
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path
	}
	return ""
}

func darwinStatus(targets darwinTargets) string {
	mainPatched, _, _, fullyPatched := darwinTargetPatchState(targets)

	return fmt.Sprintf(
		"agent_patched=%t\nide_patched=%t\nide_main_patched=%t\nproxy_listening=%t\nasar=%s\nlanguage_server=%s\nide_extension=%s\nide_language_server=%s\n",
		fullyPatched,
		fullyPatched,
		mainPatched,
		proxyPortListening(),
		firstNonEmpty(targets.asar, targets.main),
		targets.language,
		targets.extensionEntry,
		targets.language,
	)
}

func darwinStatusAll(targets []darwinTargets) string {
	status := buildDarwinStatus(targets)
	return fmt.Sprintf(
		"agent_patched=%s\nide_patched=%s\nide_main_patched=%s\nproxy_listening=%t\nasar=%s\nlanguage_server=%s\nide_extension=%s\nide_language_server=%s\n",
		boolPointerText(status.AgentPatched), boolPointerText(status.IDEPatched), boolPointerText(status.IdeMainPatched),
		status.ProxyListening, status.AsarPath, status.LSPath, status.IDEExtensionPath, status.IDELSPath,
	)
}

func getDarwinStatus() Status {
	return buildDarwinStatus(locateDarwinInstallations())
}

func buildDarwinStatus(targets []darwinTargets) Status {
	status := Status{ProxyListening: proxyPortListening()}
	agentInstalled, ideInstalled := false, false
	agentPatched, idePatched := true, true
	ideMainPatched := true
	for _, target := range targets {
		mainPatched, _, _, patched := darwinTargetPatchState(target)
		entry := TargetStatus{
			Name: target.name, Kind: target.kind, Version: target.version, AppPath: target.app,
			MainPath: target.main, ASARPath: target.asar, ExtensionPath: target.extensionEntry,
			LanguageServerPath: target.language, Patched: patched,
		}
		status.Targets = append(status.Targets, entry)
		if status.AsarPath == "" {
			status.AsarPath = firstNonEmpty(target.asar, target.main)
			status.LSPath = target.language
		}
		if target.kind == "agent" {
			agentInstalled = true
			agentPatched = agentPatched && patched
		} else {
			ideInstalled = true
			idePatched = idePatched && patched
			ideMainPatched = ideMainPatched && mainPatched
			if status.IDEExtensionPath == "" {
				status.IDEExtensionPath = target.extensionEntry
				status.IDELSPath = target.language
			}
		}
	}
	if agentInstalled {
		status.AgentPatched = boolPointer(agentPatched)
	}
	if ideInstalled {
		status.IDEPatched = boolPointer(idePatched)
		status.IdeMainPatched = boolPointer(ideMainPatched)
	}
	// Older frontends expect both aggregate flags even when macOS packages the
	// Agent Window and IDE into one bundle. Keep those fields compatible while
	// the per-target list remains the authoritative state.
	if !agentInstalled && ideInstalled {
		status.AgentPatched = boolPointer(idePatched)
	}
	if !ideInstalled && agentInstalled {
		status.IDEPatched = boolPointer(agentPatched)
		status.IdeMainPatched = boolPointer(agentPatched)
	}
	return status
}

func darwinTargetPatchState(target darwinTargets) (main, extension, language, fully bool) {
	language = fileHasPatch(
		target.language,
		[][]byte{[]byte(productionEndpoint), []byte(sandboxEndpoint)},
		[][]byte{[]byte(binaryProxyEndpoint), []byte(binarySandboxProxyEndpoint)},
	)
	imagePreviewPatched := !darwinImagePreviewNeedsPatch(target)
	if target.kind == "agent" {
		main = darwinASARPatched(target.asar)
		return main, true, language, main && language && imagePreviewPatched
	}
	main = darwinMainPatched(target.main)
	extension = target.extensionEntry == "" || darwinExtensionPatched(target.extensionEntry)
	return main, extension, language, main && extension && language && imagePreviewPatched
}

func darwinImagePreviewNeedsPatch(target darwinTargets) bool {
	if target.kind == "agent" {
		return imagePreviewASARNeedsPatch(target.asar)
	}
	return imagePreviewRenderersNeedPatch(darwinImagePreviewRendererPaths(target))
}

func darwinASARUnpackedImagePreviewRendererPaths(target darwinTargets) []string {
	if target.kind != "agent" || target.asar == "" {
		return nil
	}
	return imagePreviewASARUnpackedRendererPathsForPath(target.asar)
}

func darwinMainPatched(path string) bool {
	if !fileHasPatch(
		path,
		[][]byte{[]byte(productionEndpoint), []byte(authEligibilityOriginal)},
		[][]byte{[]byte(textProxyEndpoint), []byte(authEligibilityPatched)},
	) {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	source := string(data)
	return !darwinMainDataPattern.MatchString(source) &&
		(strings.Contains(source, darwinSharedDataArgument) || !strings.Contains(source, "--app_data_dir"))
}

func darwinExtensionPatched(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	source := string(data)
	return strings.Contains(source, darwinExtensionMarker) &&
		strings.Contains(source, baseProxyEndpoint) && !darwinCloudCodeCallPattern.MatchString(source) &&
		!darwinExtensionDataPattern.MatchString(source) &&
		(strings.Contains(source, darwinSharedDataArgument) || !strings.Contains(source, "--app_data_dir"))
}

func darwinASARPatched(path string) bool {
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
	return err == nil && bytes.Contains(launcher, []byte(baseProxyEndpoint)) &&
		!bytes.Contains(launcher, []byte(productionEndpoint)) && bytes.Contains(main, []byte(darwinASARMarker))
}

func boolPointer(value bool) *bool { return &value }

func boolPointerText(value *bool) string {
	if value == nil {
		return "None"
	}
	if *value {
		return "true"
	}
	return "false"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func fileHasPatch(path string, originals, patched [][]byte) bool {
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	hasPatched := false
	for _, marker := range patched {
		hasPatched = hasPatched || bytes.Contains(data, marker)
	}
	for _, marker := range originals {
		if bytes.Contains(data, marker) {
			return false
		}
	}
	return hasPatched
}

func proxyPortListening() bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:50999", 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func applyDarwinPatch(targets darwinTargets) (string, error) {
	if targets.app == "" || targets.language == "" {
		return "", fmt.Errorf("%s 安装不完整，缺少应用目录或 language_server", targets.name)
	}
	if targets.kind == "agent" {
		return applyDarwinASARPatch(targets)
	}
	if targets.main == "" {
		return "", fmt.Errorf("%s 中未找到主进程脚本", targets.app)
	}
	mainPlan, err := prepareDarwinMainPatch(targets.main)
	if err != nil {
		return "", err
	}
	authRecognized := bytes.Contains(mainPlan.original, []byte(authEligibilityOriginal)) ||
		bytes.Contains(mainPlan.original, []byte(authEligibilityPatched))
	languagePlan, err := preparePatch(targets.language, []byteReplacement{
		{old: []byte(productionEndpoint), new: []byte(binaryProxyEndpoint)},
		{old: []byte(sandboxEndpoint), new: []byte(binarySandboxProxyEndpoint)},
	})
	if err != nil {
		return "", err
	}

	plans := []*patchPlan{mainPlan, languagePlan}
	if targets.extensionEntry != "" {
		extensionPlan, err := prepareDarwinExtensionPatch(targets.extensionEntry)
		if err != nil {
			return "", err
		}
		plans = append(plans, extensionPlan)
	}
	for _, rendererPath := range darwinImagePreviewRendererPaths(targets) {
		if rendererPath == targets.main {
			// The endpoint plan above already applied the image-preview fallback
			// to out/main.js when the known renderer shape was present.
			continue
		}
		rendererPlan, err := prepareDarwinImagePreviewPatch(rendererPath)
		if err != nil {
			return "", err
		}
		plans = append(plans, rendererPlan)
	}
	changed := false
	for _, plan := range plans {
		changed = changed || plan.changed
	}
	if !changed {
		return fmt.Sprintf("%s 补丁已处于激活状态，无需重复应用。", targets.name), nil
	}
	if darwinMainDataPattern.Match(mainPlan.original) ||
		(len(plans) > 2 && darwinExtensionDataPattern.Match(plans[2].original)) {
		if err := mergeDarwinHistory(); err != nil {
			return "", fmt.Errorf("合并历史会话失败: %w", err)
		}
	}

	if err := saveApplyBackups(plans, nil); err != nil {
		return "", fmt.Errorf("创建补丁备份失败: %w", err)
	}
	if err := writePatchPlans(plans); err != nil {
		_ = rollbackPatchPlans(plans)
		return "", err
	}
	if languagePlan.changed {
		if err := signDarwinLanguageServer(targets.language); err != nil {
			_ = rollbackPatchPlans(plans)
			return "", fmt.Errorf("补丁已自动回滚，macOS 语言服务器签名失败: %w", err)
		}
	}

	warning := ""
	if !authRecognized {
		warning = "\n提示：此版本未匹配旧版登录资格分支，已保留其原生本地凭据登录逻辑。"
	}
	return fmt.Sprintf(
		"%s 补丁应用成功。\n应用: %s\n主进程: %s\n扩展: %s\n语言服务器: %s%s",
		targets.name, targets.app, targets.main, targets.extensionEntry, targets.language, warning,
	), nil
}

func prepareDarwinMainPatch(path string) (*patchPlan, error) {
	plan, err := preparePatch(path, []byteReplacement{
		{old: []byte(productionEndpoint), new: []byte(textProxyEndpoint)},
		{old: []byte(authEligibilityOriginal), new: []byte(authEligibilityPatched)},
	})
	if err != nil {
		return nil, err
	}
	source := string(plan.updated)
	if darwinMainDataPattern.MatchString(source) {
		source = darwinMainDataPattern.ReplaceAllString(source, darwinSharedDataArgument)
	}
	if updated, result := patchImagePreviewRenderer(source); result.Changed {
		source = updated
	}
	plan.updated = []byte(source)
	plan.changed = !bytes.Equal(plan.original, plan.updated)
	return plan, nil
}

func darwinImagePreviewRendererPaths(target darwinTargets) []string {
	if target.kind != "ide" || target.app == "" {
		return nil
	}
	return imagePreviewRendererPaths(filepath.Join(target.app, "Contents", "Resources", "app"))
}

func prepareDarwinImagePreviewPatch(path string) (*patchPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取图片预览渲染器 %s 失败: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	updated, result := patchImagePreviewRenderer(string(data))
	return &patchPlan{
		path: path, original: data, updated: []byte(updated), mode: info.Mode(), changed: result.Changed,
	}, nil
}

func applyDarwinPatches(targets []darwinTargets, onlyIDE bool) (string, error) {
	var messages []string
	selected := 0
	for _, target := range targets {
		if onlyIDE && target.kind != "ide" {
			continue
		}
		selected++
		message, err := applyDarwinPatch(target)
		if err != nil {
			return strings.Join(messages, "\n\n"), fmt.Errorf("%s 补丁失败: %w", target.name, err)
		}
		messages = append(messages, message)
	}
	if selected == 0 {
		return "", fmt.Errorf("未找到 unpacked IDE 类型的 Antigravity 安装")
	}
	return strings.Join(messages, "\n\n") + "\n\n请保持本工具运行，然后完全退出并重启对应的 Antigravity。", nil
}

func prepareDarwinExtensionPatch(path string) (*patchPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	source := string(data)
	if darwinExtensionPatched(path) {
		return &patchPlan{path: path, original: data, updated: data, mode: info.Mode()}, nil
	}
	count := len(darwinCloudCodeCallPattern.FindAllStringIndex(source, -1))
	endpointAlreadyPatched := count == 0 && strings.Contains(source, baseProxyEndpoint)
	if count == 0 && !endpointAlreadyPatched {
		return nil, fmt.Errorf("%s 中未找到受支持的 getCloudCodeUrl 调用", path)
	}
	if count > 0 {
		source = darwinCloudCodeCallPattern.ReplaceAllString(source, `"`+baseProxyEndpoint+`"`)
	}
	if darwinExtensionDataPattern.MatchString(source) {
		source = darwinExtensionDataPattern.ReplaceAllString(source, darwinSharedDataArgument)
	}
	marker := "// " + darwinExtensionMarker
	license := "/*! For license information please see extension.js.LICENSE.txt */"
	if !strings.Contains(source, darwinExtensionMarker) {
		if strings.HasPrefix(source, license) {
			source = strings.Replace(source, license, license+"\n"+marker, 1)
		} else {
			source = marker + "\n" + source
		}
	}
	updated := []byte(source)
	return &patchPlan{path: path, original: data, updated: updated, mode: info.Mode(), changed: !bytes.Equal(data, updated)}, nil
}

type darwinHistorySummary struct {
	target  string
	sources int
	copied  int
}

func (s darwinHistorySummary) message() string {
	if s.sources == 0 {
		return fmt.Sprintf("启动时历史会话检查完成：未发现独立的旧会话目录；共享目录：%s", s.target)
	}
	if s.copied == 0 {
		return fmt.Sprintf("启动时历史会话检查完成：已检查 %d 个旧目录，没有新增文件；共享目录：%s", s.sources, s.target)
	}
	return fmt.Sprintf(
		"启动时历史会话恢复完成：从 %d 个旧目录新增 %d 个文件；共享目录：%s；旧目录备份已保留",
		s.sources, s.copied, s.target,
	)
}

func mergeDarwinHistory() error {
	_, err := syncDarwinHistory()
	return err
}

func syncDarwinHistory() (darwinHistorySummary, error) {
	base := strings.TrimSpace(os.Getenv("ANTIGRAVITY_BYOK_GEMINI_DIR"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return darwinHistorySummary{}, err
		}
		base = filepath.Join(home, ".gemini")
	}
	target := filepath.Join(base, "antigravity")
	summary := darwinHistorySummary{target: target}
	sources, err := discoverDarwinHistorySources(base, target)
	if err != nil {
		return summary, err
	}
	summary.sources = len(sources)
	if len(sources) == 0 {
		return summary, nil
	}
	if info, err := os.Stat(target); os.IsNotExist(err) {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return summary, fmt.Errorf("创建共享历史目录失败: %w", err)
		}
	} else if err != nil {
		return summary, err
	} else if !info.IsDir() {
		return summary, fmt.Errorf("共享历史路径不是目录: %s", target)
	}

	resources := []string{
		"annotations", "brain", "browser_recordings", "context_state", "conversations",
		"code_tracker", "html_artifacts", "implicit", "knowledge", "playground", "plugins", "prompting", "scratch",
	}
	for _, source := range sources {
		backup := source + ".antigravity-byok-backup"
		if _, err := copyDirectoryMissingCount(source, backup); err != nil {
			return summary, fmt.Errorf("备份旧历史目录失败（%s）: %w", source, err)
		}
		for _, resource := range resources {
			copied, err := copyDirectoryMissingCount(filepath.Join(source, resource), filepath.Join(target, resource))
			if err != nil {
				return summary, fmt.Errorf("恢复历史资源失败（%s）: %w", resource, err)
			}
			summary.copied += copied
		}
		for _, name := range []string{"mcp_config.json"} {
			sourceFile, targetFile := filepath.Join(source, name), filepath.Join(target, name)
			if _, err := os.Stat(targetFile); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return summary, err
			}
			if info, err := os.Stat(sourceFile); err == nil && info.Mode().IsRegular() {
				if err := copyFileAtomic(sourceFile, targetFile, info.Mode()); err != nil {
					return summary, err
				}
				summary.copied++
			}
		}
	}
	return summary, nil
}

func copyDirectoryMissing(source, target string) error {
	_, err := copyDirectoryMissingCount(source, target)
	return err
}

func discoverDarwinHistorySources(base, target string) ([]string, error) {
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sources []string
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		path := filepath.Join(base, entry.Name())
		if !entry.IsDir() || filepath.Clean(path) == filepath.Clean(target) || !strings.HasPrefix(name, "antigravity") {
			continue
		}
		if strings.Contains(name, "antigravity-byok-backup") || strings.Contains(name, ".previous-") {
			continue
		}
		if !darwinDirectoryContainsHistory(path) {
			continue
		}
		sources = append(sources, path)
	}
	sort.Strings(sources)
	return sources, nil
}

func darwinDirectoryContainsHistory(path string) bool {
	for _, resource := range []string{"conversations", "brain", "context_state", "prompting"} {
		if info, err := os.Stat(filepath.Join(path, resource)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func copyDirectoryMissingCount(source, target string) (int, error) {
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, nil
	}
	copied := 0
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 || skipDarwinHistoryFile(entry.Name()) {
			return nil
		}
		if _, err := os.Stat(destination); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if err := copyFileAtomic(path, destination, fileInfo.Mode()); err != nil {
			return err
		}
		copied++
		return nil
	})
	return copied, err
}

func skipDarwinHistoryFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".tmp") || strings.HasSuffix(lower, ".db-wal") ||
		strings.HasSuffix(lower, ".db-shm") || strings.HasSuffix(lower, ".lock")
}

func applyDarwinASARPatch(target darwinTargets) (message string, err error) {
	if target.asar == "" {
		return "", fmt.Errorf("%s 中未找到 app.asar", target.app)
	}
	languagePlan, err := preparePatch(target.language, []byteReplacement{
		{old: []byte(productionEndpoint), new: []byte(binaryProxyEndpoint)},
		{old: []byte(sandboxEndpoint), new: []byte(binarySandboxProxyEndpoint)},
	})
	if err != nil {
		return "", err
	}
	asarWasPatched := darwinASARPatched(target.asar)
	// Rebuild app.asar only for endpoint or packed-renderer changes. Renderer
	// files declared as unpacked live beside the archive and must not be folded
	// into its manifest during an image-preview upgrade.
	asarChanged := !asarWasPatched || imagePreviewASARArchiveNeedsPatch(target.asar)
	previewPlans := make([]*patchPlan, 0)
	for _, rendererPath := range darwinASARUnpackedImagePreviewRendererPaths(target) {
		plan, planErr := prepareDarwinImagePreviewPatch(rendererPath)
		if planErr != nil {
			return "", planErr
		}
		previewPlans = append(previewPlans, plan)
	}
	if !asarChanged && !languagePlan.changed && !patchPlansChanged(previewPlans) {
		return fmt.Sprintf("%s 补丁已处于激活状态，无需重复应用。", target.name), nil
	}
	if asarWasPatched {
		if _, statErr := os.Stat(backupPath(target.asar)); statErr != nil {
			return "", fmt.Errorf("app.asar 已打补丁但缺少原始备份: %s", backupPath(target.asar))
		}
	}

	var candidate string
	var rollbackASAR string
	var rollbackASARMode os.FileMode
	if asarChanged {
		// Keep a per-operation snapshot of the active archive.  During a v3 ->
		// v4 migration the canonical backup intentionally remains the clean
		// vendor archive, while the active archive still contains the previous
		// helper patch.  A late failure (for example code-signing the language
		// server) must restore that active pre-upgrade state, not silently turn
		// the application into an unpatched install.
		activeInfo, statErr := os.Stat(target.asar)
		if statErr != nil {
			return "", statErr
		}
		rollbackFile, createErr := os.CreateTemp(filepath.Dir(target.asar), ".antigravity-wf-asar-rollback-*")
		if createErr != nil {
			return "", createErr
		}
		rollbackASAR = rollbackFile.Name()
		if closeErr := rollbackFile.Close(); closeErr != nil {
			return "", closeErr
		}
		if removeErr := os.Remove(rollbackASAR); removeErr != nil && !os.IsNotExist(removeErr) {
			return "", removeErr
		}
		if copyErr := copyFileAtomic(target.asar, rollbackASAR, activeInfo.Mode()); copyErr != nil {
			return "", fmt.Errorf("创建 app.asar 事务回滚副本失败: %w", copyErr)
		}
		rollbackASARMode = activeInfo.Mode()
		defer os.Remove(rollbackASAR)

		candidateSource := target.asar
		if asarWasPatched {
			// Preserve the canonical clean backup while rebuilding the archive.
			// Calling writeFileBackup on a previous helper patch would rotate the
			// clean restore point into a historical file and break Restore.
			candidateSource = backupPath(target.asar)
		}
		candidate, err = prepareDarwinASARCandidate(candidateSource, target.asar)
		if err != nil {
			return "", err
		}
		defer os.Remove(candidate)
		if !asarWasPatched {
			if err = writeFileBackup(target.asar); err != nil {
				return "", fmt.Errorf("创建 app.asar 备份失败: %w", err)
			}
		}
	}
	plans := append([]*patchPlan{}, previewPlans...)
	plans = append(plans, languagePlan)
	if err = saveApplyBackups(plans, nil); err != nil {
		return "", fmt.Errorf("创建补丁备份失败: %w", err)
	}
	wroteASAR := false
	defer func() {
		if err == nil {
			return
		}
		_ = rollbackPatchPlans(plans)
		if wroteASAR {
			if rollbackErr := copyFileAtomic(rollbackASAR, target.asar, rollbackASARMode); rollbackErr != nil {
				err = fmt.Errorf("%w；app.asar 事务回滚失败: %v", err, rollbackErr)
			}
		}
	}()
	if err = writePatchPlans(plans); err != nil {
		return "", err
	}
	if asarChanged {
		info, statErr := os.Stat(target.asar)
		if statErr != nil {
			return "", statErr
		}
		if err = os.Chmod(candidate, info.Mode().Perm()); err != nil {
			return "", err
		}
		if err = os.Rename(candidate, target.asar); err != nil {
			return "", fmt.Errorf("替换 app.asar 失败: %w", err)
		}
		wroteASAR = true
	}
	if languagePlan.changed {
		if err = signDarwinLanguageServer(target.language); err != nil {
			return "", fmt.Errorf("macOS 语言服务器签名失败: %w", err)
		}
	}
	if _, _, _, patched := darwinTargetPatchState(target); !patched {
		return "", fmt.Errorf("写入后的 app.asar 补丁未通过完整校验")
	}
	return fmt.Sprintf("%s 补丁应用成功。\n应用: %s\nASAR: %s\n语言服务器: %s", target.name, target.app, target.asar, target.language), nil
}

func prepareDarwinASARCandidate(sourcePath, destinationPath string) (string, error) {
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
	mainSource := string(mainData)
	if !strings.Contains(mainSource, darwinASARMarker) {
		if !strings.HasPrefix(mainSource, `"use strict";`) {
			return "", fmt.Errorf("当前 app.asar 主入口结构已变化，已停止补丁")
		}
		mainSource = strings.Replace(mainSource, `"use strict";`, `"use strict";`+"\n// "+darwinASARMarker, 1)
	}
	mainSource = strings.ReplaceAll(mainSource, productionEndpoint, textProxyEndpoint)
	mainSource = strings.ReplaceAll(mainSource, authEligibilityOriginal, authEligibilityPatched)
	if updated, result := patchImagePreviewRenderer(mainSource); result.Changed {
		mainSource = updated
	}
	launcherSource := string(launcherData)
	if strings.Contains(launcherSource, productionEndpoint) {
		launcherSource = strings.ReplaceAll(launcherSource, productionEndpoint, baseProxyEndpoint)
	} else if !strings.Contains(launcherSource, baseProxyEndpoint) {
		return "", fmt.Errorf("app.asar 中的 cloud_code_endpoint 结构已变化")
	}
	// A clean backup can live in the home directory while the application is in
	// /Applications. Build beside the destination to retain same-volume rename
	// semantics when applying the candidate.
	temp, err := os.CreateTemp(filepath.Dir(destinationPath), ".antigravity-byok-asar-*")
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
	if !darwinASARPatched(candidate) {
		_ = os.Remove(candidate)
		return "", fmt.Errorf("app.asar 补丁候选未通过完整性校验")
	}
	return candidate, nil
}

func preparePatch(path string, replacements []byteReplacement) (*patchPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	updated := append([]byte(nil), data...)
	changed := false
	hasPatchedMarker := false
	for _, replacement := range replacements {
		if len(replacement.old) == len(replacement.new) || !strings.Contains(filepath.Base(path), "language_server") {
			if bytes.Contains(updated, replacement.old) {
				updated = bytes.ReplaceAll(updated, replacement.old, replacement.new)
				changed = true
			}
		} else {
			return nil, fmt.Errorf("语言服务器补丁长度不一致: %d != %d", len(replacement.old), len(replacement.new))
		}
		hasPatchedMarker = hasPatchedMarker || bytes.Contains(data, replacement.new)
	}
	if !changed && !hasPatchedMarker {
		return nil, fmt.Errorf("%s 中未找到受支持的 Antigravity 接口地址；该版本可能尚未适配", path)
	}
	return &patchPlan{path: path, original: data, updated: updated, mode: info.Mode(), changed: changed}, nil
}

func patchPlansChanged(plans []*patchPlan) bool {
	for _, plan := range plans {
		if plan != nil && plan.changed {
			return true
		}
	}
	return false
}

func saveApplyBackups(plans []*patchPlan, signingPaths []string) error {
	for _, plan := range plans {
		if plan.changed {
			backupWriter := writeCurrentBackup
			if containsKnownDarwinPatch(plan.original) {
				// A previous BYOK version may already have patched one part of the
				// file. Preserve the older clean restore point while completing the
				// remaining changes (for example, app_data_dir migration).
				backupWriter = writeBackup
			}
			if err := backupWriter(plan.path, plan.original); err != nil {
				return err
			}
		}
	}
	for _, path := range signingPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := writeBackup(path, data); err != nil {
			return err
		}
	}
	return nil
}

func containsKnownDarwinPatch(data []byte) bool {
	for _, marker := range [][]byte{
		[]byte(baseProxyEndpoint), []byte(textProxyEndpoint), []byte(binaryProxyEndpoint),
		[]byte(binarySandboxProxyEndpoint), []byte(authEligibilityPatched),
		[]byte(darwinExtensionMarker), []byte(darwinASARMarker),
		[]byte(imagePreviewPatchMarker), []byte(imagePreviewPatchV3Marker), []byte(imagePreviewPatchV2Marker),
	} {
		if bytes.Contains(data, marker) {
			return true
		}
	}
	return false
}

func writePatchPlans(plans []*patchPlan) error {
	for _, plan := range plans {
		if !plan.changed {
			continue
		}
		if err := writeFileAtomic(plan.path, plan.updated, plan.mode); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", plan.path, err)
		}
	}
	return nil
}

func rollbackPatchPlans(plans []*patchPlan) error {
	for _, plan := range plans {
		if plan.changed {
			if err := writeFileAtomic(plan.path, plan.original, plan.mode); err != nil {
				return err
			}
		}
	}
	return nil
}

func restoreDarwinPatch(targets darwinTargets) (string, error) {
	paths := []string{targets.main, targets.extensionEntry, targets.asar, targets.language}
	paths = append(paths, darwinImagePreviewRendererPaths(targets)...)
	paths = append(paths, darwinASARUnpackedImagePreviewRendererPaths(targets)...)

	restored := 0
	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		backup := backupPath(path)
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("读取 %s 的备份失败: %w", path, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if err := copyFileAtomic(backup, path, info.Mode()); err != nil {
			return "", fmt.Errorf("恢复 %s 失败: %w", path, err)
		}
		restored++
	}
	if restored == 0 {
		return "未发现可恢复的 macOS 补丁备份。", nil
	}

	if os.Getenv("ANTIGRAVITY_BYOK_SKIP_CODESIGN") != "1" {
		if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", targets.app).CombinedOutput(); err != nil {
			return "", fmt.Errorf("文件已恢复，但原始签名校验失败（备份已保留）: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}
	for _, path := range paths {
		if path != "" && seen[path] {
			_ = os.Remove(backupPath(path))
		}
	}
	return fmt.Sprintf("%s 的原始文件与原始 macOS 签名已恢复。", targets.name), nil
}

func restoreDarwinPatches(targets []darwinTargets) (string, error) {
	var messages []string
	for _, target := range targets {
		message, err := restoreDarwinPatch(target)
		if err != nil {
			return strings.Join(messages, "\n"), fmt.Errorf("%s 恢复失败: %w", target.name, err)
		}
		messages = append(messages, message)
	}
	return strings.Join(messages, "\n") + "\n请完全退出并重启对应的 Antigravity。", nil
}

// signPatchedDarwinLanguageServer deliberately leaves the top-level Electron
// executable untouched. Re-signing the entire app ad-hoc changes its code
// identity and blocks access to Antigravity's existing Keychain credentials,
// which makes the UI appear frozen during startup. The modified nested binary
// still needs its own valid signature to execute under macOS.
func signPatchedDarwinLanguageServer(languagePath string) error {
	if os.Getenv("ANTIGRAVITY_BYOK_SKIP_CODESIGN") == "1" {
		return nil
	}
	args := []string{"--force", "--sign", "-", "--preserve-metadata=entitlements,flags,runtime"}
	if out, err := exec.Command("codesign", append(args, languagePath)...).CombinedOutput(); err != nil {
		return fmt.Errorf("language_server: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("codesign", "--verify", "--strict", languagePath).CombinedOutput(); err != nil {
		return fmt.Errorf("language_server 签名验证: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func writeBackup(sourcePath string, data []byte) error {
	path := backupPath(sourcePath)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// writeCurrentBackup keeps the canonical restore point aligned with the
// currently installed Antigravity version. If an app update changed the
// original file, the older restore point is archived by content hash instead
// of being overwritten or accidentally restored into the new version.
func writeCurrentBackup(sourcePath string, data []byte) error {
	path := backupPath(sourcePath)
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return writeFileAtomic(path, data, 0o600)
	}
	if err != nil {
		return err
	}
	if bytes.Equal(existing, data) {
		return nil
	}
	if err := archivePreviousBackup(path, existing); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func archivePreviousBackup(path string, data []byte) error {
	digest := sha256.Sum256(data)
	history := strings.TrimSuffix(path, ".bak") + fmt.Sprintf(".previous-%x.bak", digest[:8])
	if _, err := os.Stat(history); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeFileAtomic(history, data, 0o600)
}

func writeFileBackup(sourcePath string) error {
	path := backupPath(sourcePath)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return copyFileAtomic(sourcePath, path, 0o600)
	} else if err != nil {
		return err
	}
	sourceDigest, err := fileSHA256(sourcePath)
	if err != nil {
		return err
	}
	backupDigest, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if sourceDigest == backupDigest {
		return nil
	}
	history := strings.TrimSuffix(path, ".bak") + fmt.Sprintf(".previous-%x.bak", backupDigest[:8])
	if _, err := os.Stat(history); os.IsNotExist(err) {
		if err := copyFileAtomic(path, history, 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return copyFileAtomic(sourcePath, path, 0o600)
}

func fileSHA256(path string) ([32]byte, error) {
	var digest [32]byte
	file, err := os.Open(path)
	if err != nil {
		return digest, err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return digest, err
	}
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func backupPath(sourcePath string) string {
	dir := strings.TrimSpace(os.Getenv("ANTIGRAVITY_BYOK_BACKUP_DIR"))
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".antigravity-byok", "backups")
	}
	digest := sha256.Sum256([]byte(filepath.Clean(sourcePath)))
	name := fmt.Sprintf("%s-%x.bak", filepath.Base(sourcePath), digest[:8])
	return filepath.Join(dir, name)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".antigravity-byok-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode.Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func copyFileAtomic(sourcePath, targetPath string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	temp, err := os.CreateTemp(filepath.Dir(targetPath), ".antigravity-byok-copy-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode.Perm()); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := io.Copy(temp, source); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, targetPath)
}
