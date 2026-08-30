package main

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"antigravity-byok/internal/claudeconfig"
)

// ClaudeCodeApplyInput is an inbound-only Wails DTO. Credential and AuthToken
// intentionally exist only long enough to invoke Manager.Apply and are never
// stored on App or returned by any bridge method. AuthToken remains a legacy
// input alias so an already-open older frontend can finish a safe save during
// an in-place upgrade.
type ClaudeCodeApplyInput struct {
	BaseURL                     string `json:"baseUrl"`
	CredentialMode              string `json:"credentialMode"`
	Credential                  string `json:"credential"`
	AuthToken                   string `json:"authToken"`
	APIKeyHelper                string `json:"apiKeyHelper"`
	EnableGatewayModelDiscovery bool   `json:"enableGatewayModelDiscovery"`
	Model                       string `json:"model"`
}

// ClaudeCodeGatewayRequestInput exists only for an explicitly initiated
// one-shot model discovery or Claude Messages connection test. It is never
// populated from settings.json, so XIASS Tools never reads saved credentials.
type ClaudeCodeGatewayRequestInput struct {
	BaseURL        string `json:"baseUrl"`
	CredentialMode string `json:"credentialMode"`
	Credential     string `json:"credential"`
	AuthToken      string `json:"authToken"`
	Model          string `json:"model"`
}

// ClaudeCodeConfigurationStatus contains only the manager's redacted
// projection. In particular, it never serializes ANTHROPIC_AUTH_TOKEN or any
// other Claude Code environment value.
type ClaudeCodeConfigurationStatus struct {
	OK                  bool                            `json:"ok"`
	Message             string                          `json:"message"`
	Snapshot            ClaudeCodeConfigurationSnapshot `json:"snapshot"`
	Backups             []claudeconfig.BackupInfo       `json:"backups"`
	LegacyBackups       []claudeconfig.LegacyBackupInfo `json:"legacyBackups"`
	LegacyBackupWarning string                          `json:"legacyBackupWarning,omitempty"`
}

// ClaudeCodeConfigurationSnapshot is the renderer-safe projection of a
// claudeconfig.Snapshot. The config directory, settings file path, checksum,
// and file mode remain native-only and are never sent to Vue.
type ClaudeCodeConfigurationSnapshot struct {
	Exists                       bool   `json:"exists"`
	Valid                        bool   `json:"valid"`
	Model                        string `json:"model,omitempty"`
	BaseURL                      string `json:"baseUrl,omitempty"`
	CredentialMode               string `json:"credentialMode,omitempty"`
	CredentialConfigured         bool   `json:"credentialConfigured"`
	AuthTokenConfigured          bool   `json:"authTokenConfigured"`
	APIKeyHelperConfigured       bool   `json:"apiKeyHelperConfigured"`
	GatewayModelDiscoveryEnabled bool   `json:"gatewayModelDiscoveryEnabled"`
	GatewayModelDiscoveryBlocked bool   `json:"gatewayModelDiscoveryBlocked"`
	Managed                      bool   `json:"managed"`
}

// ClaudeCodeGatewayModelDiscoveryStatus is safe to render: model IDs and
// display names are public gateway metadata, while request credentials, URL
// details, headers, and raw response content never cross the bridge.
type ClaudeCodeGatewayModelDiscoveryStatus struct {
	OK         bool                        `json:"ok"`
	Message    string                      `json:"message"`
	Models     []claudeconfig.GatewayModel `json:"models"`
	HTTPStatus int                         `json:"httpStatus"`
	DurationMS int64                       `json:"durationMs"`
}

// ClaudeCodeGatewayTestStatus describes one direct, minimal Anthropic
// Messages request without returning its response body or prompt content.
type ClaudeCodeGatewayTestStatus struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"httpStatus"`
	DurationMS int64  `json:"durationMs"`
}

func (a *App) claudeCodeManager() (*claudeconfig.Manager, error) {
	return claudeconfig.NewDefaultManager()
}

// GetClaudeCodeConfiguration performs a read-only inspection of the one
// supported Claude Code user settings file. It does not inspect accounts,
// OAuth data, sessions, projects, MCP files, or other Claude paths.
func (a *App) GetClaudeCodeConfiguration() ClaudeCodeConfigurationStatus {
	manager, err := a.claudeCodeManager()
	if err != nil {
		return ClaudeCodeConfigurationStatus{
			OK:            false,
			Message:       "无法识别 Claude Code 用户设置位置。",
			Backups:       emptyClaudeCodeBackups(),
			LegacyBackups: emptyClaudeCodeLegacyBackups(),
		}
	}

	snapshot, inspectErr := manager.Inspect()
	backups, backupErr := manager.ListBackups()
	legacyBackups, legacyErr := manager.ListLegacyBackups()
	status := ClaudeCodeConfigurationStatus{
		Snapshot:      claudeCodeRendererSnapshot(snapshot),
		Backups:       nonNilClaudeCodeBackups(backups),
		LegacyBackups: nonNilClaudeCodeLegacyBackups(legacyBackups),
	}
	if legacyErr != nil {
		status.LegacyBackups = emptyClaudeCodeLegacyBackups()
		status.LegacyBackupWarning = "检测到旧版 Claude Code 设置备份目录异常，已跳过旧备份；当前用户设置未被修改。"
	}
	if inspectErr != nil {
		status.Message = "无法读取 Claude Code 用户 settings.json。请确认它是有效的普通 JSON 文件。"
		return status
	}
	if backupErr != nil {
		status.Message = "Claude Code 用户设置可读取，但备份目录无法安全验证。"
		return status
	}
	status.OK = true
	status.Message = "已读取 Claude Code 用户设置。"
	return status
}

// ApplyClaudeCodeConfiguration writes only env.ANTHROPIC_BASE_URL, one
// documented credential mode, gateway discovery preference, and model through
// the transaction-safe manager. Credentials are request-local and never placed
// on App, logs, events, or a returned status object.
func (a *App) ApplyClaudeCodeConfiguration(input ClaudeCodeApplyInput) ClaudeCodeConfigurationStatus {
	manager, err := a.claudeCodeManager()
	if err != nil {
		return ClaudeCodeConfigurationStatus{
			OK:            false,
			Message:       "无法识别 Claude Code 用户设置位置。",
			Backups:       emptyClaudeCodeBackups(),
			LegacyBackups: emptyClaudeCodeLegacyBackups(),
		}
	}
	config := claudeconfig.ApplyConfig{
		BaseURL:                     input.BaseURL,
		CredentialMode:              claudeCodeCredentialMode(input.CredentialMode),
		Credential:                  claudeCodeCredential(input.Credential, input.AuthToken),
		APIKeyHelper:                input.APIKeyHelper,
		EnableGatewayModelDiscovery: input.EnableGatewayModelDiscovery,
		Model:                       input.Model,
	}
	// Drop references as soon as the native call finishes. Go strings are
	// immutable, so this is reference minimization rather than an in-place wipe.
	defer func() {
		input.Credential = ""
		input.AuthToken = ""
		input.APIKeyHelper = ""
		config.Credential = ""
		config.AuthToken = ""
		config.APIKeyHelper = ""
	}()

	result, err := manager.Apply(config)
	if err != nil {
		return claudeCodeConfigurationAfterError(manager, "未保存 Claude Code 用户设置。请检查 API 根地址、授权令牌、模型名称及现有 settings.json。")
	}
	status := a.GetClaudeCodeConfiguration()
	if !status.OK {
		status.Message = "Claude Code 用户设置已安全写入并完成校验，但无法刷新可见状态。"
		return status
	}
	status.Message = "Claude Code 用户设置已安全保存，已创建可恢复备份 " + result.BackupID + "。"
	return status
}

// DiscoverClaudeCodeGatewayModels performs an explicit, one-shot /v1/models
// request using only the credentials supplied to this invocation. It does not
// read the stored credential or change the user's settings.
func (a *App) DiscoverClaudeCodeGatewayModels(input ClaudeCodeGatewayRequestInput) ClaudeCodeGatewayModelDiscoveryStatus {
	request := claudeGatewayRequest(input)
	defer clearClaudeGatewayRequest(&input, &request)
	result, err := claudeconfig.DiscoverGatewayModels(context.Background(), request)
	status := ClaudeCodeGatewayModelDiscoveryStatus{
		Models:     nonNilClaudeGatewayModels(result.Models),
		HTTPStatus: result.HTTPStatus,
		DurationMS: result.DurationMS,
	}
	if err != nil {
		status.Message = claudeGatewayFailureMessage(err, "获取模型目录")
		return status
	}
	status.OK = true
	if len(status.Models) == 0 {
		status.Message = "网关已响应，但没有返回可供 Claude Code 显示的 Claude / Anthropic 模型。"
		return status
	}
	status.Message = "已从网关获取可用于 Claude Code 的模型目录。"
	return status
}

// TestClaudeCodeGateway performs one small, non-retried Claude Messages
// request using request-local data. A second retry could be billable, so the
// caller receives the first precise HTTP result instead of a synthetic pass.
func (a *App) TestClaudeCodeGateway(input ClaudeCodeGatewayRequestInput) ClaudeCodeGatewayTestStatus {
	request := claudeGatewayRequest(input)
	defer clearClaudeGatewayRequest(&input, &request)
	result, err := claudeconfig.TestGatewayMessages(context.Background(), request)
	status := ClaudeCodeGatewayTestStatus{HTTPStatus: result.HTTPStatus, DurationMS: result.DurationMS}
	if err != nil {
		status.Message = claudeGatewayFailureMessage(err, "测试 Claude Messages")
		return status
	}
	status.OK = true
	status.Message = "Claude Messages 实际请求成功。"
	return status
}

// RestoreClaudeCodeConfiguration explicitly restores one checksum-verified
// user-settings backup. It never restores credentials, OAuth state, projects,
// sessions, or MCP configuration.
func (a *App) RestoreClaudeCodeConfiguration(backupID string) ClaudeCodeConfigurationStatus {
	manager, err := a.claudeCodeManager()
	if err != nil {
		return ClaudeCodeConfigurationUnavailable()
	}
	result, err := manager.Restore(strings.TrimSpace(backupID))
	if err != nil {
		return claudeCodeConfigurationAfterError(manager, "未恢复 Claude Code 用户设置备份。该备份可能已损坏、已被删除或不适用于当前设置文件。")
	}
	status := a.GetClaudeCodeConfiguration()
	if !status.OK {
		status.Message = "Claude Code 用户设置备份已恢复，但无法刷新可见状态。"
		return status
	}
	status.Message = "已恢复 Claude Code 用户设置备份 " + result.RestoredBackupID + "，并创建了新的安全备份。"
	return status
}

// DeleteClaudeCodeConfigurationBackup removes only one verified,
// manager-owned user-settings backup.
func (a *App) DeleteClaudeCodeConfigurationBackup(backupID string) ClaudeCodeConfigurationStatus {
	manager, err := a.claudeCodeManager()
	if err != nil {
		return ClaudeCodeConfigurationUnavailable()
	}
	if err := manager.DeleteBackup(strings.TrimSpace(backupID)); err != nil {
		return claudeCodeConfigurationAfterError(manager, "未删除 Claude Code 用户设置备份。只可删除通过校验的备份。")
	}
	status := a.GetClaudeCodeConfiguration()
	if status.OK {
		status.Message = "已删除选定的 Claude Code 用户设置备份。"
	}
	return status
}

// MigrateClaudeCodeLegacyBackup copies one verified legacy backup to the
// current XIASS Tools backup root. It does not restore, alter, or delete the
// source backup.
func (a *App) MigrateClaudeCodeLegacyBackup(source, backupID string) ClaudeCodeConfigurationStatus {
	manager, err := a.claudeCodeManager()
	if err != nil {
		return ClaudeCodeConfigurationUnavailable()
	}
	if _, err := manager.MigrateLegacyBackup(strings.TrimSpace(source), strings.TrimSpace(backupID)); err != nil {
		return claudeCodeConfigurationAfterError(manager, "未导入旧版 Claude Code 用户设置备份。该备份未通过安全校验或无法复制。")
	}
	status := a.GetClaudeCodeConfiguration()
	if status.OK {
		status.Message = "已将旧版 Claude Code 用户设置备份复制为新的恢复点；旧备份保持不变。"
	}
	return status
}

func ClaudeCodeConfigurationUnavailable() ClaudeCodeConfigurationStatus {
	return ClaudeCodeConfigurationStatus{
		OK:            false,
		Message:       "无法识别 Claude Code 用户设置位置。",
		Backups:       emptyClaudeCodeBackups(),
		LegacyBackups: emptyClaudeCodeLegacyBackups(),
	}
}

func claudeCodeConfigurationAfterError(manager *claudeconfig.Manager, message string) ClaudeCodeConfigurationStatus {
	snapshot, _ := manager.Inspect()
	backups, _ := manager.ListBackups()
	legacyBackups, legacyErr := manager.ListLegacyBackups()
	status := ClaudeCodeConfigurationStatus{
		OK:            false,
		Message:       message,
		Snapshot:      claudeCodeRendererSnapshot(snapshot),
		Backups:       nonNilClaudeCodeBackups(backups),
		LegacyBackups: nonNilClaudeCodeLegacyBackups(legacyBackups),
	}
	if legacyErr != nil {
		status.LegacyBackups = emptyClaudeCodeLegacyBackups()
		status.LegacyBackupWarning = "检测到旧版 Claude Code 设置备份目录异常，已跳过旧备份；当前用户设置未被修改。"
	}
	return status
}

func emptyClaudeCodeBackups() []claudeconfig.BackupInfo {
	return []claudeconfig.BackupInfo{}
}

func nonNilClaudeCodeBackups(backups []claudeconfig.BackupInfo) []claudeconfig.BackupInfo {
	if backups == nil {
		return emptyClaudeCodeBackups()
	}
	return backups
}

func emptyClaudeCodeLegacyBackups() []claudeconfig.LegacyBackupInfo {
	return []claudeconfig.LegacyBackupInfo{}
}

func nonNilClaudeCodeLegacyBackups(backups []claudeconfig.LegacyBackupInfo) []claudeconfig.LegacyBackupInfo {
	if backups == nil {
		return emptyClaudeCodeLegacyBackups()
	}
	return backups
}

func claudeCodeRendererSnapshot(snapshot claudeconfig.Snapshot) ClaudeCodeConfigurationSnapshot {
	return ClaudeCodeConfigurationSnapshot{
		Exists:                       snapshot.Location.Exists,
		Valid:                        snapshot.Valid,
		Model:                        snapshot.Model,
		BaseURL:                      snapshot.BaseURL,
		CredentialMode:               string(snapshot.CredentialMode),
		CredentialConfigured:         snapshot.CredentialConfigured,
		AuthTokenConfigured:          snapshot.AuthTokenConfigured,
		APIKeyHelperConfigured:       snapshot.APIKeyHelperConfigured,
		GatewayModelDiscoveryEnabled: snapshot.GatewayModelDiscoveryEnabled,
		GatewayModelDiscoveryBlocked: snapshot.GatewayModelDiscoveryBlocked,
		Managed:                      snapshot.Managed,
	}
}

func claudeCodeCredentialMode(value string) claudeconfig.CredentialMode {
	return claudeconfig.CredentialMode(strings.TrimSpace(value))
}

func claudeCodeCredential(credential, legacyAuthToken string) string {
	if credential != "" {
		return credential
	}
	return legacyAuthToken
}

func claudeGatewayRequest(input ClaudeCodeGatewayRequestInput) claudeconfig.GatewayRequest {
	return claudeconfig.GatewayRequest{
		BaseURL:        input.BaseURL,
		CredentialMode: claudeCodeCredentialMode(input.CredentialMode),
		Credential:     claudeCodeCredential(input.Credential, input.AuthToken),
		Model:          input.Model,
	}
}

func clearClaudeGatewayRequest(input *ClaudeCodeGatewayRequestInput, request *claudeconfig.GatewayRequest) {
	if input != nil {
		input.Credential = ""
		input.AuthToken = ""
	}
	if request != nil {
		request.Credential = ""
	}
}

func nonNilClaudeGatewayModels(models []claudeconfig.GatewayModel) []claudeconfig.GatewayModel {
	if models == nil {
		return []claudeconfig.GatewayModel{}
	}
	return models
}

func claudeGatewayFailureMessage(err error, action string) string {
	var httpError *claudeconfig.GatewayHTTPError
	switch {
	case errors.Is(err, claudeconfig.ErrGatewayHelperCheckUnsupported):
		return "apiKeyHelper 会由 Claude Code 自身执行；XIASS Tools 不会运行该脚本进行远程测试。"
	case errors.As(err, &httpError):
		return action + "未通过（HTTP " + strconv.Itoa(httpError.StatusCode) + "）。网关未返回的详细错误不会显示，以免泄露凭据或内部信息。"
	default:
		return action + "未通过。请核对本次填写的地址、凭据模式、模型与网关的 Anthropic Messages 兼容性。"
	}
}
