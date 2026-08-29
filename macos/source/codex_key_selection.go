package main

import (
	"strings"
	"time"

	"antigravity-wf-assistant/internal/codexconfig"
	"antigravity-wf-assistant/internal/codexselection"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CodexKeySelectionStatus is the renderer-safe state of the XIASS website
// Key-selection flow. It never contains a Key, callback URL, state token, or
// encoded payload.
type CodexKeySelectionStatus struct {
	OK        bool                 `json:"ok"`
	Message   string               `json:"message"`
	Selection codexselection.State `json:"selection"`
}

func (a *App) codexKeySelectionService() *codexselection.Service {
	if a == nil {
		return nil
	}
	a.codexSelectionMu.Lock()
	defer a.codexSelectionMu.Unlock()
	if a.codexKeySelection == nil {
		a.codexKeySelection = codexselection.New()
	}
	return a.codexKeySelection
}

func (a *App) closeCodexKeySelections() {
	if a == nil {
		return
	}
	a.codexSelectionMu.Lock()
	service := a.codexKeySelection
	a.codexSelectionMu.Unlock()
	if service != nil {
		service.Close()
	}
}

// StartCodexXIASSKeySelection opens the user's configured XIASS API website
// on a short-lived loopback handoff. The sensitive connect URL stays native:
// only its redacted session state is returned to Vue.
func (a *App) StartCodexXIASSKeySelection(siteURL string) CodexKeySelectionStatus {
	if a == nil || a.ctx == nil || a.exitRequested.Load() {
		return CodexKeySelectionStatus{OK: false, Message: "XIASS Tools 尚未完成启动，无法打开 Key 选择页。"}
	}
	started, err := a.codexKeySelectionService().Begin(siteURL)
	if err != nil {
		return CodexKeySelectionStatus{OK: false, Message: "无法启动 XIASS API Key 选择。请检查网站地址后重试。"}
	}
	runtime.BrowserOpenURL(a.ctx, started.ConnectURL)
	return CodexKeySelectionStatus{OK: true, Message: started.State.Message, Selection: started.State}
}

func (a *App) GetCodexXIASSKeySelectionStatus(sessionID string) CodexKeySelectionStatus {
	service := a.codexKeySelectionService()
	if service == nil {
		return CodexKeySelectionStatus{OK: false, Message: "XIASS API Key 选择服务不可用。"}
	}
	state := service.Status(sessionID)
	return CodexKeySelectionStatus{OK: state.Status == "pending" || state.Status == "ready", Message: state.Message, Selection: state}
}

// CompleteCodexXIASSKeySelectionManual supports the safe fallback where the
// user pastes the browser's complete callback URL. The URL is never persisted,
// logged, emitted, or placed in global renderer state.
func (a *App) CompleteCodexXIASSKeySelectionManual(sessionID, callbackURL string) CodexKeySelectionStatus {
	service := a.codexKeySelectionService()
	if service == nil {
		return CodexKeySelectionStatus{OK: false, Message: "XIASS API Key 选择服务不可用。"}
	}
	state := service.CompleteManualCallback(sessionID, callbackURL)
	return CodexKeySelectionStatus{OK: state.Status == "ready", Message: state.Message, Selection: state}
}

func (a *App) CancelCodexXIASSKeySelection(sessionID string) CodexKeySelectionStatus {
	service := a.codexKeySelectionService()
	if service == nil {
		return CodexKeySelectionStatus{OK: false, Message: "XIASS API Key 选择服务不可用。"}
	}
	state := service.Cancel(sessionID)
	return CodexKeySelectionStatus{OK: true, Message: state.Message, Selection: state}
}

// DiscoverCodexXIASSSelectionModels requests /v1/models with the key held in
// the native selection session. The API key cannot cross the Wails boundary.
func (a *App) DiscoverCodexXIASSSelectionModels(sessionID string) CodexModelDiscoveryResult {
	service := a.codexKeySelectionService()
	if service == nil {
		return CodexModelDiscoveryResult{OK: false, Message: "XIASS API Key 选择服务不可用。"}
	}
	ctx, cancel := a.upstreamContext(10 * time.Second)
	defer cancel()
	var models []string
	_, err := service.WithCredential(sessionID, false, func(credential codexselection.Credential) error {
		var discoveryErr error
		models, discoveryErr = codexconfig.DiscoverModels(ctx, credential.BaseURL, string(credential.APIKey), codexconfig.ModelDiscoveryOptions{})
		return discoveryErr
	})
	if err != nil {
		return CodexModelDiscoveryResult{OK: false, Message: "获取 XIASS API 上游模型失败。请检查 Key 选择状态、网络和模型服务后重试。"}
	}
	return CodexModelDiscoveryResult{OK: true, Message: "已获取 " + formatCodexModelCount(len(models)) + " 个可用模型。", Models: models}
}

// ApplyCodexXIASSSelection writes the selected native-only Key to the
// dedicated xiass_tools provider. The caller's BaseURL/APIKey fields are
// deliberately ignored: the verified website selection owns both values.
func (a *App) ApplyCodexXIASSSelection(sessionID string, input codexconfig.ApplyConfig) CodexConfigurationStatus {
	manager, err := a.codexManager()
	if err != nil {
		return codexConfigurationUnavailableStatus()
	}
	service := a.codexKeySelectionService()
	if service == nil {
		return CodexConfigurationStatus{OK: false, Message: "XIASS API Key 选择服务不可用。"}
	}
	var result codexconfig.ApplyResult
	_, err = service.WithCredential(sessionID, true, func(credential codexselection.Credential) error {
		input.APIKey = string(credential.APIKey)
		input.BaseURL = credential.BaseURL
		input.KeyName = credential.KeyName
		if strings.TrimSpace(input.ProviderName) == "" {
			input.ProviderName = credential.KeyName
		}
		var applyErr error
		result, applyErr = manager.Apply(input)
		input.APIKey = ""
		return applyErr
	})
	if err != nil {
		return codexConfigurationAfterError(manager, "未保存通过 XIASS API 选择的 Codex 配置。请重新选择 Key 并检查模型设置后重试。")
	}
	status := a.GetCodexConfiguration()
	if !status.OK {
		status.Message = "配置已安全写入并通过校验，但刷新状态失败：" + status.Message
		return status
	}
	status.Message = "已使用 XIASS API 选择的 Key 安全保存 Codex 配置，并创建可恢复备份 " + result.BackupID + "。请自行退出并重新打开 Codex 后使新配置生效。"
	return status
}
