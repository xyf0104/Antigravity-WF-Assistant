package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"antigravity-byok/internal/launcher"
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
	ctx           context.Context
	storageDir    string
	permissions   *permissions.Manager
	historyMu     sync.RWMutex
	historyRunMu  sync.Mutex
	historyStatus HistorySyncStatus
	launchMu      sync.Mutex
	updateMu      sync.Mutex
	exitRequested atomic.Bool
}

func newApp() *App {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".antigravity-byok")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700)
	storage.Init(dir)
	return &App{
		storageDir: dir, permissions: permissions.New(home, dir),
		historyStatus: HistorySyncStatus{State: "pending", Message: "等待启动时同步历史会话"},
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
	a.stopTray()
	_ = proxy.Stop()
}

// beforeClose handles application-level quit requests. Window close itself is
// handled by HideWindowOnClose, which sends the assistant to the Windows
// notification area instead. Explicit system quits follow requestQuit so the
// proxy is stopped before the process exits.
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
	AgentPatched           bool                `json:"agentPatched"`
	IDEPatched             bool                `json:"idePatched"`
	ProxyListening         bool                `json:"proxyListening"`
	ProxyManaged           bool                `json:"proxyManaged"`
	ProxyOwned             bool                `json:"proxyOwned"`
	LastRequestAt          string              `json:"lastRequestAt"`
	LastRequestPath        string              `json:"lastRequestPath"`
	LastModelFetchAt       string              `json:"lastModelFetchAt"`
	LastModelInjectionAt   string              `json:"lastModelInjectionAt"`
	LastInjectedModelCount int                 `json:"lastInjectedModelCount"`
	LastInjectedModelNames []string            `json:"lastInjectedModelNames"`
	LastInjectedModelSlugs []string            `json:"lastInjectedModelSlugs"`
	LastModelShape         string              `json:"lastModelShape"`
	LastModelIndexes       string              `json:"lastModelIndexes"`
	LastModelStatusCode    int                 `json:"lastModelStatusCode"`
	LastModelEncoding      string              `json:"lastModelEncoding"`
	LastError              string              `json:"lastError"`
	AsarPath               string              `json:"asarPath"`
	LSPath                 string              `json:"lsPath"`
	IDEExtension           string              `json:"ideExtension"`
	IDELS                  string              `json:"ideLS"`
	Log                    string              `json:"log"`
	Targets                []PatchTargetStatus `json:"targets"`
}

type PatchTargetStatus struct {
	Name               string `json:"name"`
	Kind               string `json:"kind"`
	Version            string `json:"version"`
	AppPath            string `json:"appPath"`
	ExecutablePath     string `json:"executablePath"`
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

func (a *App) SyncHistoryNow() Result { return a.syncHistory() }

func (a *App) syncHistory() Result {
	a.historyRunMu.Lock()
	defer a.historyRunMu.Unlock()
	a.setHistoryStatus("running", "正在恢复全部历史会话")
	if err := patcher.MergeHistory(); err != nil {
		message := fmt.Sprintf("历史会话自动恢复失败：%s", err.Error())
		log.Printf("[wf] %s", message)
		a.setHistoryStatus("error", message)
		return Result{OK: false, Message: message}
	}
	message := "历史会话检查完成；旧目录只补充缺失文件，原目录和备份均已保留"
	a.setHistoryStatus("success", message)
	return Result{OK: true, Message: message}
}

func (a *App) setHistoryStatus(state, message string) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	a.historyStatus = HistorySyncStatus{State: state, Message: message, LastRunAt: time.Now().Format(time.RFC3339)}
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
	boundAccountID := strings.TrimSpace(config.AccountID)
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
		if boundAccountID != "" {
			model.AccountIDs = []string{boundAccountID}
			// The credential belongs to the account pool. Do not duplicate it in
			// every discovered model on disk.
			model.APIKey = ""
		}
		model.Capabilities = storage.DefaultCapabilities(provider, modelID)
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

func (a *App) GetUpstreamAccounts() []storage.UpstreamAccount {
	accounts, _ := storage.LoadUpstreamAccounts()
	// The renderer only needs account metadata and health. Keep reusable
	// credentials inside the private Go storage layer; an edit with an empty
	// credential field is merged with the existing secret below.
	for i := range accounts {
		accounts[i].APIKey = ""
		accounts[i].Credentials = nil
	}
	return accounts
}

func (a *App) DefaultUpstreamAccount() storage.UpstreamAccount {
	return storage.UpstreamAccount{
		Name: "", Provider: "openai", Type: "api_key", APIURL: upstream.DefaultXIASSBaseURL,
		EndpointMode: "auto", APIStyle: "auto", MessagePathMode: "auto", AuthMode: "bearer", Enabled: true,
		Priority: 50, MaxConcurrency: 2,
	}
}

func (a *App) SaveUpstreamAccount(account storage.UpstreamAccount) Result {
	if strings.TrimSpace(account.ID) != "" && strings.TrimSpace(account.EffectiveAPIKey()) == "" {
		existing, err := storage.GetUpstreamAccount(account.ID)
		if err != nil {
			return Result{OK: false, Message: err.Error()}
		}
		// Leaving the secret blank in an edit means "keep existing". This lets
		// the frontend avoid ever receiving stored API keys or OAuth JSON.
		account.APIKey = existing.APIKey
		account.Credentials = existing.Credentials
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

func (a *App) resolveUpstreamConfig(config upstream.Config) (upstream.Config, error) {
	if strings.TrimSpace(config.AccountID) == "" {
		return config, nil
	}
	account, err := storage.GetUpstreamAccount(config.AccountID)
	if err != nil {
		return upstream.Config{}, err
	}
	return upstream.ConfigFromAccount(account), nil
}

func (a *App) modelValidationConfig(model storage.CustomModel) (upstream.Config, error) {
	if len(model.AccountIDs) == 0 {
		return upstream.ConfigFromModel(model), nil
	}
	account, err := storage.GetUpstreamAccount(model.AccountIDs[0])
	if err != nil {
		return upstream.Config{}, fmt.Errorf("模型绑定的账户不可用：%w", err)
	}
	return upstream.ConfigFromAccount(account), nil
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
			ExecutablePath: target.ExecutablePath,
			ExtensionPath:  target.ExtensionPath, LanguageServerPath: target.LanguageServerPath,
			Patched: target.Patched, Running: running, Launchable: launcher.Supported(),
		})
	}
	return PatchStatus{
		AgentPatched:           agentPatched,
		IDEPatched:             idePatched,
		ProxyListening:         proxy.IsListening(),
		ProxyManaged:           proxy.IsManagedListener(),
		ProxyOwned:             proxy.OwnsListener(),
		LastRequestAt:          diagnostics.LastRequestAt,
		LastRequestPath:        diagnostics.LastRequestPath,
		LastModelFetchAt:       diagnostics.LastModelFetchAt,
		LastModelInjectionAt:   diagnostics.LastModelInjectionAt,
		LastInjectedModelCount: diagnostics.LastInjectedModelCount,
		LastInjectedModelNames: diagnostics.LastInjectedModelNames,
		LastInjectedModelSlugs: diagnostics.LastInjectedModelSlugs,
		LastModelShape:         diagnostics.LastModelShape,
		LastModelIndexes:       diagnostics.LastModelIndexes,
		LastModelStatusCode:    diagnostics.LastModelStatusCode,
		LastModelEncoding:      diagnostics.LastModelContentEncoding,
		LastError:              diagnostics.LastError,
		AsarPath:               s.AsarPath,
		LSPath:                 s.LSPath,
		IDEExtension:           s.IDEExtensionPath,
		IDELS:                  s.IDELSPath,
		Targets:                targets,
	}
}

// LaunchOrRestartAntigravity starts a detected installation or asks it to
// close normally before restarting. It never force-kills Antigravity.
func (a *App) LaunchOrRestartAntigravity(appPath string) Result {
	a.launchMu.Lock()
	defer a.launchMu.Unlock()

	selectedRoot, selectedExecutable, selectedName := "", "", "Antigravity"
	for _, target := range patcher.GetStatus().Targets {
		if strings.EqualFold(filepath.Clean(target.AppPath), filepath.Clean(appPath)) {
			selectedRoot = target.AppPath
			selectedExecutable = target.ExecutablePath
			selectedName = target.Name
			break
		}
	}
	if selectedRoot == "" || selectedExecutable == "" {
		return Result{OK: false, Message: "只能启动当前自动检测到的 Antigravity 安装，请刷新状态后重试。"}
	}
	if !launcher.Supported() {
		return Result{OK: false, Message: "当前平台暂不支持安全启动与重启。"}
	}

	running, err := launcher.IsRunning(selectedRoot)
	if err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if history := a.syncHistory(); !history.OK {
		return Result{OK: false, Message: "为保护聊天历史，本次启动/重启已停止：" + history.Message}
	}

	verb := "启动"
	if running {
		verb = "重启"
		if err := launcher.QuitGracefully(selectedRoot, 30*time.Second); err != nil {
			return Result{OK: false, Message: err.Error()}
		}
		if history := a.syncHistory(); !history.OK {
			_ = launcher.Launch(selectedExecutable)
			return Result{OK: false, Message: "应用已正常退出，但历史同步失败；已尝试重新打开应用：" + history.Message}
		}
	}

	if err := launcher.Launch(selectedExecutable); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if err := launcher.WaitUntilRunning(selectedRoot, 15*time.Second); err != nil {
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
