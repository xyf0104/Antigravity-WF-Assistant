package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"antigravity-byok/internal/launcher"
	"antigravity-byok/internal/oauthflow"
	"antigravity-byok/internal/patcher"
	"antigravity-byok/internal/permissions"
	"antigravity-byok/internal/proxy"
	"antigravity-byok/internal/stats"
	"antigravity-byok/internal/storage"
	"antigravity-byok/internal/updater"
	"antigravity-byok/internal/upstream"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App holds all Wails-exposed methods.
type App struct {
	ctx            context.Context
	storageDir     string
	permissions    *permissions.Manager
	historyMu      sync.RWMutex
	historyRunMu   sync.Mutex
	historyStatus  HistorySyncStatus
	launchMu       sync.Mutex
	updateMu       sync.Mutex
	oauthMu        sync.Mutex
	oauthSessions  map[string]*pendingOAuthSession
	oauthResults   map[string]oauthAuthorizationRecord
	oauthLoopbacks map[string]*oauthLoopbackListener
	exitRequested  atomic.Bool
}

// pendingOAuthSession binds a short-lived PKCE session to the account draft
// entered in the renderer. Secrets never leave this Go-side structure.
type pendingOAuthSession struct {
	flow         *oauthflow.Flow
	account      storage.UpstreamAccount
	state        string
	expires      time.Time
	completionMu sync.Mutex
}

type OAuthAuthorizationResult struct {
	OK                       bool   `json:"ok"`
	Message                  string `json:"message"`
	SessionID                string `json:"sessionId,omitempty"`
	AuthorizationURL         string `json:"authorizationUrl,omitempty"`
	RedirectURI              string `json:"redirectUri,omitempty"`
	ProfileID                string `json:"profileId,omitempty"`
	AutomaticCallback        bool   `json:"automaticCallback"`
	ManualCompletionRequired bool   `json:"manualCompletionRequired"`
	ExpiresAt                string `json:"expiresAt,omitempty"`
}

type OAuthCompletionResult struct {
	OK        bool                    `json:"ok"`
	Message   string                  `json:"message"`
	AccountID string                  `json:"accountId,omitempty"`
	Identity  storage.AccountIdentity `json:"identity,omitempty"`
}

// UpstreamAccountView is the redacted account data sent to the renderer.
// LocalUsage comes solely from WF's proxy trace; it is not a provider billing
// estimate and is deliberately kept out of upstream_accounts.json.
type UpstreamAccountView struct {
	storage.UpstreamAccount
	LocalUsage stats.AccountUsage `json:"localUsage"`
}

func newApp() *App {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".antigravity-byok")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700)
	storage.Init(dir)
	return &App{
		storageDir:     dir,
		permissions:    permissions.New(home, dir),
		oauthSessions:  make(map[string]*pendingOAuthSession),
		oauthResults:   make(map[string]oauthAuthorizationRecord),
		oauthLoopbacks: make(map[string]*oauthLoopbackListener),
		historyStatus: HistorySyncStatus{
			State:   "pending",
			Message: "等待启动时同步历史会话",
		},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startTray()
	if err := proxy.Start(a.storageDir); err != nil {
		log.Printf("[wf] 代理启动失败: %v", err)
	}
	go a.syncHistory()
}

func (a *App) shutdown(ctx context.Context) {
	a.stopOAuthLoopbacks()
	a.stopTray()
	_ = proxy.Stop()
}

// beforeClose handles application-level quit requests such as Cmd+Q and the
// Dock's Quit command. Window close itself is handled by HideWindowOnClose,
// which sends the assistant to the menu bar instead. Routing a system quit
// through requestQuit makes it just as reliable as the status-item action.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.exitRequested.Load() {
		return false
	}
	a.requestQuit()
	return true
}

// QuitApp explicitly exits the assistant. OnShutdown stops the local proxy so
// 127.0.0.1:50999 is released for the next launch.
func (a *App) QuitApp() Result {
	if a.ctx == nil {
		return Result{OK: false, Message: "助手尚未完成启动，请稍后再试。"}
	}
	a.requestQuit()
	return Result{OK: true, Message: "正在退出助手并释放本地代理端口。"}
}

// requestQuit is shared by the menu-bar/tray menu and frontend. It stops the
// loopback proxy before ending the native event loop, so the port is released
// before the process disappears. The platform exit call deliberately bypasses
// the normal close-to-background handler.
func (a *App) requestQuit() {
	if a.ctx == nil || a.exitRequested.Swap(true) {
		return
	}
	_ = proxy.Stop()
	a.stopTray()
	a.quitNativeApplication()
	go func() {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		<-timer.C
		if a.exitRequested.Load() {
			log.Printf("[wf] 正常退出超时，已在释放代理端口后结束进程")
			os.Exit(0)
		}
	}()
}

// ─── Result types ─────────────────────────────────────────────────────────────

type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type PatchStatus struct {
	AgentPatched             bool                `json:"agentPatched"`
	IDEPatched               bool                `json:"idePatched"`
	ProxyListening           bool                `json:"proxyListening"`
	ProxyManaged             bool                `json:"proxyManaged"`
	ProxyOwned               bool                `json:"proxyOwned"`
	LastRequestAt            string              `json:"lastRequestAt"`
	LastRequestPath          string              `json:"lastRequestPath"`
	LastModelFetchAt         string              `json:"lastModelFetchAt"`
	LastModelInjectionAt     string              `json:"lastModelInjectionAt"`
	LastInjectedModelCount   int                 `json:"lastInjectedModelCount"`
	LastInjectedModelNames   []string            `json:"lastInjectedModelNames"`
	LastInjectedModelSlugs   []string            `json:"lastInjectedModelSlugs"`
	LastModelShape           string              `json:"lastModelShape"`
	LastModelIndexes         string              `json:"lastModelIndexes"`
	LastModelStatusCode      int                 `json:"lastModelStatusCode"`
	LastModelEncoding        string              `json:"lastModelEncoding"`
	LastModelRequestCanceled bool                `json:"lastModelRequestCanceled"`
	LastError                string              `json:"lastError"`
	AsarPath                 string              `json:"asarPath"`
	LSPath                   string              `json:"lsPath"`
	IDEExtension             string              `json:"ideExtension"`
	IDELS                    string              `json:"ideLS"`
	Log                      string              `json:"log"`
	Targets                  []PatchTargetStatus `json:"targets"`
}

type PatchTargetStatus struct {
	Name               string `json:"name"`
	Kind               string `json:"kind"`
	Version            string `json:"version"`
	AppPath            string `json:"appPath"`
	MainPath           string `json:"mainPath"`
	ASARPath           string `json:"asarPath"`
	ExtensionPath      string `json:"extensionPath"`
	LanguageServerPath string `json:"languageServerPath"`
	Patched            bool   `json:"patched"`
	Running            bool   `json:"running"`
	Launchable         bool   `json:"launchable"`
}

type StatsResult struct {
	TotalRequests    int     `json:"totalRequests"`
	CustomRequests   int     `json:"customRequests"`
	TotalTokens      int     `json:"totalTokens"`
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	CacheReadTokens  int     `json:"cacheReadTokens"`
	CacheWriteTokens int     `json:"cacheWriteTokens"`
	CacheHitRate     float64 `json:"cacheHitRate"`
}

type AutoApprovalSettings struct {
	Enabled     bool     `json:"enabled"`
	Mode        string   `json:"mode"`
	CustomRules []string `json:"customRules"`
}

type HistorySyncStatus struct {
	State     string `json:"state"`
	Message   string `json:"message"`
	LastRunAt string `json:"lastRunAt"`
}

type BatchModelResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Added   int    `json:"added"`
}

type UpdateCheckResult struct {
	OK      bool         `json:"ok"`
	Message string       `json:"message"`
	Info    updater.Info `json:"info"`
}

type UpdateProgress struct {
	Phase      string `json:"phase"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Percent    int    `json:"percent"`
	Message    string `json:"message"`
}

// ─── Proxy ────────────────────────────────────────────────────────────────────

func (a *App) StartProxy() Result {
	if err := proxy.Start(a.storageDir); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	return Result{OK: true, Message: fmt.Sprintf("代理已启动，监听 127.0.0.1:%d", proxy.ProxyPort)}
}

func (a *App) StopProxy() Result {
	if err := proxy.Stop(); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	return Result{OK: true, Message: "代理已停止"}
}

func (a *App) IsProxyListening() bool {
	return proxy.IsListening()
}

// GetAppSettings exposes only local, non-secret assistant preferences.
func (a *App) GetAppSettings() storage.AppSettings {
	settings, err := storage.LoadAppSettings()
	if err != nil {
		return storage.DefaultAppSettings()
	}
	return settings
}

// SaveAppSettings updates stream recovery immediately so a user never needs to
// restart the assistant just to change retry behaviour.
func (a *App) SaveAppSettings(settings storage.AppSettings) Result {
	settings = storage.NormalizeAppSettings(settings)
	if err := storage.SaveAppSettings(settings); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	proxy.ConfigureStreamRecovery(settings.StreamRecovery)
	return Result{OK: true, Message: "设置已保存并立即生效"}
}

func (a *App) CheckForUpdates() UpdateCheckResult {
	settings, err := storage.LoadAppSettings()
	if err != nil {
		settings = storage.DefaultAppSettings()
	}
	ctx, cancel := a.upstreamContext(50 * time.Second)
	defer cancel()
	info, err := updater.Check(ctx, settings.Updates.SkippedVersion)
	if err != nil {
		return UpdateCheckResult{Message: err.Error(), Info: info}
	}
	if !info.Available {
		return UpdateCheckResult{OK: true, Message: "当前已是最新版本", Info: info}
	}
	if info.Skipped {
		return UpdateCheckResult{OK: true, Message: fmt.Sprintf("已跳过 v%s；可随时在设置中重新安装", info.LatestVersion), Info: info}
	}
	return UpdateCheckResult{OK: true, Message: fmt.Sprintf("发现新版本 v%s", info.LatestVersion), Info: info}
}

func (a *App) SkipUpdateVersion(version string) Result {
	settings, err := storage.LoadAppSettings()
	if err != nil {
		settings = storage.DefaultAppSettings()
	}
	settings.Updates.SkippedVersion = strings.TrimSpace(version)
	if err := storage.SaveAppSettings(settings); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	return Result{OK: true, Message: "该版本已跳过"}
}

// InstallLatestUpdate downloads only the release installer selected by the
// updater package, validates its SHA256 manifest, then opens the native
// installer. Elevated installation remains a visible OS-owned action.
func (a *App) InstallLatestUpdate() Result {
	if a.ctx == nil {
		return Result{OK: false, Message: "助手尚未完成启动，请稍后再试。"}
	}
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
	defer cancel()
	a.emitUpdateProgress(UpdateProgress{Phase: "checking", Message: "正在验证更新信息"})
	lastPercent := -1
	path, info, err := updater.DownloadLatestInstaller(ctx, func(downloaded, total int64) {
		percent := 0
		if total > 0 {
			percent = int(downloaded * 100 / total)
		}
		if percent == lastPercent && total > 0 {
			return
		}
		lastPercent = percent
		a.emitUpdateProgress(UpdateProgress{Phase: "downloading", Downloaded: downloaded, Total: total, Percent: percent, Message: "正在下载并校验安装包"})
	})
	if err != nil {
		a.emitUpdateProgress(UpdateProgress{Phase: "error", Message: err.Error()})
		return Result{OK: false, Message: err.Error()}
	}
	a.emitUpdateProgress(UpdateProgress{Phase: "verified", Downloaded: info.AssetSize, Total: info.AssetSize, Percent: 100, Message: "校验完成，正在启动安装程序"})
	if err := updater.LaunchInstaller(path); err != nil {
		a.emitUpdateProgress(UpdateProgress{Phase: "error", Message: err.Error()})
		return Result{OK: false, Message: "无法启动更新安装程序：" + err.Error()}
	}
	a.emitUpdateProgress(UpdateProgress{Phase: "launching", Percent: 100, Message: "安装程序已启动，助手将退出并释放端口"})
	go func() {
		time.Sleep(800 * time.Millisecond)
		a.requestQuit()
	}()
	return Result{OK: true, Message: "安装程序已启动；完成系统安装后请重新打开 Antigravity WF助手。"}
}

func (a *App) emitUpdateProgress(progress UpdateProgress) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "wf:update-progress", progress)
	}
}

func (a *App) GetAutoApprovalStatus() permissions.Status {
	status, _ := a.permissions.Status()
	return status
}

func (a *App) SetAutoApproval(settings AutoApprovalSettings) Result {
	status, err := a.permissions.Apply(permissions.Settings{
		Enabled: settings.Enabled, Mode: settings.Mode, CustomRules: settings.CustomRules,
	})
	if err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if status.Enabled {
		return Result{OK: true, Message: fmt.Sprintf("命令自动批准已启用，共管理 %d 条授权规则。重新打开 Agent Window 后生效。", len(status.ManagedGrants))}
	}
	return Result{OK: true, Message: "命令自动批准已关闭，WF助手添加的授权已移除。重新打开 Agent Window 后生效。"}
}

func (a *App) GetHistorySyncStatus() HistorySyncStatus {
	a.historyMu.RLock()
	defer a.historyMu.RUnlock()
	return a.historyStatus
}

func (a *App) SyncHistoryNow() Result {
	return a.syncHistory()
}

func (a *App) syncHistory() Result {
	a.historyRunMu.Lock()
	defer a.historyRunMu.Unlock()
	a.setHistoryStatus("running", "正在恢复全部历史会话")
	message, err := patcher.Run("sync-history")
	if err != nil {
		message = fmt.Sprintf("历史会话自动恢复失败：%s", err.Error())
		log.Printf("[wf] %s", message)
		a.setHistoryStatus("error", message)
		return Result{OK: false, Message: message}
	}
	log.Printf("[wf] %s", message)
	a.setHistoryStatus("success", message)
	return Result{OK: true, Message: message}
}

func (a *App) setHistoryStatus(state, message string) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	a.historyStatus = HistorySyncStatus{
		State:     state,
		Message:   message,
		LastRunAt: time.Now().Format(time.RFC3339),
	}
}

// ─── Models ───────────────────────────────────────────────────────────────────

func (a *App) GetModels() []storage.CustomModel {
	models, _ := storage.LoadModels()
	return models
}

func (a *App) SaveModel(m storage.CustomModel) Result {
	config, err := a.modelValidationConfig(m)
	if err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if err := upstream.ValidateConfig(config); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if err := storage.AddOrUpdateModel(m); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	return Result{OK: true, Message: "模型已保存"}
}

// DefaultUpstreamConfig provides a useful first-run configuration without
// embedding any credentials. The user still supplies their own API key.
func (a *App) DefaultUpstreamConfig() upstream.Config {
	return upstream.Config{
		Provider: "openai", APIURL: upstream.DefaultXIASSBaseURL, EndpointMode: "auto", APIStyle: "auto", MessagePathMode: "auto", AuthMode: "bearer",
	}
}

// DiscoverUpstreamModels is a user-initiated, credential-safe GET /models.
// It never writes configuration or logs an API key.
func (a *App) DiscoverUpstreamModels(config upstream.Config) upstream.DiscoveryResult {
	resolved, err := a.resolveUpstreamConfig(config)
	if err != nil {
		return upstream.DiscoveryResult{Message: err.Error()}
	}
	ctx, cancel := a.upstreamContext(30 * time.Second)
	defer cancel()
	return upstream.DiscoverModels(ctx, resolved)
}

// TestUpstreamModel runs only after the user presses the test control. It is
// intentionally a tiny request and exposes no upstream response body.
func (a *App) TestUpstreamModel(config upstream.Config, model string) upstream.TestResult {
	resolved, err := a.resolveUpstreamConfig(config)
	if err != nil {
		return upstream.TestResult{Message: err.Error()}
	}
	ctx, cancel := a.upstreamContext(45 * time.Second)
	defer cancel()
	return upstream.TestModel(ctx, resolved, model)
}

// AddDiscoveredModels saves a selected batch from a previously discovered
// model list. The default UI selects all discovered models; callers may pass a
// narrowed list to respect manual selection.
func (a *App) AddDiscoveredModels(config upstream.Config, modelIDs []string) BatchModelResult {
	boundAccountIDs := configuredAccountIDs(config)
	if config.AccountID == "" && len(boundAccountIDs) > 0 {
		config.AccountID = boundAccountIDs[0]
	}
	resolved, err := a.resolveUpstreamConfig(config)
	if err != nil {
		return BatchModelResult{Message: err.Error()}
	}
	if err := upstream.ValidateConfig(resolved); err != nil {
		return BatchModelResult{Message: err.Error()}
	}
	provider := upstream.NormalizedProvider(resolved.Provider)
	seen := make(map[string]struct{}, len(modelIDs))
	added := 0
	for _, rawID := range modelIDs {
		modelID := strings.TrimSpace(strings.TrimPrefix(rawID, "models/"))
		if modelID == "" {
			continue
		}
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}
		model := storage.NewDiscoveredModel(provider, resolved.APIURL, resolved.APIKey, modelID)
		model.APIStyle = upstream.EffectiveAPIStyle(resolved)
		model.EndpointMode = upstream.NormalizedEndpointMode(resolved.EndpointMode)
		model.MessagePathMode = resolved.MessagePathMode
		model.AuthMode = strings.ToLower(strings.TrimSpace(resolved.AuthMode))
		model.AuthHeader = strings.TrimSpace(resolved.AuthHeader)
		model.Headers = cloneHeaders(resolved.Headers)
		if len(boundAccountIDs) > 0 {
			model.AccountIDs = boundAccountIDs
			// The credential belongs to the account pool. Do not duplicate it in
			// every discovered model on disk.
			model.APIKey = ""
		}
		model.Capabilities = storage.DefaultCapabilitiesForAPIStyle(provider, modelID, model.APIStyle)
		model.Capabilities.Configured = true
		if err := storage.AddOrUpdateModel(model); err != nil {
			return BatchModelResult{Message: fmt.Sprintf("保存 %s 失败：%s", modelID, err.Error()), Added: added}
		}
		added++
	}
	if added == 0 {
		return BatchModelResult{Message: "请至少选择一个上游模型"}
	}
	return BatchModelResult{OK: true, Added: added, Message: fmt.Sprintf("已添加或更新 %d 个模型；重启 Antigravity 后生效。", added)}
}

// ─── Upstream account pool ───────────────────────────────────────────────────

func (a *App) GetUpstreamAccounts() []UpstreamAccountView {
	accounts, _ := storage.LoadUpstreamAccounts()
	localUsage := stats.ComputeAccountUsage(a.storageDir)
	// The renderer only needs account metadata and health. Keep reusable
	// credentials inside the private Go storage layer; an edit with an empty
	// credential field is merged with the existing secret below.
	views := make([]UpstreamAccountView, 0, len(accounts))
	for _, account := range accounts {
		account.APIKey = ""
		account.Credentials = nil
		views = append(views, UpstreamAccountView{
			UpstreamAccount: account,
			LocalUsage:      localUsage[account.ID],
		})
	}
	return views
}

func (a *App) DefaultUpstreamAccount() storage.UpstreamAccount {
	return storage.UpstreamAccount{
		Name: "", Provider: "openai", Type: "api_key", APIURL: upstream.DefaultXIASSBaseURL,
		EndpointMode: "auto", APIStyle: "auto", MessagePathMode: "auto", AuthMode: "bearer", Enabled: true,
		Priority: 50, MaxConcurrency: 2,
		OAuth: storage.OAuthConfiguration{RedirectURI: "http://localhost:1455/auth/callback", Scopes: "openid profile email offline_access"},
	}
}

func (a *App) SaveUpstreamAccount(account storage.UpstreamAccount) Result {
	if strings.EqualFold(strings.TrimSpace(account.Type), "refresh_token") {
		return Result{OK: false, Message: "Refresh Token / Mobile RT 必须通过“兑换并保存 OAuth 账户”导入，不能作为 API Key 直接保存。"}
	}
	if strings.TrimSpace(account.ID) != "" {
		existing, err := storage.GetUpstreamAccount(account.ID)
		if err == nil {
			if strings.TrimSpace(account.EffectiveAPIKey()) == "" {
				// Leaving the secret blank in an edit means "keep existing". This lets
				// the frontend avoid ever receiving stored API keys or OAuth JSON.
				account.APIKey = existing.APIKey
				account.Credentials = existing.Credentials
			}
			// RefreshScopes is provider profile metadata that older renderer forms do
			// not expose as a field. Preserve it on a normal credential-safe edit so
			// the next automatic token refresh retains the provider-required scope.
			if strings.TrimSpace(account.OAuth.RefreshScopes) == "" {
				account.OAuth.RefreshScopes = existing.OAuth.RefreshScopes
			}
		} else if strings.TrimSpace(account.EffectiveAPIKey()) == "" {
			return Result{OK: false, Message: err.Error()}
		}
	}
	if err := upstream.ValidateConfig(upstream.ConfigFromAccount(account)); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if err := storage.SaveUpstreamAccount(account); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	return Result{OK: true, Message: "账户已保存到本地账户池"}
}

func (a *App) DeleteUpstreamAccount(id string) Result {
	if err := storage.DeleteUpstreamAccount(id); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	return Result{OK: true, Message: "账户已删除"}
}

func (a *App) SetUpstreamAccountEnabled(id string, enabled bool) Result {
	if err := storage.SetUpstreamAccountEnabled(id, enabled); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if enabled {
		return Result{OK: true, Message: "账户已恢复调度"}
	}
	return Result{OK: true, Message: "账户已暂停，不会再被新请求选中"}
}

func (a *App) ImportUpstreamAccounts(raw string) storage.AccountImportResult {
	return storage.ImportUpstreamAccounts(raw)
}

func (a *App) DiscoverAccountModels(accountID string) upstream.DiscoveryResult {
	account, err := storage.GetUpstreamAccount(accountID)
	if err != nil {
		return upstream.DiscoveryResult{Message: err.Error()}
	}
	return a.DiscoverUpstreamModels(upstream.ConfigFromAccount(account))
}

func (a *App) TestUpstreamAccount(accountID, model string) upstream.TestResult {
	account, err := storage.GetUpstreamAccount(accountID)
	if err != nil {
		return upstream.TestResult{Message: err.Error()}
	}
	return a.TestUpstreamModel(upstream.ConfigFromAccount(account), model)
}

// StartOAuthAuthorization creates a short-lived, provider-neutral PKCE
// session and opens its one-time authorization URL in the system browser. The
// account owner supplies a registered public OAuth client configuration; WF
// deliberately does not ship a third-party client ID or secret.
func (a *App) StartOAuthAuthorization(draft storage.UpstreamAccount) OAuthAuthorizationResult {
	if strings.TrimSpace(draft.ID) != "" {
		existing, err := storage.GetUpstreamAccount(draft.ID)
		if err != nil {
			return OAuthAuthorizationResult{Message: err.Error()}
		}
		// The account view is deliberately redacted. Retain existing secrets for
		// the eventual token rotation while accepting updated public settings.
		if strings.TrimSpace(draft.APIKey) == "" {
			draft.APIKey = existing.APIKey
		}
		if draft.Credentials == nil {
			draft.Credentials = existing.Credentials
		}
	}
	if strings.TrimSpace(draft.ID) == "" {
		accountID, err := newOAuthImportedAccountID()
		if err != nil {
			return OAuthAuthorizationResult{Message: "无法创建本地账户标识：" + err.Error()}
		}
		draft.ID = accountID
	}

	config := oauthflow.Config{
		AuthorizationURL: draft.OAuth.AuthorizationURL,
		TokenURL:         draft.OAuth.TokenURL,
		PublicClientID:   draft.OAuth.ClientID,
		RedirectURI:      draft.OAuth.RedirectURI,
		Scopes:           strings.Fields(draft.OAuth.Scopes),
		RefreshScopes:    strings.Fields(draft.OAuth.RefreshScopes),
	}
	flow, err := oauthflow.New(config)
	if err != nil {
		return OAuthAuthorizationResult{Message: oauthErrorMessage(err)}
	}
	// Validate the target API URL and custom-header shape without demanding an
	// access token before the authorization-code exchange has occurred.
	upstreamConfig := upstream.ConfigFromAccount(draft)
	upstreamConfig.APIKey = "oauth-pending"
	if err := upstream.ValidateConfig(upstreamConfig); err != nil {
		return OAuthAuthorizationResult{Message: err.Error()}
	}
	authorization, err := flow.Begin()
	if err != nil {
		return OAuthAuthorizationResult{Message: oauthErrorMessage(err)}
	}

	a.oauthMu.Lock()
	a.ensureOAuthMapsLocked()
	a.discardExpiredOAuthSessionsLocked(time.Now())
	a.oauthSessions[authorization.SessionID] = &pendingOAuthSession{
		flow: flow, account: draft, state: authorization.State, expires: authorization.ExpiresAt,
	}
	a.oauthMu.Unlock()
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, authorization.URL)
	}
	return OAuthAuthorizationResult{
		OK:                       true,
		Message:                  "已在浏览器打开授权页；授权后请粘贴完整回调 URL 或授权码。",
		SessionID:                authorization.SessionID,
		AuthorizationURL:         authorization.URL,
		RedirectURI:              draft.OAuth.RedirectURI,
		ManualCompletionRequired: true,
		ExpiresAt:                authorization.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

// CompleteOAuthAuthorization exchanges a copied callback URL or code. A raw
// code is bound to the retained state from StartOAuthAuthorization, while a
// complete callback has its state verified by oauthflow before any token is
// accepted.
func (a *App) CompleteOAuthAuthorization(sessionID, callback string) OAuthCompletionResult {
	sessionID = strings.TrimSpace(sessionID)
	a.oauthMu.Lock()
	a.ensureOAuthMapsLocked()
	a.discardExpiredOAuthSessionsLocked(time.Now())
	if record, found := a.oauthResults[sessionID]; found && record.result.OK {
		a.oauthMu.Unlock()
		return record.result
	}
	session := a.oauthSessions[sessionID]
	a.oauthMu.Unlock()
	if session == nil {
		return OAuthCompletionResult{Message: "授权会话不存在或已过期，请重新生成登录链接。"}
	}
	// A manual fallback and a loopback redirect can arrive at nearly the same
	// time. Serialising them at the session (not HTTP listener) level lets both
	// paths safely converge on one persisted account and one completion result.
	session.completionMu.Lock()
	defer session.completionMu.Unlock()

	a.oauthMu.Lock()
	a.discardExpiredOAuthSessionsLocked(time.Now())
	if record, found := a.oauthResults[sessionID]; found && record.result.OK {
		a.oauthMu.Unlock()
		return record.result
	}
	if a.oauthSessions[sessionID] != session {
		a.oauthMu.Unlock()
		return OAuthCompletionResult{Message: "授权会话不存在或已过期，请重新生成登录链接。"}
	}
	a.oauthMu.Unlock()

	parsed, err := oauthflow.ExtractCallback(callback)
	if err != nil {
		return OAuthCompletionResult{Message: oauthErrorMessage(err)}
	}
	if strings.TrimSpace(parsed.State) == "" {
		parsed.State = session.state
	}
	ctx, cancel := a.upstreamContext(45 * time.Second)
	defer cancel()
	token, err := session.flow.ExchangeCode(ctx, sessionID, parsed.Code, parsed.State)
	if err != nil {
		return OAuthCompletionResult{Message: oauthErrorMessage(err)}
	}

	account := applyOAuthToken(session.account, token, "OAuth 授权响应")
	result := a.SaveUpstreamAccount(account)
	if !result.OK {
		return OAuthCompletionResult{Message: result.Message}
	}
	// Fetching the persisted account returns identity claims normalized by the
	// storage layer. The local ID was assigned before the OAuth session began.
	stored, err := storage.GetUpstreamAccount(account.ID)
	completion := OAuthCompletionResult{
		OK: true, Message: "OAuth 授权已完成，账户已加入本地账户池。", AccountID: account.ID,
	}
	if err != nil {
		completion.Message = result.Message
	} else {
		completion.Identity = stored.Identity
	}
	a.recordOAuthAuthorizationResult(sessionID, completion)
	a.oauthMu.Lock()
	delete(a.oauthSessions, sessionID)
	a.oauthMu.Unlock()
	return completion
}

// ImportOAuthRefreshToken exchanges a user-supplied refresh token with the
// configured public OAuth client, then stores only the exchanged access token
// and refresh-token credential as an OAuth account. The refresh token is never
// treated as an upstream API key or returned to the renderer.
func (a *App) ImportOAuthRefreshToken(draft storage.UpstreamAccount, refreshToken string) OAuthCompletionResult {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return OAuthCompletionResult{Message: "请填写 Refresh Token 或 Mobile RT。"}
	}
	if strings.TrimSpace(draft.ID) == "" {
		accountID, err := newOAuthImportedAccountID()
		if err != nil {
			return OAuthCompletionResult{Message: "无法创建本地账户标识：" + err.Error()}
		}
		draft.ID = accountID
	} else {
		existing, err := storage.GetUpstreamAccount(draft.ID)
		if err != nil {
			return OAuthCompletionResult{Message: err.Error()}
		}
		// Account views intentionally omit credentials. Retain unrelated existing
		// OAuth metadata while applyOAuthToken replaces the access/refresh pair.
		if draft.Credentials == nil {
			draft.Credentials = existing.Credentials
		}
		draft.Identity = existing.Identity
	}

	// Validate the target inference configuration without ever accepting the
	// refresh token as that configuration's API key.
	upstreamConfig := upstream.ConfigFromAccount(draft)
	upstreamConfig.APIKey = "oauth-pending"
	if err := upstream.ValidateConfig(upstreamConfig); err != nil {
		return OAuthCompletionResult{Message: err.Error()}
	}
	flow, err := oauthflow.New(oauthflow.Config{
		AuthorizationURL: draft.OAuth.AuthorizationURL,
		TokenURL:         draft.OAuth.TokenURL,
		PublicClientID:   draft.OAuth.ClientID,
		RedirectURI:      draft.OAuth.RedirectURI,
		Scopes:           strings.Fields(draft.OAuth.Scopes),
		RefreshScopes:    strings.Fields(draft.OAuth.RefreshScopes),
	})
	if err != nil {
		return OAuthCompletionResult{Message: oauthErrorMessage(err)}
	}
	ctx, cancel := a.upstreamContext(45 * time.Second)
	defer cancel()
	token, err := flow.Refresh(ctx, refreshToken)
	if err != nil {
		return OAuthCompletionResult{Message: oauthErrorMessage(err)}
	}
	account := applyOAuthToken(draft, token, "Refresh Token / Mobile RT 导入")
	if err := storage.SaveUpstreamAccount(account); err != nil {
		return OAuthCompletionResult{Message: err.Error()}
	}
	stored, err := storage.GetUpstreamAccount(account.ID)
	if err != nil {
		return OAuthCompletionResult{OK: true, Message: "刷新令牌已兑换并保存为 OAuth 账户。", AccountID: account.ID}
	}
	return OAuthCompletionResult{
		OK: true, Message: "刷新令牌已兑换并保存为 OAuth 账户。",
		AccountID: stored.ID, Identity: stored.Identity,
	}
}

func newOAuthImportedAccountID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "acct-" + hex.EncodeToString(bytes), nil
}

// RefreshUpstreamOAuthAccount rotates an access token only after the user
// explicitly requests it. Automatic scheduling refreshes are handled in the
// storage layer when a bound account is near its access-token expiry.
func (a *App) RefreshUpstreamOAuthAccount(accountID string) Result {
	account, err := storage.GetUpstreamAccount(accountID)
	if err != nil {
		return Result{Message: err.Error()}
	}
	refreshToken := credentialText(account.Credentials, "refresh_token", "refreshToken", "mobile_rt", "mobileRT")
	if refreshToken == "" {
		return Result{Message: "该账户没有可用的刷新令牌，请重新授权或导入完整 OAuth JSON。"}
	}
	flow, err := oauthflow.New(oauthflow.Config{
		AuthorizationURL: account.OAuth.AuthorizationURL,
		TokenURL:         account.OAuth.TokenURL,
		PublicClientID:   account.OAuth.ClientID,
		RedirectURI:      account.OAuth.RedirectURI,
		Scopes:           strings.Fields(account.OAuth.Scopes),
		RefreshScopes:    strings.Fields(account.OAuth.RefreshScopes),
	})
	if err != nil {
		return Result{Message: oauthErrorMessage(err)}
	}
	ctx, cancel := a.upstreamContext(45 * time.Second)
	defer cancel()
	token, err := flow.Refresh(ctx, refreshToken)
	if err != nil {
		return Result{Message: oauthErrorMessage(err)}
	}
	account = applyOAuthToken(account, token, "OAuth 刷新响应")
	if err := storage.SaveUpstreamAccount(account); err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true, Message: "访问令牌已刷新"}
}

// RefreshUpstreamAccountQuota calls an optional, provider-documented quota
// endpoint configured by the account owner. It never runs as part of loading
// the account page, model discovery, or a normal inference request.
func (a *App) RefreshUpstreamAccountQuota(accountID string) upstream.QuotaResult {
	account, err := storage.GetUpstreamAccount(accountID)
	if err != nil {
		return upstream.QuotaResult{Message: err.Error()}
	}
	ctx, cancel := a.upstreamContext(30 * time.Second)
	defer cancel()
	result := upstream.FetchQuota(ctx, upstream.ConfigFromAccount(account), account.QuotaURL)
	if result.OK {
		if err := storage.SaveQuotaSnapshot(account.ID, result.Snapshot); err != nil {
			result.OK = false
			result.Message = err.Error()
		}
	}
	return result
}

func (a *App) discardExpiredOAuthSessionsLocked(now time.Time) {
	for sessionID, session := range a.oauthSessions {
		if session == nil || !now.Before(session.expires) {
			delete(a.oauthSessions, sessionID)
			if callback := a.oauthLoopbacks[sessionID]; callback != nil {
				callback.Close()
				delete(a.oauthLoopbacks, sessionID)
			}
		}
	}
	for sessionID, record := range a.oauthResults {
		if !now.Before(record.expires) {
			delete(a.oauthResults, sessionID)
		}
	}
}

func applyOAuthToken(account storage.UpstreamAccount, token oauthflow.Token, source string) storage.UpstreamAccount {
	credentials := make(map[string]any, len(account.Credentials)+7)
	for key, value := range account.Credentials {
		credentials[key] = value
	}
	credentials["access_token"] = token.AccessToken
	if token.RefreshToken != "" {
		credentials["refresh_token"] = token.RefreshToken
	}
	if token.IDToken != "" {
		credentials["id_token"] = token.IDToken
	}
	if token.TokenType != "" {
		credentials["token_type"] = token.TokenType
	}
	if token.Scope != "" {
		credentials["scope"] = token.Scope
	}
	// Keep the public OAuth client beside the rotated tokens. This mirrors
	// XIASS exports and lets a later JSON re-import refresh with the exact
	// registered client rather than guessing one. It is not a client secret.
	if clientID := strings.TrimSpace(account.OAuth.ClientID); clientID != "" {
		credentials["client_id"] = clientID
	}
	account.Type = "oauth"
	account.APIKey = token.AccessToken
	account.Credentials = credentials
	if token.ExpiresAt.IsZero() {
		account.AuthExpiresAt = ""
	} else {
		account.AuthExpiresAt = token.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if account.AuthMode == "" {
		account.AuthMode = "bearer"
	}
	account.Identity.Source = source
	account.Identity.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return account
}

func credentialText(credentials map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := credentials[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func oauthErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, oauthflow.ErrInvalidConfig):
		return "OAuth 配置无效：请检查授权地址、令牌地址、公开客户端 ID 和已注册的回调地址。"
	case errors.Is(err, oauthflow.ErrAuthorizationDenied):
		return "授权被取消或拒绝。"
	case errors.Is(err, oauthflow.ErrInvalidCallback):
		return "回调地址或授权码无效。"
	case errors.Is(err, oauthflow.ErrStateMismatch):
		return "回调状态不匹配，请重新生成登录链接。"
	case errors.Is(err, oauthflow.ErrSessionNotFound):
		return "授权会话不存在或已过期，请重新生成登录链接。"
	case errors.Is(err, oauthflow.ErrTokenExchange):
		return "令牌兑换失败，请检查 OAuth 配置、授权码和网络后重试。"
	default:
		return "OAuth 操作失败：" + err.Error()
	}
}

func (a *App) resolveUpstreamConfig(config upstream.Config) (upstream.Config, error) {
	accountIDs := configuredAccountIDs(config)
	if len(accountIDs) == 0 {
		return config, nil
	}
	configuredProvider := strings.TrimSpace(config.Provider)
	var primary storage.UpstreamAccount
	for index, accountID := range accountIDs {
		account, err := storage.GetUpstreamAccount(accountID)
		if err != nil {
			return upstream.Config{}, err
		}
		if !account.Enabled {
			return upstream.Config{}, fmt.Errorf("所选账户已暂停：%s", account.Name)
		}
		if strings.TrimSpace(account.EffectiveAPIKey()) == "" {
			return upstream.Config{}, fmt.Errorf("所选账户没有可用凭据：%s", account.Name)
		}
		if configuredProvider != "" && upstream.NormalizedProvider(account.Provider) != upstream.NormalizedProvider(configuredProvider) {
			return upstream.Config{}, fmt.Errorf("所选账户必须与模型使用同一种上游协议")
		}
		if index == 0 {
			primary = account
			continue
		}
		if upstream.NormalizedProvider(account.Provider) != upstream.NormalizedProvider(primary.Provider) {
			return upstream.Config{}, fmt.Errorf("所选账户必须使用同一种上游协议")
		}
	}
	return upstream.ConfigFromAccount(primary), nil
}

func (a *App) modelValidationConfig(model storage.CustomModel) (upstream.Config, error) {
	accountIDs := normalizedAccountIDs(model.AccountIDs)
	if len(accountIDs) == 0 {
		return upstream.ConfigFromModel(model), nil
	}
	var primary storage.UpstreamAccount
	for index, accountID := range accountIDs {
		account, err := storage.GetUpstreamAccount(accountID)
		if err != nil {
			return upstream.Config{}, fmt.Errorf("模型绑定的账户不可用：%w", err)
		}
		if index == 0 {
			primary = account
			continue
		}
		if upstream.NormalizedProvider(account.Provider) != upstream.NormalizedProvider(primary.Provider) {
			return upstream.Config{}, fmt.Errorf("模型绑定的账户必须使用同一种上游协议")
		}
	}
	if strings.TrimSpace(model.Provider) != "" && upstream.NormalizedProvider(model.Provider) != upstream.NormalizedProvider(primary.Provider) {
		return upstream.Config{}, fmt.Errorf("模型与绑定账户必须使用同一种上游协议")
	}
	return upstream.ConfigFromAccount(primary), nil
}

func configuredAccountIDs(config upstream.Config) []string {
	values := append([]string{}, config.AccountIDs...)
	if accountID := strings.TrimSpace(config.AccountID); accountID != "" {
		values = append(values, accountID)
	}
	return normalizedAccountIDs(values)
}

func normalizedAccountIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (a *App) upstreamContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, timeout)
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string]string, len(headers))
	for name, value := range headers {
		result[name] = value
	}
	return result
}

func (a *App) DeleteModel(name string) Result {
	if err := storage.DeleteModel(name); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	return Result{OK: true, Message: "模型已删除"}
}

// ─── Patch ────────────────────────────────────────────────────────────────────

func (a *App) GetPatchStatus() PatchStatus {
	s := patcher.GetStatus()
	diagnostics := proxy.GetDiagnostics()
	agentPatched := s.AgentPatched != nil && *s.AgentPatched
	idePatched := s.IDEPatched != nil && *s.IDEPatched
	targets := make([]PatchTargetStatus, 0, len(s.Targets))
	for _, target := range s.Targets {
		running := false
		if launcher.Supported() {
			running, _ = launcher.IsRunning(target.AppPath)
		}
		targets = append(targets, PatchTargetStatus{
			Name: target.Name, Kind: target.Kind, Version: target.Version,
			AppPath: target.AppPath, MainPath: target.MainPath, ASARPath: target.ASARPath,
			ExtensionPath: target.ExtensionPath, LanguageServerPath: target.LanguageServerPath,
			Patched: target.Patched, Running: running, Launchable: launcher.Supported(),
		})
	}
	return PatchStatus{
		AgentPatched:             agentPatched,
		IDEPatched:               idePatched,
		ProxyListening:           proxy.IsListening(),
		ProxyManaged:             proxy.IsManagedListener(),
		ProxyOwned:               proxy.OwnsListener(),
		LastRequestAt:            diagnostics.LastRequestAt,
		LastRequestPath:          diagnostics.LastRequestPath,
		LastModelFetchAt:         diagnostics.LastModelFetchAt,
		LastModelInjectionAt:     diagnostics.LastModelInjectionAt,
		LastInjectedModelCount:   diagnostics.LastInjectedModelCount,
		LastInjectedModelNames:   diagnostics.LastInjectedModelNames,
		LastInjectedModelSlugs:   diagnostics.LastInjectedModelSlugs,
		LastModelShape:           diagnostics.LastModelShape,
		LastModelIndexes:         diagnostics.LastModelIndexes,
		LastModelStatusCode:      diagnostics.LastModelStatusCode,
		LastModelEncoding:        diagnostics.LastModelContentEncoding,
		LastModelRequestCanceled: diagnostics.LastModelRequestCanceled,
		LastError:                diagnostics.LastError,
		AsarPath:                 s.AsarPath,
		LSPath:                   s.LSPath,
		IDEExtension:             s.IDEExtensionPath,
		IDELS:                    s.IDELSPath,
		Targets:                  targets,
	}
}

// LaunchOrRestartAntigravity starts a detected installation, or performs a
// safe restart when it is already running. Restart never force-kills: it asks
// macOS to quit normally, waits for the process to exit, synchronises history,
// and only then launches the selected bundle again.
func (a *App) LaunchOrRestartAntigravity(appPath string) Result {
	a.launchMu.Lock()
	defer a.launchMu.Unlock()

	selected := ""
	selectedName := "Antigravity"
	for _, target := range patcher.GetStatus().Targets {
		if filepath.Clean(target.AppPath) == filepath.Clean(appPath) {
			selected = target.AppPath
			selectedName = target.Name
			break
		}
	}
	if selected == "" {
		return Result{OK: false, Message: "只能启动当前自动检测到的 Antigravity 安装，请刷新状态后重试。"}
	}
	if !launcher.Supported() {
		return Result{OK: false, Message: "当前平台暂不支持安全启动与重启。"}
	}

	running, err := launcher.IsRunning(selected)
	if err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if history := a.syncHistory(); !history.OK {
		return Result{OK: false, Message: "为保护聊天历史，本次启动/重启已停止：" + history.Message}
	}

	verb := "启动"
	if running {
		verb = "重启"
		if err := launcher.QuitGracefully(selected, 30*time.Second); err != nil {
			return Result{OK: false, Message: err.Error()}
		}
		if history := a.syncHistory(); !history.OK {
			_ = launcher.Launch(selected)
			return Result{OK: false, Message: "应用已正常退出，但历史同步失败；已尝试重新打开应用：" + history.Message}
		}
	}

	if err := launcher.Launch(selected); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if err := launcher.WaitUntilRunning(selected, 15*time.Second); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	return Result{OK: true, Message: fmt.Sprintf("%s已%s；聊天历史已在操作前后安全同步。", selectedName, verb)}
}

func (a *App) ApplyPatch() Result {
	out, err := patcher.Run("apply")
	if err != nil {
		return Result{OK: false, Message: fmt.Sprintf("%s\n%s", err.Error(), out)}
	}
	if err := proxy.Start(a.storageDir); err != nil {
		return Result{OK: false, Message: fmt.Sprintf("补丁已写入，但代理启动失败：%s\n%s", err.Error(), out)}
	}
	return Result{OK: true, Message: out}
}

func (a *App) ApplyIDEPatch() Result {
	out, err := patcher.Run("apply-ide")
	if err != nil {
		return Result{OK: false, Message: fmt.Sprintf("%s\n%s", err.Error(), out)}
	}
	if err := proxy.Start(a.storageDir); err != nil {
		return Result{OK: false, Message: fmt.Sprintf("IDE 补丁已写入，但代理启动失败：%s\n%s", err.Error(), out)}
	}
	return Result{OK: true, Message: out}
}

func (a *App) RestorePatch() Result {
	out, err := patcher.Run("restore")
	if err != nil {
		return Result{OK: false, Message: fmt.Sprintf("%s\n%s", err.Error(), out)}
	}
	return Result{OK: true, Message: out}
}

// ─── Stats ────────────────────────────────────────────────────────────────────

func (a *App) GetStats() StatsResult {
	s := stats.Compute(a.storageDir)
	return StatsResult{
		TotalRequests:    s.TotalRequests,
		CustomRequests:   s.CustomRequests,
		TotalTokens:      s.TotalTokens,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		CacheReadTokens:  s.CacheReadTokens,
		CacheWriteTokens: s.CacheWriteTokens,
		CacheHitRate:     s.CacheHitRate,
	}
}
