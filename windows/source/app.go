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
	"antigravity-byok/internal/upstream"
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
	if err := upstream.ValidateConfig(upstream.ConfigFromModel(m)); err != nil {
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
		Provider: "openai", APIURL: upstream.DefaultXIASSBaseURL, APIStyle: "auto", AuthMode: "bearer",
	}
}

// DiscoverUpstreamModels is a user-initiated, credential-safe GET /models.
// It never writes configuration or logs an API key.
func (a *App) DiscoverUpstreamModels(config upstream.Config) upstream.DiscoveryResult {
	ctx, cancel := a.upstreamContext(30 * time.Second)
	defer cancel()
	return upstream.DiscoverModels(ctx, config)
}

// TestUpstreamModel runs only after the user presses the test control. It is
// intentionally a tiny request and exposes no upstream response body.
func (a *App) TestUpstreamModel(config upstream.Config, model string) upstream.TestResult {
	ctx, cancel := a.upstreamContext(45 * time.Second)
	defer cancel()
	return upstream.TestModel(ctx, config, model)
}

// AddDiscoveredModels saves a selected batch from a previously discovered
// model list. The default UI selects all discovered models; callers may pass a
// narrowed list to respect manual selection.
func (a *App) AddDiscoveredModels(config upstream.Config, modelIDs []string) BatchModelResult {
	if err := upstream.ValidateConfig(config); err != nil {
		return BatchModelResult{Message: err.Error()}
	}
	provider := upstream.NormalizedProvider(config.Provider)
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
		model := storage.NewDiscoveredModel(provider, config.APIURL, config.APIKey, modelID)
		model.APIStyle = upstream.EffectiveAPIStyle(config)
		model.AuthMode = strings.ToLower(strings.TrimSpace(config.AuthMode))
		model.AuthHeader = strings.TrimSpace(config.AuthHeader)
		model.Headers = cloneHeaders(config.Headers)
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
