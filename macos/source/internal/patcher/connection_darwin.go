//go:build darwin

package patcher

// This is the v1.5.2 macOS connection path.  The legacy byte-patching
// helpers remain below in patcher_darwin.go only for historical restore and
// isolated regression fixtures; production IDE connections use the official
// user-level jetski.cloudCodeUrl setting verified in the installed bundle.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// darwinProcessList is a test seam. A process-list failure is treated as a
// stop condition before a bundle write rather than risking a live Electron
// update while its renderer is mapped in memory.
var darwinProcessList = func() ([]byte, error) {
	return exec.Command("ps", "-axo", "command=").Output()
}

func darwinTargetConnectionSupport(target darwinTargets) (bool, string, string) {
	if target.kind == "ide" {
		if _, _, err := darwinTargetUserSettingsPath(target); err != nil {
			return false, "", err.Error()
		}
		return true, "user-settings", ""
	}
	if target.kind == "agent" {
		if target.asar == "" || target.language == "" || existingFile(target.asar) == "" || existingFile(target.language) == "" {
			return false, "", "该 Antigravity 2.0 安装缺少可验证的 app.asar 或 Language Server；未修改任何文件"
		}
		if !darwinASARHasSupportedEntrypoints(target.asar) {
			return false, "", "该 Antigravity 2.0 启动结构尚未验证；未修改任何文件"
		}
		if err := verifyDarwinAgentASARIntegrity(target); err != nil {
			canonicalBackup := backupPath(target.asar)
			legacyUpgradeSafe := darwinASARContainsKnownPatch(target.asar) && existingFile(canonicalBackup) != "" &&
				verifyDarwinAgentASARIntegrityAgainst(target, canonicalBackup) == nil
			if !legacyUpgradeSafe {
				return false, "", fmt.Sprintf("该 Antigravity 2.0 的 app.asar 完整性元数据未通过验证：%v；未修改任何文件", err)
			}
		}
		archiveRecognized, err := validateDarwinAgentEmbeddedUISource(target.language)
		if err != nil {
			return false, "", err.Error()
		}
		if !archiveRecognized {
			return false, "", "该 Antigravity 2.0 图片界面结构尚未验证；未修改任何文件"
		}
		return true, "asar-language-server", ""
	}
	return false, "", "未识别的 Antigravity 安装类型"
}

func darwinSettingsPathForStatus(target darwinTargets) string {
	path, _, err := darwinTargetUserSettingsPath(target)
	if err != nil {
		return ""
	}
	return path
}

func darwinTargetsRunning(targets []darwinTargets) (bool, error) {
	output, err := darwinProcessList()
	if err != nil {
		return false, fmt.Errorf("检查 Antigravity 运行状态失败: %w", err)
	}
	commands := string(output)
	for _, target := range targets {
		path := filepath.Clean(strings.TrimSpace(target.app))
		if path != "" && strings.Contains(commands, path+"/Contents/") {
			return true, nil
		}
	}
	return false, nil
}

func applyDarwinTargetsForKind(targets []darwinTargets, kind string) (string, error) {
	selected := make([]darwinTargets, 0, len(targets))
	for _, target := range targets {
		if kind == "" || target.kind == kind {
			selected = append(selected, target)
		}
	}
	if len(selected) == 0 {
		switch kind {
		case "ide":
			return "", fmt.Errorf("未找到独立 IDE 类型的 Antigravity 安装")
		case "agent":
			return "", fmt.Errorf("未找到 Antigravity 2.0 类型的安装")
		default:
			return "", fmt.Errorf("未找到 Antigravity 安装")
		}
	}

	compatible := make([]darwinTargets, 0, len(selected))
	messages := make([]string, 0, len(selected))
	for _, target := range selected {
		supported, _, reason := darwinTargetConnectionSupport(target)
		if !supported {
			messages = append(messages, fmt.Sprintf("%s 未连接：%s", target.name, reason))
			continue
		}
		compatible = append(compatible, target)
	}
	if len(compatible) == 0 {
		return strings.Join(messages, "\n\n"), fmt.Errorf("未找到可安全连接的 Antigravity 安装；助手没有修改任何安装文件")
	}
	running, err := darwinTargetsRunning(compatible)
	if err != nil {
		return strings.Join(messages, "\n\n"), err
	}
	if running {
		return strings.Join(messages, "\n\n"), fmt.Errorf("请先在 Antigravity 中保存内容并完全退出，再连接本地代理；为保护安装完整性，助手不会强制结束进程")
	}
	for _, target := range compatible {
		var message string
		if target.kind == "ide" {
			message, err = applyDarwinSafeIDETarget(target)
		} else {
			// The Agent/2.0 package has no proven user-setting chain. Its existing
			// ASAR path remains gated by exact archive entry validation and keeps
			// its per-file rollback/signing transaction.
			message, err = applyDarwinASARPatch(target)
		}
		if err != nil {
			return strings.Join(messages, "\n\n"), fmt.Errorf("%s 连接失败: %w", target.name, err)
		}
		messages = append(messages, message)
	}
	return strings.Join(messages, "\n\n") + "\n\n请保持本工具运行，然后重新打开对应的 Antigravity。", nil
}

// darwinImageGenerationUIRendererPaths limits the cosmetic patch to the two
// renderer bundles that own the chat image UI. In particular, out/main.js is
// deliberately excluded: it is the Electron main process and no longer needs
// any endpoint rewrite under the user-setting connection strategy.
func darwinImageGenerationUIRendererPaths(target darwinTargets) []string {
	if target.kind != "ide" || target.app == "" {
		return nil
	}
	appRoot := filepath.Join(target.app, "Contents", "Resources", "app")
	paths := make([]string, 0, 2)
	for _, relative := range []string{
		"out/jetskiAgent/main.js",
		"out/vs/workbench/workbench.desktop.main.js",
	} {
		path := filepath.Join(appRoot, filepath.FromSlash(relative))
		if existingFile(path) != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func darwinImageRendererReady(data []byte) bool {
	// The UI marker is written only by strict, whole-component matchers. The
	// fallback marker proves the media resolver was paired with that UI block;
	// a random title string is never considered a supported renderer.
	return bytes.Contains(data, []byte(imagePreviewPatchMarker)) &&
		bytes.Contains(data, []byte(imageGenerationUIPatchMarker))
}

func prepareDarwinSafeImageRendererPlan(path string) (*patchPlan, bool, error) {
	active, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	_, activeResult := patchImagePreviewRenderer(string(active))
	if !activeResult.Changed && darwinImageRendererReady(active) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, false, statErr
		}
		return &patchPlan{path: path, original: active, updated: active, mode: info.Mode()}, true, nil
	}

	// An upgrade from a helper-authored renderer is reconstructed from the
	// canonical clean backup. If that backup is absent, writing on top of an
	// unknown/old patch could make Restore permanently lose vendor bytes.
	source := path
	if containsKnownDarwinPatch(active) {
		backup := matchingDarwinBackupPath(path, func(data []byte) bool {
			if containsKnownDarwinPatch(data) {
				return false
			}
			updated, result := patchImagePreviewRenderer(string(data))
			return result.Changed && darwinImageRendererReady([]byte(updated))
		})
		if backup == "" {
			return nil, false, fmt.Errorf("%s 已带有旧版助手图片补丁但缺少原始备份；请先用官方安装器覆盖重装后再连接", path)
		}
		source = backup
	}
	plan, err := prepareDarwinImagePreviewPatch(source)
	if err != nil {
		return nil, false, err
	}
	plan.path = path
	if !darwinImageRendererReady(plan.updated) {
		return plan, false, nil
	}
	// The plan source can be a clean backup while the destination is the live
	// renderer. Preserve destination metadata for the atomic replacement.
	if info, statErr := os.Stat(path); statErr == nil {
		plan.mode = info.Mode()
	} else {
		return nil, false, statErr
	}
	return plan, true, nil
}

func applyDarwinSafeIDETarget(target darwinTargets) (message string, err error) {
	settingsPath, _, err := darwinTargetUserSettingsPath(target)
	if err != nil {
		return "", err
	}
	endpoint := currentPatchProxyEndpoint().Base
	settingPlan, settingChanged, err := prepareDarwinEnsureCloudCodeSetting(settingsPath, endpoint)
	if err != nil {
		return "", err
	}

	rendererPaths := darwinImageGenerationUIRendererPaths(target)
	if len(rendererPaths) == 0 {
		return "", fmt.Errorf("未找到已验证的 IDE 图片界面 renderer；未修改任何文件")
	}
	rendererPlans := make([]*patchPlan, 0, len(rendererPaths))
	verifiedRenderers := make([]string, 0, len(rendererPaths))
	for _, rendererPath := range rendererPaths {
		plan, ready, planErr := prepareDarwinSafeImageRendererPlan(rendererPath)
		if planErr != nil {
			return "", planErr
		}
		if !ready {
			continue
		}
		rendererPlans = append(rendererPlans, plan)
		verifiedRenderers = append(verifiedRenderers, rendererPath)
	}
	if len(verifiedRenderers) == 0 {
		return "", fmt.Errorf("当前 IDE 图片标题结构尚未通过安全匹配；未修改任何文件")
	}
	productPlan, err := prepareDarwinIDEProductChecksumPatch(target, rendererPlans)
	if err != nil {
		return "", err
	}

	backupPlans := make([]*patchPlan, 0, len(rendererPlans)+1)
	for _, plan := range rendererPlans {
		if plan.changed {
			backupPlans = append(backupPlans, plan)
		}
	}
	if productPlan != nil {
		backupPlans = append(backupPlans, productPlan)
	}
	if err = saveApplyBackups(backupPlans, nil); err != nil {
		return "", fmt.Errorf("创建图片界面备份失败: %w", err)
	}
	plans := append([]*patchPlan{}, backupPlans...)
	if settingChanged {
		plans = append(plans, settingPlan)
	}
	snapshots, snapshotErr := snapshotDarwinPatchTargets(plans)
	if snapshotErr != nil {
		return "", fmt.Errorf("创建事务回滚快照失败: %w", snapshotErr)
	}
	defer func() {
		if err == nil {
			return
		}
		if rollbackErr := restoreDarwinPatchSnapshots(snapshots); rollbackErr != nil {
			err = fmt.Errorf("%w；连接事务回滚失败: %v", err, rollbackErr)
		}
	}()
	if err = writePatchPlans(plans); err != nil {
		return "", err
	}
	if !darwinCloudCodeSettingIsConfigured(settingsPath, endpoint) {
		return "", fmt.Errorf("用户级代理设置写入后未通过校验")
	}
	if err = verifyDarwinIDEProductChecksums(target, verifiedRenderers); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"%s 已安全连接本地代理并启用实际生图模型标题。\n设置文件: %s\n已验证图片 renderer: %d 个\nIDE 完整性 checksum 已同步。\n未修改主进程、扩展或 Language Server。",
		target.name, settingsPath, len(verifiedRenderers),
	), nil
}

func prepareDarwinRestorePlan(path string) (*patchPlan, bool, error) {
	backup := backupPath(path)
	restored, err := os.ReadFile(backup)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	active, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if bytes.Equal(active, restored) {
		return nil, false, nil
	}
	return &patchPlan{path: path, original: active, updated: restored, mode: info.Mode(), changed: true}, true, nil
}

func restoreDarwinSafeIDETarget(target darwinTargets) (message string, err error) {
	var plans []*patchPlan
	if settingsPath := darwinSettingsPathForStatus(target); settingsPath != "" {
		plan, changed, planErr := prepareDarwinRemoveCloudCodeSetting(settingsPath, currentPatchProxyEndpoint().Base)
		if planErr != nil {
			return "", fmt.Errorf("恢复用户设置失败: %w", planErr)
		}
		if changed {
			plans = append(plans, plan)
		}
	}
	backupPaths := make([]string, 0, 3)
	for _, path := range append(darwinImageGenerationUIRendererPaths(target), darwinIDEProductPath(target)) {
		if path == "" {
			continue
		}
		plan, changed, planErr := prepareDarwinRestorePlan(path)
		if planErr != nil {
			return "", planErr
		}
		if changed {
			plans = append(plans, plan)
			backupPaths = append(backupPaths, path)
		}
	}
	if len(plans) == 0 {
		return fmt.Sprintf("%s 未发现可恢复的本助手连接配置。", target.name), nil
	}
	snapshots, snapshotErr := snapshotDarwinPatchTargets(plans)
	if snapshotErr != nil {
		return "", snapshotErr
	}
	if err = writePatchPlans(plans); err != nil {
		if rollbackErr := restoreDarwinPatchSnapshots(snapshots); rollbackErr != nil {
			return "", fmt.Errorf("%w；回滚失败: %v", err, rollbackErr)
		}
		return "", err
	}
	if err = verifyDarwinIDEProductChecksums(target, darwinImageGenerationUIRendererPaths(target)); err != nil {
		_ = restoreDarwinPatchSnapshots(snapshots)
		return "", err
	}
	for _, path := range backupPaths {
		_ = os.Remove(backupPath(path))
	}
	return fmt.Sprintf("%s 已恢复原机连接配置。未删除其他用户设置、模型、账户或聊天数据。", target.name), nil
}

func restoreDarwinTargets(targets []darwinTargets) (string, error) {
	if len(targets) == 0 {
		return "未找到 Antigravity 安装；未修改任何文件。", nil
	}
	running, err := darwinTargetsRunning(targets)
	if err != nil {
		return "", err
	}
	if running {
		return "", fmt.Errorf("请先正常关闭所有 Antigravity 窗口，再恢复原机配置；助手不会强制结束进程")
	}
	messages := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.kind == "ide" {
			message, restoreErr := restoreDarwinSafeIDETarget(target)
			if restoreErr != nil {
				return strings.Join(messages, "\n"), fmt.Errorf("%s 恢复失败: %w", target.name, restoreErr)
			}
			messages = append(messages, message)
			continue
		}
		// Agent/2.0 still owns an ASAR/Language Server transaction and has no
		// official settings path. Reuse its exact-backup restore implementation.
		message, restoreErr := restoreDarwinPatch(target)
		if restoreErr != nil {
			return strings.Join(messages, "\n"), fmt.Errorf("%s 恢复失败: %w", target.name, restoreErr)
		}
		messages = append(messages, message)
	}
	return strings.Join(messages, "\n") + "\n恢复完成。未删除其他用户设置、模型、账户或聊天数据。", nil
}
