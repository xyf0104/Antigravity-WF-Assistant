//go:build windows

package patcher

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	winapi "golang.org/x/sys/windows"
)

var windowsOperationMu sync.Mutex

// windowsASARPostReplaceHook is deliberately nil in production. Windows-only
// regression tests use it to force a validation failure after app.asar has
// been replaced, proving that a migration rolls back to the user's immediate
// pre-upgrade state.
var windowsASARPostReplaceHook func()

func runWindows(action string) (message string, err error) {
	windowsOperationMu.Lock()
	defer windowsOperationMu.Unlock()
	// Restore only consumes snapshots captured before the most recent upgrade;
	// it must remain available even when a hand-edited runtime port state is
	// corrupt.
	if action == "status" {
		_ = refreshPatchProxyEndpoint()
	} else if action != "restore" {
		if err := refreshPatchProxyEndpoint(); err != nil {
			return "", err
		}
	}
	targets := locateWindowsInstallations()
	if action == "status" {
		return windowsStatusText(buildWindowsStatus(targets)), nil
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("未找到可支持的 Antigravity 安装（已检查环境变量、LocalAppData、Program Files、运行中进程、注册表和各磁盘常用目录）")
	}
	defer func() {
		if err != nil {
			err = windowsPermissionHint(err)
		}
	}()
	switch action {
	case "apply":
		return applyWindowsTargetsForKind(targets, "")
	case "apply-ide":
		return applyWindowsTargetsForKind(targets, "ide")
	case "apply-agent":
		return applyWindowsTargetsForKind(targets, "agent")
	case "restore":
		return restoreWindowsTargets(targets)
	default:
		return "", fmt.Errorf("未知补丁操作: %s", action)
	}
}

func getWindowsStatus() Status {
	// Status remains available even if a hand-edited runtime state is invalid;
	// apply will surface that error and refuse to write an inconsistent patch.
	_ = refreshPatchProxyEndpoint()
	return buildWindowsStatus(locateWindowsInstallations())
}

func buildWindowsStatus(targets []windowsTarget) Status {
	status := Status{ProxyListening: windowsProxyListening()}
	agentInstalled, ideInstalled := false, false
	agentPatched, idePatched, ideMainPatched := true, true, true
	for _, target := range targets {
		supported, mode, reason := windowsTargetConnectionSupport(target)
		mainPatched, _, _, patched := windowsTargetPatchState(target)
		if target.kind == "ide" && target.version != "" {
			patched = supported && windowsCloudCodeSettingIsConfigured(windowsSettingsPathForStatus(target), windowsBaseProxyEndpoint)
			mainPatched = patched
		}
		status.Targets = append(status.Targets, TargetStatus{
			Name: target.name, Kind: target.kind, Version: target.version, AppPath: target.root,
			ExecutablePath: target.executable,
			MainPath:       target.main, ASARPath: target.asar, ExtensionPath: target.extensionEntry,
			LanguageServerPath: target.language, Supported: supported, ConnectionMode: mode, Reason: reason, Patched: patched,
		})
		if status.AsarPath == "" {
			status.AsarPath = firstWindowsValue(target.asar, target.main)
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
		status.AgentPatched = windowsBoolPointer(agentPatched)
	}
	if ideInstalled {
		status.IDEPatched = windowsBoolPointer(idePatched)
		status.IdeMainPatched = windowsBoolPointer(ideMainPatched)
	}
	if !agentInstalled && ideInstalled {
		status.AgentPatched = windowsBoolPointer(idePatched)
	}
	if !ideInstalled && agentInstalled {
		status.IDEPatched = windowsBoolPointer(agentPatched)
		status.IdeMainPatched = windowsBoolPointer(agentPatched)
	}
	return status
}

func windowsStatusText(status Status) string {
	return fmt.Sprintf(
		"agent_patched=%s\nide_patched=%s\nide_main_patched=%s\nproxy_listening=%t\nasar=%s\nlanguage_server=%s\nide_extension=%s\nide_language_server=%s\n",
		windowsBoolText(status.AgentPatched), windowsBoolText(status.IDEPatched), windowsBoolText(status.IdeMainPatched),
		status.ProxyListening, status.AsarPath, status.LSPath, status.IDEExtensionPath, status.IDELSPath,
	)
}

func windowsBoolPointer(value bool) *bool { return &value }

func windowsBoolText(value *bool) string {
	if value == nil {
		return "None"
	}
	if *value {
		return "true"
	}
	return "false"
}

func firstWindowsValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func applyWindowsTargets(targets []windowsTarget, onlyIDE bool) (string, error) {
	if onlyIDE {
		return applyWindowsTargetsForKind(targets, "ide")
	}
	return applyWindowsTargetsForKind(targets, "")
}

func applyWindowsTargetsForKind(targets []windowsTarget, targetKind string) (string, error) {
	var selected []windowsTarget
	for _, target := range targets {
		if targetKind == "" || target.kind == targetKind {
			selected = append(selected, target)
		}
	}
	if len(selected) == 0 {
		switch targetKind {
		case "ide":
			return "", fmt.Errorf("未找到独立 IDE 类型的 Antigravity 安装")
		case "agent":
			return "", fmt.Errorf("未找到 Antigravity Agent / 2.x 类型的安装")
		default:
			return "", fmt.Errorf("未找到 Antigravity 安装")
		}
	}

	// A computer can have an official IDE and a newer Agent/2.x build at the
	// same time. Only act on targets whose official user-setting chain was
	// verified. An unsupported target must never prevent a compatible IDE from
	// connecting, and it must never be treated as a reason to write its bundle.
	compatible := make([]windowsTarget, 0, len(selected))
	messages := make([]string, 0, len(selected))
	for _, target := range selected {
		supported, _, reason := windowsTargetConnectionSupport(target)
		if !supported {
			messages = append(messages, fmt.Sprintf("%s 未连接：%s", target.name, reason))
			continue
		}
		compatible = append(compatible, target)
	}
	if len(compatible) == 0 {
		return strings.Join(messages, "\n\n"), fmt.Errorf("未找到可安全连接的 Antigravity 安装；助手没有修改任何安装文件")
	}
	if windowsTargetsRunning(compatible) {
		return strings.Join(messages, "\n\n"), fmt.Errorf("请先在 Antigravity 中保存内容并完全退出，再连接本地代理；为保护安装完整性，助手不会强制结束进程")
	}
	for _, target := range compatible {
		message, err := applyWindowsTarget(target)
		if err != nil {
			return strings.Join(messages, "\n\n"), fmt.Errorf("%s 连接失败: %w", target.name, err)
		}
		messages = append(messages, message)
	}
	return strings.Join(messages, "\n\n") + "\n\n请保持本工具运行，然后重新打开对应的 Antigravity。", nil
}

func windowsTargetsNeedHistoryMerge(targets []windowsTarget) bool {
	for _, target := range targets {
		if target.kind == "ide" {
			return true
		}
	}
	return false
}

func applyWindowsTarget(target windowsTarget) (message string, err error) {
	if target.kind == "agent" {
		return applyWindowsASARTarget(target)
	}
	settingsPath, _, supportErr := windowsTargetUserSettingsPath(target)
	if supportErr != nil {
		return "", supportErr
	}
	settingPlan, settingChanged, err := prepareWindowsEnsureCloudCodeSetting(settingsPath, windowsBaseProxyEndpoint)
	if err != nil {
		return "", err
	}

	rendererPaths := windowsImageGenerationUIRendererPaths(target)
	if len(rendererPaths) == 0 {
		return "", fmt.Errorf("未找到已验证的 IDE 图片界面 renderer；未修改任何文件")
	}
	rendererPlans := make([]*windowsPatchPlan, 0, len(rendererPaths))
	verifiedRenderers := make([]string, 0, len(rendererPaths))
	for _, rendererPath := range rendererPaths {
		rendererSource, sourceErr := windowsPatchSource(rendererPath)
		if sourceErr != nil {
			return "", sourceErr
		}
		plan, planErr := prepareWindowsImagePreviewPatch(rendererSource)
		if planErr != nil {
			return "", planErr
		}
		plan.path = rendererPath
		if !bytes.Contains(plan.updated, []byte(imageGenerationUIPatchMarker)) ||
			!bytes.Contains(plan.updated, []byte(imageGenerationDedupePatchMarker)) {
			continue
		}
		rendererPlans = append(rendererPlans, plan)
		verifiedRenderers = append(verifiedRenderers, rendererPath)
	}
	if len(verifiedRenderers) == 0 {
		return "", fmt.Errorf("当前 IDE 图片标题结构尚未通过安全匹配；未修改任何文件")
	}
	productPlan, productErr := prepareWindowsIDEProductChecksumPatch(target, rendererPlans)
	if productErr != nil {
		return "", productErr
	}
	backupPlans := append([]*windowsPatchPlan{}, rendererPlans...)
	if productPlan != nil {
		backupPlans = append(backupPlans, productPlan)
	}
	if settingChanged {
		// The user may already have an older WF or third-party endpoint. Keep an
		// exact persistent snapshot before replacing it so Restore returns to the
		// state immediately preceding this upgrade instead of deleting it.
		backupPlans = append(backupPlans, settingPlan)
	}
	if err := saveWindowsPlanBackups(backupPlans); err != nil {
		return "", fmt.Errorf("创建图片界面备份失败: %w", err)
	}
	plans := append([]*windowsPatchPlan{}, backupPlans...)
	snapshots, snapshotErr := windowsRollbackSnapshots(plans)
	if snapshotErr != nil {
		return "", fmt.Errorf("创建事务回滚快照失败: %w", snapshotErr)
	}
	defer func() {
		if err == nil {
			return
		}
		if rollbackErr := restoreWindowsRollbackSnapshots(snapshots); rollbackErr != nil {
			err = fmt.Errorf("%w；回滚到操作前状态失败: %v", err, rollbackErr)
		}
	}()
	if err = writeWindowsPlans(plans); err != nil {
		return "", err
	}
	if !windowsCloudCodeSettingIsConfigured(settingsPath, windowsBaseProxyEndpoint) {
		return "", fmt.Errorf("用户级代理设置写入后未通过校验")
	}
	for _, rendererPath := range verifiedRenderers {
		data, readErr := os.ReadFile(rendererPath)
		if readErr != nil || !bytes.Contains(data, []byte(imageGenerationUIPatchMarker)) {
			return "", fmt.Errorf("图片界面补丁写入后未通过校验: %s", rendererPath)
		}
	}
	if err = verifyWindowsIDEProductChecksums(target, verifiedRenderers); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"%s 已安全连接本地代理并启用实际生图模型标题。\n设置文件: %s\n已验证图片 renderer: %d 个\nIDE 完整性 checksum 已同步。\n未修改主进程、扩展或 Language Server。",
		target.name, settingsPath, len(verifiedRenderers),
	), nil
}

func windowsSettingsPathForStatus(target windowsTarget) string {
	path, _, err := windowsTargetUserSettingsPath(target)
	if err != nil {
		return ""
	}
	return path
}

// applyWindowsLegacyTarget is retained only for isolated regression fixtures
// covering historical backup formats. Production flows must never call it:
// newer Antigravity builds verify their bundled files at startup.
func applyWindowsLegacyTarget(target windowsTarget) (message string, err error) {
	if target.executable == "" {
		return "", fmt.Errorf("%s 安装不完整，缺少主程序", target.root)
	}
	if target.kind == "agent" {
		return applyWindowsASARTarget(target)
	}
	if target.main == "" {
		return "", fmt.Errorf("%s 中未找到主进程脚本", target.root)
	}

	mainSource, err := windowsPatchSource(target.main)
	if err != nil {
		return "", err
	}
	mainPlan, err := prepareWindowsMainPatch(mainSource)
	if err != nil {
		return "", err
	}
	mainPlan.path = target.main
	plans := []*windowsPatchPlan{mainPlan}
	if target.extensionEntry != "" {
		extensionSource, err := windowsPatchSource(target.extensionEntry)
		if err != nil {
			return "", err
		}
		extensionPlan, err := prepareWindowsExtensionPatch(extensionSource)
		if err != nil {
			return "", err
		}
		extensionPlan.path = target.extensionEntry
		plans = append(plans, extensionPlan)
	}
	languageSource, err := windowsPatchSource(target.language)
	if err != nil {
		return "", err
	}
	languagePlan, embedded, err := prepareWindowsLanguagePatch(languageSource)
	if err != nil {
		return "", err
	}
	if languagePlan != nil {
		languagePlan.path = target.language
		plans = append(plans, languagePlan)
	}
	for _, rendererPath := range windowsImagePreviewRendererPaths(target) {
		if rendererPath == target.main {
			// prepareWindowsMainPatch already covers out/main.js. The other
			// renderer bundles receive a narrowly scoped compatibility plan.
			continue
		}
		rendererSource, err := windowsPatchSource(rendererPath)
		if err != nil {
			return "", err
		}
		rendererPlan, err := prepareWindowsImagePreviewPatch(rendererSource)
		if err != nil {
			return "", err
		}
		rendererPlan.path = rendererPath
		plans = append(plans, rendererPlan)
	}
	if !windowsPlansChanged(plans) {
		return fmt.Sprintf("%s 补丁已处于激活状态，无需重复应用。", target.name), nil
	}
	if err := saveWindowsPlanBackups(plans); err != nil {
		return "", fmt.Errorf("创建补丁备份失败: %w", err)
	}
	rollbackSnapshots, snapshotErr := windowsRollbackSnapshots(plans)
	if snapshotErr != nil {
		return "", fmt.Errorf("创建事务回滚快照失败: %w", snapshotErr)
	}
	defer func() {
		if err != nil {
			if rollbackErr := restoreWindowsRollbackSnapshots(rollbackSnapshots); rollbackErr != nil {
				err = fmt.Errorf("%w；回滚到操作前状态失败: %v", err, rollbackErr)
			}
		}
	}()
	if err = writeWindowsPlans(plans); err != nil {
		return "", err
	}
	if _, _, _, patched := windowsTargetPatchState(target); !patched {
		return "", fmt.Errorf("写入后的 Windows IDE 补丁未通过完整校验")
	}
	warning := windowsLanguageWarning(target.language, embedded)
	return fmt.Sprintf(
		"%s 补丁应用成功。\n应用: %s\n主进程: %s\n扩展: %s\n语言服务器: %s%s",
		target.name, target.root, target.main, target.extensionEntry, windowsOptionalPath(target.language), warning,
	), nil
}

func applyWindowsASARTarget(target windowsTarget) (message string, err error) {
	if target.asar == "" {
		return "", fmt.Errorf("%s 中未找到 app.asar", target.root)
	}
	asarSource, err := windowsPatchSource(target.asar)
	if err != nil {
		return "", err
	}
	// Rebuild the archive only when its own endpoint or packed renderer needs
	// work. Unpacked renderer entries are patched as separate files so their
	// ASAR manifest semantics remain unchanged.
	asarChanged := !windowsASARPatched(target.asar) || asarSource != target.asar || imagePreviewASARArchiveNeedsPatch(target.asar)
	var candidate string
	if asarChanged {
		candidate, err = prepareWindowsASARCandidate(asarSource, target.asar)
		if err != nil {
			return "", err
		}
		defer os.Remove(candidate)
	}
	languageSource, err := windowsPatchSource(target.language)
	if err != nil {
		return "", err
	}
	languagePlan, embedded, err := prepareWindowsLanguagePatch(languageSource)
	if err != nil {
		return "", err
	}
	languagePlan, agentUIEmbedded, err := prepareWindowsAgentEmbeddedUIPlan(languagePlan)
	if err != nil {
		return "", err
	}
	embedded = embedded || agentUIEmbedded
	if languagePlan != nil {
		languagePlan.path = target.language
	}
	previewPlans := make([]*windowsPatchPlan, 0)
	for _, rendererPath := range windowsASARUnpackedImagePreviewRendererPaths(target) {
		rendererSource, sourceErr := windowsPatchSource(rendererPath)
		if sourceErr != nil {
			return "", sourceErr
		}
		plan, planErr := prepareWindowsImagePreviewPatch(rendererSource)
		if planErr != nil {
			return "", planErr
		}
		plan.path = rendererPath
		previewPlans = append(previewPlans, plan)
	}
	if !asarChanged && (languagePlan == nil || !languagePlan.changed) && !windowsPlansChanged(previewPlans) {
		return fmt.Sprintf("%s 补丁已处于激活状态，无需重复应用。", target.name), nil
	}
	if asarChanged {
		if err = saveWindowsBackupFrom(target.asar, asarSource); err != nil {
			return "", fmt.Errorf("创建 app.asar 备份失败: %w", err)
		}
	}
	plans := append([]*windowsPatchPlan{}, previewPlans...)
	if languagePlan != nil {
		plans = append(plans, languagePlan)
	}
	if err = saveWindowsPlanBackups(plans); err != nil {
		return "", fmt.Errorf("创建补丁备份失败: %w", err)
	}
	rollbackExtraPaths := make([]string, 0, 1)
	if asarChanged {
		rollbackExtraPaths = append(rollbackExtraPaths, target.asar)
	}
	rollbackSnapshots, snapshotErr := windowsRollbackSnapshots(plans, rollbackExtraPaths...)
	if snapshotErr != nil {
		return "", fmt.Errorf("创建事务回滚快照失败: %w", snapshotErr)
	}
	defer func() {
		if err == nil {
			return
		}
		if rollbackErr := restoreWindowsRollbackSnapshots(rollbackSnapshots); rollbackErr != nil {
			err = fmt.Errorf("%w；回滚到操作前状态失败: %v", err, rollbackErr)
		}
	}()
	if err = writeWindowsPlans(plans); err != nil {
		return "", err
	}
	if asarChanged {
		if err = windowsReplaceFile(candidate, target.asar); err != nil {
			return "", fmt.Errorf("替换 app.asar 失败: %w", err)
		}
		if windowsASARPostReplaceHook != nil {
			windowsASARPostReplaceHook()
		}
	}
	if _, _, _, patched := windowsTargetPatchState(target); !patched {
		return "", fmt.Errorf("写入后的 Windows app.asar 补丁未通过完整校验")
	}
	warning := windowsLanguageWarning(target.language, embedded)
	return fmt.Sprintf(
		"%s 补丁应用成功。\n应用: %s\nASAR: %s\n语言服务器: %s%s",
		target.name, target.root, target.asar, windowsOptionalPath(target.language), warning,
	), nil
}

func windowsPatchSource(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if _, err := os.ReadFile(path); err != nil {
		return "", err
	}
	// Always upgrade the bytes that are active on this computer. They can be an
	// official file, an old WF build, or a third-party modification. Every
	// changed plan is persisted by saveWindowsPlanBackups before the first
	// installed byte is replaced, so a failed transaction and the public
	// Restore action both return to this exact pre-upgrade state. Structural
	// patchers still have to recognise their required insertion points; this
	// does not copy a foreign whole-file fixture over an unknown version.
	return path, nil
}

func windowsPlansChanged(plans []*windowsPatchPlan) bool {
	for _, plan := range plans {
		if plan != nil && plan.changed {
			return true
		}
	}
	return false
}

// windowsRollbackSnapshots captures the actual active files immediately
// before an apply operation starts writing. Persistent backups provide a
// user-requested pre-upgrade restore point; these in-memory snapshots keep a
// partially failed multi-file transaction from exposing mixed structures.
func windowsRollbackSnapshots(plans []*windowsPatchPlan, extraPaths ...string) ([]windowsRollbackSnapshot, error) {
	paths := append([]string(nil), extraPaths...)
	for _, plan := range plans {
		if plan != nil && plan.changed {
			paths = append(paths, plan.path)
		}
	}
	seen := make(map[string]bool, len(paths))
	snapshots := make([]windowsRollbackSnapshot, 0, len(paths))
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			snapshots = append(snapshots, windowsRollbackSnapshot{path: path, existed: false})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("读取 %s: %w", path, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("检查 %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s 不是常规文件", path)
		}
		snapshots = append(snapshots, windowsRollbackSnapshot{
			path: path, data: data, mode: info.Mode(), existed: true,
		})
	}
	return snapshots, nil
}

type windowsRollbackSnapshot struct {
	path    string
	data    []byte
	mode    os.FileMode
	existed bool
}

func restoreWindowsRollbackSnapshots(snapshots []windowsRollbackSnapshot) error {
	// Restore in reverse write order. The files are independent today, but the
	// ordering keeps the helper correct if a future target introduces a loader
	// that observes a companion file while app.asar is being restored.
	for index := len(snapshots) - 1; index >= 0; index-- {
		snapshot := snapshots[index]
		if !snapshot.existed {
			if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("移除本次新建的 %s: %w", snapshot.path, err)
			}
			continue
		}
		if err := windowsWriteFileAtomic(snapshot.path, snapshot.data, snapshot.mode); err != nil {
			return fmt.Errorf("恢复 %s: %w", snapshot.path, err)
		}
	}
	return nil
}

func saveWindowsPlanBackups(plans []*windowsPatchPlan) error {
	for _, plan := range plans {
		if plan == nil || !plan.changed {
			continue
		}
		if err := saveWindowsBackup(plan.path, plan.original); err != nil {
			return err
		}
	}
	return nil
}

func saveWindowsBackupFrom(targetPath, sourcePath string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	return saveWindowsBackup(targetPath, data)
}

func saveWindowsBackup(sourcePath string, data []byte) error {
	path := windowsBackupPath(sourcePath)
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return windowsWriteFileAtomic(path, data, 0o600)
	}
	if err != nil {
		return err
	}
	if bytes.Equal(existing, data) {
		return nil
	}
	digest := sha256.Sum256(existing)
	history := strings.TrimSuffix(path, ".bak") + fmt.Sprintf(".previous-%x.bak", digest[:8])
	if windowsExistingFile(history) == "" {
		if err := windowsWriteFileAtomic(history, existing, 0o600); err != nil {
			return err
		}
	}
	return windowsWriteFileAtomic(path, data, 0o600)
}

func writeWindowsPlans(plans []*windowsPatchPlan) error {
	for _, plan := range plans {
		if plan == nil || !plan.changed {
			continue
		}
		if err := windowsWriteFileAtomic(plan.path, plan.updated, plan.mode); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", plan.path, err)
		}
	}
	return nil
}

func restoreWindowsTargets(targets []windowsTarget) (string, error) {
	if len(targets) == 0 {
		return "未找到 Antigravity 安装；未修改任何文件。", nil
	}
	if windowsTargetsRunning(targets) {
		return "", fmt.Errorf("请先正常关闭所有 Antigravity 窗口，再恢复升级前状态；助手不会强制结束进程")
	}

	var plans []*windowsPatchPlan
	var messages []string
	seen := map[string]bool{}
	for _, target := range targets {
		targetRestored := 0
		var targetPlans []*windowsPatchPlan
		if target.kind == "ide" {
			if settingsPath := windowsSettingsPathForStatus(target); settingsPath != "" {
				plan, ok, restoreErr := prepareWindowsRestorePlan(settingsPath)
				if restoreErr != nil {
					return strings.Join(messages, "\n"), fmt.Errorf("%s 恢复用户设置失败: %w", target.name, restoreErr)
				}
				if !ok {
					plan, ok, restoreErr = prepareWindowsRemoveCloudCodeSetting(settingsPath, windowsBaseProxyEndpoint)
					if restoreErr != nil {
						return strings.Join(messages, "\n"), fmt.Errorf("%s 恢复用户设置失败: %w", target.name, restoreErr)
					}
				}
				if ok {
					plans = append(plans, plan)
					targetRestored++
				}
				seen[settingsPath] = true
			}
		}

		paths := []string{target.main, target.extensionEntry, target.asar, target.language}
		paths = append(paths, windowsImagePreviewRendererPaths(target)...)
		paths = append(paths, windowsASARUnpackedImagePreviewRendererPaths(target)...)
		for _, path := range paths {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			plan, ok, err := prepareWindowsRestorePlan(path)
			if err != nil {
				return strings.Join(messages, "\n"), fmt.Errorf("%s 恢复失败: %w", target.name, err)
			}
			if ok {
				plans = append(plans, plan)
				targetPlans = append(targetPlans, plan)
				targetRestored++
			}
		}
		if target.kind == "ide" {
			productPlan, productErr := prepareWindowsIDEProductChecksumPatch(target, targetPlans)
			if productErr != nil {
				return strings.Join(messages, "\n"), fmt.Errorf("%s 恢复完整性 checksum 失败: %w", target.name, productErr)
			}
			if productPlan != nil {
				plans = append(plans, productPlan)
				targetRestored++
			}
		}
		if targetRestored == 0 {
			messages = append(messages, fmt.Sprintf("%s 未发现可恢复的升级前状态。", target.name))
		} else {
			messages = append(messages, fmt.Sprintf("%s 将恢复 %d 项升级前配置。", target.name, targetRestored))
		}
	}
	if len(plans) == 0 {
		return strings.Join(messages, "\n") + "\n未修改任何用户文件或安装文件。", nil
	}
	snapshots, err := windowsRollbackSnapshots(plans)
	if err != nil {
		return strings.Join(messages, "\n"), err
	}
	if err := writeWindowsPlans(plans); err != nil {
		if rollbackErr := restoreWindowsRollbackSnapshots(snapshots); rollbackErr != nil {
			return strings.Join(messages, "\n"), fmt.Errorf("%w；回滚失败: %v", err, rollbackErr)
		}
		return strings.Join(messages, "\n"), err
	}
	for _, target := range targets {
		if target.kind == "ide" {
			if err := verifyWindowsIDEProductChecksums(target, windowsImageGenerationUIRendererPaths(target)); err != nil {
				_ = restoreWindowsRollbackSnapshots(snapshots)
				return strings.Join(messages, "\n"), err
			}
		}
	}
	return strings.Join(messages, "\n") + "\n已恢复到本次升级前状态。未删除其他用户设置、AppData、.gemini 或聊天数据。", nil
}

// restoreWindowsLegacyTargets exists solely for historical regression fixtures.
// Production code must call restoreWindowsTargets, which intentionally never
// writes an executable, bundle, ASAR, extension, renderer, or language server.
func restoreWindowsLegacyTargets(targets []windowsTarget) (string, error) {
	var messages []string
	for _, target := range targets {
		var plans []*windowsPatchPlan
		paths := []string{target.main, target.extensionEntry, target.asar, target.language}
		paths = append(paths, windowsImagePreviewRendererPaths(target)...)
		paths = append(paths, windowsASARUnpackedImagePreviewRendererPaths(target)...)
		seen := map[string]bool{}
		for _, path := range paths {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			plan, ok, err := prepareWindowsRestorePlan(path)
			if err != nil {
				return strings.Join(messages, "\n"), fmt.Errorf("%s 恢复失败: %w", target.name, err)
			}
			if ok {
				plans = append(plans, plan)
			}
		}
		if len(plans) == 0 {
			messages = append(messages, fmt.Sprintf("%s 未发现可恢复的升级前备份。", target.name))
		} else {
			if err := writeWindowsPlans(plans); err != nil {
				return strings.Join(messages, "\n"), fmt.Errorf("%s 恢复失败: %w", target.name, err)
			}
			messages = append(messages, fmt.Sprintf("%s 已恢复 %d 个升级前文件。", target.name, len(plans)))
		}
	}
	return strings.Join(messages, "\n") + "\n请重新打开对应的 Antigravity。", nil
}

func restoreWindowsFile(path string) error {
	ok, err := restoreWindowsFileIfAvailable(path)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("没有找到升级前备份: %s", path)
	}
	return nil
}

func restoreWindowsFileIfAvailable(path string) (bool, error) {
	plan, ok, err := prepareWindowsRestorePlan(path)
	if err != nil || !ok {
		return ok, err
	}
	return true, windowsWriteFileAtomic(plan.path, plan.updated, plan.mode)
}

func prepareWindowsRestorePlan(path string) (*windowsPatchPlan, bool, error) {
	active, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	// A clean active file commonly means Antigravity was upgraded or repaired.
	// Never write an older helper backup over that newer official file.
	if !windowsContainsKnownPatch(active) {
		return nil, false, nil
	}
	backup := windowsBackupPath(path)
	if windowsExistingFile(backup) == "" {
		for _, legacy := range windowsLegacyBackupPaths(path) {
			if windowsExistingFile(legacy) != "" {
				backup = legacy
				break
			}
		}
	}
	if windowsExistingFile(backup) == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return nil, false, err
	}
	// A backup intentionally represents the exact state immediately before the
	// latest WF upgrade. It may therefore contain an older WF marker or a
	// third-party modification; restoring those bytes is the requested action.
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if bytes.Equal(active, data) {
		return nil, false, nil
	}
	return &windowsPatchPlan{path: path, original: active, updated: data, mode: info.Mode(), changed: true}, true, nil
}

func windowsWriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".antigravity-byok-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode.Perm()); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
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
	return windowsReplaceFile(tempPath, path)
}

func windowsReplaceFile(source, target string) error {
	from, err := winapi.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := winapi.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return winapi.MoveFileEx(from, to, winapi.MOVEFILE_REPLACE_EXISTING|winapi.MOVEFILE_WRITE_THROUGH)
}

func windowsProxyListening() bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", currentPatchProxyEndpoint().Port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func stopWindowsProducts(targets []windowsTarget) {
	names := map[string]bool{}
	for _, target := range targets {
		if target.kind == "agent" {
			names["antigravity.exe"] = true
		} else {
			names["antigravity ide.exe"] = true
		}
		for _, path := range []string{target.executable, target.language} {
			if path != "" {
				names[strings.ToLower(filepath.Base(path))] = true
			}
		}
	}
	var sorted []string
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		cmd := exec.Command("taskkill.exe", "/F", "/T", "/IM", name)
		configureCommand(cmd)
		_ = cmd.Run()
	}
	time.Sleep(700 * time.Millisecond)
}

func windowsTargetsRunning(targets []windowsTarget) bool {
	names := map[string]bool{}
	for _, target := range targets {
		if target.executable != "" {
			names[strings.ToLower(filepath.Base(target.executable))] = true
		}
	}
	for name := range names {
		cmd := exec.Command("tasklist.exe", "/FI", "IMAGENAME eq "+name, "/FO", "CSV", "/NH")
		configureCommand(cmd)
		output, err := cmd.Output()
		if err == nil && strings.Contains(strings.ToLower(string(output)), name) {
			return true
		}
	}
	return false
}

func windowsLanguageWarning(path string, embedded bool) string {
	if path == "" {
		return "\n提示：该版本没有独立 Language Server，已通过入口脚本传递代理地址。"
	}
	if !embedded {
		return "\n提示：该 Language Server 不再内置固定地址，已跳过二进制替换并通过入口脚本传递代理地址。"
	}
	return ""
}

func windowsOptionalPath(path string) string {
	if path == "" {
		return "（此版本无独立文件）"
	}
	return path
}

func windowsPermissionHint(err error) error {
	if errors.Is(err, os.ErrPermission) || errors.Is(err, winapi.ERROR_ACCESS_DENIED) ||
		strings.Contains(strings.ToLower(err.Error()), "access is denied") {
		return fmt.Errorf("%w；安装目录需要管理员权限，请右键 Antigravity WF助手并选择“以管理员身份运行”", err)
	}
	return err
}

func mergeWindowsHistory() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return mergeWindowsHistoryAt(home)
}

func mergeWindowsHistoryOnStartup() error {
	windowsOperationMu.Lock()
	defer windowsOperationMu.Unlock()
	return mergeWindowsHistory()
}

func mergeWindowsHistoryAt(home string) error {
	geminiRoot := filepath.Join(home, ".gemini")
	target := filepath.Join(geminiRoot, "antigravity")
	resources := []string{
		"annotations", "brain", "browser_recordings", "context_state", "conversations", "html_artifacts",
		"implicit", "knowledge", "playground", "plugins", "prompting", "scratch",
	}
	entries, err := os.ReadDir(geminiRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var sources []string
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if !entry.IsDir() || !strings.HasPrefix(name, "antigravity") ||
			name == "antigravity" || strings.Contains(name, "antigravity-byok-backup") {
			continue
		}
		source := filepath.Join(geminiRoot, entry.Name())
		for _, resource := range resources {
			if info, statErr := os.Stat(filepath.Join(source, resource)); statErr == nil && info.IsDir() {
				sources = append(sources, source)
				break
			}
		}
	}
	if len(sources) == 0 {
		return nil
	}
	sort.Strings(sources)
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	for _, source := range sources {
		backup := source + ".antigravity-byok-backup"
		if _, statErr := os.Stat(backup); os.IsNotExist(statErr) {
			if err := copyWindowsTreeMissing(source, backup); err != nil {
				return err
			}
		}
		for _, resource := range resources {
			if err := copyWindowsTreeMissing(filepath.Join(source, resource), filepath.Join(target, resource)); err != nil {
				return err
			}
		}
		config := filepath.Join(source, "mcp_config.json")
		if windowsExistingFile(config) != "" && windowsExistingFile(filepath.Join(target, "mcp_config.json")) == "" {
			if err := copyWindowsFile(config, filepath.Join(target, "mcp_config.json")); err != nil {
				return err
			}
		}
	}
	return nil
}

func mergeWindowsHistorySource(source, target string, resources []string) error {
	for _, resource := range resources {
		if err := copyWindowsTreeMissing(filepath.Join(source, resource), filepath.Join(target, resource)); err != nil {
			return err
		}
	}
	config := filepath.Join(source, "mcp_config.json")
	if windowsExistingFile(config) != "" && windowsExistingFile(filepath.Join(target, "mcp_config.json")) == "" {
		return copyWindowsFile(config, filepath.Join(target, "mcp_config.json"))
	}
	return nil
}

func copyWindowsTreeMissing(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	var files []string
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(files)
	for _, path := range files {
		relative, _ := filepath.Rel(source, path)
		destination := filepath.Join(target, relative)
		if windowsExistingFile(destination) == "" {
			if err := copyWindowsFile(path, destination); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyWindowsFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".antigravity-byok-copy-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := io.Copy(temp, input); err != nil {
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
	return windowsReplaceFile(tempPath, target)
}
