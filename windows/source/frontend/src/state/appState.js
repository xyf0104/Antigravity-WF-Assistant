import { reactive, computed } from "vue";

// ─── Wails bindings ─────────────────────────────────────────────────────────
const go = () => window.go?.main?.App;

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

  // 补丁状态
  patch: {
    agentPatched: false,
    idePatched: false,
    proxyListening: false,
    proxyManaged: false,
    proxyOwned: false,
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
});

// ─── 派生状态 ─────────────────────────────────────────────────────────────────
export const statusTone = computed(() => {
  if (!state.patch.proxyListening) return "err";
	if (!state.patch.proxyManaged) return "err";
	if (state.patch.targets?.length) {
		return state.patch.targets.every((target) => target.patched) ? "ok" : "warn";
	}
  if (state.patch.agentPatched && state.patch.idePatched) return "ok";
  if (state.patch.agentPatched || state.patch.idePatched) return "warn";
  return "err";
});

export const statusLabel = computed(() => {
  if (!state.patch.proxyListening) return "代理未运行";
	if (!state.patch.proxyManaged) return "端口被旧版或其他程序占用";
	if (state.patch.targets?.length) {
		const patched = state.patch.targets.filter((target) => target.patched).length;
		if (patched === state.patch.targets.length) return "全部安装已激活";
		if (patched > 0) return `${patched}/${state.patch.targets.length} 个安装已激活`;
		return `发现 ${state.patch.targets.length} 个安装，尚未补丁`;
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

export async function loadPatchStatus() {
  state.patchLoading = true;
  try {
    const s = await call("GetPatchStatus");
    state.patch = { ...state.patch, ...(s || {}), targets: s?.targets || [] };
    state.proxyRunning = s.proxyManaged;
  } catch (e) {
    if (go()) console.error("loadPatchStatus", e);
  } finally {
    state.patchLoading = false;
  }
}

export async function applyPatch() {
  state.patchBusy = true;
  state.patchLog = "";
  try {
    const res = await call("ApplyPatch");
    state.patchLog = res?.message || "";
    if (res?.ok) await loadPatchStatus();
    return res;
  } finally {
    state.patchBusy = false;
  }
}

export async function applyIDEPatch() {
  state.patchBusy = true;
  state.patchLog = "";
  try {
    const res = await call("ApplyIDEPatch");
    state.patchLog = res?.message || "";
    if (res?.ok) await loadPatchStatus();
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
    await Promise.all([loadPatchStatus(), loadHistorySync()]);
    return res;
  } finally {
    state.antigravityActionBusy[appPath] = false;
  }
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
	const [, , , , , , settings] = await Promise.all([
		loadPatchStatus(), loadStats(), loadModels(), loadAccounts(), loadAutoApproval(), waitForStartupHistorySync(), loadSettings(),
	]);
	bindUpdateEvents();
	if (settings?.updates?.autoCheck) void checkForUpdates();
}
