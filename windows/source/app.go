package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"antigravity-byok/internal/agent"
	"antigravity-byok/internal/agentdiscovery"
	"antigravity-byok/internal/codexdesktop"
	"antigravity-byok/internal/codexselection"
	"antigravity-byok/internal/diagnostics"
	"antigravity-byok/internal/launcher"
	"antigravity-byok/internal/patcher"
	"antigravity-byok/internal/permissions"
	"antigravity-byok/internal/proxy"
	"antigravity-byok/internal/stats"
	"antigravity-byok/internal/storage"
	"antigravity-byok/internal/totp"
	"antigravity-byok/internal/updater"
	"antigravity-byok/internal/upstream"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App holds all Wails-exposed methods.
type App struct {
	ctx                   context.Context
	storageDir            string
	permissions           *permissions.Manager
	totpVault             *totp.Vault
	agentRegistry         *agent.Registry
	antigravityAgent      *antigravityAgentAdapter
	agentRefreshMu        sync.Mutex
	historyMu             sync.RWMutex
	historyRunMu          sync.Mutex
	historyStatus         HistorySyncStatus
	launchMu              sync.Mutex
	patchMu               sync.Mutex
	installStateMu        sync.Mutex
	updateMu              sync.Mutex
	updateCheckMu         sync.Mutex
	updateCheckCancel     context.CancelFunc
	updateCheckGeneration uint64
	accountTestMu         sync.Mutex
	accountTestCancels    map[string]*activeAccountTest
	oauthMu               sync.Mutex
	oauthSessions         map[string]*pendingOAuthSession
	oauthResults          map[string]oauthAuthorizationRecord
	oauthLoopbacks        map[string]*oauthLoopbackListener
	codexSelectionMu      sync.Mutex
	codexKeySelection     *codexselection.Service
	codexDesktopMu        sync.Mutex
	codexDesktopControl   codexDesktopControlService
	codexDesktopOperation sync.Mutex
	exitRequested         atomic.Bool
}

// activeAccountTest is deliberately keyed by an opaque renderer-generated
// request ID rather than an account ID. A user may test two different models
// from the same account, while a cancellation must only stop the one modal
// request the user just closed.
type activeAccountTest struct {
	cancel context.CancelFunc
}

func newApp() *App {
	home, _ := os.UserHomeDir()
	dir := resolveXIASSStorageDir(home)
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700)
	storage.Init(dir)
	if err := diagnostics.Init(dir); err != nil {
		log.Printf("[xiass-tools] 无法初始化本地诊断日志: %v", err)
	}
	application := &App{
		storageDir:          dir,
		permissions:         permissions.New(home, dir),
		accountTestCancels:  make(map[string]*activeAccountTest),
		oauthSessions:       make(map[string]*pendingOAuthSession),
		oauthResults:        make(map[string]oauthAuthorizationRecord),
		oauthLoopbacks:      make(map[string]*oauthLoopbackListener),
		codexKeySelection:   codexselection.New(),
		codexDesktopControl: codexdesktop.NewController(),
		historyStatus:       HistorySyncStatus{State: "pending", Message: "等待启动时同步历史会话"},
	}
	if vault, vaultErr := totp.New(dir); vaultErr != nil {
		log.Printf("[xiass-tools] 无法初始化系统凭据库：%v", vaultErr)
	} else {
		application.totpVault = vault
	}
	application.initializeAgentRegistry()
	return application
}

// initializeAgentRegistry binds only adapters with real, local implementations.
// A binding failure is logged and leaves the corresponding public profile
// conservatively unbound rather than making startup fail or advertising an
// integration that cannot be used.
func (a *App) initializeAgentRegistry() {
	registry := agent.NewDefaultRegistry()
	antigravityAdapter := newAntigravityAgentAdapter()
	if err := registry.Bind(antigravityAdapter); err != nil {
		log.Printf("[xiass-tools] 无法绑定 Antigravity 集成: %v", err)
	} else {
		a.antigravityAgent = antigravityAdapter
	}
	if err := registry.Bind(newCodexAgentAdapter()); err != nil {
		log.Printf("[xiass-tools] 无法绑定 Codex 集成: %v", err)
	}
	if err := registry.Bind(newClaudeCodeAgentAdapter()); err != nil {
		log.Printf("[xiass-tools] 无法绑定 Claude Code 集成: %v", err)
	}
	if err := registry.Bind(newCursorMCPAgentAdapter()); err != nil {
		log.Printf("[xiass-tools] 无法绑定 Cursor MCP 集成: %v", err)
	}
	if err := registry.Bind(newWindsurfMCPAgentAdapter()); err != nil {
		log.Printf("[xiass-tools] 无法绑定 Windsurf MCP 集成: %v", err)
	}
	for _, adapter := range agentdiscovery.NewAdapters() {
		// Each discovery adapter below has a concrete, narrowly-scoped binding
		// above. Avoid overwriting its verified capability status with a generic
		// read-only duplicate detector.
		switch adapter.Metadata().ID {
		case agent.ClaudeCodeID, agent.CursorID, agent.WindsurfID:
			continue
		}
		if err := registry.Bind(adapter); err != nil {
			log.Printf("[xiass-tools] 无法绑定本机 Agent 检测器: %v", err)
		}
	}
	a.agentRegistry = registry
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startTray()
	if err := proxy.Start(a.storageDir); err != nil {
		log.Printf("[xiass-tools] 代理启动失败: %v", err)
	}
	go a.syncHistory()
}

func (a *App) shutdown(ctx context.Context) {
	a.releaseExitResources()
	a.stopTray()
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
	a.releaseExitResources()
	a.stopTray()
	a.quitNativeApplication()
	go func() {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		<-timer.C
		if a.exitRequested.Load() {
			log.Printf("[xiass-tools] 正常退出超时，已在释放代理端口后结束进程")
			os.Exit(0)
		}
	}()
}

// releaseExitResources closes every locally owned listener before a native
// quit is requested. The watchdog in requestQuit deliberately has an
// os.Exit fallback for a stuck platform event loop, which bypasses Wails'
// OnShutdown hook; keeping this cleanup here therefore makes explicit quits
// safe on that fallback path as well.
func (a *App) releaseExitResources() {
	a.stopOAuthLoopbacks()
	a.closeCodexKeySelections()
	_ = proxy.Stop()
}

// ─── Result types ─────────────────────────────────────────────────────────────

type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ExportDiagnosticLogs opens the native save dialog and creates a bounded,
// credential-safe ZIP for support. Account/model/OAuth stores are never added;
// diagnostics.Export also applies a second redaction pass to every text file.
func (a *App) ExportDiagnosticLogs() Result {
	if a.ctx == nil {
		return Result{OK: false, Message: "助手尚未完成启动，请稍后再试。"}
	}
	filename := "XIASS-Tools-Diagnostics-" + time.Now().Format("20060102-150405") + ".zip"
	destination, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出诊断日志",
		DefaultFilename: filename,
		Filters: []runtime.FileFilter{{
			DisplayName: "ZIP 诊断包 (*.zip)",
			Pattern:     "*.zip",
		}},
	})
	if err != nil {
		return Result{OK: false, Message: "无法打开日志保存窗口: " + err.Error()}
	}
	if strings.TrimSpace(destination) == "" {
		return Result{OK: true, Message: "已取消导出诊断日志。"}
	}
	if !strings.EqualFold(filepath.Ext(destination), ".zip") {
		destination += ".zip"
	}
	home, _ := os.UserHomeDir()
	snapshot := map[string]any{
		"patchStatus": a.GetPatchStatus(),
		"historySync": a.GetHistorySyncStatus(),
	}
	if err := diagnostics.Export(destination, a.storageDir, diagnostics.Options{
		Version: updater.CurrentVersion, Snapshot: snapshot, HomeDir: home,
	}); err != nil {
		return Result{OK: false, Message: "导出诊断日志失败: " + err.Error()}
	}
	log.Printf("[xiass-tools] 用户已导出脱敏诊断日志")
	return Result{OK: true, Message: "诊断日志已导出：" + destination}
}

type PatchStatus struct {
	AgentPatched             bool                `json:"agentPatched"`
	IDEPatched               bool                `json:"idePatched"`
	ProxyListening           bool                `json:"proxyListening"`
	ProxyManaged             bool                `json:"proxyManaged"`
	ProxyOwned               bool                `json:"proxyOwned"`
	ProxyRepatchRequired     bool                `json:"proxyRepatchRequired"`
	ProductRepatchRequired   bool                `json:"productRepatchRequired"`
	ProductRepatchMessage    string              `json:"productRepatchMessage"`
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
	ExecutablePath     string `json:"executablePath"`
	MainPath           string `json:"mainPath"`
	ASARPath           string `json:"asarPath"`
	ExtensionPath      string `json:"extensionPath"`
	LanguageServerPath string `json:"languageServerPath"`
	Supported          bool   `json:"supported"`
	ConnectionMode     string `json:"connectionMode"`
	Reason             string `json:"reason"`
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
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	Added     int    `json:"added"`
	Bound     int    `json:"bound"`
	Unchanged int    `json:"unchanged"`
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

type PatchProgress struct {
	Phase     string `json:"phase"`
	Operation string `json:"operation"`
	Percent   int    `json:"percent"`
	Message   string `json:"message"`
}

// ─── Proxy ────────────────────────────────────────────────────────────────────

func (a *App) StartProxy() Result {
	if err := proxy.Start(a.storageDir); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	return Result{OK: true, Message: "本地代理已启动"}
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
	ctx, cancel, generation := a.beginUpdateCheck()
	defer a.finishUpdateCheck(generation, cancel)
	info, err := updater.CheckWithCache(ctx, settings.Updates.SkippedVersion, filepath.Join(a.storageDir, "update-release-cache.json"))
	if err != nil {
		return UpdateCheckResult{Message: updateCheckErrorMessage(err), Info: info}
	}
	if info.Cached {
		return UpdateCheckResult{OK: true, Message: cachedUpdateCheckMessage(info), Info: info}
	}
	if !info.Available {
		return UpdateCheckResult{OK: true, Message: "当前已是最新版本", Info: info}
	}
	if info.Skipped {
		return UpdateCheckResult{OK: true, Message: fmt.Sprintf("已跳过 v%s；可随时在设置中重新安装", info.LatestVersion), Info: info}
	}
	return UpdateCheckResult{OK: true, Message: fmt.Sprintf("发现新版本 v%s", info.LatestVersion), Info: info}
}

// CancelUpdateCheck interrupts the short-lived request immediately. The
// renderer also clears its local spinner without waiting for a stale Wails
// promise, so closing Settings or losing the network never leaves a forever
// loading state behind.
func (a *App) CancelUpdateCheck() Result {
	a.updateCheckMu.Lock()
	cancel := a.updateCheckCancel
	a.updateCheckMu.Unlock()
	if cancel == nil {
		return Result{OK: true, Message: "当前没有正在进行的更新检查"}
	}
	cancel()
	return Result{OK: true, Message: "已取消检查更新"}
}

func (a *App) beginUpdateCheck() (context.Context, context.CancelFunc, uint64) {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	a.updateCheckMu.Lock()
	if a.updateCheckCancel != nil {
		a.updateCheckCancel()
	}
	ctx, cancel := context.WithTimeout(parent, updater.CheckTimeout)
	a.updateCheckGeneration++
	generation := a.updateCheckGeneration
	a.updateCheckCancel = cancel
	a.updateCheckMu.Unlock()
	return ctx, cancel, generation
}

func (a *App) finishUpdateCheck(generation uint64, cancel context.CancelFunc) {
	cancel()
	a.updateCheckMu.Lock()
	if generation == a.updateCheckGeneration && a.updateCheckCancel != nil {
		a.updateCheckCancel = nil
	}
	a.updateCheckMu.Unlock()
}

func updateCheckErrorMessage(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "已取消检查更新"
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("检查更新超时（最多 %d 秒）。请检查网络后重试。", int(updater.CheckTimeout/time.Second))
	default:
		return "检查更新失败：" + err.Error()
	}
}

func cachedUpdateCheckMessage(info updater.Info) string {
	checkedAt := strings.TrimSpace(info.CheckedAt)
	if checkedAt == "" {
		checkedAt = "上次成功检查"
	} else {
		checkedAt = "上次检查 " + checkedAt
	}
	if info.CacheReason == "fresh" {
		if info.Available {
			return fmt.Sprintf("使用最近成功检查结果（%s）：发现 v%s。", checkedAt, info.LatestVersion)
		}
		return fmt.Sprintf("使用最近成功检查结果（%s）。", checkedAt)
	}
	prefix := "GitHub 暂时无法确认新版本"
	if info.CacheReason == "timeout" {
		prefix = fmt.Sprintf("检查更新在 %d 秒后超时", int(updater.CheckTimeout/time.Second))
	}
	if info.Available {
		return fmt.Sprintf("%s，显示缓存结果（%s）：发现 v%s；安装前仍会重新验证。", prefix, checkedAt, info.LatestVersion)
	}
	return fmt.Sprintf("%s，显示缓存结果（%s）；这不代表一定是最新版本。", prefix, checkedAt)
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
	// LaunchInstaller has successfully created the installer process. Exit as
	// soon as the launch is confirmed instead of waiting an arbitrary interval:
	// on a busy Windows machine NSIS can otherwise try to replace this running
	// executable before the delayed quit has released it.
	go a.requestQuit()
	return Result{OK: true, Message: "安装程序已启动；完成系统安装后请重新打开 XIASS Tools。"}
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
	return Result{OK: true, Message: "命令自动批准已关闭，XIASS Tools 添加的授权已移除。重新打开 Agent Window 后生效。"}
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
		log.Printf("[xiass-tools] %s", message)
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

func (a *App) historySyncIsRecent(maxAge time.Duration) bool {
	a.historyMu.RLock()
	status := a.historyStatus
	a.historyMu.RUnlock()
	if status.State != "success" || strings.TrimSpace(status.LastRunAt) == "" {
		return false
	}
	lastRun, err := time.Parse(time.RFC3339, status.LastRunAt)
	return err == nil && time.Since(lastRun) >= 0 && time.Since(lastRun) <= maxAge
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
		Provider: "openai", APIURL: upstream.DefaultXIASSBaseURL, EndpointMode: "auto", APIStyle: "chat_completions", MessagePathMode: "auto", AuthMode: "bearer",
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

// TestUpstreamModelDetailed is the replayable XIASS-style account test used
// by the account-card modal. The returned log is already credential-safe: the
// renderer never receives authorization values, headers, raw request bodies,
// or complete upstream response bodies.
func (a *App) TestUpstreamModelDetailed(config upstream.Config, request upstream.AccountTestRequest) upstream.AccountTestResult {
	request.RequestID = strings.TrimSpace(request.RequestID)
	resolved, err := a.resolveUpstreamConfig(config)
	if err != nil {
		return upstream.AccountTestResult{
			AccountID: request.AccountID,
			RequestID: request.RequestID,
			Message:   err.Error(),
			Steps:     []upstream.AccountTestStep{{Type: "error", Tone: "error", Text: err.Error()}},
		}
	}
	ctx, release, err := a.beginAccountTest(request.RequestID, 60*time.Second)
	if err != nil {
		return failedAccountTestRequest(request, err)
	}
	defer release()
	return upstream.RunAccountTest(ctx, resolved, request)
}

// AddDiscoveredModels saves a selected batch from a previously discovered
// model list. The default UI selects all discovered models; callers may pass a
// narrowed list to respect manual selection.
func (a *App) AddDiscoveredModels(config upstream.Config, modelIDs []string) BatchModelResult {
	upstreamName := strings.TrimSpace(config.UpstreamName)
	boundAccountIDs := configuredAccountIDs(config)
	if config.AccountID == "" && len(boundAccountIDs) > 0 {
		config.AccountID = boundAccountIDs[0]
	}
	resolved, err := a.resolveUpstreamConfig(config)
	if err != nil {
		return BatchModelResult{Message: err.Error()}
	}
	resolved.UpstreamName = upstreamName
	if err := upstream.ValidateConfig(resolved); err != nil {
		return BatchModelResult{Message: err.Error()}
	}
	return a.saveDiscoveredModels(resolved, boundAccountIDs, modelIDs, nil)
}

// saveDiscoveredModels writes a discovery result without ever copying an
// account credential into a model configuration. Account-backed imports use a
// storage-layer merge so one account can be added to an existing pool without
// replacing its peers or creating a duplicate Antigravity model.
func (a *App) saveDiscoveredModels(resolved upstream.Config, boundAccountIDs, modelIDs []string, snapshot *storage.AccountSyncSnapshot) BatchModelResult {
	provider := upstream.NormalizedProvider(resolved.Provider)
	seen := make(map[string]struct{}, len(modelIDs))
	candidates := make([]storage.CustomModel, 0, len(modelIDs))
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
		model.UpstreamName = resolved.UpstreamName
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
		candidates = append(candidates, model)
	}
	if len(candidates) == 0 {
		return BatchModelResult{Message: "请至少选择一个上游模型"}
	}

	if len(boundAccountIDs) > 0 {
		var (
			merged storage.DiscoveredAccountModelMergeResult
			err    error
		)
		if snapshot != nil {
			merged, err = storage.MergeDiscoveredAccountModelsForCurrentAccount(*snapshot, candidates)
		} else {
			merged, err = storage.MergeDiscoveredAccountModels(candidates)
		}
		if err != nil {
			if errors.Is(err, storage.ErrAccountSyncChanged) {
				return BatchModelResult{Message: err.Error()}
			}
			return BatchModelResult{Message: "保存模型失败：" + err.Error()}
		}
		return BatchModelResult{
			OK: true, Added: merged.Added, Bound: merged.Bound, Unchanged: merged.Unchanged,
			Message: accountModelSyncMessage(merged.Added, merged.Bound, merged.Unchanged),
		}
	}

	added := 0
	for _, model := range candidates {
		if err := storage.AddOrUpdateModel(model); err != nil {
			return BatchModelResult{Message: fmt.Sprintf("保存 %s 失败：%s", model.ExternalModelName, err.Error()), Added: added}
		}
		added++
	}
	return BatchModelResult{OK: true, Added: added, Message: fmt.Sprintf("已添加或更新 %d 个模型；重启 Antigravity 后生效。", added)}
}

func accountModelSyncMessage(added, bound, unchanged int) string {
	parts := make([]string, 0, 3)
	if added > 0 {
		parts = append(parts, fmt.Sprintf("新增 %d 个模型", added))
	}
	if bound > 0 {
		parts = append(parts, fmt.Sprintf("已将 %d 个已有模型加入账户池", bound))
	}
	if unchanged > 0 {
		parts = append(parts, fmt.Sprintf("%d 个模型已绑定", unchanged))
	}
	if len(parts) == 0 {
		return "没有可同步的模型"
	}
	return "已同步到 Antigravity：" + strings.Join(parts, "；") + "。"
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
		EndpointMode: "auto", APIStyle: "chat_completions", MessagePathMode: "auto", AuthMode: "bearer", Enabled: true,
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
	// This is an explicit account-card action, not a scheduler acquisition. A
	// paused or cooling-down account remains discoverable/testable so it can be
	// repaired before being re-enabled in the pool.
	ctx, cancel := a.upstreamContext(30 * time.Second)
	defer cancel()
	return upstream.DiscoverModels(ctx, upstream.ConfigFromAccount(account))
}

// SyncUpstreamAccountModels is the account-card one-click path: discover every
// model available to this saved account, then atomically make those models
// available to Antigravity. Explicit account actions bypass scheduler status,
// so a paused account can be repaired and synced before it is enabled again.
func (a *App) SyncUpstreamAccountModels(accountID string) BatchModelResult {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return BatchModelResult{Message: "请选择需要同步的账户"}
	}
	account, err := storage.GetUpstreamAccount(accountID)
	if err != nil {
		return BatchModelResult{Message: err.Error()}
	}
	snapshot := storage.NewAccountSyncSnapshot(account)
	config := upstream.ConfigFromAccount(account)
	if err := upstream.ValidateConfig(config); err != nil {
		return BatchModelResult{Message: err.Error()}
	}
	ctx, cancel := a.upstreamContext(30 * time.Second)
	defer cancel()
	discovery := upstream.DiscoverModels(ctx, config)
	if !discovery.OK {
		return BatchModelResult{Message: "无法获取该账户的模型列表：" + strings.TrimSpace(discovery.Message)}
	}
	modelIDs := make([]string, 0, len(discovery.Models))
	for _, model := range discovery.Models {
		if modelID := strings.TrimSpace(model.ID); modelID != "" {
			modelIDs = append(modelIDs, modelID)
		}
	}
	result := a.saveDiscoveredModels(config, []string{account.ID}, modelIDs, &snapshot)
	if result.OK && result.Message == "" {
		result.Message = accountModelSyncMessage(result.Added, result.Bound, result.Unchanged)
	}
	return result
}

func (a *App) TestUpstreamAccount(accountID, model string) upstream.TestResult {
	account, err := storage.GetUpstreamAccount(accountID)
	if err != nil {
		return upstream.TestResult{Message: err.Error()}
	}
	return a.TestUpstreamModel(upstream.ConfigFromAccount(account), model)
}

// TestUpstreamAccountDetailed resolves the reusable account on the Go side
// before testing it. Keeping accountId, model, prompt, and mode in one
// request matches the XIASS account-card interaction while preserving the
// legacy two-argument TestUpstreamAccount API for existing views.
func (a *App) TestUpstreamAccountDetailed(request upstream.AccountTestRequest) upstream.AccountTestResult {
	request.RequestID = strings.TrimSpace(request.RequestID)
	accountID := strings.TrimSpace(request.AccountID)
	if accountID == "" {
		return upstream.AccountTestResult{
			RequestID: request.RequestID,
			Message:   "请选择需要测试的账户",
			Steps:     []upstream.AccountTestStep{{Type: "error", Tone: "error", Text: "请选择需要测试的账户"}},
		}
	}
	account, err := storage.GetUpstreamAccount(accountID)
	if err != nil {
		return upstream.AccountTestResult{
			AccountID: accountID,
			RequestID: request.RequestID,
			Message:   err.Error(),
			Steps:     []upstream.AccountTestStep{{Type: "error", Tone: "error", Text: err.Error()}},
		}
	}
	request.AccountID = account.ID
	// Manual account tests intentionally bypass pool eligibility. An account
	// that is paused for scheduling, cooling down, or being repaired still
	// needs an explicit XIASS-style health probe from its own card.
	ctx, release, err := a.beginAccountTest(request.RequestID, 60*time.Second)
	if err != nil {
		return failedAccountTestRequest(request, err)
	}
	defer release()
	return upstream.RunAccountTest(ctx, upstream.ConfigFromAccount(account), request)
}

// CancelUpstreamAccountTest stops one explicit account-card probe. It never
// touches the proxy, an account's saved credentials, or another test for the
// same account. The renderer must create a fresh opaque requestId for every
// test attempt; duplicate active IDs are rejected by beginAccountTest.
func (a *App) CancelUpstreamAccountTest(requestID string) Result {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return Result{Message: "缺少测试请求标识，无法取消测试。"}
	}
	a.accountTestMu.Lock()
	active := a.accountTestCancels[requestID]
	a.accountTestMu.Unlock()
	if active == nil {
		return Result{Message: "没有正在进行的账户测试。"}
	}
	active.cancel()
	return Result{OK: true, Message: "已取消正在进行的账户测试。"}
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

const accountTestRequestIDMaxBytes = 128

// beginAccountTest owns the cancellation lifetime for an explicit account
// test. A duplicate active request ID is rejected instead of replacing the
// existing cancel function: this keeps a delayed cancel from one modal action
// from ever being applied to a newer test action with the same ID. Cleanup is
// pointer-guarded as an additional race safety net.
func (a *App) beginAccountTest(requestID string, timeout time.Duration) (context.Context, func(), error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		ctx, cancel := a.upstreamContext(timeout)
		return ctx, cancel, nil
	}
	if len(requestID) > accountTestRequestIDMaxBytes || strings.ContainsAny(requestID, "\x00\r\n\t") {
		return nil, nil, errors.New("测试请求标识无效，请重新开始测试。")
	}

	ctx, cancel := a.upstreamContext(timeout)
	active := &activeAccountTest{cancel: cancel}
	a.accountTestMu.Lock()
	if a.accountTestCancels == nil {
		a.accountTestCancels = make(map[string]*activeAccountTest)
	}
	if _, exists := a.accountTestCancels[requestID]; exists {
		a.accountTestMu.Unlock()
		cancel()
		return nil, nil, errors.New("同一测试请求仍在进行中，请等待其结束后重试。")
	}
	a.accountTestCancels[requestID] = active
	a.accountTestMu.Unlock()

	return ctx, func() {
		cancel()
		a.accountTestMu.Lock()
		if a.accountTestCancels[requestID] == active {
			delete(a.accountTestCancels, requestID)
		}
		a.accountTestMu.Unlock()
	}, nil
}

func failedAccountTestRequest(request upstream.AccountTestRequest, err error) upstream.AccountTestResult {
	message := "账户测试无法开始"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return upstream.AccountTestResult{
		AccountID: request.AccountID,
		RequestID: request.RequestID,
		Message:   message,
		Steps:     []upstream.AccountTestStep{{Type: "error", Tone: "error", Text: message}},
	}
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
	return a.patchStatusFrom(patcher.GetStatus())
}

// GetQuickPatchStatus keeps the first dashboard paint responsive. It reports
// standard-path installations and their authoritative product versions while
// the UI starts a full compatibility refresh in the background.
func (a *App) GetQuickPatchStatus() PatchStatus {
	return a.patchStatusFrom(patcher.GetQuickStatus())
}

// RefreshPatchStatus explicitly re-runs process/registry discovery and all
// compatibility checks. The dashboard's Refresh button calls this method.
func (a *App) RefreshPatchStatus() PatchStatus {
	return a.patchStatusFrom(patcher.RefreshStatus())
}

func (a *App) patchStatusFrom(s patcher.Status) PatchStatus {
	diagnostics := proxy.GetDiagnostics()
	agentPatched := s.AgentPatched != nil && *s.AgentPatched
	idePatched := s.IDEPatched != nil && *s.IDEPatched
	proxyRepatchRequired := currentProxyRepatchRequired()
	productRepatchRequired, productRepatchMessage := a.antigravityProductRepatchState(s)
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
			Supported: target.Supported, ConnectionMode: target.ConnectionMode, Reason: target.Reason,
			Patched: target.Patched, Running: running, Launchable: launcher.Supported(),
		})
	}
	return PatchStatus{
		AgentPatched:             agentPatched,
		IDEPatched:               idePatched,
		ProxyListening:           proxy.IsListening(),
		ProxyManaged:             proxy.IsManagedListener(),
		ProxyOwned:               proxy.OwnsListener(),
		ProxyRepatchRequired:     proxyRepatchRequired,
		ProductRepatchRequired:   productRepatchRequired,
		ProductRepatchMessage:    productRepatchMessage,
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

func currentProxyRepatchRequired() bool {
	pending, err := storage.HasStagedProxyRuntimePort()
	return err == nil && pending
}

// ensureProxyReadyForAntigravityLaunch starts (or verifies) the local helper
// before an Antigravity process is launched.  A patched installation must
// never be opened while its endpoint is unavailable, nor while a staged port
// migration still needs every installation to be rewritten by ApplyPatch.
//
// The second staged-state check is intentional: Start can discover that the
// committed port is occupied and safely select a fallback port.  In that
// case, launching now would leave existing patched targets pointing at the
// old endpoint.
func (a *App) ensureProxyReadyForAntigravityLaunch() error {
	if pending, err := storage.HasStagedProxyRuntimePort(); err != nil {
		return fmt.Errorf("无法确认本地代理端口切换状态；为避免 Antigravity 连接到错误代理，本次启动已停止: %w", err)
	} else if pending {
		return errors.New("本地代理已安全选择新的连接方式，但 Antigravity 仍可能指向旧连接。请先点击“应用全部补丁”，完成后再启动")
	}

	if err := proxy.Start(a.storageDir); err != nil {
		return fmt.Errorf("本地代理启动失败，未启动 Antigravity: %w", err)
	}
	if pending, err := storage.HasStagedProxyRuntimePort(); err != nil {
		return fmt.Errorf("无法确认本地代理端口切换状态；为避免 Antigravity 连接到错误代理，本次启动已停止: %w", err)
	} else if pending {
		return errors.New("本地代理连接正在安全调整中。请先点击“应用全部补丁”，完成后再启动 Antigravity")
	}
	if !proxy.IsManagedListener() {
		return errors.New("本地代理未通过健康检查，未启动 Antigravity。请稍后重试或重新打开 XIASS Tools")
	}
	return nil
}

// LaunchOrRestartAntigravity starts a detected installation or asks it to
// close normally before restarting. It never force-kills Antigravity.
func (a *App) LaunchOrRestartAntigravity(appPath string) Result {
	a.launchMu.Lock()
	defer a.launchMu.Unlock()

	selectedRoot, selectedExecutable, selectedName := "", "", "Antigravity"
	// The dashboard already gives us a detected app path. Reuse the quick
	// standard/saved-path snapshot instead of triggering a deep compatibility
	// scan merely to resolve the executable before launch.
	for _, target := range patcher.GetQuickStatus().Targets {
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
	if err := a.ensureProxyReadyForAntigravityLaunch(); err != nil {
		return Result{OK: false, Message: err.Error()}
	}

	running, err := launcher.IsRunning(selectedRoot)
	if err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	if running || !a.historySyncIsRecent(2*time.Minute) {
		if history := a.syncHistory(); !history.OK {
			return Result{OK: false, Message: "为保护聊天历史，本次启动/重启已停止：" + history.Message}
		}
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

func (a *App) applyAllPatchesWithResolvedEndpoint() Result {
	// Resolve and bind the endpoint before writing any Antigravity file. The
	// patcher reads the same persisted non-secret runtime state, so a fallback
	// selected after a 50999 collision is never written as a dead endpoint.
	if err := proxy.Start(a.storageDir); err != nil {
		return Result{OK: false, Message: fmt.Sprintf("本地代理启动失败，未写入补丁：%s", err)}
	}
	out, err := patcher.Run("apply")
	if err != nil {
		return Result{OK: false, Message: fmt.Sprintf("%s\n%s", err.Error(), out)}
	}
	if err := proxy.CommitSelectedPort(); err != nil {
		return Result{OK: false, Message: fmt.Sprintf("补丁已写入，但本地代理端口切换尚未安全确认：%s。请保持助手运行并重新执行“应用全部补丁”。", err)}
	}
	return Result{OK: true, Message: out}
}

func (a *App) applyIDEPatchWithResolvedEndpoint() Result {
	if pending, err := storage.HasStagedProxyRuntimePort(); err != nil {
		return Result{OK: false, Message: "无法确认本地代理端口切换状态；为保护所有已补丁的 Antigravity，本次仅 IDE 补丁已停止：" + err.Error()}
	} else if pending {
		return Result{OK: false, Message: "本地代理连接正在安全调整中。请使用“应用全部补丁”，避免其他 Antigravity 安装仍指向旧连接。"}
	}
	if err := proxy.Start(a.storageDir); err != nil {
		return Result{OK: false, Message: fmt.Sprintf("本地代理启动失败，未写入 IDE 补丁：%s", err)}
	}
	if pending, err := storage.HasStagedProxyRuntimePort(); err != nil {
		return Result{OK: false, Message: "无法确认本地代理端口切换状态；为保护所有已补丁的 Antigravity，本次仅 IDE 补丁已停止：" + err.Error()}
	} else if pending {
		return Result{OK: false, Message: "本地代理连接正在安全调整中。请使用“应用全部补丁”，避免其他 Antigravity 安装仍指向旧连接。"}
	}
	out, err := patcher.Run("apply-ide")
	if err != nil {
		return Result{OK: false, Message: fmt.Sprintf("%s\n%s", err.Error(), out)}
	}
	if err := proxy.CommitSelectedPort(); err != nil {
		return Result{OK: false, Message: fmt.Sprintf("IDE 补丁已写入，但本地代理端口切换尚未安全确认：%s。请保持助手运行并重新执行“仅 IDE 补丁”。", err)}
	}
	return Result{OK: true, Message: out}
}

func (a *App) ApplyPatch() Result {
	return a.runPatchAction("apply", "全部连接", a.applyAllPatchesWithResolvedEndpoint)
}

func (a *App) ApplyIDEPatch() Result {
	return a.runPatchAction("apply-ide", "连接 Antigravity IDE", a.applyIDEPatchWithResolvedEndpoint)
}

func (a *App) ApplyAgentPatch() Result {
	return a.runPatchAction("apply-agent", "连接 Antigravity 2.0", func() Result {
		if pending, err := storage.HasStagedProxyRuntimePort(); err != nil {
			return Result{OK: false, Message: "无法确认本地代理端口切换状态；为保护所有已连接的 Antigravity，本次操作已停止：" + err.Error()}
		} else if pending {
			return Result{OK: false, Message: "本地代理连接正在安全调整中。请使用“全部连接”，避免其他 Antigravity 安装仍指向旧连接。"}
		}
		if err := proxy.Start(a.storageDir); err != nil {
			return Result{OK: false, Message: fmt.Sprintf("本地代理启动失败，未写入 Antigravity 2.0 补丁：%s", err)}
		}
		if pending, err := storage.HasStagedProxyRuntimePort(); err != nil {
			return Result{OK: false, Message: "无法确认本地代理端口切换状态；为保护所有已连接的 Antigravity，本次操作已停止：" + err.Error()}
		} else if pending {
			return Result{OK: false, Message: "本地代理连接正在安全调整中。请使用“全部连接”，避免其他 Antigravity 安装仍指向旧连接。"}
		}
		out, err := patcher.Run("apply-agent")
		if err != nil {
			return Result{OK: false, Message: fmt.Sprintf("%s\n%s", err.Error(), out)}
		}
		if err := proxy.CommitSelectedPort(); err != nil {
			return Result{OK: false, Message: fmt.Sprintf("Antigravity 2.0 补丁已写入，但本地代理端口切换尚未安全确认：%s。请保持助手运行并重新执行“仅连接 Antigravity 2.0”。", err)}
		}
		return Result{OK: true, Message: out}
	})
}

func (a *App) runPatchAction(actionName string, operation string, action func() Result) Result {
	if !a.patchMu.TryLock() {
		return Result{OK: false, Message: "已有连接任务正在进行，请等待当前任务完成。"}
	}
	defer a.patchMu.Unlock()

	a.emitPatchProgress(PatchProgress{Phase: "analyzing", Operation: operation, Percent: 5, Message: "正在识别已安装产品与兼容结构"})
	progressContext, cancelProgress := context.WithCancel(context.Background())
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		ticker := time.NewTicker(650 * time.Millisecond)
		defer ticker.Stop()
		percent := 12
		for {
			select {
			case <-progressContext.Done():
				return
			case <-ticker.C:
				a.emitPatchProgress(PatchProgress{Phase: "patching", Operation: operation, Percent: percent, Message: "正在验证结构并安全写入补丁"})
				if percent < 84 {
					percent += 2
				}
			}
		}
	}()

	result := action()
	cancelProgress()
	<-progressDone
	if !result.OK {
		a.emitPatchProgress(PatchProgress{Phase: "error", Operation: operation, Percent: 100, Message: result.Message})
		return result
	}
	if err := a.recordConnectedAntigravityTargets(actionName); err != nil {
		log.Printf("[xiass-tools] 无法保存已连接的 Antigravity 安装路径: %v", err)
	}
	a.emitPatchProgress(PatchProgress{Phase: "verifying", Operation: operation, Percent: 90, Message: "正在核验补丁状态与安装完整性"})
	a.emitPatchProgress(PatchProgress{Phase: "complete", Operation: operation, Percent: 100, Message: "连接成功，可以打开 Antigravity"})
	return result
}

func (a *App) emitPatchProgress(progress PatchProgress) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "wf:patch-progress", progress)
	}
}

func (a *App) RestorePatch() Result {
	out, err := patcher.Run("restore")
	if err != nil {
		return Result{OK: false, Message: fmt.Sprintf("%s\n%s", err.Error(), out)}
	}
	if err := a.clearConnectedAntigravityTargets(); err != nil {
		log.Printf("[xiass-tools] 无法清除 Antigravity 连接基线: %v", err)
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
