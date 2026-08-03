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
	await Promise.all([loadPatchStatus(), loadStats(), loadModels(), loadAutoApproval(), waitForStartupHistorySync()]);
}
