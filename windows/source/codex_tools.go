package main

import (
	"fmt"
	"strings"
	"time"

	"antigravity-byok/internal/codexconfig"
)

// CodexConfigurationStatus is a credential-safe view for the renderer. The
// API key used during Apply or Discover never appears in this result and is
// not retained by App.
type CodexConfigurationStatus struct {
	OK                  bool                            `json:"ok"`
	Message             string                          `json:"message"`
	Snapshot            codexconfig.ConfigSnapshot      `json:"snapshot"`
	Backups             []codexconfig.BackupInfo        `json:"backups,omitempty"`
	HistoryBackups      []codexconfig.HistoryBackupInfo `json:"historyBackups,omitempty"`
	LegacyBackups       []codexconfig.LegacyBackupInfo  `json:"legacyBackups"`
	LegacyBackupWarning string                          `json:"legacyBackupWarning,omitempty"`
}

type CodexModelDiscoveryResult struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Models  []string `json:"models,omitempty"`
}

type CodexHistoryRepairStatus struct {
	OK      bool                            `json:"ok"`
	Message string                          `json:"message"`
	Result  codexconfig.HistoryRepairResult `json:"result"`
}

func (a *App) codexManager() (*codexconfig.Manager, error) {
	return codexconfig.NewDefaultManager()
}

// GetCodexConfiguration reads only the public, redacted projection of
// config.toml and the names of verified XIASS Tools backups.
func (a *App) GetCodexConfiguration() CodexConfigurationStatus {
	manager, err := a.codexManager()
	if err != nil {
		return codexConfigurationStatusForRenderer(codexConfigurationUnavailableStatus())
	}
	snapshot, inspectErr := manager.Inspect()
	backups, backupErr := manager.ListBackups()
	historyBackups, historyErr := codexconfig.NewHistoryRepairerWithManager(manager).ListBackups()
	legacyBackups, legacyWarning := codexLegacyBackupStatus(manager)
	if inspectErr != nil {
		return codexConfigurationStatusForRenderer(CodexConfigurationStatus{OK: false, Message: "无法安全读取 Codex 配置；当前文件未被修改。", Snapshot: snapshot, Backups: backups, HistoryBackups: historyBackups, LegacyBackups: legacyBackups, LegacyBackupWarning: legacyWarning})
	}
	if backupErr != nil {
		return codexConfigurationStatusForRenderer(CodexConfigurationStatus{OK: false, Message: "Codex 配置可读取，但备份目录未通过安全检查。", Snapshot: snapshot, HistoryBackups: historyBackups, LegacyBackups: legacyBackups, LegacyBackupWarning: legacyWarning})
	}
	if historyErr != nil {
		return codexConfigurationStatusForRenderer(CodexConfigurationStatus{OK: false, Message: "Codex 配置可读取，但历史备份目录未通过安全检查。", Snapshot: snapshot, Backups: backups, LegacyBackups: legacyBackups, LegacyBackupWarning: legacyWarning})
	}
	return codexConfigurationStatusForRenderer(CodexConfigurationStatus{OK: true, Message: "已读取本机 Codex 配置。", Snapshot: snapshot, Backups: backups, HistoryBackups: historyBackups, LegacyBackups: legacyBackups, LegacyBackupWarning: legacyWarning})
}

// ApplyCodexConfiguration delegates all mutation to codexconfig.Manager. The
// manager validates input, creates a checksum-protected backup, writes
// atomically, reads back its result, and rolls back on a failure.
func (a *App) ApplyCodexConfiguration(input codexconfig.ApplyConfig) CodexConfigurationStatus {
	manager, err := a.codexManager()
	if err != nil {
		return codexConfigurationUnavailableStatus()
	}
	result, err := manager.Apply(input)
	if err != nil {
		return codexConfigurationAfterError(manager, "未保存 Codex 配置。请检查 API 地址、Key、模型名称及现有 config.toml 后重试。")
	}
	status := a.GetCodexConfiguration()
	if !status.OK {
		status.Message = "配置已安全写入并通过校验，但刷新状态失败：" + status.Message
		return status
	}
	status.Message = "Codex 配置已安全保存，已创建可恢复备份 " + result.BackupID + "。"
	return status
}

// DiscoverCodexModels sends one explicitly user-requested request to the
// compatible upstream's /v1/models endpoint. API keys remain request-local.
func (a *App) DiscoverCodexModels(baseURL, apiKey string) CodexModelDiscoveryResult {
	ctx, cancel := a.upstreamContext(10 * time.Second)
	defer cancel()
	models, err := codexconfig.DiscoverModels(ctx, baseURL, apiKey, codexconfig.ModelDiscoveryOptions{})
	if err != nil {
		return CodexModelDiscoveryResult{OK: false, Message: "获取 Codex 上游模型失败。请检查 API 地址、Key、网络和模型服务后重试。"}
	}
	return CodexModelDiscoveryResult{OK: true, Message: "已获取 " + formatCodexModelCount(len(models)) + " 个可用模型。", Models: models}
}

func (a *App) RestoreCodexConfiguration(backupID string) CodexConfigurationStatus {
	manager, err := a.codexManager()
	if err != nil {
		return codexConfigurationUnavailableStatus()
	}
	result, err := manager.Restore(strings.TrimSpace(backupID))
	if err != nil {
		return codexConfigurationAfterError(manager, "未恢复 Codex 配置。该备份可能已损坏、已被删除或不适用于当前配置。")
	}
	status := a.GetCodexConfiguration()
	if !status.OK {
		status.Message = "配置备份已恢复，但刷新状态失败：" + status.Message
		return status
	}
	status.Message = "已恢复 Codex 配置备份 " + result.RestoredBackupID + "，并创建新的安全备份。"
	return status
}

func (a *App) DeleteCodexConfigurationBackup(backupID string) CodexConfigurationStatus {
	manager, err := a.codexManager()
	if err != nil {
		return codexConfigurationUnavailableStatus()
	}
	if err := manager.DeleteBackup(strings.TrimSpace(backupID)); err != nil {
		return codexConfigurationAfterError(manager, "未删除 Codex 配置备份。只可删除通过校验的备份。")
	}
	status := a.GetCodexConfiguration()
	if status.OK {
		status.Message = "已删除选定的 Codex 配置备份。"
	}
	return status
}

// ImportCodexLegacyConfigBackup stages one verified first-party XIASS Codex
// Helper configuration archive as a current XIASS Tools backup. It never
// restores the active config.toml.
func (a *App) ImportCodexLegacyConfigBackup(sourceID string) CodexConfigurationStatus {
	manager, err := a.codexManager()
	if err != nil {
		return codexConfigurationUnavailableStatus()
	}
	if _, err := manager.ImportLegacyConfigBackup(strings.TrimSpace(sourceID)); err != nil {
		return codexConfigurationAfterError(manager, "导入旧版 Codex 配置备份失败：该备份未通过安全校验或导入未完成。")
	}
	status := a.GetCodexConfiguration()
	if !status.OK {
		status.Message = "旧版 Codex 配置备份已安全导入，但当前状态刷新失败；请重新打开此页面查看备份。"
		return status
	}
	status.Message = "已导入旧版 Codex 配置备份，现可在配置备份中恢复。"
	return status
}

// ImportCodexLegacyHistoryBackup stages one verified, completed first-party
// XIASS Codex Helper history archive. It never restores or writes active
// Codex sessions, SQLite databases, configuration, or workspace state.
func (a *App) ImportCodexLegacyHistoryBackup(sourceID string) CodexConfigurationStatus {
	manager, err := a.codexManager()
	if err != nil {
		return CodexConfigurationStatus{OK: false, Message: "无法识别本机 Codex 配置目录：" + err.Error(), LegacyBackups: emptyCodexLegacyBackups()}
	}
	if _, err := manager.ImportLegacyHistoryBackup(strings.TrimSpace(sourceID)); err != nil {
		return codexConfigurationAfterError(manager, "导入旧版 Codex 历史备份失败：该备份未通过安全校验或导入未完成。")
	}
	status := a.GetCodexConfiguration()
	if !status.OK {
		status.Message = "旧版 Codex 历史备份已安全导入，但当前状态刷新失败；请重新打开此页面查看备份。"
		return status
	}
	status.Message = "已导入旧版 Codex 历史备份，现可在历史备份中恢复。"
	return status
}

// RepairCodexHistory is intentionally explicit. It repairs provider metadata
// and incompatible local response records only after the user asks for it;
// normal configuration saves never rewrite conversation history.
func (a *App) RepairCodexHistory(compatibility bool) CodexHistoryRepairStatus {
	manager, err := a.codexManager()
	if err != nil {
		return CodexHistoryRepairStatus{OK: false, Message: "无法识别本机 Codex 配置目录；历史未被修改。"}
	}
	repairer := codexconfig.NewHistoryRepairerWithManager(manager)
	var result codexconfig.HistoryRepairResult
	if compatibility {
		result, err = repairer.RepairCurrentProviderCompatibility()
	} else {
		result, err = repairer.RepairCurrentProvider()
	}
	if err != nil {
		return CodexHistoryRepairStatus{OK: false, Message: "Codex 历史修复未完成；系统已保留可恢复备份，当前内容未被进一步修改。", Result: result}
	}
	if result.Skipped {
		return CodexHistoryRepairStatus{OK: true, Message: "已检查 Codex 历史，无需修改：" + result.SkipReason, Result: result}
	}
	return CodexHistoryRepairStatus{OK: true, Message: "Codex 历史已安全检查并完成必要修复。", Result: result}
}

func (a *App) RestoreCodexHistoryBackup(backupID string) CodexConfigurationStatus {
	manager, err := a.codexManager()
	if err != nil {
		return codexConfigurationUnavailableStatus()
	}
	if err := codexconfig.NewHistoryRepairerWithManager(manager).RestoreBackup(strings.TrimSpace(backupID)); err != nil {
		return codexConfigurationAfterError(manager, "未恢复 Codex 历史备份。该备份可能已损坏、已被删除或当前数据不再适用。")
	}
	status := a.GetCodexConfiguration()
	if status.OK {
		status.Message = "已恢复选定的 Codex 历史备份。"
	}
	return status
}

func (a *App) DeleteCodexHistoryBackup(backupID string) CodexConfigurationStatus {
	manager, err := a.codexManager()
	if err != nil {
		return codexConfigurationUnavailableStatus()
	}
	if err := codexconfig.NewHistoryRepairerWithManager(manager).DeleteBackup(strings.TrimSpace(backupID)); err != nil {
		return codexConfigurationAfterError(manager, "未删除 Codex 历史备份。只可删除通过校验的备份。")
	}
	status := a.GetCodexConfiguration()
	if status.OK {
		status.Message = "已删除选定的 Codex 历史备份。"
	}
	return status
}

func codexConfigurationAfterError(manager *codexconfig.Manager, message string) CodexConfigurationStatus {
	snapshot, _ := manager.Inspect()
	backups, _ := manager.ListBackups()
	historyBackups, _ := codexconfig.NewHistoryRepairerWithManager(manager).ListBackups()
	legacyBackups, legacyWarning := codexLegacyBackupStatus(manager)
	return codexConfigurationStatusForRenderer(CodexConfigurationStatus{OK: false, Message: message, Snapshot: snapshot, Backups: backups, HistoryBackups: historyBackups, LegacyBackups: legacyBackups, LegacyBackupWarning: legacyWarning})
}

// codexConfigurationStatusForRenderer removes machine-specific paths before a
// snapshot crosses the Wails boundary. The UI only needs the existence flag;
// exposing CODEX_HOME or config.toml locations would let renderer state retain
// local filesystem details with no user-facing benefit.
func codexConfigurationStatusForRenderer(status CodexConfigurationStatus) CodexConfigurationStatus {
	status.Snapshot.Location.CodexHome = ""
	status.Snapshot.Location.ConfigPath = ""
	return status
}

func codexConfigurationUnavailableStatus() CodexConfigurationStatus {
	return CodexConfigurationStatus{
		OK:            false,
		Message:       "无法识别本机 Codex 配置目录。请确认 CODEX_HOME 或默认配置目录可用。",
		LegacyBackups: emptyCodexLegacyBackups(),
	}
}

func codexLegacyBackupStatus(manager *codexconfig.Manager) ([]codexconfig.LegacyBackupInfo, string) {
	backups, err := manager.ListLegacyBackups()
	if err != nil {
		return emptyCodexLegacyBackups(), "检测到旧版 XIASS Codex Helper 备份目录异常，已跳过旧备份导入；当前 Codex 配置不受影响。"
	}
	if backups == nil {
		backups = emptyCodexLegacyBackups()
	}
	return backups, ""
}

func emptyCodexLegacyBackups() []codexconfig.LegacyBackupInfo {
	return []codexconfig.LegacyBackupInfo{}
}

func formatCodexModelCount(count int) string {
	return fmt.Sprintf("%d", count)
}
