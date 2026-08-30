import { reactive, computed } from "vue";

// ─── Wails bindings ─────────────────────────────────────────────────────────
const go = () => window.go?.main?.App;
export const agentPreviewRuntimeMessage = "本地预览未连接原生运行时；安装版会在启动后检查本机工具。";

async function call(method, ...args) {
  const fn = go()?.[method];
  if (!fn) throw new Error(`方法 ${method} 未找到`);
  return fn(...args);
}

// ─── State ───────────────────────────────────────────────────────────────────
export const state = reactive({
  // 模型
  models: [],
  modelsLoading: false,

  // 上游账户池
  accounts: [],
  accountsLoading: false,

  // 统计
  stats: {
    totalRequests: 0,
    customRequests: 0,
    totalTokens: 0,
    promptTokens: 0,
    completionTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    cacheHitRate: 0,
  },
  statsLoading: false,
	dashboardRefreshing: false,
	dashboardDeepScanComplete: false,

  // 补丁状态
  patch: {
    agentPatched: false,
    idePatched: false,
    proxyListening: false,
    proxyManaged: false,
    proxyOwned: false,
		proxyRepatchRequired: false,
		productRepatchRequired: false,
		productRepatchMessage: "",
    lastRequestAt: "",
    lastRequestPath: "",
    lastModelFetchAt: "",
    lastModelInjectionAt: "",
    lastInjectedModelCount: 0,
    lastInjectedModelNames: [],
    lastInjectedModelSlugs: [],
    lastModelShape: "",
    lastModelIndexes: "",
    lastModelStatusCode: 0,
    lastModelEncoding: "",
    lastModelRequestCanceled: false,
    lastError: "",
    asarPath: "",
    lsPath: "",
    ideExtension: "",
    ideLS: "",
		targets: [],
  },
  patchLoading: false,
  patchBusy: false,
  patchLog: "",
  patchProgress: { phase: "idle", operation: "", percent: 0, message: "" },

  // Antigravity 启动器
  antigravityActionBusy: {},
  antigravityActionMessage: "",

  // 代理
  proxyRunning: false,

  autoApproval: {
    enabled: false,
    mode: "development",
    customRules: [],
    managedGrants: [],
    configPath: "",
    backupPath: "",
  },
  autoApprovalBusy: false,

  historySync: {
    state: "pending",
    message: "等待启动时同步历史会话",
    lastRunAt: "",
  },
  historySyncBusy: false,

  // 本地运行与更新设置
  settings: {
    schemaVersion: 1,
    streamRecovery: { enabled: true, maxAttempts: 2, maxDelaySeconds: 20 },
    updates: { autoCheck: true, skippedVersion: "" },
    oauth: { googleDesktopClientId: "" },
  },
  settingsLoading: false,
  settingsBusy: false,
  update: {
    checking: false,
    installing: false,
    message: "",
    info: {
      currentVersion: "",
      latestVersion: "",
      available: false,
      skipped: false,
      releaseUrl: "",
      assetName: "",
      assetSize: 0,
      publishedAt: "",
      notes: "",
			cached: false,
			cacheReason: "",
			checkedAt: "",
    },
    progress: { phase: "idle", downloaded: 0, total: 0, percent: 0, message: "" },
  },

  // Agent 工具中心。这里只保存经过原生后端脱敏后的本机状态；不会保存
  // 配置文件原文、API Key、OAuth 令牌或会话内容。
  agents: {
    platforms: [],
    diagnostics: [],
    loading: false,
    actionBusy: {},
    actionMessages: {},
		// Native Cursor/Windsurf selections are deliberately ephemeral and
		// redacted. They contain only the safe status needed to reveal the
		// launch action; no filesystem path ever enters Vue state.
		manualSelections: {},
    preview: !go(),
    message: !go() ? agentPreviewRuntimeMessage : "",
    updatedAt: "",
  },
});

// ─── 派生状态 ─────────────────────────────────────────────────────────────────
export const statusTone = computed(() => {
  if (!state.patch.proxyListening) return "err";
	if (!state.patch.proxyManaged) return "err";
	if (state.patch.targets?.length) {
		const supported = state.patch.targets.filter((target) => target.supported);
		return supported.length && supported.every((target) => target.patched) ? "ok" : "warn";
	}
  if (state.patch.agentPatched && state.patch.idePatched) return "ok";
  if (state.patch.agentPatched || state.patch.idePatched) return "warn";
  return "err";
});

export const statusLabel = computed(() => {
  if (!state.patch.proxyListening) return "代理未运行";
	if (!state.patch.proxyManaged) return "本地代理由其他程序占用";
	if (state.patch.targets?.length) {
		const supported = state.patch.targets.filter((target) => target.supported);
		const connected = supported.filter((target) => target.patched).length;
		if (!supported.length) return "未发现可安全连接的安装";
		if (connected === supported.length) return "本地代理已安全连接";
		if (connected > 0) return `${connected}/${supported.length} 个安装已连接`;
		return `发现 ${supported.length} 个可安全连接安装`;
	}
  if (state.patch.agentPatched && state.patch.idePatched) return "全部已激活";
  if (state.patch.agentPatched) return "Agent Window 已激活";
  if (state.patch.idePatched) return "IDE 已激活";
  return "尚未打补丁";
});

export const cacheHitPct = computed(() => {
  const r = state.stats.cacheHitRate;
  if (!Number.isFinite(r) || r === 0) return "—";
  return (Math.min(1, Math.max(0, r)) * 100).toFixed(1) + "%";
});

export function formatK(n) {
  if (!n) return "0";
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
  return String(n);
}

// ─── Actions ─────────────────────────────────────────────────────────────────

export async function loadModels() {
  state.modelsLoading = true;
  try {
    state.models = (await call("GetModels")) || [];
  } catch (e) {
    if (go()) console.error("loadModels", e);
  } finally {
    state.modelsLoading = false;
  }
}

export async function saveModel(model) {
  const res = await call("SaveModel", model);
  if (res?.ok) await loadModels();
  return res;
}

export async function deleteModel(name) {
  const res = await call("DeleteModel", name);
  if (res?.ok) await loadModels();
  return res;
}

export async function defaultUpstreamConfig() {
  return call("DefaultUpstreamConfig");
}

export async function discoverUpstreamModels(config) {
  return call("DiscoverUpstreamModels", config);
}

export async function testUpstreamModel(config, model) {
  return call("TestUpstreamModel", config, model);
}

// Detailed temporary-model probe used by the model editor. The native side
// receives the configuration only for this user-initiated request and returns
// a redacted, bounded activity log compatible with AccountTestModal.
export async function testUpstreamModelDetailed(config, request) {
  return call("TestUpstreamModelDetailed", config, request);
}

export async function addDiscoveredModels(config, modelIds) {
  const res = await call("AddDiscoveredModels", config, modelIds);
  if (res?.ok) await loadModels();
  return res;
}

export async function loadAccounts() {
  state.accountsLoading = true;
  try {
    state.accounts = (await call("GetUpstreamAccounts")) || [];
  } catch (e) {
    if (go()) console.error("loadAccounts", e);
  } finally {
    state.accountsLoading = false;
  }
}

export async function defaultUpstreamAccount() {
  return call("DefaultUpstreamAccount");
}

export async function saveUpstreamAccount(account) {
  const res = await call("SaveUpstreamAccount", account);
  if (res?.ok) await loadAccounts();
  return res;
}

export async function deleteUpstreamAccount(id) {
  const res = await call("DeleteUpstreamAccount", id);
  if (res?.ok) await loadAccounts();
  return res;
}

export async function setUpstreamAccountEnabled(id, enabled) {
  const res = await call("SetUpstreamAccountEnabled", id, enabled);
  if (res?.ok) await loadAccounts();
  return res;
}

export async function importUpstreamAccounts(raw) {
  const res = await call("ImportUpstreamAccounts", raw);
  if (res?.ok) await loadAccounts();
  return res;
}

export async function discoverAccountModels(accountID) {
  return call("DiscoverAccountModels", accountID);
}

// The account-card quick path discovers every model for exactly one saved
// account and merges it into Antigravity. Refresh both sides of the binding on
// success so the account card and Models view never disagree about the pool.
export async function syncUpstreamAccountModels(accountID) {
  const res = await call("SyncUpstreamAccountModels", accountID);
  if (!res?.ok) return res;
  state.modelsLoading = true;
  state.accountsLoading = true;
  try {
    const [models, accounts] = await Promise.all([
      call("GetModels"),
      call("GetUpstreamAccounts"),
    ]);
    state.models = models || [];
    state.accounts = accounts || [];
  } catch (error) {
    // The native sync succeeded, but presenting an out-of-date pool as if it
    // were current would make a retry unsafe and confusing.
    return {
      ...res,
      refreshFailed: true,
      message: `${res.message || "模型已同步。"} 本地列表刷新失败，请重新进入账户池确认。`,
    };
  } finally {
    state.modelsLoading = false;
    state.accountsLoading = false;
  }
  return res;
}

export async function testUpstreamAccount(accountID, model) {
  return call("TestUpstreamAccount", accountID, model);
}

// The detailed account-card probe returns a credential-safe, replayable log,
// bounded text output, and strictly validated image previews. It is kept
// separate from the legacy two-argument probe so existing integrations stay
// compatible while every saved account can use the XIASS-style test modal.
export async function testUpstreamAccountDetailed(request) {
  return call("TestUpstreamAccountDetailed", request);
}

// Cancels only the explicit account-card test identified by requestId. This
// is intentionally separate from proxy or account controls: closing a test
// dialog must never pause the account or affect another model request.
export async function cancelUpstreamAccountTest(requestID) {
  return call("CancelUpstreamAccountTest", requestID);
}

export async function startOAuthAuthorization(account) {
  return call("StartOAuthAuthorization", account);
}

// These two bindings intentionally mirror the provider-profile API exposed by
// App. Profiles are supplied by the native backend so the renderer never ships
// a third-party OAuth client ID, secret, or provider-owned endpoint.
export async function getOAuthLoginProfiles() {
  return call("GetOAuthLoginProfiles");
}

export async function startOAuthProviderAuthorization(profileID, account) {
  return call("StartOAuthProviderAuthorization", profileID, account);
}

// The native status endpoint returns only redacted authorization progress and
// display metadata. It is intentionally queried by the view only while a
// loopback OAuth session is pending; credentials never reach renderer state.
export async function getOAuthAuthorizationStatus(sessionID) {
  return call("GetOAuthAuthorizationStatus", sessionID);
}

export async function completeOAuthAuthorization(sessionID, callback) {
  const res = await call("CompleteOAuthAuthorization", sessionID, callback);
  if (res?.ok) await loadAccounts();
  return res;
}

export async function importOAuthRefreshToken(account, refreshToken) {
  const res = await call("ImportOAuthRefreshToken", account, refreshToken);
  if (res?.ok) await loadAccounts();
  return res;
}

export async function refreshUpstreamOAuthAccount(accountID) {
  const res = await call("RefreshUpstreamOAuthAccount", accountID);
  if (res?.ok) await loadAccounts();
  return res;
}

export async function refreshUpstreamAccountQuota(accountID) {
  const res = await call("RefreshUpstreamAccountQuota", accountID);
  if (res?.ok) await loadAccounts();
  return res;
}

export async function loadStats() {
  state.statsLoading = true;
  try {
    state.stats = await call("GetStats");
  } catch (e) {
    if (go()) console.error("loadStats", e);
  } finally {
    state.statsLoading = false;
  }
}

export async function loadPatchStatus(options = {}) {
  state.patchLoading = true;
  try {
	const method = options.quick
		? "GetQuickPatchStatus"
		: options.refresh
			? "RefreshPatchStatus"
			: "GetPatchStatus";
    const s = await call(method);
    state.patch = { ...state.patch, ...(s || {}), targets: s?.targets || [] };
	state.proxyRunning = !!s?.proxyManaged;
	return s;
  } catch (e) {
    if (go()) console.error("loadPatchStatus", e);
  } finally {
    state.patchLoading = false;
  }
}

let agentStatusPromise = null;

// The normal pass is deliberately non-blocking during startup. A deep scan is
// only requested after an explicit user refresh, so opening XIASS Tools stays
// responsive even when a desktop IDE bundle is large.
export async function loadAgentStatuses(options = {}) {
  if (agentStatusPromise) return agentStatusPromise;
  if (!go()) {
    state.agents.platforms = [];
    state.agents.diagnostics = [];
    state.agents.loading = false;
    state.agents.preview = true;
    state.agents.message = agentPreviewRuntimeMessage;
    state.agents.updatedAt = "";
    return null;
  }

  state.agents.preview = false;
  state.agents.loading = true;
  const method = options.refresh ? "RefreshAgentStatuses" : "GetAgentStatuses";
  agentStatusPromise = call(method)
    .then((result) => {
      state.agents.platforms = result?.agents || [];
      state.agents.diagnostics = result?.diagnostics || [];
      state.agents.updatedAt = result?.generatedAt || "";
      state.agents.message = "";
      return result;
    })
    .catch((error) => {
      state.agents.preview = false;
      state.agents.message = error?.message || "本机工具检查失败。";
      if (go()) console.error("loadAgentStatuses", error);
      return null;
    })
    .finally(() => {
      state.agents.loading = false;
      agentStatusPromise = null;
    });
  return agentStatusPromise;
}

export function refreshAgentStatuses() {
  return loadAgentStatuses({ refresh: true });
}

export async function diagnoseAgent(agentID) {
  const key = `${agentID}:diagnose`;
  state.agents.actionBusy = { ...state.agents.actionBusy, [key]: true };
  try {
    const result = await call("DiagnoseAgent", agentID);
    if (result?.diagnostics) {
      state.agents.diagnostics = result.diagnostics;
    }
    return result;
  } finally {
    const next = { ...state.agents.actionBusy };
    delete next[key];
    state.agents.actionBusy = next;
  }
}

// The renderer may identify a supported application, but it never supplies an
// executable path or launch arguments. The native side re-runs bounded local
// discovery and only opens a verified Cursor or Windsurf installation.
export async function launchDetectedAgent(agentID) {
  const identifier = String(agentID || "").trim();
  if (!identifier) return { ok: false, message: "未指定要打开的应用。" };
  const key = `${identifier}:launch`;
  state.agents.actionBusy = { ...state.agents.actionBusy, [key]: true };
  try {
    const result = await call("LaunchDetectedAgent", identifier);
    if (result?.ok) void loadAgentStatuses();
    return result || { ok: false, message: "无法启动该应用。" };
  } catch {
    // Do not expose bridge or platform errors here: they can contain a local
    // path and are not useful to the person operating the tools center.
    return { ok: false, message: "无法启动该应用。请刷新检查后重试。" };
  } finally {
    const next = { ...state.agents.actionBusy };
    delete next[key];
    state.agents.actionBusy = next;
  }
}

// Cursor/Windsurf application selection stays native-only. The renderer sends
// one fixed integration ID, never a filesystem path; the native dialog result
// is structurally verified and reduced to this redacted status.
export async function selectAgentDesktopInstallation(agentID) {
	const identifier = String(agentID || "").trim();
	if (!["cursor", "windsurf"].includes(identifier)) {
		return { ok: false, message: "该工具不支持选择桌面应用。" };
	}
	const key = `${identifier}:choose`;
	state.agents.actionBusy = { ...state.agents.actionBusy, [key]: true };
	try {
		const result = await call("SelectAgentDesktopInstallation", identifier);
		if (result && typeof result === "object") {
			state.agents.manualSelections = {
				...state.agents.manualSelections,
				[identifier]: {
					selected: Boolean(result.selected),
					canLaunch: Boolean(result.canLaunch),
					version: typeof result.version === "string" ? result.version : "",
				},
			};
		}
		return result || { ok: false, message: "无法选择应用。" };
	} catch {
		return { ok: false, message: "无法打开应用选择窗口。请刷新检查后重试。" };
	} finally {
		const next = { ...state.agents.actionBusy };
		delete next[key];
		state.agents.actionBusy = next;
	}
}

// ─── Codex local configuration ─────────────────────────────────────────────
// The native bindings return redacted snapshots only. Callers intentionally
// pass an API key only to the two explicit operations below; this state module
// never assigns that key into the global reactive state or localStorage.
export async function getCodexConfiguration() {
  return call("GetCodexConfiguration");
}

export async function applyCodexConfiguration(config) {
  return call("ApplyCodexConfiguration", config);
}

// Disconnect is intentionally a parameterless, explicit native action. The
// renderer cannot choose another Provider ID, a path, or a Desktop lifecycle
// action; the backend only removes the fixed xiass_tools Provider and returns
// a redacted post-operation snapshot.
export async function removeCodexXIASSProvider() {
  return call("RemoveCodexXIASSProvider");
}

// Legacy migration is deliberately distinct from a normal save: it takes no
// key, endpoint, or other renderer-controlled target. The
// native side recognizes only one verified first-party predecessor and never
// exposes its opaque credentials back to JavaScript.
export async function migrateCodexLegacyProvider() {
  return call("MigrateCodexLegacyProvider");
}

export async function discoverCodexModels(baseURL, apiKey) {
  return call("DiscoverCodexModels", baseURL, apiKey);
}

// Saved Codex-account candidates are reduced by native code to an opaque ID,
// display label, credential-mode label and already-bound Responses model IDs.
// This module deliberately never accepts or retains an endpoint, API key,
// custom header, OAuth token or refresh state for that flow.
export async function getCodexAccountCandidates() {
  return call("GetCodexAccountCandidates");
}

export async function discoverCodexAccountModels(accountID) {
  const id = typeof accountID === "string" ? accountID.trim() : "";
  if (!id) return { ok: false, message: "请先选择一个已保存的 Codex 兼容账户。", models: [] };
  try {
    return await call("DiscoverCodexAccountModels", id);
  } finally {
    // The ID is opaque rather than secret, but keeping it request-local avoids
    // accidental retention in global reactive state during a modal refresh.
    accountID = "";
  }
}

// Apply a selected saved account entirely on the native side. Do not extend
// this DTO with endpoint or credential fields: native revalidates the account
// and maps only the lossless OpenAI Responses + Bearer contract.
export async function applyCodexConfigurationFromAccount(input) {
  return call("ApplyCodexConfigurationFromAccount", input);
}

export async function restoreCodexConfiguration(backupID) {
  return call("RestoreCodexConfiguration", backupID);
}

export async function deleteCodexConfigurationBackup(backupID) {
  return call("DeleteCodexConfigurationBackup", backupID);
}

export async function repairCodexHistory(compatibility = true) {
  return call("RepairCodexHistory", Boolean(compatibility));
}

export async function restoreCodexHistoryBackup(backupID) {
  return call("RestoreCodexHistoryBackup", backupID);
}

export async function deleteCodexHistoryBackup(backupID) {
  return call("DeleteCodexHistoryBackup", backupID);
}

// Legacy XIASS Codex Helper archives are imported only as new, current-format
// recovery points. The native layer validates the fixed legacy storage roots;
// this wrapper deliberately accepts an opaque source ID rather than a path.
export async function importCodexLegacyConfigBackup(sourceID) {
  return call("ImportCodexLegacyConfigBackup", sourceID);
}

export async function importCodexLegacyHistoryBackup(sourceID) {
  return call("ImportCodexLegacyHistoryBackup", sourceID);
}

// XIASS website Key selection is deliberately separate from OAuth. The native
// layer opens the website and retains the actual callback URL and API Key;
// Vue receives only a redacted session status and never writes it to app state
// or localStorage.
export async function startCodexXIASSKeySelection(siteURL) {
  return call("StartCodexXIASSKeySelection", siteURL);
}

export async function getCodexXIASSKeySelectionStatus(sessionID) {
  return call("GetCodexXIASSKeySelectionStatus", sessionID);
}

export async function completeCodexXIASSKeySelectionManual(sessionID, callbackURL) {
  return call("CompleteCodexXIASSKeySelectionManual", sessionID, callbackURL);
}

export async function cancelCodexXIASSKeySelection(sessionID) {
  return call("CancelCodexXIASSKeySelection", sessionID);
}

export async function discoverCodexXIASSSelectionModels(sessionID) {
  return call("DiscoverCodexXIASSSelectionModels", sessionID);
}

export async function applyCodexXIASSSelection(sessionID, config) {
  return call("ApplyCodexXIASSSelection", sessionID, config);
}

// ─── Codex Desktop control ────────────────────────────────────────────────
// Desktop control is deliberately separate from the Codex config lifecycle.
// The native layer exposes a redacted status only (no app path, process ID,
// launch arguments, credentials, or conversation data). These wrappers also
// keep older installed XIASS Tools builds harmless: a missing new binding
// becomes an explicit unavailable result rather than a renderer exception.
const codexDesktopMethodAliases = Object.freeze({
  status: ["GetCodexDesktopControlStatus", "GetCodexDesktopStatus"],
  select: ["SelectCodexDesktopInstallation", "SelectCodexDesktopApp"],
  manualPath: ["SelectCodexDesktopInstallationPath"],
  launch: ["LaunchCodexDesktop", "OpenCodexDesktop"],
  stop: ["StopCodexDesktop", "StopCodexDesktopApp"],
  restart: ["RestartCodexDesktop", "RestartCodexDesktopApp"],
});
const codexDesktopConfirmationPhrase = "CONFIRM_CODEX_DESKTOP_LIFECYCLE";

function codexDesktopUnavailableResult() {
  return {
    ok: false,
    unavailable: true,
    message: "当前安装包尚未包含 Codex Desktop 控制功能。",
  };
}

function codexDesktopFailedResult() {
  return {
    ok: false,
    unavailable: false,
    message: "Codex Desktop 操作未完成。请刷新状态后重试。",
  };
}

function codexDesktopMethod(methods) {
  const app = go();
  if (!app) return null;
  return methods.find((method) => typeof app[method] === "function") || null;
}

async function callCodexDesktop(methods, ...args) {
  const method = codexDesktopMethod(methods);
  if (!method) return codexDesktopUnavailableResult();
  try {
    // Do not log the native response. It is intentionally consumed only by
    // the local Codex modal after it has been reduced to public status fields.
    return await go()[method](...args);
  } catch {
    return codexDesktopFailedResult();
  }
}

async function callConfirmedCodexDesktopAction(methods, confirmed) {
  if (!confirmed) {
    return {
      ok: false,
      confirmationRequired: true,
      message: "请先确认此 Codex Desktop 操作。",
    };
  }
  // The native controller accepts this stable acknowledgement string. Do not
  // retry a failed lifecycle action: a stop/restart is never made implicit or
  // repeated behind the user's back.
  return callCodexDesktop(methods, codexDesktopConfirmationPhrase);
}

export async function getCodexDesktopControlStatus() {
  return callCodexDesktop(codexDesktopMethodAliases.status);
}

export async function selectCodexDesktopApp() {
  return callCodexDesktop(codexDesktopMethodAliases.select);
}

// A pasted application path remains a component-local, one-shot value. This
// wrapper has no reactive/global state and returns only the redacted native
// selection result.
export async function selectCodexDesktopPath(path) {
  const value = typeof path === "string" ? path.trim() : "";
  if (!value) {
    return {
      ok: false,
      unavailable: false,
      message: "请粘贴 Codex 或 ChatGPT Desktop 的本机应用路径。",
    };
  }
  return callCodexDesktop(codexDesktopMethodAliases.manualPath, value);
}

export async function launchCodexDesktop() {
  return callCodexDesktop(codexDesktopMethodAliases.launch);
}

export async function stopCodexDesktop(confirmed = false) {
  return callConfirmedCodexDesktopAction(codexDesktopMethodAliases.stop, Boolean(confirmed));
}

export async function restartCodexDesktop(confirmed = false) {
  return callConfirmedCodexDesktopAction(codexDesktopMethodAliases.restart, Boolean(confirmed));
}

// The lifecycle transaction is an opt-in composite operation. It preserves
// the existing lightweight save path above, while allowing a user who has
// explicitly confirmed the risk to ask native code to stop, save, optionally
// repair compatibility history, and relaunch one verified Codex Desktop app.
// `input.config` may contain a request-local API key; neither wrapper stores
// it in reactive state, browser storage, diagnostics, or console output.
const codexLifecycleMethodAliases = Object.freeze({
  manual: ["ApplyCodexConfigurationWithLifecycle"],
  savedAccount: ["ApplyCodexConfigurationFromAccountWithLifecycle"],
  xiassSelection: ["ApplyCodexXIASSSelectionWithLifecycle"],
  legacyMigration: ["MigrateCodexLegacyProviderWithLifecycle"],
});

function codexLifecycleUnavailableResult() {
  return {
    ok: false,
    unavailable: true,
    message: "当前安装包尚未包含 Codex 高级生命周期操作。",
  };
}

function codexLifecycleFailedResult() {
  return {
    ok: false,
    unavailable: false,
    message: "Codex 高级操作未完成。",
  };
}

async function callCodexLifecycle(methods, ...args) {
  const method = codexDesktopMethod(methods);
  if (!method) return codexLifecycleUnavailableResult();
  try {
    // Lifecycle responses are consumed only by the local modal after it
    // reduces them to an allowlisted status. Do not log native responses.
    return await go()[method](...args);
  } catch {
    return codexLifecycleFailedResult();
  }
}

function lifecycleInputWithConfirmation(input, confirmed) {
  return {
    ...(input && typeof input === "object" ? input : {}),
    confirmation: confirmed ? codexDesktopConfirmationPhrase : "",
  };
}

export async function applyCodexConfigurationWithLifecycle(input, confirmed = false) {
  return callCodexLifecycle(
    codexLifecycleMethodAliases.manual,
    lifecycleInputWithConfirmation(input, Boolean(confirmed)),
  );
}

export async function applyCodexConfigurationFromAccountWithLifecycle(input, confirmed = false) {
  return callCodexLifecycle(
    codexLifecycleMethodAliases.savedAccount,
    lifecycleInputWithConfirmation(input, Boolean(confirmed)),
  );
}

export async function applyCodexXIASSSelectionWithLifecycle(sessionID, input, confirmed = false) {
  return callCodexLifecycle(
    codexLifecycleMethodAliases.xiassSelection,
    sessionID,
    lifecycleInputWithConfirmation(input, Boolean(confirmed)),
  );
}

// A running Codex Desktop can only be migrated through its native, explicit
// exit/relaunch transaction. This wrapper never sends a Provider ID or any
// credential and never retries a lifecycle operation behind the user's back.
export async function migrateCodexLegacyProviderWithLifecycle(confirmed = false) {
  if (!confirmed) {
    return {
      ok: false,
      confirmationRequired: true,
      message: "请先确认 Codex Desktop 生命周期操作。",
    };
  }
  return callCodexLifecycle(
    codexLifecycleMethodAliases.legacyMigration,
    codexDesktopConfirmationPhrase,
  );
}

// ─── Claude Code user settings ─────────────────────────────────────────────
// These bindings expose only redacted settings status. The authorization token
// is intentionally supplied from the Claude modal's local component state and
// never enters this global reactive state or browser storage.
export async function getClaudeCodeConfiguration() {
  return call("GetClaudeCodeConfiguration");
}

export async function applyClaudeCodeConfiguration(input) {
  return call("ApplyClaudeCodeConfiguration", input);
}

// A saved-account candidate is intentionally redacted by the native bridge.
// The renderer receives only the opaque account ID, a local display label,
// credential-mode label and already-bound model IDs — never an API URL, key,
// header, OAuth token or refresh token.
export async function getClaudeCodeAccountCandidates() {
  return call("GetClaudeCodeAccountCandidates");
}

// Apply one explicitly selected compatible account entirely on the native
// side. Do not extend this DTO with a credential: the native bridge resolves
// it from the local account vault and clears it before returning a redacted
// settings status.
export async function applyClaudeCodeConfigurationFromAccount(input) {
  return call("ApplyClaudeCodeConfigurationFromAccount", input);
}

// Gateway discovery and connection testing deliberately accept request-local
// credentials from the Claude modal. They never read saved credentials or
// place them in global reactive state, localStorage, logs, or events.
export async function discoverClaudeCodeGatewayModels(input) {
  return call("DiscoverClaudeCodeGatewayModels", input);
}

export async function testClaudeCodeGateway(input) {
  return call("TestClaudeCodeGateway", input);
}

export async function restoreClaudeCodeConfiguration(backupID) {
  return call("RestoreClaudeCodeConfiguration", backupID);
}

export async function deleteClaudeCodeConfigurationBackup(backupID) {
  return call("DeleteClaudeCodeConfigurationBackup", backupID);
}

export async function migrateClaudeCodeLegacyBackup(source, backupID) {
  return call("MigrateClaudeCodeLegacyBackup", source, backupID);
}

// Cursor/Windsurf MCP calls keep the remote endpoint inside the modal's local
// draft. Nothing from these calls is retained in global reactive state or
// browser storage, and native status never echoes an endpoint or config path.
export async function getMCPConfiguration(target) {
  return call("GetMCPConfiguration", target);
}

export async function applyMCPConfiguration(input) {
  return call("ApplyMCPConfiguration", input);
}

// Cursor and Windsurf now expose fixed target-scoped native entry points. The
// generic calls above remain only as a compatibility fallback for an older
// installed XIASS Tools binary; a discovered scoped binding is never retried
// through the generic route after an error, so an explicit save cannot run
// twice. Remote URLs and recovery metadata stay in the modal's local state.
const mcpTargetMethods = Object.freeze({
  cursor: {
    get: ["GetCursorMCPConfiguration"],
    apply: ["ApplyCursorMCPConfiguration"],
    remove: ["RemoveCursorMCPConfiguration"],
    list: ["ListCursorMCPBackups"],
    restore: ["RestoreCursorMCPBackup"],
    delete: ["DeleteCursorMCPBackup"],
  },
  windsurf: {
    get: ["GetWindsurfMCPConfiguration"],
    apply: ["ApplyWindsurfMCPConfiguration"],
    remove: ["RemoveWindsurfMCPConfiguration"],
    list: ["ListWindsurfMCPBackups"],
    restore: ["RestoreWindsurfMCPBackup"],
    delete: ["DeleteWindsurfMCPBackup"],
  },
});

function normalizedMCPConfigurationTarget(target) {
  const value = String(target || "").trim().toLowerCase();
  return Object.prototype.hasOwnProperty.call(mcpTargetMethods, value) ? value : "";
}

function mcpTargetScopedMethod(target, action) {
  const methods = mcpTargetMethods[normalizedMCPConfigurationTarget(target)]?.[action];
  const app = go();
  if (!app || !Array.isArray(methods)) return null;
  return methods.find((method) => typeof app[method] === "function") || null;
}

function mcpTargetScopedUnavailable(action) {
  const backupAction = ["list", "restore", "delete"].includes(action);
  return {
    ok: false,
    unavailable: true,
    backups: backupAction ? [] : undefined,
    message: backupAction ? "当前安装包尚未包含 MCP 恢复点功能。" : "当前安装包尚未包含目标专用 MCP 配置功能。",
  };
}

function mcpTargetScopedFailure(action) {
  const backupAction = ["list", "restore", "delete"].includes(action);
  return {
    ok: false,
    unavailable: false,
    backups: backupAction ? [] : undefined,
    message: backupAction ? "MCP 恢复点操作未完成。" : "MCP 配置操作未完成。",
  };
}

async function callTargetScopedMCP(target, action, args = []) {
  const method = mcpTargetScopedMethod(target, action);
  if (!method) return null;
  try {
    // Never log a native MCP response: it may only be reduced to the safe
    // view-model in MCPConfigurationModal.
    return await go()[method](...args);
  } catch {
    return mcpTargetScopedFailure(action);
  }
}

export async function getTargetMCPConfiguration(target) {
  const normalized = normalizedMCPConfigurationTarget(target);
  if (!normalized) return mcpTargetScopedUnavailable("get");
  const result = await callTargetScopedMCP(normalized, "get");
  if (result !== null) return result;
  return getMCPConfiguration(normalized);
}

export async function applyTargetMCPConfiguration(target, remoteURL) {
  const normalized = normalizedMCPConfigurationTarget(target);
  if (!normalized) return mcpTargetScopedUnavailable("apply");
  const scopedInput = { remoteUrl: String(remoteURL || "") };
  try {
    const result = await callTargetScopedMCP(normalized, "apply", [scopedInput]);
    if (result !== null) return result;
    return applyMCPConfiguration({ target: normalized, remoteUrl: scopedInput.remoteUrl });
  } finally {
    scopedInput.remoteUrl = "";
  }
}

// Removing a managed MCP connection is intentionally target-scoped only. In
// contrast to the read/apply compatibility path, it must never fall back to a
// generic method in an older binary: that guarantees this action has no
// renderer-controlled target or server identifier.
export async function removeTargetMCPConfiguration(target) {
  const normalized = normalizedMCPConfigurationTarget(target);
  if (!normalized) return mcpTargetScopedUnavailable("remove");
  const result = await callTargetScopedMCP(normalized, "remove");
  return result ?? mcpTargetScopedUnavailable("remove");
}

export async function listTargetMCPBackups(target) {
  const normalized = normalizedMCPConfigurationTarget(target);
  if (!normalized) return mcpTargetScopedUnavailable("list");
  const result = await callTargetScopedMCP(normalized, "list");
  return result ?? mcpTargetScopedUnavailable("list");
}

export async function restoreTargetMCPBackup(target, backupID) {
  const normalized = normalizedMCPConfigurationTarget(target);
  if (!normalized) return mcpTargetScopedUnavailable("restore");
  const opaqueID = String(backupID || "");
  try {
    const result = await callTargetScopedMCP(normalized, "restore", [opaqueID]);
    return result ?? mcpTargetScopedUnavailable("restore");
  } finally {
    // The opaque ID is not retained in shared state or surfaced to the UI.
    // It exists only for this direct, user-confirmed native request.
  }
}

export async function deleteTargetMCPBackup(target, backupID) {
  const normalized = normalizedMCPConfigurationTarget(target);
  if (!normalized) return mcpTargetScopedUnavailable("delete");
  const opaqueID = String(backupID || "");
  try {
    const result = await callTargetScopedMCP(normalized, "delete", [opaqueID]);
    return result ?? mcpTargetScopedUnavailable("delete");
  } finally {
    // See restoreTargetMCPBackup: the opaque ID never reaches global state.
  }
}

// Cursor project MCP is intentionally a separate bridge from global MCP. The
// renderer never supplies a project path: the native directory chooser creates
// a short-lived opaque selection ID, which is the only project selector used
// by subsequent calls.
function cursorProjectMCPUnavailable(message = "当前安装包尚未包含 Cursor 项目级 MCP 配置功能。") {
  return {
    ok: false,
    unavailable: true,
    message,
    backups: [],
    snapshot: { target: "cursor" },
  };
}

function cursorProjectMCPSelectionID(value) {
  return typeof value === "string" ? value.trim() : "";
}

export async function chooseCursorProjectMCPConfiguration() {
  try {
    return await call("ChooseCursorProjectMCPConfiguration");
  } catch {
    return cursorProjectMCPUnavailable();
  }
}

export async function getCursorProjectMCPConfiguration(selectionID) {
  let opaqueID = cursorProjectMCPSelectionID(selectionID);
  if (!opaqueID) return cursorProjectMCPUnavailable("请先选择 Cursor 项目目录。");
  try {
    return await call("GetCursorProjectMCPConfiguration", opaqueID);
  } catch {
    return cursorProjectMCPUnavailable();
  } finally {
    opaqueID = "";
  }
}

export async function applyCursorProjectMCPConfiguration(selectionID, remoteURL) {
  const request = {
    selectionId: cursorProjectMCPSelectionID(selectionID),
    remoteUrl: String(remoteURL || ""),
  };
  if (!request.selectionId) return cursorProjectMCPUnavailable("请先选择 Cursor 项目目录。");
  try {
    return await call("ApplyCursorProjectMCPConfiguration", request);
  } catch {
    return cursorProjectMCPUnavailable();
  } finally {
    request.selectionId = "";
    request.remoteUrl = "";
  }
}

export async function removeCursorProjectMCPConfiguration(selectionID) {
  const request = { selectionId: cursorProjectMCPSelectionID(selectionID) };
  if (!request.selectionId) return cursorProjectMCPUnavailable("请先选择 Cursor 项目目录。");
  try {
    return await call("RemoveCursorProjectMCPConfiguration", request);
  } catch {
    return cursorProjectMCPUnavailable();
  } finally {
    request.selectionId = "";
  }
}

export async function listCursorProjectMCPBackups(selectionID) {
  const request = { selectionId: cursorProjectMCPSelectionID(selectionID) };
  if (!request.selectionId) return cursorProjectMCPUnavailable("请先选择 Cursor 项目目录。");
  try {
    return await call("ListCursorProjectMCPBackups", request);
  } catch {
    return cursorProjectMCPUnavailable();
  } finally {
    request.selectionId = "";
  }
}

export async function restoreCursorProjectMCPBackup(selectionID, backupID) {
  const request = {
    selectionId: cursorProjectMCPSelectionID(selectionID),
    backupId: String(backupID || ""),
  };
  if (!request.selectionId) return cursorProjectMCPUnavailable("请先选择 Cursor 项目目录。");
  try {
    return await call("RestoreCursorProjectMCPBackup", request);
  } catch {
    return cursorProjectMCPUnavailable();
  } finally {
    request.selectionId = "";
    request.backupId = "";
  }
}

export async function deleteCursorProjectMCPBackup(selectionID, backupID) {
  const request = {
    selectionId: cursorProjectMCPSelectionID(selectionID),
    backupId: String(backupID || ""),
  };
  if (!request.selectionId) return cursorProjectMCPUnavailable("请先选择 Cursor 项目目录。");
  try {
    return await call("DeleteCursorProjectMCPBackup", request);
  } catch {
    return cursorProjectMCPUnavailable();
  } finally {
    request.selectionId = "";
    request.backupId = "";
  }
}

let dashboardRefreshPromise = null;

// Refresh every value shown on the dashboard as one operation. Manual refresh
// forces registry/process discovery; the background startup pass can reuse a
// recent deep result so navigating back to Home remains instant.
export async function refreshDashboard({ forcePatch = true } = {}) {
	if (dashboardRefreshPromise) return dashboardRefreshPromise;
	state.dashboardRefreshing = true;
	dashboardRefreshPromise = Promise.all([
		loadPatchStatus(forcePatch ? { refresh: true } : {}),
		loadStats(),
		loadHistorySync(),
	]).then((result) => {
		if (forcePatch) state.dashboardDeepScanComplete = true;
		return result;
	}).finally(() => {
		state.dashboardRefreshing = false;
		dashboardRefreshPromise = null;
	});
	return dashboardRefreshPromise;
}

export async function applyPatch() {
  state.patchBusy = true;
  state.patchLog = "";
  state.patchProgress = { phase: "starting", operation: "全部连接", percent: 0, message: "正在准备连接" };
  try {
    const res = await call("ApplyPatch");
    state.patchLog = res?.message || "";
		if (res?.ok) {
			markPatchTargetsConnected(["ide", "agent"]);
			void loadPatchStatus();
		}
    return res;
  } finally {
    state.patchBusy = false;
  }
}

export async function applyIDEPatch() {
  state.patchBusy = true;
  state.patchLog = "";
  state.patchProgress = { phase: "starting", operation: "连接 Antigravity IDE", percent: 0, message: "正在准备连接" };
  try {
    const res = await call("ApplyIDEPatch");
    state.patchLog = res?.message || "";
		if (res?.ok) {
			markPatchTargetsConnected(["ide"]);
			void loadPatchStatus();
		}
    return res;
  } finally {
    state.patchBusy = false;
  }
}

export async function applyAgentPatch() {
  state.patchBusy = true;
  state.patchLog = "";
  state.patchProgress = { phase: "starting", operation: "连接 Antigravity 2.0", percent: 0, message: "正在准备连接" };
  try {
    const res = await call("ApplyAgentPatch");
    state.patchLog = res?.message || "";
		if (res?.ok) {
			markPatchTargetsConnected(["agent"]);
			void loadPatchStatus();
		}
    return res;
  } finally {
    state.patchBusy = false;
  }
}

export async function restorePatch() {
  state.patchBusy = true;
  state.patchLog = "";
  try {
    const res = await call("RestorePatch");
    state.patchLog = res?.message || "";
    if (res?.ok) await loadPatchStatus();
    return res;
  } finally {
    state.patchBusy = false;
  }
}

export async function launchOrRestartAntigravity(appPath) {
  state.antigravityActionBusy[appPath] = true;
  state.antigravityActionMessage = "";
  try {
    const res = await call("LaunchOrRestartAntigravity", appPath);
    state.antigravityActionMessage = res?.message || "";
		void Promise.all([loadPatchStatus(), loadHistorySync()]);
    return res;
  } finally {
    state.antigravityActionBusy[appPath] = false;
  }
}

function markPatchTargetsConnected(kinds) {
	const selected = new Set(kinds);
	state.patch.targets = (state.patch.targets || []).map((target) =>
		selected.has(target.kind) && target.supported
			? { ...target, patched: true }
			: target
	);
	if (selected.has("ide")) state.patch.idePatched = true;
	if (selected.has("agent")) state.patch.agentPatched = true;
	state.patch.productRepatchRequired = false;
	state.patch.productRepatchMessage = "";
}

export async function startProxy() {
  const res = await call("StartProxy");
  if (res?.ok) await loadPatchStatus();
  return res;
}

export async function stopProxy() {
  const res = await call("StopProxy");
  if (res?.ok) await loadPatchStatus();
  return res;
}

export async function loadAutoApproval() {
  try {
    state.autoApproval = await call("GetAutoApprovalStatus");
  } catch (e) {
    if (go()) console.error("loadAutoApproval", e);
  }
}

export async function saveAutoApproval(settings) {
  state.autoApprovalBusy = true;
  try {
    const res = await call("SetAutoApproval", settings);
    if (res?.ok) await loadAutoApproval();
    return res;
  } finally {
    state.autoApprovalBusy = false;
  }
}

export async function loadHistorySync() {
  try {
    state.historySync = await call("GetHistorySyncStatus");
    return state.historySync;
  } catch (e) {
    if (go()) console.error("loadHistorySync", e);
    return state.historySync;
  }
}

export async function syncHistoryNow() {
  state.historySyncBusy = true;
  try {
    const res = await call("SyncHistoryNow");
    await loadHistorySync();
    return res;
  } finally {
    state.historySyncBusy = false;
  }
}

export async function loadSettings() {
  state.settingsLoading = true;
  try {
    const settings = await call("GetAppSettings");
    if (settings) state.settings = settings;
    return state.settings;
  } catch (e) {
    if (go()) console.error("loadSettings", e);
    return state.settings;
  } finally {
    state.settingsLoading = false;
  }
}

export async function saveSettings(settings) {
  state.settingsBusy = true;
  try {
    const res = await call("SaveAppSettings", settings);
    if (res?.ok) await loadSettings();
    return res;
  } finally {
    state.settingsBusy = false;
  }
}

export async function exportDiagnosticLogs() {
  return call("ExportDiagnosticLogs");
}

// TOTP secrets never enter this shared state. These bridge calls are kept
// deliberately stateless so the Settings view owns the short-lived draft and
// the user-requested one-time code only for as long as it is visible.
export function getTOTPEntries() {
  return call("GetTOTPEntries");
}

export function addTOTPEntry(input) {
  return call("AddTOTPEntry", input);
}

export function generateTOTPCode(id) {
  return call("GenerateTOTPCode", id);
}

export function deleteTOTPEntry(id) {
  return call("DeleteTOTPEntry", id);
}

export function exportTOTPEncrypted(password) {
  return call("ExportTOTPEncrypted", password);
}

export function importTOTPEncrypted(password) {
  return call("ImportTOTPEncrypted", password);
}

let updateCheckGeneration = 0;

export async function checkForUpdates() {
	if (state.update.checking) {
		return { ok: false, message: "正在检查更新，请稍候或取消后重试。" };
	}
	const generation = ++updateCheckGeneration;
  state.update.checking = true;
  state.update.message = "正在检查更新…";
  try {
    const res = await call("CheckForUpdates");
		if (generation !== updateCheckGeneration) return { ok: false, message: "更新检查已取消" };
    if (res?.info) state.update.info = { ...state.update.info, ...res.info };
    state.update.message = res?.message || (res?.ok ? "检查完成" : "检查更新失败");
    return res;
  } catch (e) {
		if (generation !== updateCheckGeneration) return { ok: false, message: "更新检查已取消" };
    state.update.message = e?.message || "检查更新失败";
    return { ok: false, message: state.update.message };
  } finally {
		if (generation === updateCheckGeneration) state.update.checking = false;
  }
}

// Wails promises cannot be force-aborted by the renderer. Invalidate the
// local request first, then ask the native short-lived context to cancel; the
// interface becomes responsive even if an old bridge result arrives later.
export async function cancelUpdateCheck() {
	if (!state.update.checking) return { ok: true, message: "当前没有正在进行的更新检查" };
	updateCheckGeneration += 1;
	state.update.checking = false;
	state.update.message = "正在取消检查更新…";
	try {
		const res = await call("CancelUpdateCheck");
		state.update.message = res?.message || "已取消检查更新";
		return res || { ok: true, message: state.update.message };
	} catch (e) {
		state.update.message = e?.message || "无法取消检查；它将在 5 秒超时后自动结束。";
		return { ok: false, message: state.update.message };
	}
}

export async function skipUpdateVersion(version) {
  const res = await call("SkipUpdateVersion", version);
  if (res?.ok) {
    state.update.info.skipped = true;
    await loadSettings();
  }
  return res;
}

export async function installLatestUpdate() {
  state.update.installing = true;
  state.update.progress = { phase: "checking", downloaded: 0, total: 0, percent: 0, message: "正在验证更新信息" };
  try {
    const res = await call("InstallLatestUpdate");
    if (res?.message) state.update.message = res.message;
    return res;
  } catch (e) {
    state.update.message = e?.message || "启动更新失败";
    return { ok: false, message: state.update.message };
  } finally {
    state.update.installing = false;
  }
}

let updateEventsBound = false;
let patchEventsBound = false;

export function bindPatchEvents() {
  if (patchEventsBound) return;
  const runtime = window.runtime;
  if (typeof runtime?.EventsOn !== "function") return;
  patchEventsBound = true;
  runtime.EventsOn("wf:patch-progress", (progress) => {
    if (!progress) return;
    state.patchProgress = { ...state.patchProgress, ...progress };
  });
}

function bindUpdateEvents() {
  if (updateEventsBound) return;
  const runtime = window.runtime;
  if (typeof runtime?.EventsOn !== "function") return;
  updateEventsBound = true;
  runtime.EventsOn("wf:update-progress", (progress) => {
    if (!progress) return;
    state.update.progress = { ...state.update.progress, ...progress };
    if (progress.message) state.update.message = progress.message;
    if (progress.phase === "error" || progress.phase === "launching") {
      state.update.installing = false;
    }
  });
}

// QuitApp is intentionally separate from the window close button: closing the
// window only minimises the assistant, while this call stops the proxy and
// releases its loopback port during application shutdown.
export async function requestQuit() {
  return call("QuitApp");
}

async function waitForStartupHistorySync() {
  if (!go()) return;
  for (let attempt = 0; attempt < 120; attempt++) {
    const status = await loadHistorySync();
    if (status?.state !== "pending" && status?.state !== "running") return;
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
}

// 初始加载
export async function bootstrap() {
	bindPatchEvents();
	const [, , , , , , settings] = await Promise.all([
		loadPatchStatus({ quick: true }), loadStats(), loadModels(), loadAccounts(), loadAutoApproval(), waitForStartupHistorySync(), loadSettings(),
	]);
	bindUpdateEvents();
	if (settings?.updates?.autoCheck) void checkForUpdates();
	void loadAgentStatuses();
}
