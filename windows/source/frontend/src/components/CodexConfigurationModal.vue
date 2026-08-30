<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import Modal from "@/components/ui/Modal.vue";
import Button from "@/components/ui/Button.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import {
  getCodexConfiguration,
  applyCodexConfiguration,
  removeCodexXIASSProvider,
	 migrateCodexLegacyProvider,
	 migrateCodexLegacyProviderWithLifecycle,
  discoverCodexModels,
  getCodexAccountCandidates,
  discoverCodexAccountModels,
  applyCodexConfigurationFromAccount,
  applyCodexConfigurationFromAccountWithLifecycle,
  restoreCodexConfiguration,
  deleteCodexConfigurationBackup,
  repairCodexHistory,
  restoreCodexHistoryBackup,
  deleteCodexHistoryBackup,
  importCodexLegacyConfigBackup,
  importCodexLegacyHistoryBackup,
  startCodexXIASSKeySelection,
  getCodexXIASSKeySelectionStatus,
  completeCodexXIASSKeySelectionManual,
  cancelCodexXIASSKeySelection,
  discoverCodexXIASSSelectionModels,
  applyCodexXIASSSelection,
  applyCodexConfigurationWithLifecycle,
  applyCodexXIASSSelectionWithLifecycle,
  getCodexDesktopControlStatus,
  selectCodexDesktopApp,
  selectCodexDesktopPath,
  launchCodexDesktop,
  stopCodexDesktop,
  restartCodexDesktop,
} from "@/state/appState";

const props = defineProps({
  open: Boolean,
  inline: Boolean,
});

const emit = defineEmits(["close", "changed"]);

const loading = ref(false);
const saving = ref(false);
const removingXIASSProvider = ref(false);
const migratingLegacyProvider = ref(false);
const discovering = ref(false);
const repairingHistory = ref(false);
const actionID = ref("");
const error = ref("");
const notice = ref("");
const data = ref(null);
const models = ref([]);
const selectingXIASSKey = ref(false);
const completingXIASSKey = ref(false);
const xiassSiteURL = ref("https://api.xiass.com");
const xiassSelection = ref(null);
const manualCallbackURL = ref("");
const manualCodexDesktopPath = ref("");
const restartRequired = ref(false);
const contextMode = ref("372000");
const apiKeyInput = ref(null);
const desktopLoading = ref(false);
const desktopAction = ref("");
const desktopStatus = ref(createDesktopStatus());
const providerChangePending = ref(false);
const historyCompatibilityChecked = ref(false);
const lifecycleApplying = ref(false);
const lifecycleAcknowledged = ref(false);
const lifecycleLaunchAfter = ref(true);
const lifecycleRepairHistory = ref(true);
const lifecycleOutcome = ref(null);
// The native bridge returns only these redacted fields. Endpoint URLs, API
// keys, custom headers, OAuth tokens and refresh state never enter Vue.
const savedCodexAccountCandidates = ref([]);
const savedCodexAccountCandidatesLoading = ref(false);
const savedCodexAccountCandidatesMessage = ref("");
const selectedSavedCodexAccountID = ref("");
const selectedSavedCodexAccountModel = ref("");
let xiassSelectionPoll = null;

const contextPresetOptions = [
  { label: "235K", value: "235000" },
  { label: "372K", value: "372000" },
  { label: "512K", value: "512000" },
  { label: "1M", value: "1000000" },
  { label: "自定义", value: "custom" },
];
// This draft stays entirely within this component. In particular APIKey is
// reset on every close and is never copied into the global app state.
const draft = ref(createDraft());

const snapshot = computed(() => data.value?.snapshot || {});
const backupItems = computed(() => data.value?.backups || []);
const historyBackupItems = computed(() => data.value?.historyBackups || []);
const legacyBackupItems = computed(() => Array.isArray(data.value?.legacyBackups) ? data.value.legacyBackups : []);
const legacyConfigurationBackups = computed(() => legacyBackupItems.value.filter((backup) => backup?.kind === "config" && backup?.importable));
const legacyHistoryBackups = computed(() => legacyBackupItems.value.filter((backup) => backup?.kind === "history" && backup?.importable));
const legacyBackupWarning = computed(() => String(data.value?.legacyBackupWarning || "").trim());
const configLocation = computed(() => snapshot.value?.location || {});
const activeXIASSProvider = computed(() => snapshot.value?.model_provider === "xiass_tools");
// Do not turn the configuration status green only because a parsed TOML file
// names xiass_tools. The native snapshot proves its non-secret semantics
// (responses transport, bearer presence, actor header and managed top-level
// settings) before setting this flag.
const configured = computed(() => activeXIASSProvider.value && snapshot.value?.managed_provider_verified === true);
const managedProviderNeedsRepair = computed(() => activeXIASSProvider.value && !configured.value);
const hasXIASSProvider = computed(() => (Array.isArray(snapshot.value?.providers) ? snapshot.value.providers : [])
  .some((provider) => String(provider?.id || "").trim() === "xiass_tools"));
const canRemoveXIASSProvider = computed(() => configured.value || hasXIASSProvider.value);
const hasConfiguration = computed(() => Boolean(configLocation.value?.exists));
const configStatusTitle = computed(() => {
  if (configured.value) return "XIASS Tools 已配置";
  if (managedProviderNeedsRepair.value) return "XIASS Tools 配置需要修复";
  if (hasConfiguration.value) return "已发现现有 Codex 配置";
  return "尚未创建 Codex 配置";
});
const configStatusDescription = computed(() => {
  if (!data.value) return "等待读取";
  if (!data.value.ok) return "无法验证本机 config.toml。";
  if (configured.value) return "已验证 XIASS Tools 配置。";
	if (managedProviderNeedsRepair.value) return "当前 xiass_tools Provider 未通过完整性验证；请先退出 Codex，再重新保存配置或恢复已验证的备份。";
  if (hasConfiguration.value) return "已读取现有 config.toml。";
  return "尚未创建 config.toml。";
});
const xiassSelectionReady = computed(() => xiassSelection.value?.status === "ready");
const xiassSelectionPending = computed(() => xiassSelection.value?.status === "pending");
const xiassSelectionSessionID = computed(() => xiassSelection.value?.sessionId || "");
const selectedSavedCodexAccount = computed(() => savedCodexAccountCandidates.value.find((candidate) => candidate.id === selectedSavedCodexAccountID.value) || null);
const selectedSavedCodexAccountModels = computed(() => selectedSavedCodexAccount.value?.models || []);
const usingSavedCodexAccount = computed(() => Boolean(selectedSavedCodexAccount.value?.id));
const hasConfiguredCredential = computed(() => usingSavedCodexAccount.value || xiassSelectionReady.value || Boolean(draft.value.api_key?.trim()));
const legacyProviderMigration = computed(() => {
  const raw = snapshot.value?.legacy_provider_migration;
  const providerID = String(raw?.provider_id || "").trim();
  const supported = providerID === "xiass" || providerID === "codex_local_access";
  return {
    available: raw?.available === true && supported,
    providerID: supported ? providerID : "",
    wasActive: raw?.was_active === true,
  };
});
const canMigrateLegacyProvider = computed(() => data.value?.ok === true && legacyProviderMigration.value.available);
const contextWindow = computed(() => Number(draft.value.model_context_window) || 0);
const autoCompactLimit = computed(() => Number(draft.value.model_auto_compact_token_limit) || 0);
const expectedAutoCompactLimit = computed(() => Math.floor(contextWindow.value * 0.9));
const autoCompactLinked = computed(() => contextWindow.value > 0 && autoCompactLimit.value === expectedAutoCompactLimit.value);
const autoCompactPercent = computed(() => {
  if (!contextWindow.value || !autoCompactLimit.value) return 0;
  return Math.round((autoCompactLimit.value / contextWindow.value) * 100);
});
const contextSummary = computed(() => {
  const windowLabel = formatTokenCount(contextWindow.value);
  const compactLabel = formatTokenCount(autoCompactLimit.value);
  const linkLabel = autoCompactLinked.value ? "已按 90% 联动" : `当前为 ${autoCompactPercent.value}%`;
  return `当前窗口 ${windowLabel} · 自动压缩 ${compactLabel}（${linkLabel}）`;
});
const desktopBridgeAvailable = computed(() => desktopStatus.value.bridgeAvailable);
const desktopPresent = computed(() => desktopStatus.value.discovered || desktopStatus.value.selected);
const desktopRunning = computed(() => desktopStatus.value.running);
const desktopBusy = computed(() => Boolean(desktopAction.value) || lifecycleApplying.value || removingXIASSProvider.value || migratingLegacyProvider.value);
const desktopStatusLabel = computed(() => {
  if (!desktopBridgeAvailable.value) return "控制组件待更新";
  if (desktopRunning.value) return "Codex 正在运行";
  if (desktopStatus.value.selected) return "已验证选择的 App";
  if (desktopPresent.value) return "已发现 Codex Desktop";
  return "尚未发现 Codex Desktop";
});
const desktopStatusDescription = computed(() => {
  if (!desktopBridgeAvailable.value) return "当前安装包仍可安全配置 Codex；更新 XIASS Tools 后可使用桌面控制。";
  if (desktopRunning.value) return "已验证本机运行状态。涉及历史兼容的操作前，请先完成当前工作并退出 Codex。";
  if (desktopStatus.value.selected) return "已验证你选择的 Codex App。安装位置、进程信息和任何账号数据均不会显示或保存到页面。";
  if (desktopPresent.value) return "已在公开安装位置验证 Codex Desktop。可选择 App 来确认要控制的本机安装。";
  return "可手动选择 Codex App。XIASS Tools 只接受通过公开应用元数据验证的安装。";
});
const desktopCanSelect = computed(() => desktopBridgeAvailable.value && desktopStatus.value.canSelect && !desktopBusy.value);
const desktopCanLaunch = computed(() => desktopBridgeAvailable.value && desktopStatus.value.canLaunch && !desktopBusy.value);
const desktopCanStop = computed(() => desktopBridgeAvailable.value && desktopStatus.value.canStop && !desktopBusy.value);
const desktopCanRestart = computed(() => desktopBridgeAvailable.value && desktopStatus.value.canRestart && !desktopBusy.value);
const historyCompatibilityState = computed(() => {
  if (!providerChangePending.value) return "需要时可手动检查历史；XIASS Tools 不会自动扫描或重写会话。";
  if (desktopRunning.value) return "Provider 已变更：请先退出 Codex，再检查兼容历史。";
  if (historyCompatibilityChecked.value) return "Provider 变更后的兼容历史已检查；可重新打开 Codex。";
  return "Provider 已变更：Codex 已停止后，可创建恢复点并检查兼容历史。";
});
const historyRepairBlockedByDesktop = computed(() => desktopBridgeAvailable.value && desktopRunning.value);
const providerWillChange = computed(() => providerID(snapshot.value?.model_provider) !== "xiass_tools");
const lifecycleShouldRepairHistory = computed(() => providerWillChange.value && lifecycleRepairHistory.value);
const lifecycleLaunchUnavailable = computed(() => lifecycleLaunchAfter.value && !desktopPresent.value);
const lifecycleAcknowledgementCopy = computed(() => {
  if (desktopRunning.value) return "我已保存 Codex 中所有未保存的工作，并确认允许 XIASS Tools 正常退出、完成已选操作后重新启动 Codex。";
  if (lifecycleLaunchAfter.value) return "我确认允许 XIASS Tools 安全写入配置，并在成功后启动已验证的 Codex Desktop。";
  return "我确认允许 XIASS Tools 安全写入配置，并按已选择的 Provider 兼容步骤执行。";
});
const lifecycleActionLabel = computed(() => {
  if (lifecycleLaunchAfter.value && lifecycleShouldRepairHistory.value) return "安全保存、检查历史并启动 Codex";
  if (lifecycleLaunchAfter.value) return "安全保存并启动 Codex";
  if (lifecycleShouldRepairHistory.value) return "安全保存并检查兼容历史";
  return "安全保存并完成选定步骤";
});
const lifecycleActionEnabled = computed(() => (
  hasConfiguredCredential.value && lifecycleAcknowledged.value && !lifecycleApplying.value && !saving.value && !discovering.value && !desktopBusy.value && !lifecycleLaunchUnavailable.value
));
const lifecycleOutcomeTone = computed(() => {
  if (!lifecycleOutcome.value) return "";
  if (lifecycleOutcome.value.ok) return "success";
  if (lifecycleOutcome.value.rolledBack) return "rollback";
  return "error";
});
const lifecycleOutcomeTitle = computed(() => {
  const outcome = lifecycleOutcome.value;
  if (!outcome) return "";
  if (outcome.unavailable) return "高级操作暂不可用";
  if (outcome.ok) return "高级操作已完成";
  if (outcome.rolledBack) return "操作未完成，已回滚";
  if (outcome.desktopStopped && !outcome.relaunched) return "操作已停止，Codex 保持关闭";
  return "高级操作未完成";
});
const lifecycleOutcomeDescription = computed(() => lifecycleOutcomeCopy(lifecycleOutcome.value));

function createDraft(snapshot = {}) {
  const context = snapshot.context || {};
	const provider = providerByID(snapshot.providers, "xiass_tools");
	const activeProvider = providerByID(snapshot.providers, snapshot.model_provider);
  return {
		base_url: provider?.base_url || activeProvider?.base_url || "https://api.xiass.com",
    api_key: "",
		key_name: provider?.name || "XIASS Tools",
		provider_name: provider?.name || "XIASS Tools",
    model: snapshot.model || "gpt-5.6-sol",
    review_model: snapshot.review_model || snapshot.model || "gpt-5.6-sol",
    web_search: snapshot.web_search || "live",
    model_context_window: context.model_context_window || 372000,
    model_auto_compact_token_limit: context.model_auto_compact_token_limit || 334800,
  };
}

function cleanSavedCodexAccountText(value) {
  return typeof value === "string" ? value.trim() : "";
}

function normalizeSavedCodexAccountModel(value) {
  const id = cleanSavedCodexAccountText(value?.id);
  if (!id) return null;
  return { id, displayName: cleanSavedCodexAccountText(value?.displayName) || id };
}

// Treat native output as an untrusted boundary even though it is purposely
// redacted. Keep only the exact four display fields used by this modal so a
// future native field cannot accidentally enter reactive state or devtools.
function normalizeSavedCodexAccountCandidate(value) {
  const id = cleanSavedCodexAccountText(value?.id);
  if (!id) return null;
  const seenModels = new Set();
  const models = Array.isArray(value?.models)
    ? value.models
      .map(normalizeSavedCodexAccountModel)
      .filter((model) => model && !seenModels.has(model.id) && (seenModels.add(model.id), true))
    : [];
  return {
    id,
    label: cleanSavedCodexAccountText(value?.label) || "OpenAI Responses 账户",
    credentialMode: cleanSavedCodexAccountText(value?.credentialMode) || "凭据已保存",
    models,
  };
}

function clearSavedCodexAccountSelection({ clearCandidates = false } = {}) {
  selectedSavedCodexAccountID.value = "";
  selectedSavedCodexAccountModel.value = "";
  if (clearCandidates) {
    savedCodexAccountCandidates.value = [];
    savedCodexAccountCandidatesMessage.value = "";
  }
}

function savedCodexAccountRequest(candidate = selectedSavedCodexAccount.value) {
  if (!candidate?.id) return null;
  return {
    accountId: candidate.id,
    model: String(draft.value.model || "").trim(),
    reviewModel: String(draft.value.review_model || "").trim(),
    webSearch: String(draft.value.web_search || "").trim(),
    modelContextWindow: Number(draft.value.model_context_window) || 0,
    modelAutoCompactTokenLimit: Number(draft.value.model_auto_compact_token_limit) || 0,
  };
}

function clearSavedCodexAccountRequest(request) {
  if (!request || typeof request !== "object") return;
  request.accountId = "";
  request.model = "";
  request.reviewModel = "";
  request.webSearch = "";
  request.modelContextWindow = 0;
  request.modelAutoCompactTokenLimit = 0;
}

async function refreshSavedCodexAccountCandidates() {
  savedCodexAccountCandidatesLoading.value = true;
  try {
    const result = await getCodexAccountCandidates();
    const candidates = Array.isArray(result?.candidates)
      ? result.candidates.map(normalizeSavedCodexAccountCandidate).filter(Boolean)
      : [];
    savedCodexAccountCandidates.value = candidates;
    savedCodexAccountCandidatesMessage.value = cleanSavedCodexAccountText(result?.message)
      || (candidates.length ? "已读取可复用的 OpenAI Responses 账户。" : "没有可直接用于 Codex 的已启用账户。");
    const selected = candidates.find((candidate) => candidate.id === selectedSavedCodexAccountID.value);
    if (!selected) {
      clearSavedCodexAccountSelection();
    } else if (!selected.models.some((model) => model.id === selectedSavedCodexAccountModel.value)) {
      selectedSavedCodexAccountModel.value = selected.models.length === 1 ? selected.models[0].id : "";
    }
  } catch {
    savedCodexAccountCandidates.value = [];
    savedCodexAccountCandidatesMessage.value = "无法读取本机账户池；你仍可使用下方手动配置。";
    clearSavedCodexAccountSelection();
  } finally {
    savedCodexAccountCandidatesLoading.value = false;
  }
}

function selectSavedCodexAccount(candidate) {
  if (!candidate?.id || saving.value || discovering.value || lifecycleApplying.value) return;
  // Key selection and saved-account selection are mutually exclusive. A
  // browser-selected Key remains native-only, but cancelling the selection
  // avoids an ambiguous save path and clears any short-lived callback state.
  cancelXIASSKeySelection();
  draft.value.api_key = "";
  selectedSavedCodexAccountID.value = candidate.id;
  selectedSavedCodexAccountModel.value = candidate.models.length === 1 ? candidate.models[0].id : "";
  if (selectedSavedCodexAccountModel.value) chooseSavedCodexAccountModel(candidate.models[0]);
}

function chooseSavedCodexAccountModel(model) {
  if (!model?.id || saving.value || discovering.value || lifecycleApplying.value) return;
  const id = String(model.id).trim();
  if (!id) return;
  selectedSavedCodexAccountModel.value = id;
  draft.value.model = id;
  if (!String(draft.value.review_model || "").trim() || !selectedSavedCodexAccountModels.value.some((item) => item.id === draft.value.review_model)) {
    draft.value.review_model = id;
  }
}

function stopUsingSavedCodexAccount() {
  if (saving.value || discovering.value || lifecycleApplying.value) return;
  clearSavedCodexAccountSelection();
  draft.value.api_key = "";
}

function createDesktopStatus() {
  return {
    ok: false,
    bridgeAvailable: true,
    discovered: false,
    selected: false,
    running: false,
    canSelect: false,
    canLaunch: false,
    canStop: false,
    canRestart: false,
    version: "",
  };
}

function firstDesktopBoolean(values, fallback = false) {
  for (const value of values) {
    if (typeof value === "boolean") return value;
  }
  return fallback;
}

function safeDesktopVersion(value) {
  if (typeof value !== "string") return "";
  // Version is public package metadata. Limit it defensively so a malformed
  // native response can never become a display channel for local paths.
  const normalized = value.trim().replace(/[^0-9A-Za-z.+_-]/g, "");
  return normalized.slice(0, 48);
}

function normalizeDesktopStatus(response) {
  const raw = response && typeof response === "object" ? (response.status && typeof response.status === "object" ? response.status : response) : {};
  const installation = raw.installation && typeof raw.installation === "object" ? raw.installation : {};
  const hasKnownStatusField = ["ok", "unavailable", "discovered", "detected", "selected", "verifiedSelection", "userSelected", "running", "canSelect", "canLaunch", "canStop", "canRestart", "installation"]
    .some((field) => Object.prototype.hasOwnProperty.call(raw, field));
  const unavailable = raw.unavailable === true || !hasKnownStatusField;
  const discovered = firstDesktopBoolean([raw.discovered, raw.detected, installation.present]);
  const selected = firstDesktopBoolean([raw.selected, raw.verifiedSelection, raw.userSelected]);
  const running = firstDesktopBoolean([raw.running]);
  const bridgeAvailable = !unavailable;
  return {
    ok: raw.ok !== false,
    bridgeAvailable,
    discovered,
    selected,
    running,
    canSelect: firstDesktopBoolean([raw.canSelect], bridgeAvailable),
    canLaunch: firstDesktopBoolean([raw.canLaunch], bridgeAvailable && (selected || discovered)),
    canStop: firstDesktopBoolean([raw.canStop], bridgeAvailable && running),
    canRestart: firstDesktopBoolean([raw.canRestart], bridgeAvailable && running),
    version: safeDesktopVersion(installation.version || raw.version),
  };
}

function applyDesktopStatus(result) {
  const next = normalizeDesktopStatus(result);
  desktopStatus.value = next;
  return next;
}

function providerID(value) {
  return String(value || "").trim();
}

function markProviderChange(previousProvider, nextProvider) {
  const before = providerID(previousProvider);
  const after = providerID(nextProvider);
  if (!before || !after || before === after) return;
  providerChangePending.value = true;
  historyCompatibilityChecked.value = false;
}

function resetLifecycleOptions() {
  lifecycleAcknowledged.value = false;
  lifecycleLaunchAfter.value = desktopPresent.value;
  lifecycleRepairHistory.value = providerWillChange.value;
  lifecycleOutcome.value = null;
}

function normalizeLifecycleStatus(response, providerChanged) {
  const raw = response && typeof response === "object" ? response : {};
  const hasKnownStatusField = ["ok", "unavailable", "applied", "rolledBack", "desktopStopped", "relaunched", "historyRepairAttempted", "historyRepairSkipped", "historyRepaired", "configuration", "desktop"]
    .some((field) => Object.prototype.hasOwnProperty.call(raw, field));
  return {
    ok: raw.ok === true,
    unavailable: raw.unavailable === true || !hasKnownStatusField,
    applied: raw.applied === true,
    providerChanged: Boolean(providerChanged),
    desktopStopped: raw.desktopStopped === true,
    historyRepairAttempted: raw.historyRepairAttempted === true,
    historyRepairSkipped: raw.historyRepairSkipped === true,
    historyRepaired: raw.historyRepaired === true,
    rolledBack: raw.rolledBack === true,
    relaunched: raw.relaunched === true,
  };
}

function lifecycleOutcomeCopy(outcome) {
  if (!outcome) return "";
  if (outcome.unavailable) return "当前安装包尚未包含 Codex 高级生命周期操作。普通安全保存配置仍可使用。";
  if (outcome.ok) {
    const steps = ["Codex 配置已安全保存。"];
    if (outcome.historyRepairAttempted && outcome.historyRepaired) steps.push("已按你的选择检查并修复 Provider 兼容历史。");
    else if (outcome.historyRepairSkipped) steps.push("Provider 已变更，但你选择了不扫描兼容历史；现有会话没有被改写。");
    else if (!outcome.providerChanged) steps.push("Provider 未变更，未触发全量历史扫描。");
    if (outcome.relaunched) steps.push("Codex Desktop 已完成启动确认。");
    else steps.push("Codex 保持关闭；请在准备好后手动打开。");
    return steps.join(" ");
  }
  if (outcome.rolledBack && outcome.desktopStopped && !outcome.relaunched) {
    return "高级操作未完成，已回滚可验证的配置与历史变更。Codex 已保持关闭；请先检查配置，再手动恢复或启动 Codex。";
  }
  if (outcome.rolledBack) return "高级操作未完成，已回滚可验证的配置与历史变更。请重新检查设置后再试。";
  if (outcome.desktopStopped && !outcome.relaunched) return "高级操作未完成，Codex 已保持关闭。需要你先手动恢复或检查配置后再启动 Codex。";
  if (!outcome.applied) return "高级操作在写入前被安全阻止；当前配置保持不变。";
  return "高级操作未能完整完成。为保护当前状态，Codex 没有被自动启动；请检查后手动恢复。";
}

function applyLifecycleStatus(response, providerChanged) {
  const raw = response && typeof response === "object" ? response : {};
  const outcome = normalizeLifecycleStatus(raw, providerChanged);
  lifecycleOutcome.value = outcome;
  if (raw.desktop && typeof raw.desktop === "object") applyDesktopStatus(raw.desktop);
  // The lifecycle backend projects the same redacted configuration DTO used
  // by the ordinary save flow. Keep only that projection, never the complete
  // transaction response or its native diagnostic details.
  if (raw.configuration && typeof raw.configuration === "object") data.value = raw.configuration;
  return outcome;
}

function providerBaseURL(providers, providerID) {
	return providerByID(providers, providerID)?.base_url || "";
}

function providerByID(providers, providerID) {
	if (!Array.isArray(providers) || !providerID) return null;
	return providers.find((provider) => provider.id === providerID) || null;
}

function contextModeFor(value) {
  const normalized = String(Number(value) || "");
  return contextPresetOptions.some((option) => option.value === normalized) ? normalized : "custom";
}

function setContextMode(value) {
  contextMode.value = String(value);
  if (value !== "custom") syncContextWindow(Number(value));
}

function syncCustomContextWindow() {
  contextMode.value = "custom";
  syncContextWindow(draft.value.model_context_window);
}

function syncContextWindow(value) {
  const parsed = Math.round(Number(value));
  if (!Number.isFinite(parsed)) return;
  const normalized = Math.min(1050000, Math.max(64000, parsed));
  draft.value.model_context_window = normalized;
  draft.value.model_auto_compact_token_limit = Math.floor(normalized * 0.9);
}

function formatTokenCount(value) {
  const tokens = Number(value) || 0;
  if (tokens >= 1000000) {
    const millions = tokens / 1000000;
    return `${Number.isInteger(millions) ? millions : millions.toFixed(2).replace(/0+$/, "").replace(/\.$/, "")}M`;
  }
  if (tokens >= 1000) {
    const thousands = tokens / 1000;
    return `${Number.isInteger(thousands) ? thousands : thousands.toFixed(1).replace(/\.0$/, "")}K`;
  }
  return `${tokens}`;
}

function applyRestartGuidance(message, { providerChanged = false } = {}) {
  restartRequired.value = true;
  notice.value = [
    message || "Codex 配置已安全写入。",
    providerChanged ? "Provider 已变更：请先退出 Codex，再按需检查兼容历史，最后重新打开应用。" : "请在完成当前工作后重新启动 Codex，使 config.toml 变更生效。",
  ]
    .filter(Boolean)
    .join(" ");
}

async function load() {
  loading.value = true;
  error.value = "";
	 restartRequired.value = false;
  providerChangePending.value = false;
  historyCompatibilityChecked.value = false;
  resetLifecycleOptions();
  try {
    const [result] = await Promise.all([
      getCodexConfiguration(),
      refreshCodexDesktopStatus(),
		  refreshSavedCodexAccountCandidates(),
    ]);
    data.value = result;
    if (!result?.ok) {
      error.value = "无法读取或验证本机 config.toml。请检查配置后重试。";
      return;
    }
    const next = createDraft(result.snapshot || {});
    // Preserve only non-secret, in-progress form values when reopening after
    // a discovery; the key itself always begins blank.
    draft.value = { ...next, api_key: "" };
		contextMode.value = contextModeFor(next.model_context_window);
    resetLifecycleOptions();
  } catch (cause) {
    error.value = "无法读取或验证本机 config.toml。请检查配置后重试。";
  } finally {
    loading.value = false;
  }
}

async function refreshCodexDesktopStatus() {
  desktopLoading.value = true;
  try {
    return applyDesktopStatus(await getCodexDesktopControlStatus());
  } catch {
    // The state wrapper already converts unavailable or failed bridge calls to
    // a bounded result. Keep this final guard so opening a configuration modal
    // cannot fail simply because an older desktop bridge is absent.
    return applyDesktopStatus({ ok: false, unavailable: true });
  } finally {
    desktopLoading.value = false;
  }
}

function desktopActionFailure(action, status) {
  if (status?.unavailable || !status?.bridgeAvailable) {
    error.value = "当前安装包尚未包含 Codex Desktop 控制功能。配置和备份功能不受影响。";
    return;
  }
  if (action === "select" || action === "manual-path") {
    error.value = "未能验证所选 Codex App。现有配置和会话没有被更改。";
    return;
  }
  error.value = "Codex Desktop 操作未完成。请刷新状态后再试。";
}

async function useManualCodexDesktopPath() {
  if (removingXIASSProvider.value || desktopBusy.value) return;
  error.value = "";
  notice.value = "";
  let selectedPath = manualCodexDesktopPath.value.trim();
  // Do not retain a filesystem path in component state while the native
  // validator runs. It is never copied into appState, logs, or diagnostics.
  manualCodexDesktopPath.value = "";
  if (!selectedPath) {
    error.value = "请粘贴 Codex.app、Codex.exe 或 ChatGPT.exe 的本机路径。";
    return;
  }

  desktopAction.value = "manual-path";
  try {
    const status = applyDesktopStatus(await selectCodexDesktopPath(selectedPath));
    if (!status.ok) {
      desktopActionFailure("manual-path", status);
      return;
    }
    const refreshed = await refreshCodexDesktopStatus();
    if (!refreshed.bridgeAvailable) {
      desktopActionFailure("manual-path", refreshed);
      return;
    }
    notice.value = "已验证手动粘贴的 Codex Desktop 应用路径。";
    emit("changed");
  } catch {
    desktopActionFailure("manual-path", desktopStatus.value);
  } finally {
    selectedPath = "";
    desktopAction.value = "";
  }
}

async function runDesktopAction(action) {
  if (removingXIASSProvider.value) return;
  error.value = "";
  notice.value = "";
  desktopAction.value = action;
  try {
    let result;
    if (action === "select") result = await selectCodexDesktopApp();
    if (action === "launch") result = await launchCodexDesktop();
    if (action === "stop") result = await stopCodexDesktop(true);
    if (action === "restart") result = await restartCodexDesktop(true);
    const status = applyDesktopStatus(result);
    if (!status.ok) {
      desktopActionFailure(action, status);
      return;
    }
    // Always pull a fresh, redacted observation after an explicit lifecycle
    // action. The UI never trusts a cached running state before history work.
    const refreshed = await refreshCodexDesktopStatus();
    if (!refreshed.bridgeAvailable) {
      desktopActionFailure(action, refreshed);
      return;
    }
    notice.value = {
      select: "Codex App 已通过本机验证。",
      launch: "已请求打开 Codex Desktop。",
      stop: "Codex Desktop 已退出。现在可以安全检查兼容历史。",
      restart: "已请求重新启动 Codex Desktop。",
    }[action] || "Codex Desktop 状态已更新。";
    if (action === "launch" || action === "restart") restartRequired.value = false;
    emit("changed");
  } catch {
    desktopActionFailure(action, desktopStatus.value);
  } finally {
    desktopAction.value = "";
  }
}

function confirmDesktopAction(action) {
  const copy = {
    stop: "将退出 Codex Desktop。未保存的编辑可能丢失；请先确认当前工作已保存。退出后可安全检查兼容历史，是否继续？",
    restart: "将退出并重新打开 Codex Desktop。未保存的编辑可能丢失；请先确认当前工作已保存，是否继续？",
  };
  if (!window.confirm(copy[action] || "确认继续此 Codex Desktop 操作吗？")) return;
  void runDesktopAction(action);
}

function stopXIASSSelectionPolling() {
  if (xiassSelectionPoll) {
    window.clearInterval(xiassSelectionPoll);
    xiassSelectionPoll = null;
  }
}

function applyXIASSSelectionStatus(result) {
  const selection = result?.selection;
  if (!selection) return;
  xiassSelection.value = selection;
  if (selection.status === "ready") {
	clearSavedCodexAccountSelection();
    draft.value.api_key = "";
    if (selection.baseUrl) draft.value.base_url = selection.baseUrl;
    if (selection.keyName && (!draft.value.provider_name?.trim() || draft.value.provider_name === "XIASS Tools")) draft.value.provider_name = selection.keyName;
  }
  if (selection.status !== "pending") stopXIASSSelectionPolling();
}

async function refreshXIASSSelectionStatus() {
  const sessionID = xiassSelectionSessionID.value;
  if (!sessionID || !xiassSelectionPending.value) {
    stopXIASSSelectionPolling();
    return;
  }
  try {
    const result = await getCodexXIASSKeySelectionStatus(sessionID);
    applyXIASSSelectionStatus(result);
    if (!result?.ok && result?.selection?.status !== "pending") {
      error.value = "XIASS API Key 选择未完成。请返回浏览器完成选择后重试。";
    }
  } catch {
    stopXIASSSelectionPolling();
    error.value = "无法读取 XIASS API Key 选择状态。请返回浏览器完成选择后重试。";
  }
}

async function startXIASSKeySelection() {
  error.value = "";
  notice.value = "";
  clearSavedCodexAccountSelection();
  draft.value.api_key = "";
  stopXIASSSelectionPolling();
  const oldSessionID = xiassSelectionSessionID.value;
  if (oldSessionID) {
    void cancelCodexXIASSKeySelection(oldSessionID);
  }
  xiassSelection.value = null;
  selectingXIASSKey.value = true;
  try {
    const result = await startCodexXIASSKeySelection(xiassSiteURL.value);
    applyXIASSSelectionStatus(result);
    if (!result?.ok || !result?.selection?.sessionId) {
      error.value = "无法打开 XIASS API Key 选择页。请检查网络后重试。";
      return;
    }
    notice.value = "已在浏览器打开 XIASS API Key 选择页。";
    xiassSelectionPoll = window.setInterval(() => void refreshXIASSSelectionStatus(), 1000);
  } catch (cause) {
    error.value = "无法打开 XIASS API Key 选择页。请检查网络后重试。";
  } finally {
    selectingXIASSKey.value = false;
  }
}

async function completeXIASSKeySelectionManually() {
  const sessionID = xiassSelectionSessionID.value;
  const callbackURL = manualCallbackURL.value.trim();
  if (!sessionID || !callbackURL) {
    error.value = "请先打开 XIASS API Key 选择页，再粘贴浏览器中的完整回调地址。";
    return;
  }
  error.value = "";
  notice.value = "";
  completingXIASSKey.value = true;
  // The callback URL can contain the selected API Key in its fragment. Keep it
  // only in this short-lived component field and clear it before rendering any
  // result, regardless of success or failure.
  manualCallbackURL.value = "";
  try {
    const result = await completeCodexXIASSKeySelectionManual(sessionID, callbackURL);
    applyXIASSSelectionStatus(result);
    if (!result?.ok) {
      error.value = "未能完成 XIASS API Key 选择。请返回浏览器完成选择后重试。";
      return;
    }
    notice.value = "已安全接收 XIASS API Key。";
  } catch (cause) {
    error.value = "未能完成 XIASS API Key 选择。请返回浏览器完成选择后重试。";
  } finally {
    completingXIASSKey.value = false;
  }
}

function cancelXIASSKeySelection() {
  const sessionID = xiassSelectionSessionID.value;
  stopXIASSSelectionPolling();
  xiassSelection.value = null;
  manualCallbackURL.value = "";
  if (sessionID) void cancelCodexXIASSKeySelection(sessionID);
}

async function discoverModels() {
  error.value = "";
  notice.value = "";
  if (!hasConfiguredCredential.value) {
    error.value = "请先选择已保存的兼容账户，或输入用于此次请求的 API Key。";
    return;
  }
  discovering.value = true;
  try {
    const selectedAccount = selectedSavedCodexAccount.value;
    const result = selectedAccount?.id
      ? await discoverCodexAccountModels(selectedAccount.id)
      : xiassSelectionReady.value
        ? await discoverCodexXIASSSelectionModels(xiassSelectionSessionID.value)
        : await discoverCodexModels(draft.value.base_url, draft.value.api_key);
    if (!result?.ok) {
      error.value = selectedAccount?.id
        ? "获取所选账户的 Codex 上游模型失败。请刷新账户池或检查上游后重试。"
        : "获取上游模型失败。请检查 API 地址与本次 Key 后重试。";
      return;
    }
    models.value = result.models || [];
    if (models.value.length && !models.value.includes(draft.value.model)) {
      draft.value.model = models.value[0];
    }
    if (models.value.length && !models.value.includes(draft.value.review_model)) {
      draft.value.review_model = draft.value.model;
    }
    notice.value = `已发现 ${models.value.length} 个模型 ID（尚未验证 Responses 推理）。`;
  } catch (cause) {
    error.value = usingSavedCodexAccount.value
      ? "获取所选账户的 Codex 上游模型失败。请刷新账户池或检查上游后重试。"
      : "获取上游模型失败。请检查 API 地址与本次 Key 后重试。";
  } finally {
    discovering.value = false;
  }
}

async function save() {
  if (removingXIASSProvider.value || migratingLegacyProvider.value) return;
  error.value = "";
  notice.value = "";
  if (!hasConfiguredCredential.value) {
    error.value = "为安全写入 Codex 配置，请选择已保存的兼容账户，或输入 API Key。";
    return;
  }
  saving.value = true;
  const previousProvider = providerID(snapshot.value?.model_provider);
	const selectedAccount = selectedSavedCodexAccount.value;
	const accountRequest = selectedAccount?.id ? savedCodexAccountRequest(selectedAccount) : null;
  try {
    const result = accountRequest
      ? await applyCodexConfigurationFromAccount(accountRequest)
      : xiassSelectionReady.value
        ? await applyCodexXIASSSelection(xiassSelectionSessionID.value, { ...draft.value, api_key: "", base_url: "" })
        : await applyCodexConfiguration({ ...draft.value });
    data.value = result;
    if (!result?.ok) {
      error.value = accountRequest
        ? "未将所选账户应用到 Codex。请刷新账户池后重新选择，或改用手动配置。"
        : "保存 Codex 配置失败。请检查填写内容与 config.toml 状态后重试。";
      return;
    }
    draft.value.api_key = "";
    if (xiassSelectionReady.value) {
      stopXIASSSelectionPolling();
      xiassSelection.value = { ...xiassSelection.value, status: "applied", message: "已安全保存 Codex 配置。" };
    }
    markProviderChange(previousProvider, "xiass_tools");
		applyRestartGuidance("Codex 配置已安全保存。", { providerChanged: providerChangePending.value });
    emit("changed");
  } catch (cause) {
    error.value = accountRequest
      ? "未将所选账户应用到 Codex。请刷新账户池后重新选择，或改用手动配置。"
      : "保存 Codex 配置失败。请检查填写内容与 config.toml 状态后重试。";
  } finally {
	clearSavedCodexAccountRequest(accountRequest);
    saving.value = false;
  }
}

async function removeXIASSProvider() {
	if (migratingLegacyProvider.value) return;
  error.value = "";
  notice.value = "";
  const wasActive = activeXIASSProvider.value;
  removingXIASSProvider.value = true;
  try {
    const result = await removeCodexXIASSProvider();
    data.value = result;
    if (!result?.ok) {
      error.value = "移除 XIASS Tools Codex Provider 未完成。当前配置保持不变；请检查 config.toml 后重试。";
      return;
    }

    // A disconnect must also discard request-local credentials from this
    // component. It does not inspect or revoke any Codex account credential.
    cancelXIASSKeySelection();
    models.value = [];
    const next = createDraft(result.snapshot || {});
    draft.value = { ...next, api_key: "" };
    contextMode.value = contextModeFor(next.model_context_window);
    providerChangePending.value = false;
    historyCompatibilityChecked.value = false;
    resetLifecycleOptions();
    restartRequired.value = wasActive;
    notice.value = wasActive
      ? "XIASS Tools Codex Provider 已移除。当前模型选择已清除；请在完成当前工作后自行重新启动 Codex。"
      : "XIASS Tools Codex Provider 已移除；其他 Provider 和当前模型选择保持不变。";
    emit("changed");
  } catch {
    error.value = "移除 XIASS Tools Codex Provider 未完成。当前配置保持不变；请检查 config.toml 后重试。";
  } finally {
    removingXIASSProvider.value = false;
  }
}

function resetAfterLegacyProviderMigration(result, previousProvider, { relaunched = false, wasActive = false } = {}) {
  const next = createDraft(result?.snapshot || {});
  // The migration never consumes the form's credential. Clear any local
  // draft anyway so an explicit config rewrite cannot leave a secret around
  // in an otherwise unrelated browser form.
  draft.value = { ...next, api_key: "" };
  contextMode.value = contextModeFor(next.model_context_window);
  models.value = [];
  cancelXIASSKeySelection();
  markProviderChange(previousProvider, result?.snapshot?.model_provider);
  historyCompatibilityChecked.value = false;
  resetLifecycleOptions();
  restartRequired.value = wasActive && !relaunched;
}

async function migrateLegacyProvider() {
  if (!canMigrateLegacyProvider.value || migratingLegacyProvider.value) return;
  error.value = "";
  notice.value = "";
  migratingLegacyProvider.value = true;
  const previousProvider = providerID(snapshot.value?.model_provider);
  const migrationSource = legacyProviderMigration.value.providerID;
	const migrationWasActive = legacyProviderMigration.value.wasActive;
  try {
    // Never infer safety from the modal-open snapshot. A fresh lifecycle
    // observation decides whether this explicit action must use the confirmed
    // native exit/relaunch transaction; the direct native method rechecks too.
    const desktop = await refreshCodexDesktopStatus();
    if (desktop.bridgeAvailable && desktop.running) {
      const lifecycle = await migrateCodexLegacyProviderWithLifecycle(true);
      const configuration = lifecycle?.configuration;
      if (configuration && typeof configuration === "object") data.value = configuration;
      if (lifecycle?.desktop && typeof lifecycle.desktop === "object") applyDesktopStatus(lifecycle.desktop);
      if (!lifecycle?.ok || lifecycle?.migrated !== true) {
        error.value = lifecycle?.rolledBack
          ? "旧 Provider 迁移未完成，已回滚可验证的配置变更。Codex 已恢复到安全状态。"
          : "旧 Provider 迁移未完成。为保护当前配置，Codex 没有被强制结束或自动继续。";
        return;
      }
		resetAfterLegacyProviderMigration(data.value, previousProvider, { relaunched: lifecycle?.relaunched === true, wasActive: migrationWasActive });
      notice.value = `已将一个已验证的旧版 Provider（${migrationSource}）迁移至 XIASS Tools；未读取或显示其凭据，也未扫描会话。`;
      emit("changed");
      return;
    }

    const result = await migrateCodexLegacyProvider();
    data.value = result;
    if (!result?.ok) {
      error.value = "旧 Provider 迁移未完成。当前配置保持不变；请先退出 Codex 或使用确认后的生命周期操作后重试。";
      return;
    }
    if (result?.legacyProviderMigrationCompleted !== true) {
      error.value = "未发现可安全迁移的旧版 Provider；当前配置没有被更改。";
      return;
    }
		resetAfterLegacyProviderMigration(result, previousProvider, { wasActive: migrationWasActive });
    notice.value = `已将一个已验证的旧版 Provider（${migrationSource}）迁移至 XIASS Tools；未读取或显示其凭据，也未扫描会话。`;
    emit("changed");
  } catch {
    error.value = "旧 Provider 迁移未完成。当前配置保持不变；请先退出 Codex 或使用确认后的生命周期操作后重试。";
  } finally {
    migratingLegacyProvider.value = false;
  }
}

function confirmLegacyProviderMigration() {
  if (!canMigrateLegacyProvider.value) return;
  const source = legacyProviderMigration.value.providerID;
  const desktopDetail = desktopRunning.value
    ? "Codex Desktop 正在运行。此操作会先正常请求退出，迁移成功后再重新启动；不会强制结束进程。"
    : "Codex Desktop 未在运行。操作会在原生层再次确认安全后才写入 config.toml。";
  const activeDetail = legacyProviderMigration.value.wasActive
    ? "当前模型选择会从该旧 Provider 切换为 xiass_tools，模型、审查模型、上下文和联网搜索设置将保持不变。"
    : "该旧 Provider 当前未被使用；正在使用的其他 Provider、模型和设置将保持不变。";
  if (!window.confirm(`${desktopDetail} 将仅迁移一个已验证的旧版 Provider（${source}），并先创建恢复备份。${activeDetail} 不会读取 auth.json、OAuth、Cookie、会话或其他 Provider。是否继续？`)) return;
  void migrateLegacyProvider();
}

function confirmRemoveXIASSProvider() {
  if (!canRemoveXIASSProvider.value) return;
  const confirmation = activeXIASSProvider.value
    ? "将移除 XIASS Tools 的 xiass_tools Codex Provider，并清除当前默认模型和审查模型选择。其他 Provider、MCP、Desktop、未知设置和历史会话不会被修改；当前配置会先创建恢复备份。该操作不会退出、重启或打开 Codex。是否继续？"
    : "将移除 XIASS Tools 的 xiass_tools Codex Provider。其他 Provider、当前模型选择、MCP、Desktop、未知设置和历史会话不会被修改；当前配置会先创建恢复备份。该操作不会退出、重启或打开 Codex。是否继续？";
  if (!window.confirm(confirmation)) return;
  void removeXIASSProvider();
}

function confirmLifecycleApply() {
  if (removingXIASSProvider.value) return;
  error.value = "";
  notice.value = "";
  lifecycleOutcome.value = null;
  if (!hasConfiguredCredential.value) {
    error.value = "高级操作需要已保存的兼容账户、本次 API Key，或先通过 XIASS API 网站完成安全选择。";
    if (!usingSavedCodexAccount.value) void nextTick(() => apiKeyInput.value?.focus());
    return;
  }
  if (!lifecycleAcknowledged.value) {
    error.value = "请先确认已保存工作，并确认本次高级操作的启动或退出风险。";
    return;
  }
  if (lifecycleLaunchUnavailable.value) {
    error.value = "尚未发现已验证的 Codex Desktop。请先选择并验证 App，或取消“完成后启动 Codex Desktop”。";
    return;
  }
  const historyDetail = lifecycleShouldRepairHistory.value
    ? "将检查 Provider 兼容历史并创建可恢复备份。"
    : providerWillChange.value
      ? "不会扫描或改写兼容历史。"
      : "Provider 未变更，不会扫描历史。";
  const desktopDetail = desktopRunning.value
    ? "Codex 正在运行，事务会先正常请求退出；未保存的工作可能丢失。"
    : lifecycleLaunchAfter.value
      ? "事务成功后会启动已验证的 Codex Desktop。"
      : "事务完成后会保持 Codex 关闭。";
  if (!window.confirm(`${desktopDetail} ${historyDetail} 失败时会优先回滚可验证的变更。确认继续吗？`)) return;
  void applyLifecycleConfiguration();
}

async function applyLifecycleConfiguration() {
  error.value = "";
  notice.value = "";
  lifecycleApplying.value = true;
  const previousProvider = providerID(snapshot.value?.model_provider);
  const providerChanged = providerWillChange.value;
	const selectedAccount = selectedSavedCodexAccount.value;
	const accountRequest = selectedAccount?.id ? savedCodexAccountRequest(selectedAccount) : null;
  const lifecycleConfig = xiassSelectionReady.value
    ? { ...draft.value, api_key: "", base_url: "" }
    : { ...draft.value };
  const lifecycleInput = accountRequest
		? {
			account: accountRequest,
			repairHistoryOnProviderChange: providerChanged && lifecycleRepairHistory.value,
			launchAfter: lifecycleLaunchAfter.value,
		}
		: {
			config: lifecycleConfig,
			repairHistoryOnProviderChange: providerChanged && lifecycleRepairHistory.value,
			launchAfter: lifecycleLaunchAfter.value,
		};
  try {
    const result = accountRequest
		? await applyCodexConfigurationFromAccountWithLifecycle(lifecycleInput, true)
      : xiassSelectionReady.value
        ? await applyCodexXIASSSelectionWithLifecycle(xiassSelectionSessionID.value, lifecycleInput, true)
        : await applyCodexConfigurationWithLifecycle(lifecycleInput, true);
    const outcome = applyLifecycleStatus(result, providerChanged);
    if (!outcome.ok) {
      error.value = lifecycleOutcomeCopy(outcome);
      return;
    }

    // A successful direct-Key transaction must not leave a copy in the local
    // draft. The browser-selected credential remains native-only throughout.
    draft.value.api_key = "";
    if (xiassSelectionReady.value) {
      stopXIASSSelectionPolling();
      xiassSelection.value = { ...xiassSelection.value, status: "applied", message: "已安全完成 Codex 高级操作。" };
    }
    markProviderChange(previousProvider, "xiass_tools");
    historyCompatibilityChecked.value = outcome.historyRepaired;
    restartRequired.value = !outcome.relaunched;
    const next = createDraft(data.value?.snapshot || {});
    draft.value = { ...next, api_key: "" };
    contextMode.value = contextModeFor(next.model_context_window);
    notice.value = lifecycleOutcomeCopy(outcome);
    await Promise.allSettled([
      refreshAfterHistoryAction(),
      refreshCodexDesktopStatus(),
    ]);
    emit("changed");
  } catch {
    const outcome = applyLifecycleStatus({ ok: false }, providerChanged);
    error.value = lifecycleOutcomeCopy(outcome);
  } finally {
    // The nested request copy is short-lived even when an operation fails.
	if (lifecycleInput?.config) lifecycleInput.config.api_key = "";
	clearSavedCodexAccountRequest(accountRequest);
    lifecycleAcknowledged.value = false;
    lifecycleApplying.value = false;
  }
}

async function runConfigurationBackupAction(action, backupID) {
  if (removingXIASSProvider.value) return;
  error.value = "";
  notice.value = "";
  // A restore writes config.toml, so perform a fresh observation rather than
  // relying on the modal-open state. The native layer repeats this guard so a
  // direct bridge call or a race cannot restore under a running Codex Desktop.
  if (action === "restore") {
    const desktop = await refreshCodexDesktopStatus();
    if (desktop.bridgeAvailable && desktop.running) {
      error.value = "Codex Desktop 正在运行。请先完成当前工作并退出 Codex，再恢复配置备份。";
      return;
    }
  }
  actionID.value = `config:${action}:${backupID}`;
  const previousProvider = providerID(snapshot.value?.model_provider);
  try {
    const result = action === "restore"
      ? await restoreCodexConfiguration(backupID)
      : await deleteCodexConfigurationBackup(backupID);
    data.value = result;
    if (!result?.ok) {
      error.value = "配置备份操作失败。请确认备份仍可用后重试。";
      return;
    }
	if (action === "restore") {
		const next = createDraft(result.snapshot || {});
		draft.value = { ...next, api_key: "" };
		contextMode.value = contextModeFor(next.model_context_window);
		markProviderChange(previousProvider, result.snapshot?.model_provider);
		const restoredLegacyProvider = result?.legacyProviderMigrationCompleted === true;
		applyRestartGuidance(
			restoredLegacyProvider
				? "已恢复 Codex 配置备份，并完成已验证旧版 XIASS Provider 的前向迁移。"
				: "已恢复 Codex 配置备份。",
			{ providerChanged: providerChangePending.value },
		);
	} else {
			notice.value = "已删除选定的 Codex 配置备份。";
		}
    emit("changed");
  } catch (cause) {
    error.value = "配置备份操作失败。请确认备份仍可用后重试。";
  } finally {
    actionID.value = "";
  }
}

async function repairHistory() {
  if (removingXIASSProvider.value) return;
  error.value = "";
  notice.value = "";
  // A fresh process observation is required immediately before a history
  // operation. We never infer safety from a prior modal-open snapshot.
  const desktop = await refreshCodexDesktopStatus();
  if (desktop.bridgeAvailable && desktop.running) {
    error.value = "Codex Desktop 仍在运行。请先完成当前工作并退出 Codex，再检查兼容历史。";
    return;
  }
  repairingHistory.value = true;
  try {
    const result = await repairCodexHistory(true);
    if (!result?.ok) {
      error.value = "Codex 历史检查或修复未完成。请先退出 Codex，并确认 config.toml 有效后重试。";
      return;
    }
    notice.value = "Codex 历史已检查；如有变更，已创建可恢复备份。";
    if (providerChangePending.value) historyCompatibilityChecked.value = true;
    await refreshAfterHistoryAction();
    emit("changed");
  } catch (cause) {
    error.value = "Codex 历史检查或修复未完成。请先退出 Codex，并确认 config.toml 有效后重试。";
  } finally {
    repairingHistory.value = false;
  }
}

async function runHistoryBackupAction(action, backupID) {
  if (removingXIASSProvider.value) return;
  error.value = "";
  notice.value = "";
  if (action === "restore") {
    const desktop = await refreshCodexDesktopStatus();
    if (desktop.bridgeAvailable && desktop.running) {
      error.value = "Codex Desktop 仍在运行。请先完成当前工作并退出 Codex，再恢复兼容历史。";
      return;
    }
  }
  actionID.value = `history:${action}:${backupID}`;
  try {
    const result = action === "restore"
      ? await restoreCodexHistoryBackup(backupID)
      : await deleteCodexHistoryBackup(backupID);
    data.value = result;
    if (!result?.ok) {
      error.value = "历史备份操作失败。请确认备份仍可用后重试。";
      return;
    }
    notice.value = action === "restore" ? "已恢复选定的 Codex 历史备份。" : "已删除选定的 Codex 历史备份。";
    if (action === "restore" && providerChangePending.value) historyCompatibilityChecked.value = true;
    emit("changed");
  } catch (cause) {
    error.value = "历史备份操作失败。请确认备份仍可用后重试。";
  } finally {
    actionID.value = "";
  }
}

async function refreshAfterHistoryAction() {
  const result = await getCodexConfiguration();
  if (result) data.value = result;
}

async function runLegacyBackupImport(kind, sourceID) {
  if (removingXIASSProvider.value) return;
  error.value = "";
  notice.value = "";
  actionID.value = `legacy:${kind}:${sourceID}`;
  try {
    const result = kind === "config"
      ? await importCodexLegacyConfigBackup(sourceID)
      : await importCodexLegacyHistoryBackup(sourceID);
    data.value = result;
    if (!result?.ok) {
      error.value = "导入旧版备份失败。该备份未通过安全校验或未能完成导入。";
      return;
    }

    // The import response is already refreshed by the native layer. Refresh
    // once more when available so the newly created recovery point appears in
    // the existing backup list without exposing backend diagnostics.
    try {
      await refreshAfterHistoryAction();
    } catch {
      // Preserve the verified import response when a follow-up status refresh
      // is temporarily unavailable; the import itself has completed safely.
    }
    notice.value = kind === "config"
      ? "旧版配置备份已导入为新的恢复点。"
      : "旧版历史备份已导入为新的恢复点。";
    emit("changed");
  } catch {
    error.value = "导入旧版备份失败。该备份未通过安全校验或未能完成导入。";
  } finally {
    actionID.value = "";
  }
}

function confirmLegacyConfigurationImport(sourceID) {
  if (!window.confirm("将导入这份旧版 Codex 配置备份，仅创建新的恢复点；不会自动恢复或修改当前配置，是否继续？")) return;
  void runLegacyBackupImport("config", sourceID);
}

function confirmLegacyHistoryImport(sourceID) {
  if (!window.confirm("将导入这份旧版 Codex 历史备份，仅创建新的恢复点；不会自动恢复或修改当前会话，是否继续？")) return;
  void runLegacyBackupImport("history", sourceID);
}

function confirmConfigurationRestore(backupID) {
	if (!window.confirm("将恢复这份 Codex 配置备份。请先确认 Codex 已退出；当前配置会先自动备份，完成后可重新启动 Codex，是否继续？")) return;
  void runConfigurationBackupAction("restore", backupID);
}

function confirmHistoryRestore(backupID) {
  if (!window.confirm("将恢复这份 Codex 历史备份。请先确认 Codex 已退出；只会恢复该备份记录的本地会话修改，是否继续？")) return;
  void runHistoryBackupAction("restore", backupID);
}

function confirmConfigurationDelete(backupID) {
  if (!window.confirm("确定删除这份 Codex 配置备份吗？该操作不可恢复。")) return;
  void runConfigurationBackupAction("delete", backupID);
}

function confirmHistoryDelete(backupID) {
  if (!window.confirm("确定删除这份 Codex 历史备份吗？该操作不可恢复。")) return;
  void runHistoryBackupAction("delete", backupID);
}

function close() {
  cancelXIASSKeySelection();
	clearSavedCodexAccountSelection({ clearCandidates: true });
  draft.value.api_key = "";
  manualCodexDesktopPath.value = "";
  error.value = "";
  notice.value = "";
	 restartRequired.value = false;
  providerChangePending.value = false;
  historyCompatibilityChecked.value = false;
  resetLifecycleOptions();
  emit("close");
}

function formatTime(value) {
  if (!value) return "—";
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) return value;
  return time.toLocaleString();
}

watch(
  () => props.open,
  (open) => {
    if (open) void load();
    else {
      draft.value.api_key = "";
      manualCodexDesktopPath.value = "";
		clearSavedCodexAccountSelection({ clearCandidates: true });
      providerChangePending.value = false;
      historyCompatibilityChecked.value = false;
      resetLifecycleOptions();
    }
  },
);

onBeforeUnmount(() => {
  manualCodexDesktopPath.value = "";
  clearSavedCodexAccountSelection({ clearCandidates: true });
  cancelXIASSKeySelection();
});
</script>

<template>
  <Modal :open="open" title="配置 Codex" wide persistent :inline="inline" :closable="!saving && !removingXIASSProvider && !migratingLegacyProvider && !discovering && !repairingHistory && !desktopBusy" @close="close">
    <div class="codex-config">
      <p class="intro">XIASS Tools 仅管理 <code>config.toml</code> 中独立的 <code>xiass_tools</code> Provider。不会读取 <code>auth.json</code>、不会替换无关 Provider；Codex Desktop 的打开、退出与重启只会在你主动确认后执行。</p>

      <div v-if="loading" class="state-block">正在读取本机 Codex 配置…</div>

      <template v-else>
        <div class="config-status" :class="{ configured, invalid: data && (!data.ok || managedProviderNeedsRepair) }">
          <div>
            <strong>{{ configStatusTitle }}</strong>
            <span>{{ configStatusDescription }}</span>
          </div>
          <code>{{ hasConfiguration ? "已找到本机 config.toml" : "尚未找到本机 config.toml" }}</code>
        </div>

        <section class="desktop-control-section" aria-labelledby="codex-desktop-title">
          <div class="section-title">
            <strong id="codex-desktop-title">Codex Desktop 协作</strong>
            <span>先验证本机 App，再由你主动打开、退出或重启。安装路径仅在原生选择或手动粘贴时短暂用于本机校验，不会写入状态、日志或诊断。</span>
          </div>
          <div class="desktop-status" :class="{ running: desktopRunning, present: desktopPresent, unavailable: !desktopBridgeAvailable }" role="status" aria-live="polite">
            <div class="desktop-signal" aria-hidden="true"><i></i><i></i><i></i></div>
            <div class="desktop-status-copy">
              <strong>{{ desktopLoading ? "正在检查 Codex Desktop" : desktopStatusLabel }}</strong>
              <span>{{ desktopStatusDescription }}</span>
            </div>
            <code v-if="desktopStatus.version">v{{ desktopStatus.version }}</code>
          </div>
          <div v-if="desktopBridgeAvailable" class="desktop-actions" role="group" aria-label="Codex Desktop 控制">
            <Button variant="tinted" size="sm" :loading="desktopAction === 'select'" :disabled="!desktopCanSelect" @click="runDesktopAction('select')">选择并验证 App</Button>
            <Button variant="plain" size="sm" :loading="desktopAction === 'launch'" :disabled="!desktopCanLaunch" @click="runDesktopAction('launch')">打开 Codex</Button>
            <Button v-if="desktopRunning" variant="plain" size="sm" :loading="desktopAction === 'stop'" :disabled="!desktopCanStop" @click="confirmDesktopAction('stop')">退出 Codex</Button>
            <Button v-if="desktopRunning" variant="filled" size="sm" :loading="desktopAction === 'restart'" :disabled="!desktopCanRestart" @click="confirmDesktopAction('restart')">重新启动</Button>
            <button type="button" class="desktop-refresh" :disabled="desktopLoading || desktopBusy" aria-label="刷新 Codex Desktop 状态" title="刷新 Codex Desktop 状态" @click="refreshCodexDesktopStatus">
              <svg :class="{ spin: desktopLoading }" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 11a8 8 0 1 0 2 5.3M20 4v7h-7" /></svg>
              <span>刷新状态</span>
            </button>
          </div>
          <details v-if="desktopBridgeAvailable" class="manual-desktop-path">
            <summary>自动检测不到？粘贴本机 App 路径</summary>
            <div>
              <input v-model="manualCodexDesktopPath" autocomplete="off" spellcheck="false" :disabled="desktopBusy" placeholder="Codex.app、Codex.exe 或 ChatGPT.exe 的完整路径" />
              <Button variant="plain" size="sm" :loading="desktopAction === 'manual-path'" :disabled="!desktopCanSelect || !manualCodexDesktopPath.trim()" @click="useManualCodexDesktopPath">验证路径</Button>
            </div>
            <p>路径只用于本次原生结构验证，提交后立即从页面清除；不会保存、显示在状态中或导出到日志与诊断。</p>
          </details>
        </section>

        <div v-if="restartRequired" class="restart-guidance" role="status">
          <strong>需要重启 Codex</strong>
          <span>config.toml 已变更。请在完成当前工作后使用上方控制区重新启动，或自行退出并重新打开 Codex，使新配置生效。</span>
        </div>

        <div v-if="error" class="feedback error" role="alert">{{ error }}</div>
        <div v-if="notice" class="feedback success" role="status">{{ notice }}</div>

        <section v-if="canRemoveXIASSProvider" class="remove-provider-section" aria-labelledby="remove-xiass-provider-title">
          <div class="section-title">
            <strong id="remove-xiass-provider-title">移除 XIASS Tools Provider</strong>
            <span>仅移除 <code>xiass_tools</code>。会先创建恢复备份；不会读取账号凭据，不会改动其他 Provider、MCP、Desktop、未知设置或历史会话，也不会控制 Codex Desktop。</span>
          </div>
          <div class="remove-provider-actions">
            <p v-if="activeXIASSProvider">当前正在使用 XIASS Tools。移除后会同时清除当前默认模型和审查模型选择。</p>
            <p v-else>XIASS Tools 当前不是活动 Provider。移除不会改变正在使用的模型选择。</p>
            <Button variant="danger" size="sm" :loading="removingXIASSProvider" :disabled="saving || discovering || repairingHistory || desktopBusy || lifecycleApplying" @click="confirmRemoveXIASSProvider">移除 XIASS Tools Provider</Button>
          </div>
        </section>

        <section class="saved-codex-account-section" :aria-busy="savedCodexAccountCandidatesLoading ? 'true' : 'false'">
          <div class="section-title saved-codex-account-heading">
            <div>
              <strong>使用已保存的上游账户</strong>
              <span>仅列出已启用、OpenAI Responses + Bearer 静态凭据且可无损映射的账户。账户地址、密钥、请求头、OAuth 与刷新令牌不会进入界面；每次保存前都会在原生层重新验证。</span>
            </div>
            <button type="button" class="saved-codex-account-refresh" :disabled="savedCodexAccountCandidatesLoading || saving || discovering || lifecycleApplying" @click="refreshSavedCodexAccountCandidates">{{ savedCodexAccountCandidatesLoading ? "读取中…" : "刷新账户" }}</button>
          </div>
          <p v-if="savedCodexAccountCandidatesMessage" class="saved-codex-account-message">{{ savedCodexAccountCandidatesMessage }}</p>
          <div v-if="savedCodexAccountCandidates.length" class="saved-codex-account-grid" role="group" aria-label="可复用 Codex 上游账户">
            <button
              v-for="candidate in savedCodexAccountCandidates"
              :key="candidate.id"
              type="button"
              class="saved-codex-account-card"
              :class="{ selected: selectedSavedCodexAccountID === candidate.id }"
              :aria-pressed="selectedSavedCodexAccountID === candidate.id"
              :disabled="saving || discovering || lifecycleApplying"
              @click="selectSavedCodexAccount(candidate)"
            >
              <strong>{{ candidate.label }}</strong>
              <span>{{ candidate.credentialMode }}</span>
              <small>{{ candidate.models.length ? `${candidate.models.length} 个已绑定 Responses 模型` : "可在下方手动填写模型 ID" }}</small>
            </button>
          </div>
          <div v-if="selectedSavedCodexAccount" class="saved-codex-account-selection">
            <div class="saved-codex-account-selection-copy">
              <strong>已选择 {{ selectedSavedCodexAccount.label }}</strong>
              <span>“获取上游模型”“安全保存配置”和“高级：安全保存、检查历史并启动 Codex”都会使用该账户；不会使用表单中的 API 地址或 API Key。</span>
            </div>
            <div v-if="selectedSavedCodexAccountModels.length" class="saved-codex-account-models" role="group" aria-label="已绑定 Codex Responses 模型">
              <button
                v-for="model in selectedSavedCodexAccountModels"
                :key="model.id"
                type="button"
                :class="{ selected: selectedSavedCodexAccountModel === model.id }"
                :disabled="saving || discovering || lifecycleApplying"
                @click="chooseSavedCodexAccountModel(model)"
              >
                <strong>{{ model.displayName }}</strong>
                <span v-if="model.displayName !== model.id">{{ model.id }}</span>
              </button>
            </div>
            <p v-else class="saved-codex-account-empty">该账户暂无已绑定的 Responses 模型。可先在“模型”页面绑定模型，或在下方手动填写明确的 Responses 模型 ID。</p>
            <div class="saved-codex-account-actions">
              <span>应用时会先创建可恢复的 <code>config.toml</code> 备份，并进行原子写入与回读校验；不会读取 <code>auth.json</code>、Cookie、客户端会话或外部账号文件。</span>
              <Button variant="plain" size="sm" :disabled="saving || discovering || lifecycleApplying" @click="stopUsingSavedCodexAccount">改用其他连接方式</Button>
            </div>
          </div>
        </section>

        <section class="form-section">
          <div class="section-title"><strong>上游连接</strong><span>{{ usingSavedCodexAccount ? "当前使用已保存的兼容账户。你仍可改用 XIASS API 网站选择 Key，或手动填写兼容 API；切换后会清除本次账户选择。" : "可直接从 XIASS API 网站选择自己的 Key，或手动填写兼容 API。网站选择的 Key 不会回传或保存到页面中。" }}</span></div>
          <div class="xiass-key-selection" :class="{ ready: xiassSelectionReady, pending: xiassSelectionPending }">
            <div class="xiass-key-copy">
              <strong>{{ xiassSelectionReady ? "XIASS API Key 已安全选择" : xiassSelectionPending ? "正在等待 XIASS API 网站完成选择" : "从 XIASS API 选择 Key" }}</strong>
              <span>{{ xiassSelection?.message || "浏览器会打开你的 XIASS API Key 选择页；完成后自动回到这里。" }}</span>
            </div>
            <div class="xiass-key-actions">
              <label class="site-field"><span>XIASS 网站</span><input v-model.trim="xiassSiteURL" :disabled="usingSavedCodexAccount || selectingXIASSKey || xiassSelectionPending || xiassSelectionReady" autocomplete="url" placeholder="https://api.xiass.com" /></label>
              <Button variant="plain" :loading="selectingXIASSKey" :disabled="usingSavedCodexAccount || selectingXIASSKey || saving || discovering" @click="startXIASSKeySelection">{{ xiassSelectionPending || xiassSelectionReady ? "重新选择 Key" : "在浏览器选择 Key" }}</Button>
              <button v-if="xiassSelectionPending || xiassSelectionReady" type="button" class="text-action" :disabled="completingXIASSKey" @click="cancelXIASSKeySelection">取消</button>
            </div>
            <details v-if="xiassSelectionPending" class="manual-callback">
              <summary>浏览器没有自动返回？粘贴完整回调地址</summary>
              <div>
                <input v-model="manualCallbackURL" autocomplete="off" spellcheck="false" placeholder="http://127.0.0.1:端口/callback#state=…&payload=…" />
                <Button variant="plain" :loading="completingXIASSKey" :disabled="completingXIASSKey" @click="completeXIASSKeySelectionManually">完成回调</Button>
              </div>
              <p>地址只用于本次原生验证，提交后立即从页面清除，不会写入日志或本地存储。</p>
            </details>
          </div>
          <label class="field wide"><span>API 地址</span><input v-model.trim="draft.base_url" :disabled="usingSavedCodexAccount || xiassSelectionReady" autocomplete="url" placeholder="https://api.xiass.com" /><small v-if="usingSavedCodexAccount">当前地址来自已保存账户，保存时由原生层重新读取并验证；如需改用手动地址，请先点击“改用其他连接方式”。</small><small v-else-if="xiassSelectionReady">当前地址来自已验证的网站选择；如需改用其他地址，请先取消此选择后手动填写。</small></label>
          <div class="split-fields">
            <label class="field"><span>API Key</span><input v-if="!usingSavedCodexAccount && !xiassSelectionReady" ref="apiKeyInput" v-model="draft.api_key" type="password" autocomplete="off" placeholder="仅用于当前操作，不会回显" /><span v-else class="selection-key-state">{{ usingSavedCodexAccount ? "已选择本机保存的兼容账户；凭据不会显示在此处。" : "已由 XIASS API 网站安全选择；Key 不会显示在此处。" }}</span></label>
            <label class="field"><span>显示名称</span><input v-model.trim="draft.provider_name" :disabled="usingSavedCodexAccount" maxlength="200" placeholder="XIASS Tools" /><small v-if="usingSavedCodexAccount">保存时使用已选择账户的名称，避免界面名称与实际凭据来源不一致。</small></label>
          </div>
          <div class="model-tools">
            <Button variant="plain" :loading="discovering" :disabled="discovering || saving" @click="discoverModels">获取上游模型</Button>
            <span>{{ usingSavedCodexAccount ? "模型发现只会使用已选择账户在原生层保存的凭据请求一次 /v1/models；凭据不会回传页面。它只发现模型 ID，不会自动调用或验证 Responses 推理。" : "模型发现只请求一次 /v1/models，不会缓存 API Key；它只发现模型 ID，不会自动调用或验证 Responses 推理。网站选择模式全程由原生层持有 Key。" }}</span>
          </div>
          <div v-if="canMigrateLegacyProvider" class="legacy-migration" aria-live="polite">
            <div>
              <strong>可安全迁移一个旧版 Provider</strong>
              <span>已原生验证 <code>{{ legacyProviderMigration.providerID }}</code> 是唯一可迁移的第一方旧 Provider。迁移会创建 <code>xiass_tools</code>，保留安全可证明的模型、审查模型、上下文和联网搜索设置；不会读取凭据、不会触及其他 Provider 或会话。</span>
            </div>
            <Button variant="tinted" :loading="migratingLegacyProvider" :disabled="saving || discovering || removingXIASSProvider || lifecycleApplying" @click="confirmLegacyProviderMigration">迁移此旧 Provider</Button>
          </div>
        </section>

        <section class="form-section">
          <div class="section-title"><strong>运行模型</strong><span>仅写入 Codex 支持的 <code>responses</code> Provider 配置。</span></div>
          <div class="split-fields">
            <label class="field"><span>默认模型</span><input v-model.trim="draft.model" list="codex-discovered-models" placeholder="gpt-5.6-sol" /></label>
            <label class="field"><span>审查模型</span><input v-model.trim="draft.review_model" list="codex-discovered-models" placeholder="与默认模型相同" /></label>
          </div>
          <datalist id="codex-discovered-models"><option v-for="model in models" :key="model" :value="model" /></datalist>
          <div class="context-settings">
            <div class="context-heading">
              <div><span>上下文窗口</span><strong>{{ formatTokenCount(contextWindow) }}</strong></div>
              <span :class="{ linked: autoCompactLinked }">{{ autoCompactLinked ? "90% 自动压缩已联动" : `当前阈值 ${autoCompactPercent}%` }}</span>
            </div>
            <div class="context-presets" role="group" aria-label="上下文窗口档位">
              <SegmentedControl :options="contextPresetOptions" :model-value="contextMode" @update:model-value="setContextMode" />
            </div>
            <label v-if="contextMode === 'custom'" class="field custom-context"><span>自定义窗口（Token）</span><input v-model.number="draft.model_context_window" type="number" min="64000" max="1050000" step="1000" @change="syncCustomContextWindow" /><small>支持 64K 至 1.05M；确认后自动将压缩阈值设为 90%。</small></label>
            <p class="context-summary" role="status">{{ contextSummary }}</p>
          </div>
          <label class="field wide"><span>联网搜索</span><select v-model="draft.web_search"><option value="live">实时联网</option><option value="cached">使用缓存</option><option value="disabled">关闭</option></select></label>
        </section>

        <section class="lifecycle-section" aria-labelledby="codex-lifecycle-title">
          <div class="section-title">
            <strong id="codex-lifecycle-title">一次性安全应用</strong>
            <span>保留上方普通“安全保存配置”的轻量流程。这里的高级操作只会在你明确确认后，按固定顺序处理 Codex Desktop、配置和可选历史兼容。</span>
          </div>
          <details class="lifecycle-details">
            <summary>
              <span>高级：安全保存、检查历史并启动 Codex</span>
              <small>需要确认</small>
            </summary>
            <div class="lifecycle-panel">
              <p>事务会先验证当前状态；若 Codex 正在运行，会先正常请求退出。任一步无法安全确认时，事务会停止，并优先回滚可验证的配置与历史变更。</p>

              <div class="lifecycle-provider-state" :class="{ changing: providerWillChange }" role="status">
                <strong>{{ providerWillChange ? "当前 Provider 将切换至 XIASS Tools" : "当前仍使用 XIASS Tools Provider" }}</strong>
                <span v-if="providerWillChange">可选地检查与新 Provider 相关的兼容历史；只有勾选后才会执行。</span>
                <span v-else>Provider 未变更。本高级操作不会触发全量历史扫描，也不会改写现有会话。</span>
              </div>

              <label v-if="providerWillChange" class="lifecycle-option">
                <input v-model="lifecycleRepairHistory" type="checkbox" :disabled="lifecycleApplying" />
                <span><strong>检查 Provider 兼容历史</strong><small>先创建可恢复备份；仅处理本次 Provider 切换所需的兼容记录。</small></span>
              </label>
              <p v-else class="lifecycle-skip-note">Provider 未变更，历史检查保持关闭。</p>

              <label class="lifecycle-option">
                <input v-model="lifecycleLaunchAfter" type="checkbox" :disabled="lifecycleApplying" />
                <span><strong>完成后启动 Codex Desktop</strong><small>只会启动已验证的本机 Codex Desktop；不会启动任意选择的程序。</small></span>
              </label>
              <p v-if="lifecycleLaunchUnavailable" class="lifecycle-warning">尚未发现已验证的 Codex Desktop。请先在上方选择并验证 App，或取消启动选项后只执行安全保存。</p>

              <label class="lifecycle-acknowledgement">
                <input v-model="lifecycleAcknowledged" type="checkbox" :disabled="lifecycleApplying" />
                <span>{{ lifecycleAcknowledgementCopy }}</span>
              </label>

              <div v-if="lifecycleOutcome" class="lifecycle-outcome" :class="lifecycleOutcomeTone" role="status" aria-live="polite">
                <strong>{{ lifecycleOutcomeTitle }}</strong>
                <span>{{ lifecycleOutcomeDescription }}</span>
              </div>

              <Button variant="filled" :loading="lifecycleApplying" :disabled="!lifecycleActionEnabled" @click="confirmLifecycleApply">{{ lifecycleActionLabel }}</Button>
            </div>
          </details>
        </section>

        <section class="history-section">
          <div class="section-title"><strong>会话兼容与恢复</strong><span>仅在手动点击后检查并修复因切换 Provider 产生的不兼容本地记录；先创建可恢复备份，不会自动扫描或重写会话。</span></div>
          <div class="history-flow" :class="{ pending: providerChangePending, complete: historyCompatibilityChecked, blocked: historyRepairBlockedByDesktop }" role="status" aria-live="polite">
            <div class="history-flow-heading"><strong>安全顺序</strong><span>{{ historyCompatibilityState }}</span></div>
            <ol>
              <li :class="{ done: restartRequired }"><span>1</span><div><strong>保存 Provider</strong><small>先通过上方“安全保存配置”创建可恢复配置备份。</small></div></li>
              <li :class="{ done: !desktopRunning && (providerChangePending || !desktopBridgeAvailable), active: desktopRunning }"><span>2</span><div><strong>退出 Codex</strong><small>若已检测到运行中的桌面应用，请先保存工作并退出。</small></div></li>
              <li :class="{ done: historyCompatibilityChecked }"><span>3</span><div><strong>检查兼容历史</strong><small>仅在此处手动触发；完成后可重新打开 Codex。</small></div></li>
            </ol>
          </div>
          <Button variant="plain" :loading="repairingHistory" :disabled="repairingHistory || saving || removingXIASSProvider || desktopLoading || desktopBusy || historyRepairBlockedByDesktop" @click="repairHistory">检查并修复兼容历史</Button>
          <p v-if="historyRepairBlockedByDesktop" class="history-blocked">Codex Desktop 正在运行。为避免写入活跃会话，请先在上方退出 Codex 后再继续。</p>
        </section>

        <details class="backup-section">
          <summary>配置备份（{{ backupItems.length }}）</summary>
          <p v-if="!backupItems.length">尚无 XIASS Tools 创建的配置备份。</p>
          <div v-for="backup in backupItems" :key="backup.id" class="backup-row">
            <div><strong>{{ backup.reason }}</strong><span>{{ formatTime(backup.created_at) }}</span></div>
            <div><button type="button" :disabled="actionID || removingXIASSProvider" @click="confirmConfigurationRestore(backup.id)">恢复</button><button type="button" :disabled="actionID || removingXIASSProvider" @click="confirmConfigurationDelete(backup.id)">删除</button></div>
          </div>
        </details>

        <details class="backup-section">
          <summary>历史备份（{{ historyBackupItems.length }}）</summary>
          <p v-if="!historyBackupItems.length">尚无 XIASS Tools 创建的历史修复备份。</p>
          <div v-for="backup in historyBackupItems" :key="backup.id" class="backup-row">
            <div><strong>{{ backup.target_provider || "Provider 修复" }}</strong><span>{{ formatTime(backup.created_at) }} · 已处理 {{ backup.sanitized_records || 0 }} 条记录</span></div>
            <div><button type="button" :disabled="actionID || removingXIASSProvider" @click="confirmHistoryRestore(backup.id)">恢复</button><button type="button" :disabled="actionID || removingXIASSProvider" @click="confirmHistoryDelete(backup.id)">删除</button></div>
          </div>
        </details>

        <details v-if="legacyBackupItems.length || legacyBackupWarning" class="backup-section legacy-backup-section">
          <summary>发现旧版 XIASS Codex Helper 备份（{{ legacyBackupItems.length }}）</summary>
          <div v-if="legacyBackupWarning" class="legacy-backup-warning" role="status">{{ legacyBackupWarning }}</div>
          <p v-if="!legacyConfigurationBackups.length && !legacyHistoryBackups.length">未发现可安全导入的旧版备份。</p>

          <div v-if="legacyConfigurationBackups.length" class="legacy-backup-group">
            <strong>旧配置备份</strong>
            <div v-for="backup in legacyConfigurationBackups" :key="`legacy-config:${backup.source_id}`" class="backup-row">
              <div><strong>{{ backup.reason || "旧版配置备份" }}</strong><span>{{ formatTime(backup.created_at) }}</span></div>
              <div><button type="button" :disabled="Boolean(actionID) || removingXIASSProvider" @click="confirmLegacyConfigurationImport(backup.source_id)">{{ actionID === `legacy:config:${backup.source_id}` ? "正在导入…" : "导入为新的恢复点" }}</button></div>
            </div>
          </div>

          <div v-if="legacyHistoryBackups.length" class="legacy-backup-group">
            <strong>旧历史备份</strong>
            <div v-for="backup in legacyHistoryBackups" :key="`legacy-history:${backup.source_id}`" class="backup-row">
              <div><strong>{{ backup.target_provider || "旧版历史备份" }}</strong><span>{{ formatTime(backup.created_at) }}</span></div>
              <div><button type="button" :disabled="Boolean(actionID) || removingXIASSProvider" @click="confirmLegacyHistoryImport(backup.source_id)">{{ actionID === `legacy:history:${backup.source_id}` ? "正在导入…" : "导入为新的恢复点" }}</button></div>
            </div>
          </div>
        </details>
      </template>
    </div>
    <template #footer>
      <Button variant="plain" :disabled="saving || removingXIASSProvider || migratingLegacyProvider || discovering || repairingHistory || desktopBusy || lifecycleApplying" @click="close">关闭</Button>
      <Button variant="filled" :loading="saving" :disabled="loading || saving || removingXIASSProvider || migratingLegacyProvider || discovering || repairingHistory || desktopBusy || lifecycleApplying" @click="save">安全保存配置</Button>
    </template>
  </Modal>
</template>

<style scoped>
.codex-config { display: grid; max-height: min(72vh, 760px); gap: 20px; overflow: auto; padding: 2px 8px 18px 2px; }
.intro { margin: 0; color: var(--text-secondary); font-size: 12px; line-height: 1.65; }
	.intro code, .section-title code, .model-tools code, .legacy-migration code { color: var(--accent-strong); font-family: var(--font-num); font-size: .94em; }
.state-block { border: 1px dashed var(--separator-strong); border-radius: 10px; color: var(--text-tertiary); padding: 22px; text-align: center; }
.config-status { display: grid; gap: 8px; border: 1px solid var(--separator); border-left: 3px solid var(--blue); border-radius: 10px; background: var(--bg-inset); padding: 11px 12px; }
.config-status.configured { border-left-color: var(--green); }
.config-status.invalid { border-left-color: var(--red); }
.config-status > div { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.config-status strong { color: var(--text-primary); font-size: 13px; }
.config-status span { color: var(--text-secondary); font-size: 12px; text-align: right; }
	.config-status code { overflow: hidden; color: var(--text-tertiary); font-family: var(--font-num); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
	.desktop-control-section { display: grid; gap: 10px; border-top: 1px solid var(--separator); padding-top: 14px; }
	.desktop-status { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; border: 1px solid var(--separator); border-left: 3px solid var(--blue); border-radius: 10px; background: linear-gradient(105deg, color-mix(in srgb, var(--blue) 8%, var(--bg-inset)), var(--bg-inset) 64%); padding: 10px 11px; }
	.desktop-status.present { border-left-color: var(--teal); background: linear-gradient(105deg, color-mix(in srgb, var(--teal) 8%, var(--bg-inset)), var(--bg-inset) 64%); }
	.desktop-status.running { border-left-color: var(--green); background: linear-gradient(105deg, color-mix(in srgb, var(--green) 9%, var(--bg-inset)), var(--bg-inset) 64%); }
	.desktop-status.unavailable { border-left-color: var(--orange); background: linear-gradient(105deg, color-mix(in srgb, var(--orange) 8%, var(--bg-inset)), var(--bg-inset) 64%); }
	.desktop-signal { display: flex; align-items: end; gap: 3px; width: 17px; height: 18px; }
	.desktop-signal i { display: block; width: 3px; border-radius: 999px; background: var(--blue); }
	.desktop-signal i:nth-child(1) { height: 7px; opacity: .52; }
	.desktop-signal i:nth-child(2) { height: 12px; opacity: .74; }
	.desktop-signal i:nth-child(3) { height: 17px; }
	.desktop-status.present .desktop-signal i { background: var(--teal); }
	.desktop-status.running .desktop-signal i { background: var(--green); }
	.desktop-status.unavailable .desktop-signal i { background: var(--orange); }
	.desktop-status-copy { display: grid; min-width: 0; gap: 2px; }
	.desktop-status-copy strong { color: var(--text-primary); font-size: 12px; }
	.desktop-status-copy span { color: var(--text-secondary); font-size: 11px; line-height: 1.5; }
	.desktop-status code { align-self: start; color: var(--text-tertiary); font-family: var(--font-num); font-size: 10px; white-space: nowrap; }
	.desktop-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; }
	.desktop-refresh { display: inline-flex; min-height: 26px; align-items: center; gap: 5px; border: 0; border-radius: 7px; color: var(--text-tertiary); font: inherit; font-size: 11px; padding: 0 5px; transition: color .16s var(--ease), background .16s var(--ease); }
	.desktop-refresh svg { width: 14px; height: 14px; fill: none; stroke: currentColor; stroke-width: 1.7; stroke-linecap: round; stroke-linejoin: round; }
	.desktop-refresh:hover:not(:disabled) { background: var(--bg-fill-hover); color: var(--accent-strong); }
	.desktop-refresh:focus-visible, .desktop-actions :deep(.btn:focus-visible), .manual-desktop-path :deep(.btn:focus-visible), .manual-desktop-path input:focus-visible { outline: 2px solid var(--accent-strong); outline-offset: 2px; }
	.desktop-refresh:disabled { cursor: wait; opacity: .48; }
	.manual-desktop-path { border-top: 1px solid var(--separator); padding-top: 9px; }
	.manual-desktop-path summary { cursor: pointer; color: var(--text-secondary); font-size: 11px; }
	.manual-desktop-path > div { display: flex; align-items: center; gap: 7px; margin-top: 8px; }
	.manual-desktop-path input { flex: 1 1 auto; width: 100%; min-width: 0; border: 1px solid var(--separator-strong); border-radius: 7px; outline: none; background: var(--bg-base); color: var(--text-primary); font: inherit; font-size: 11px; padding: 8px 9px; }
	.manual-desktop-path input:focus { border-color: var(--accent-strong); box-shadow: 0 0 0 3px var(--accent-soft); }
	.manual-desktop-path p { margin: 7px 0 0; color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
	.restart-guidance { display: grid; gap: 3px; border: 1px solid color-mix(in srgb, var(--orange) 34%, var(--separator)); border-left: 3px solid var(--orange); border-radius: 10px; background: color-mix(in srgb, var(--orange) 7%, var(--bg-inset)); padding: 10px 11px; }
	.restart-guidance strong { color: var(--text-primary); font-size: 12px; }
	.restart-guidance span { color: var(--text-secondary); font-size: 11px; line-height: 1.5; }
.feedback { border: 1px solid var(--separator); border-radius: 9px; padding: 9px 10px; font-size: 12px; line-height: 1.5; }
.feedback.error { border-color: color-mix(in srgb, var(--red) 35%, transparent); background: color-mix(in srgb, var(--red) 8%, transparent); color: var(--red); }
.feedback.success { border-color: color-mix(in srgb, var(--green) 35%, transparent); background: color-mix(in srgb, var(--green) 8%, transparent); color: var(--green); }
.form-section, .history-section, .lifecycle-section, .remove-provider-section { display: grid; gap: 10px; border-top: 1px solid var(--separator); padding-top: 14px; }
.saved-codex-account-section { display: grid; gap: 9px; border: 1px solid color-mix(in srgb, var(--accent-strong) 30%, var(--separator)); border-left: 3px solid var(--accent-strong); border-radius: 10px; background: color-mix(in srgb, var(--accent-soft) 34%, var(--bg-inset)); padding: 10px 11px; }
.saved-codex-account-heading { align-items: start; display: flex; justify-content: space-between; gap: 12px; }
.saved-codex-account-heading > div { display: grid; min-width: 0; gap: 3px; }
.saved-codex-account-refresh { flex: 0 0 auto; border: 1px solid var(--accent-border); border-radius: 7px; background: var(--bg-base); color: var(--accent-strong); font: inherit; font-size: 10px; font-weight: 700; padding: 6px 8px; }
.saved-codex-account-refresh:hover:not(:disabled) { border-color: var(--accent-strong); background: var(--accent-soft); }
.saved-codex-account-refresh:focus-visible, .saved-codex-account-card:focus-visible, .saved-codex-account-models button:focus-visible, .saved-codex-account-actions :deep(.btn:focus-visible) { outline: 2px solid var(--accent-strong); outline-offset: 2px; }
.saved-codex-account-refresh:disabled, .saved-codex-account-card:disabled, .saved-codex-account-models button:disabled { cursor: not-allowed; opacity: .52; }
.saved-codex-account-message { margin: 0; color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.saved-codex-account-grid, .saved-codex-account-models { display: grid; grid-template-columns: repeat(auto-fit, minmax(158px, 1fr)); gap: 6px; }
.saved-codex-account-card, .saved-codex-account-models button { display: grid; min-width: 0; gap: 2px; border: 1px solid var(--separator); border-radius: 8px; background: var(--bg-base); color: var(--text-secondary); text-align: left; padding: 8px 9px; }
.saved-codex-account-card:hover:not(:disabled), .saved-codex-account-card.selected, .saved-codex-account-models button:hover:not(:disabled), .saved-codex-account-models button.selected { border-color: var(--accent-strong); background: var(--accent-soft); color: var(--accent-strong); }
.saved-codex-account-card strong, .saved-codex-account-card span, .saved-codex-account-card small, .saved-codex-account-models strong, .saved-codex-account-models span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.saved-codex-account-card strong, .saved-codex-account-models strong { font-size: 11px; }
.saved-codex-account-card span, .saved-codex-account-card small, .saved-codex-account-models span { color: var(--text-tertiary); font-family: var(--font-num); font-size: 9px; }
.saved-codex-account-selection { display: grid; gap: 8px; border-top: 1px solid var(--separator); margin-top: 1px; padding-top: 9px; }
.saved-codex-account-selection-copy { display: grid; gap: 3px; }
.saved-codex-account-selection-copy strong { color: var(--text-primary); font-size: 11px; }
.saved-codex-account-selection-copy span, .saved-codex-account-empty { margin: 0; color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.saved-codex-account-actions { display: flex; align-items: center; justify-content: space-between; gap: 9px; border: 1px solid var(--separator); border-radius: 8px; background: var(--bg-base); padding: 8px 9px; }
.saved-codex-account-actions span { color: var(--text-tertiary); font-size: 9.5px; line-height: 1.4; }
.saved-codex-account-actions code { color: var(--accent-strong); font-family: var(--font-num); font-size: .95em; }
.remove-provider-actions { display: flex; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid color-mix(in srgb, var(--red) 28%, var(--separator)); border-radius: 10px; background: color-mix(in srgb, var(--red) 5%, var(--bg-inset)); padding: 10px 11px; }
.remove-provider-actions p { margin: 0; color: var(--text-secondary); font-size: 12px; line-height: 1.5; }
.remove-provider-actions :deep(.btn:focus-visible) { outline: 2px solid var(--accent-strong); outline-offset: 2px; }
@media (max-width: 620px) { .remove-provider-actions, .saved-codex-account-actions, .saved-codex-account-heading { align-items: stretch; flex-direction: column; } .remove-provider-actions :deep(.btn), .saved-codex-account-actions :deep(.btn), .saved-codex-account-refresh { width: 100%; } }
.section-title { display: grid; gap: 3px; }
.section-title strong { color: var(--text-primary); font-size: 13px; }
.section-title span { color: var(--text-tertiary); font-size: 11px; line-height: 1.5; }
.split-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.field { display: grid; min-width: 0; gap: 5px; }
.field.wide { width: 100%; }
.field span { color: var(--text-tertiary); font-size: 11px; }
.field input, .field select { width: 100%; min-width: 0; border: 1px solid var(--separator-strong); border-radius: 8px; outline: none; background: var(--bg-base); color: var(--text-primary); font: inherit; font-size: 12px; padding: 9px 10px; }
.field input:focus, .field select:focus { border-color: var(--accent-strong); box-shadow: 0 0 0 3px var(--accent-soft); }
	.lifecycle-details { border: 1px solid var(--separator); border-left: 3px solid var(--accent-strong); border-radius: 10px; background: color-mix(in srgb, var(--accent-soft) 24%, var(--bg-inset)); overflow: clip; }
	.lifecycle-details summary { display: flex; align-items: center; justify-content: space-between; gap: 12px; cursor: pointer; color: var(--text-primary); font-size: 12px; font-weight: 680; list-style: none; padding: 11px 12px; }
	.lifecycle-details summary::-webkit-details-marker { display: none; }
	.lifecycle-details summary::before { width: 7px; height: 7px; border: 1px solid color-mix(in srgb, var(--accent-strong) 60%, var(--separator)); border-radius: 50%; background: var(--accent-soft); content: ""; }
	.lifecycle-details summary > span { flex: 1 1 auto; }
	.lifecycle-details summary small { flex: 0 0 auto; color: var(--accent-strong); font-size: 10px; font-weight: 650; }
	.lifecycle-details summary:focus-visible { outline: 2px solid var(--accent-strong); outline-offset: -2px; }
	.lifecycle-panel { display: grid; gap: 10px; border-top: 1px solid var(--separator); padding: 11px 12px 12px; }
	.lifecycle-panel > p { margin: 0; color: var(--text-secondary); font-size: 11px; line-height: 1.55; }
	.lifecycle-provider-state { display: grid; gap: 3px; border: 1px solid var(--separator); border-left: 3px solid var(--teal); border-radius: 8px; background: var(--bg-base); padding: 9px 10px; }
	.lifecycle-provider-state.changing { border-left-color: var(--orange); }
	.lifecycle-provider-state strong { color: var(--text-primary); font-size: 11px; }
	.lifecycle-provider-state span { color: var(--text-secondary); font-size: 10px; line-height: 1.5; }
	.lifecycle-option, .lifecycle-acknowledgement { display: flex; align-items: flex-start; gap: 9px; border: 1px solid var(--separator); border-radius: 8px; background: color-mix(in srgb, var(--bg-base) 84%, transparent); color: var(--text-secondary); cursor: pointer; padding: 9px 10px; }
	.lifecycle-option input, .lifecycle-acknowledgement input { width: 15px; height: 15px; flex: 0 0 auto; accent-color: var(--accent); margin: 1px 0 0; }
	.lifecycle-option input:focus-visible, .lifecycle-acknowledgement input:focus-visible { outline: 2px solid var(--accent-strong); outline-offset: 2px; }
	.lifecycle-option:has(input:checked), .lifecycle-acknowledgement:has(input:checked) { border-color: color-mix(in srgb, var(--accent-strong) 42%, var(--separator)); background: color-mix(in srgb, var(--accent-soft) 42%, var(--bg-base)); }
	.lifecycle-option > span { display: grid; gap: 2px; }
	.lifecycle-option strong { color: var(--text-primary); font-size: 11px; }
	.lifecycle-option small { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
	.lifecycle-acknowledgement { border-left: 3px solid var(--orange); font-size: 11px; line-height: 1.5; }
	.lifecycle-acknowledgement:has(input:checked) { border-left-color: var(--green); }
	.lifecycle-skip-note { margin: 0; color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
	.lifecycle-warning { margin: -3px 0 0; color: var(--orange); font-size: 10px; line-height: 1.5; }
	.lifecycle-outcome { display: grid; gap: 3px; border: 1px solid var(--separator); border-left: 3px solid var(--red); border-radius: 8px; background: var(--bg-base); padding: 9px 10px; }
	.lifecycle-outcome.success { border-left-color: var(--green); background: color-mix(in srgb, var(--green) 6%, var(--bg-base)); }
	.lifecycle-outcome.rollback { border-left-color: var(--orange); background: color-mix(in srgb, var(--orange) 7%, var(--bg-base)); }
	.lifecycle-outcome strong { color: var(--text-primary); font-size: 11px; }
	.lifecycle-outcome span { color: var(--text-secondary); font-size: 10px; line-height: 1.5; }
	.lifecycle-panel :deep(.btn) { justify-self: start; }
	.model-tools { display: flex; align-items: center; gap: 10px; color: var(--text-tertiary); font-size: 11px; line-height: 1.45; }
	.legacy-migration { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-top: 1px solid var(--separator); padding-top: 11px; }
	.legacy-migration > div { display: grid; min-width: 0; gap: 3px; }
	.legacy-migration strong { color: var(--text-primary); font-size: 12px; }
	.legacy-migration span { color: var(--text-tertiary); font-size: 10px; line-height: 1.5; }
	.legacy-migration :deep(.btn) { flex: 0 0 auto; }
	.context-settings { display: grid; gap: 9px; border: 1px solid var(--separator); border-radius: 10px; background: var(--bg-inset); padding: 11px; }
	.context-heading { display: flex; align-items: end; justify-content: space-between; gap: 12px; }
	.context-heading > div { display: grid; gap: 1px; }
	.context-heading > div span { color: var(--text-tertiary); font-size: 10px; }
	.context-heading strong { color: var(--text-primary); font-family: var(--font-num); font-size: 19px; line-height: 1.1; }
	.context-heading > span { color: var(--orange); font-size: 11px; font-weight: 650; text-align: right; }
	.context-heading > span.linked { color: var(--green); }
	.context-presets { min-width: 0; }
	.context-presets :deep(.seg) { display: flex; width: 100%; }
	.context-presets :deep(.item) { min-width: 0; flex: 1 1 0; padding-right: 7px; padding-left: 7px; }
	.custom-context { border-top: 1px solid var(--separator); padding-top: 9px; }
	.context-summary { margin: 0; border-top: 1px solid var(--separator); color: var(--text-secondary); font-family: var(--font-num); font-size: 10px; line-height: 1.5; padding-top: 8px; }
.xiass-key-selection { display: grid; gap: 10px; border: 1px solid var(--separator); border-left: 3px solid var(--accent-strong); border-radius: 10px; background: color-mix(in srgb, var(--accent-soft) 35%, var(--bg-inset)); padding: 11px 12px; }
.xiass-key-selection.pending { border-left-color: var(--blue); }
.xiass-key-selection.ready { border-left-color: var(--green); }
.xiass-key-copy { display: grid; gap: 3px; }
.xiass-key-copy strong { color: var(--text-primary); font-size: 12px; }
.xiass-key-copy span { color: var(--text-secondary); font-size: 11px; line-height: 1.5; }
.xiass-key-actions { display: flex; align-items: end; flex-wrap: wrap; gap: 8px; }
.site-field { display: grid; flex: 1 1 205px; gap: 4px; }
.site-field span { color: var(--text-tertiary); font-size: 10px; }
.site-field input, .manual-callback input { width: 100%; min-width: 0; border: 1px solid var(--separator-strong); border-radius: 7px; outline: none; background: var(--bg-base); color: var(--text-primary); font: inherit; font-size: 11px; padding: 8px 9px; }
.site-field input:focus, .manual-callback input:focus { border-color: var(--accent-strong); box-shadow: 0 0 0 3px var(--accent-soft); }
.text-action { border: 0; color: var(--text-tertiary); font: inherit; font-size: 11px; padding: 8px 2px; }
.text-action:hover:not(:disabled) { color: var(--red); }
.manual-callback { border-top: 1px solid var(--separator); padding-top: 9px; }
.manual-callback summary { cursor: pointer; color: var(--text-secondary); font-size: 11px; }
.manual-callback > div { display: flex; align-items: center; gap: 7px; margin-top: 8px; }
.manual-callback > div input { flex: 1 1 auto; }
.manual-callback p { margin: 7px 0 0; color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.selection-key-state { min-height: 38px; border: 1px dashed color-mix(in srgb, var(--green) 46%, var(--separator)); border-radius: 8px; background: color-mix(in srgb, var(--green) 6%, transparent); color: var(--green); font-size: 11px; line-height: 1.45; padding: 9px 10px; }
.field small { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
	.history-section :deep(.button) { justify-self: start; }
	.history-flow { display: grid; gap: 9px; border: 1px solid var(--separator); border-left: 3px solid var(--blue); border-radius: 10px; background: var(--bg-inset); padding: 10px 11px; }
	.history-flow.pending { border-left-color: var(--orange); }
	.history-flow.complete { border-left-color: var(--green); }
	.history-flow.blocked { border-left-color: var(--red); }
	.history-flow-heading { display: grid; gap: 2px; }
	.history-flow-heading strong { color: var(--text-primary); font-size: 12px; }
	.history-flow-heading span { color: var(--text-secondary); font-size: 11px; line-height: 1.45; }
	.history-flow ol { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 6px; list-style: none; margin: 0; padding: 0; }
	.history-flow li { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 6px; min-width: 0; border-top: 1px solid var(--separator); padding-top: 8px; }
	.history-flow li > span { display: inline-grid; width: 16px; height: 16px; place-items: center; border: 1px solid var(--separator-strong); border-radius: 50%; color: var(--text-tertiary); font-family: var(--font-num); font-size: 9px; }
	.history-flow li > div { display: grid; min-width: 0; gap: 1px; }
	.history-flow li strong { color: var(--text-secondary); font-size: 10px; }
	.history-flow li small { color: var(--text-tertiary); font-size: 9px; line-height: 1.38; }
	.history-flow li.done > span { border-color: color-mix(in srgb, var(--green) 50%, var(--separator)); background: color-mix(in srgb, var(--green) 12%, transparent); color: var(--green); }
	.history-flow li.done strong { color: var(--green); }
	.history-flow li.active > span { border-color: color-mix(in srgb, var(--orange) 52%, var(--separator)); background: color-mix(in srgb, var(--orange) 12%, transparent); color: var(--orange); }
	.history-flow li.active strong { color: var(--orange); }
	.history-blocked { margin: -3px 0 0; color: var(--red); font-size: 10px; line-height: 1.5; }
.backup-section { border-top: 1px solid var(--separator); padding-top: 12px; }
.backup-section summary { cursor: pointer; color: var(--text-primary); font-size: 12px; font-weight: 680; }
.backup-section p { color: var(--text-tertiary); font-size: 11px; }
.backup-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-top: 1px solid var(--separator); padding: 9px 0; }
.backup-row > div:first-child { display: grid; min-width: 0; gap: 2px; }
.backup-row strong { overflow: hidden; color: var(--text-secondary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.backup-row span { color: var(--text-tertiary); font-size: 10px; }
.backup-row > div:last-child { display: flex; flex: 0 0 auto; gap: 6px; }
.backup-row button { border: 1px solid var(--separator); border-radius: 6px; color: var(--text-secondary); font-size: 10px; padding: 4px 7px; }
.backup-row button:hover:not(:disabled) { border-color: var(--accent-border); color: var(--accent-strong); }
.backup-row button:disabled { cursor: wait; opacity: .5; }
.legacy-backup-section summary { color: var(--accent-strong); }
.legacy-backup-warning { margin-top: 10px; border: 1px solid color-mix(in srgb, var(--orange) 34%, var(--separator)); border-left: 3px solid var(--orange); border-radius: 8px; background: color-mix(in srgb, var(--orange) 7%, var(--bg-inset)); color: var(--text-secondary); font-size: 11px; line-height: 1.5; padding: 9px 10px; }
.legacy-backup-group { display: grid; gap: 1px; margin-top: 11px; }
.legacy-backup-group > strong { color: var(--text-primary); font-size: 11px; }
.legacy-backup-group .backup-row:last-child { border-bottom: 1px solid var(--separator); }
	@media (max-width: 680px) { .split-fields { grid-template-columns: 1fr; } .config-status > div { display: grid; } .config-status span { text-align: left; } .desktop-status { grid-template-columns: auto minmax(0, 1fr); } .desktop-status code { grid-column: 2; } .desktop-actions { align-items: stretch; flex-direction: column; } .desktop-actions :deep(.btn), .desktop-refresh, .manual-desktop-path :deep(.btn), .lifecycle-panel :deep(.btn) { justify-content: center; width: 100%; } .xiass-key-actions, .manual-callback > div, .manual-desktop-path > div, .legacy-migration, .model-tools { align-items: stretch; flex-direction: column; } .site-field, .manual-callback > div input, .manual-desktop-path input { flex: 0 0 auto; width: 100%; } .model-tools :deep(.btn) { width: 100%; } .context-heading { align-items: flex-start; flex-direction: column; } .context-heading > span { text-align: left; } .history-flow ol { grid-template-columns: 1fr; } }
</style>
