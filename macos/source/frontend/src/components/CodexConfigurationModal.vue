<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import Modal from "@/components/ui/Modal.vue";
import Button from "@/components/ui/Button.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import {
  getCodexConfiguration,
  applyCodexConfiguration,
  discoverCodexModels,
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
} from "@/state/appState";

const props = defineProps({
  open: Boolean,
});

const emit = defineEmits(["close", "changed"]);

const loading = ref(false);
const saving = ref(false);
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
const restartRequired = ref(false);
const contextMode = ref("372000");
const apiKeyInput = ref(null);
let xiassSelectionPoll = null;

const contextPresetOptions = [
  { label: "235K", value: "235000" },
  { label: "372K", value: "372000" },
  { label: "512K", value: "512000" },
  { label: "1M", value: "1000000" },
  { label: "自定义", value: "custom" },
];
const legacyProviderIDs = new Set(["xiass", "codex_local_access"]);

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
const configured = computed(() => snapshot.value?.model_provider === "xiass_tools");
const hasConfiguration = computed(() => Boolean(configLocation.value?.exists));
const configStatusDescription = computed(() => {
  if (!data.value) return "等待读取";
  if (!data.value.ok) return "无法验证本机 config.toml。";
  if (configured.value) return "已验证 XIASS Tools 配置。";
  if (hasConfiguration.value) return "已读取现有 config.toml。";
  return "尚未创建 config.toml。";
});
const xiassSelectionReady = computed(() => xiassSelection.value?.status === "ready");
const xiassSelectionPending = computed(() => xiassSelection.value?.status === "pending");
const xiassSelectionSessionID = computed(() => xiassSelection.value?.sessionId || "");
const hasConfiguredCredential = computed(() => xiassSelectionReady.value || Boolean(draft.value.api_key?.trim()));
const legacyProviders = computed(() => {
  const seen = new Set();
  return (Array.isArray(snapshot.value?.providers) ? snapshot.value.providers : [])
    .map((provider) => String(provider?.id || "").trim())
    .filter((providerID) => {
      if (!legacyProviderIDs.has(providerID) || seen.has(providerID)) return false;
      seen.add(providerID);
      return true;
    });
});
const legacyProviderSummary = computed(() => legacyProviders.value.join("、"));
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

function applyRestartGuidance(message, { migrated = false, migratedProviders = legacyProviderSummary.value } = {}) {
  const migrationNotice = migrated ? `已迁移旧 Provider（${migratedProviders}）。` : "";
  restartRequired.value = true;
  notice.value = [message || "Codex 配置已安全写入。", migrationNotice, "请自行退出并重新启动 Codex，使 config.toml 变更生效。XIASS Tools 不会关闭 Codex。"]
    .filter(Boolean)
    .join(" ");
}

function startLegacyMigration() {
  error.value = "";
  notice.value = "";
  if (hasConfiguredCredential.value) {
    void save();
    return;
  }
  error.value = "迁移旧 Provider 需要提供新的 API Key，或先通过 XIASS API 网站完成安全选择。";
  void nextTick(() => apiKeyInput.value?.focus());
}

async function load() {
  loading.value = true;
  error.value = "";
	 restartRequired.value = false;
  try {
    const result = await getCodexConfiguration();
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
  } catch (cause) {
    error.value = "无法读取或验证本机 config.toml。请检查配置后重试。";
  } finally {
    loading.value = false;
  }
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
    error.value = "请先输入用于此次请求的 API Key。";
    return;
  }
  discovering.value = true;
  try {
    const result = xiassSelectionReady.value
      ? await discoverCodexXIASSSelectionModels(xiassSelectionSessionID.value)
      : await discoverCodexModels(draft.value.base_url, draft.value.api_key);
    if (!result?.ok) {
      error.value = "获取上游模型失败。请检查 API 地址与本次 Key 后重试。";
      return;
    }
    models.value = result.models || [];
    if (models.value.length && !models.value.includes(draft.value.model)) {
      draft.value.model = models.value[0];
    }
    if (models.value.length && !models.value.includes(draft.value.review_model)) {
      draft.value.review_model = draft.value.model;
    }
    notice.value = `已获取 ${models.value.length} 个可用模型。`;
  } catch (cause) {
    error.value = "获取上游模型失败。请检查 API 地址与本次 Key 后重试。";
  } finally {
    discovering.value = false;
  }
}

async function save() {
  error.value = "";
  notice.value = "";
  if (!hasConfiguredCredential.value) {
    error.value = "为安全写入 Codex 配置，请输入 API Key。";
    return;
  }
  saving.value = true;
  const migratingProviders = legacyProviders.value.slice();
  try {
    const result = xiassSelectionReady.value
      ? await applyCodexXIASSSelection(xiassSelectionSessionID.value, { ...draft.value, api_key: "", base_url: "" })
      : await applyCodexConfiguration({ ...draft.value });
    data.value = result;
    if (!result?.ok) {
      error.value = "保存 Codex 配置失败。请检查填写内容与 config.toml 状态后重试。";
      return;
    }
    draft.value.api_key = "";
    if (xiassSelectionReady.value) {
      stopXIASSSelectionPolling();
      xiassSelection.value = { ...xiassSelection.value, status: "applied", message: "已安全保存 Codex 配置。" };
    }
		applyRestartGuidance("Codex 配置已安全保存。", { migrated: migratingProviders.length > 0, migratedProviders: migratingProviders.join("、") });
    emit("changed");
  } catch (cause) {
    error.value = "保存 Codex 配置失败。请检查填写内容与 config.toml 状态后重试。";
  } finally {
    saving.value = false;
  }
}

async function runConfigurationBackupAction(action, backupID) {
  error.value = "";
  notice.value = "";
  actionID.value = `config:${action}:${backupID}`;
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
		applyRestartGuidance("已恢复 Codex 配置备份。");
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
  error.value = "";
  notice.value = "";
  repairingHistory.value = true;
  try {
    const result = await repairCodexHistory(true);
    if (!result?.ok) {
      error.value = "Codex 历史检查或修复未完成。请先退出 Codex，并确认 config.toml 有效后重试。";
      return;
    }
    notice.value = "Codex 历史已检查；如有变更，已创建可恢复备份。";
    await refreshAfterHistoryAction();
    emit("changed");
  } catch (cause) {
    error.value = "Codex 历史检查或修复未完成。请先退出 Codex，并确认 config.toml 有效后重试。";
  } finally {
    repairingHistory.value = false;
  }
}

async function runHistoryBackupAction(action, backupID) {
  error.value = "";
  notice.value = "";
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
	if (!window.confirm("将恢复这份 Codex 配置备份。当前配置会先自动备份；完成后请自行退出并重新启动 Codex，是否继续？")) return;
  void runConfigurationBackupAction("restore", backupID);
}

function confirmHistoryRestore(backupID) {
  if (!window.confirm("将恢复这份 Codex 历史备份。只会恢复该备份记录的本地会话修改，是否继续？")) return;
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
  draft.value.api_key = "";
  error.value = "";
  notice.value = "";
	 restartRequired.value = false;
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
    else draft.value.api_key = "";
  },
);

onBeforeUnmount(() => cancelXIASSKeySelection());
</script>

<template>
  <Modal :open="open" title="配置 Codex" wide persistent :closable="!saving && !discovering && !repairingHistory" @close="close">
    <div class="codex-config">
      <p class="intro">XIASS Tools 仅管理 <code>config.toml</code> 中独立的 <code>xiass_tools</code> Provider。不会读取 <code>auth.json</code>、不会替换无关 Provider，也不会启动或关闭 Codex。</p>

      <div v-if="loading" class="state-block">正在读取本机 Codex 配置…</div>

      <template v-else>
        <div class="config-status" :class="{ configured, invalid: data && !data.ok }">
          <div>
            <strong>{{ configured ? "XIASS Tools 已配置" : hasConfiguration ? "已发现现有 Codex 配置" : "尚未创建 Codex 配置" }}</strong>
            <span>{{ configStatusDescription }}</span>
          </div>
          <code>{{ hasConfiguration ? "已找到本机 config.toml" : "尚未找到本机 config.toml" }}</code>
        </div>

        <div v-if="restartRequired" class="restart-guidance" role="status">
          <strong>需要自行退出并重启 Codex</strong>
          <span>config.toml 已变更。XIASS Tools 不会关闭 Codex；请在完成当前工作后自行退出并重新启动，使新配置生效。</span>
        </div>

        <div v-if="error" class="feedback error" role="alert">{{ error }}</div>
        <div v-if="notice" class="feedback success" role="status">{{ notice }}</div>

        <section class="form-section">
          <div class="section-title"><strong>上游连接</strong><span>可直接从 XIASS API 网站选择自己的 Key，或手动填写兼容 API。网站选择的 Key 不会回传或保存到页面中。</span></div>
          <div class="xiass-key-selection" :class="{ ready: xiassSelectionReady, pending: xiassSelectionPending }">
            <div class="xiass-key-copy">
              <strong>{{ xiassSelectionReady ? "XIASS API Key 已安全选择" : xiassSelectionPending ? "正在等待 XIASS API 网站完成选择" : "从 XIASS API 选择 Key" }}</strong>
              <span>{{ xiassSelection?.message || "浏览器会打开你的 XIASS API Key 选择页；完成后自动回到这里。" }}</span>
            </div>
            <div class="xiass-key-actions">
              <label class="site-field"><span>XIASS 网站</span><input v-model.trim="xiassSiteURL" :disabled="selectingXIASSKey || xiassSelectionPending || xiassSelectionReady" autocomplete="url" placeholder="https://api.xiass.com" /></label>
              <Button variant="plain" :loading="selectingXIASSKey" :disabled="selectingXIASSKey || saving || discovering" @click="startXIASSKeySelection">{{ xiassSelectionPending || xiassSelectionReady ? "重新选择 Key" : "在浏览器选择 Key" }}</Button>
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
          <label class="field wide"><span>API 地址</span><input v-model.trim="draft.base_url" :disabled="xiassSelectionReady" autocomplete="url" placeholder="https://api.xiass.com" /><small v-if="xiassSelectionReady">当前地址来自已验证的网站选择；如需改用其他地址，请先取消此选择后手动填写。</small></label>
          <div class="split-fields">
            <label class="field"><span>API Key</span><input v-if="!xiassSelectionReady" ref="apiKeyInput" v-model="draft.api_key" type="password" autocomplete="off" placeholder="仅用于当前操作，不会回显" /><span v-else class="selection-key-state">已由 XIASS API 网站安全选择；Key 不会显示在此处。</span></label>
            <label class="field"><span>显示名称</span><input v-model.trim="draft.provider_name" maxlength="200" placeholder="XIASS Tools" /></label>
          </div>
          <div class="model-tools">
            <Button variant="plain" :loading="discovering" :disabled="discovering || saving" @click="discoverModels">获取上游模型</Button>
            <span>模型发现只请求一次 <code>/v1/models</code>，不会缓存 API Key；网站选择模式全程由原生层持有 Key。</span>
          </div>
          <div v-if="legacyProviders.length" class="legacy-migration">
            <div>
              <strong>发现旧 Provider：{{ legacyProviderSummary }}</strong>
              <span>迁移会通过既有的安全保存流程创建 <code>xiass_tools</code> Provider，并移除这两个旧 Provider；不会更改其他 Provider 或历史记录。</span>
            </div>
            <Button variant="tinted" :disabled="saving || discovering" @click="startLegacyMigration">迁移旧 Provider</Button>
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

        <section class="history-section">
          <div class="section-title"><strong>会话兼容与恢复</strong><span>仅在手动点击后检查并修复因切换 Provider 产生的不兼容本地记录；先创建可恢复备份。</span></div>
          <Button variant="plain" :loading="repairingHistory" :disabled="repairingHistory || saving" @click="repairHistory">检查并修复兼容历史</Button>
        </section>

        <details class="backup-section">
          <summary>配置备份（{{ backupItems.length }}）</summary>
          <p v-if="!backupItems.length">尚无 XIASS Tools 创建的配置备份。</p>
          <div v-for="backup in backupItems" :key="backup.id" class="backup-row">
            <div><strong>{{ backup.reason }}</strong><span>{{ formatTime(backup.created_at) }}</span></div>
            <div><button type="button" :disabled="actionID" @click="confirmConfigurationRestore(backup.id)">恢复</button><button type="button" :disabled="actionID" @click="confirmConfigurationDelete(backup.id)">删除</button></div>
          </div>
        </details>

        <details class="backup-section">
          <summary>历史备份（{{ historyBackupItems.length }}）</summary>
          <p v-if="!historyBackupItems.length">尚无 XIASS Tools 创建的历史修复备份。</p>
          <div v-for="backup in historyBackupItems" :key="backup.id" class="backup-row">
            <div><strong>{{ backup.target_provider || "Provider 修复" }}</strong><span>{{ formatTime(backup.created_at) }} · 已处理 {{ backup.sanitized_records || 0 }} 条记录</span></div>
            <div><button type="button" :disabled="actionID" @click="confirmHistoryRestore(backup.id)">恢复</button><button type="button" :disabled="actionID" @click="confirmHistoryDelete(backup.id)">删除</button></div>
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
              <div><button type="button" :disabled="Boolean(actionID)" @click="confirmLegacyConfigurationImport(backup.source_id)">{{ actionID === `legacy:config:${backup.source_id}` ? "正在导入…" : "导入为新的恢复点" }}</button></div>
            </div>
          </div>

          <div v-if="legacyHistoryBackups.length" class="legacy-backup-group">
            <strong>旧历史备份</strong>
            <div v-for="backup in legacyHistoryBackups" :key="`legacy-history:${backup.source_id}`" class="backup-row">
              <div><strong>{{ backup.target_provider || "旧版历史备份" }}</strong><span>{{ formatTime(backup.created_at) }}</span></div>
              <div><button type="button" :disabled="Boolean(actionID)" @click="confirmLegacyHistoryImport(backup.source_id)">{{ actionID === `legacy:history:${backup.source_id}` ? "正在导入…" : "导入为新的恢复点" }}</button></div>
            </div>
          </div>
        </details>
      </template>
    </div>
    <template #footer>
      <Button variant="plain" :disabled="saving || discovering || repairingHistory" @click="close">关闭</Button>
      <Button variant="filled" :loading="saving" :disabled="loading || saving || discovering || repairingHistory" @click="save">安全保存配置</Button>
    </template>
  </Modal>
</template>

<style scoped>
.codex-config { display: grid; max-height: min(68vh, 700px); gap: 15px; overflow: auto; padding-right: 3px; }
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
	.restart-guidance { display: grid; gap: 3px; border: 1px solid color-mix(in srgb, var(--orange) 34%, var(--separator)); border-left: 3px solid var(--orange); border-radius: 10px; background: color-mix(in srgb, var(--orange) 7%, var(--bg-inset)); padding: 10px 11px; }
	.restart-guidance strong { color: var(--text-primary); font-size: 12px; }
	.restart-guidance span { color: var(--text-secondary); font-size: 11px; line-height: 1.5; }
.feedback { border: 1px solid var(--separator); border-radius: 9px; padding: 9px 10px; font-size: 12px; line-height: 1.5; }
.feedback.error { border-color: color-mix(in srgb, var(--red) 35%, transparent); background: color-mix(in srgb, var(--red) 8%, transparent); color: var(--red); }
.feedback.success { border-color: color-mix(in srgb, var(--green) 35%, transparent); background: color-mix(in srgb, var(--green) 8%, transparent); color: var(--green); }
.form-section, .history-section { display: grid; gap: 10px; border-top: 1px solid var(--separator); padding-top: 14px; }
.section-title { display: grid; gap: 3px; }
.section-title strong { color: var(--text-primary); font-size: 13px; }
.section-title span { color: var(--text-tertiary); font-size: 11px; line-height: 1.5; }
.split-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.field { display: grid; min-width: 0; gap: 5px; }
.field.wide { width: 100%; }
.field span { color: var(--text-tertiary); font-size: 11px; }
.field input, .field select { width: 100%; min-width: 0; border: 1px solid var(--separator-strong); border-radius: 8px; outline: none; background: var(--bg-base); color: var(--text-primary); font: inherit; font-size: 12px; padding: 9px 10px; }
.field input:focus, .field select:focus { border-color: var(--accent-strong); box-shadow: 0 0 0 3px var(--accent-soft); }
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
	@media (max-width: 680px) { .split-fields { grid-template-columns: 1fr; } .config-status > div { display: grid; } .config-status span { text-align: left; } .xiass-key-actions, .manual-callback > div, .legacy-migration { align-items: stretch; flex-direction: column; } .context-heading { align-items: flex-start; flex-direction: column; } .context-heading > span { text-align: left; } }
</style>
