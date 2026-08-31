<script setup>
import { computed, nextTick, ref, watch } from "vue";
import Modal from "@/components/ui/Modal.vue";
import Button from "@/components/ui/Button.vue";
import {
  getClaudeCodeConfiguration,
  applyClaudeCodeConfiguration,
  getClaudeCodeAccountCandidates,
  applyClaudeCodeConfigurationFromAccount,
  discoverClaudeCodeGatewayModels,
  testClaudeCodeGateway,
  restoreClaudeCodeConfiguration,
  deleteClaudeCodeConfigurationBackup,
  migrateClaudeCodeLegacyBackup,
} from "@/state/appState";

const props = defineProps({
  open: Boolean,
  inline: Boolean,
  section: { type: String, default: "" },
});
const emit = defineEmits(["close", "changed"]);

const data = ref(null);
const configRoot = ref(null);
const draft = ref({
  baseUrl: "",
  credentialMode: "auth_token",
  credential: "",
  apiKeyHelper: "",
  enableGatewayModelDiscovery: false,
  model: "",
});
const loading = ref(false);
const saving = ref(false);
const actionID = ref("");
const error = ref("");
const notice = ref("");
const gatewayModels = ref([]);
const gatewayModelStatus = ref(null);
const gatewayTestStatus = ref(null);
// Only explicitly redacted account metadata is allowed into this component.
// Credentials, endpoints, headers and OAuth materials stay in the native
// account vault and are never added to Vue state.
const savedAccountCandidates = ref([]);
const savedAccountCandidatesLoading = ref(false);
const savedAccountCandidatesMessage = ref("");
const selectedSavedAccountID = ref("");
const selectedSavedAccountModel = ref("");
const applyingSavedAccount = ref(false);

const credentialModes = Object.freeze([
  { value: "auth_token", label: "Bearer 令牌", hint: "写入 ANTHROPIC_AUTH_TOKEN，并以 Authorization: Bearer 发送。" },
  { value: "api_key", label: "API Key", hint: "写入 ANTHROPIC_API_KEY，并以 X-Api-Key 发送。" },
  { value: "api_key_helper", label: "密钥脚本", hint: "写入 apiKeyHelper；Claude Code 会在自身受控环境中执行它。" },
]);

const snapshot = computed(() => data.value?.snapshot || {});
const busy = computed(() => loading.value || saving.value || applyingSavedAccount.value || Boolean(actionID.value));
const valid = computed(() => Boolean(snapshot.value.valid));
// A readable settings.json alone is not enough to permit a mutation. The
// native result also proves that a verified rollback point can be created.
// Keep one-shot gateway discovery/test separate because they do not write
// Claude Code settings and always use only the credential entered this time.
const canManage = computed(() => valid.value && data.value?.ok === true);
const helperMode = computed(() => draft.value.credentialMode === "api_key_helper");
const gatewayDiscoveryLabel = computed(() => {
  if (!snapshot.value.gatewayModelDiscoveryEnabled) return "未启用";
  return snapshot.value.gatewayModelDiscoveryBlocked ? "已启用，但当前受限" : "已启用";
});
const gatewayDiscoveryClass = computed(() => {
  if (snapshot.value.gatewayModelDiscoveryBlocked) return "blocked";
  return snapshot.value.gatewayModelDiscoveryEnabled ? "configured" : "missing";
});
const selectedCredentialMode = computed(() => credentialModes.find((mode) => mode.value === draft.value.credentialMode) || credentialModes[0]);
const currentCredentialLabel = computed(() => {
  if (!snapshot.value.credentialConfigured) return "未配置";
  if (snapshot.value.credentialMode === "api_key") return "API Key 已配置";
  if (snapshot.value.credentialMode === "api_key_helper") return "密钥脚本已配置";
  return "Bearer 令牌已配置";
});
const directCredentialReady = computed(() => Boolean(draft.value.credential.trim()) && !helperMode.value);
const readyToSave = computed(() => Boolean(
	canManage.value
  &&
  draft.value.baseUrl.trim()
  && draft.value.model.trim()
  && (helperMode.value ? draft.value.apiKeyHelper.trim() : draft.value.credential.trim()),
));
const readyToDiscover = computed(() => Boolean(draft.value.baseUrl.trim()) && directCredentialReady.value);
const readyToTest = computed(() => readyToDiscover.value && Boolean(draft.value.model.trim()));
const backups = computed(() => data.value?.backups || []);
const legacyBackups = computed(() => data.value?.legacyBackups || []);
const selectedSavedAccount = computed(() => savedAccountCandidates.value.find((candidate) => candidate.id === selectedSavedAccountID.value) || null);
const selectedSavedAccountModels = computed(() => selectedSavedAccount.value?.models || []);
const canApplySavedAccount = computed(() => Boolean(
  canManage.value
  && selectedSavedAccount.value?.id
  && selectedSavedAccountModel.value
  && !busy.value,
));

// Claude's native backup DTO deliberately uses the established created_at
// field. Accept the former camel-case spelling only for an already-running
// older binary, so timestamps stay readable during an in-place upgrade.
function backupCreatedAt(backup) {
  return backup?.created_at || backup?.createdAt || "";
}

function formatBackupTime(backup) {
  const raw = backupCreatedAt(backup);
  const timestamp = Date.parse(raw);
  return Number.isFinite(timestamp) ? new Date(timestamp).toLocaleString() : "时间不可用";
}

function resetVisibleCredentials() {
  draft.value.credential = "";
  draft.value.apiKeyHelper = "";
}

function resetGatewayResults() {
  gatewayModels.value = [];
  gatewayModelStatus.value = null;
  gatewayTestStatus.value = null;
}

function cleanText(value) {
  return typeof value === "string" ? value.trim() : "";
}

function normalizeSavedAccountModel(value) {
  const id = cleanText(value?.id);
  if (!id) return null;
  return { id, displayName: cleanText(value?.displayName) || id };
}

// Treat the native response as an untrusted boundary even though the backend
// is deliberately redacted: select and retain only the four display fields
// this view needs. A future bridge field cannot accidentally enter reactive
// state, devtools, logs, or browser storage through this component.
function normalizeSavedAccountCandidate(value) {
  const id = cleanText(value?.id);
  if (!id) return null;
  const seenModels = new Set();
  const models = Array.isArray(value?.models)
    ? value.models
      .map(normalizeSavedAccountModel)
      .filter((model) => model && !seenModels.has(model.id) && (seenModels.add(model.id), true))
    : [];
  return {
    id,
    label: cleanText(value?.label) || "Claude 兼容账户",
    credentialMode: cleanText(value?.credentialMode) || "凭据已保存",
    models,
  };
}

function clearSavedAccountSelection({ clearCandidates = false } = {}) {
  selectedSavedAccountID.value = "";
  selectedSavedAccountModel.value = "";
  if (clearCandidates) {
    savedAccountCandidates.value = [];
    savedAccountCandidatesMessage.value = "";
  }
}

async function refreshSavedAccountCandidates() {
  savedAccountCandidatesLoading.value = true;
  try {
    const result = await getClaudeCodeAccountCandidates();
    const candidates = Array.isArray(result?.candidates)
      ? result.candidates.map(normalizeSavedAccountCandidate).filter(Boolean)
      : [];
    savedAccountCandidates.value = candidates;
    savedAccountCandidatesMessage.value = cleanText(result?.message)
      || (candidates.length ? "已读取可复用的 Claude Code 兼容账户。" : "没有可直接用于 Claude Code 的已启用账户。");
    const selected = candidates.find((candidate) => candidate.id === selectedSavedAccountID.value);
    if (!selected) {
      clearSavedAccountSelection();
    } else if (!selected.models.some((model) => model.id === selectedSavedAccountModel.value)) {
      selectedSavedAccountModel.value = selected.models.length === 1 ? selected.models[0].id : "";
    }
  } catch {
    savedAccountCandidates.value = [];
    savedAccountCandidatesMessage.value = "无法读取本机账户池；你仍可使用下方手动配置。";
    clearSavedAccountSelection();
  } finally {
    savedAccountCandidatesLoading.value = false;
  }
}

function selectSavedAccount(candidate) {
  if (!candidate?.id || busy.value) return;
  selectedSavedAccountID.value = candidate.id;
  selectedSavedAccountModel.value = candidate.models.length === 1 ? candidate.models[0].id : "";
}

function chooseSavedAccountModel(model) {
  if (!model?.id || busy.value) return;
  selectedSavedAccountModel.value = model.id;
}

async function applySavedAccount() {
  error.value = "";
  notice.value = "";
  const candidate = selectedSavedAccount.value;
  const model = cleanText(selectedSavedAccountModel.value);
  if (!canManage.value || !candidate?.id || !model) {
    error.value = "请选择一个已启用的 Claude 兼容账户及其已绑定模型。";
    return;
  }
  const request = {
    accountId: candidate.id,
    model,
    enableGatewayModelDiscovery: Boolean(draft.value.enableGatewayModelDiscovery),
  };
  applyingSavedAccount.value = true;
  try {
    const result = await applyClaudeCodeConfigurationFromAccount(request);
    request.accountId = "";
    request.model = "";
    applyStatus(result);
    if (!result?.ok) {
      error.value = "未将已保存账户应用到 Claude Code。请刷新账户池后重新选择，或改用手动配置。";
      return;
    }
    notice.value = "已将所选账户安全应用到 Claude Code，并创建可恢复备份。凭据未进入界面。";
    emit("changed");
  } catch {
    request.accountId = "";
    request.model = "";
    error.value = "未将已保存账户应用到 Claude Code。";
  } finally {
    request.accountId = "";
    request.model = "";
    applyingSavedAccount.value = false;
  }
}

function applyStatus(result, { updateDraft = true } = {}) {
  data.value = result || null;
  if (!updateDraft) return;
  const next = result?.snapshot || {};
  draft.value = {
    baseUrl: next.baseUrl || "",
    credentialMode: next.credentialMode || "auth_token",
    credential: "",
    apiKeyHelper: "",
    enableGatewayModelDiscovery: Boolean(next.gatewayModelDiscoveryEnabled),
    model: next.model || "",
  };
  resetGatewayResults();
}

async function refresh() {
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const result = await getClaudeCodeConfiguration();
    applyStatus(result);
    if (!result?.ok) {
      error.value = "无法读取 Claude Code 用户设置。请确认 settings.json 是有效的普通 JSON 文件。";
      return;
    }
    notice.value = "已读取 Claude Code 用户设置。";
  } catch {
    data.value = null;
    resetVisibleCredentials();
    resetGatewayResults();
    error.value = "无法读取 Claude Code 用户设置。";
  } finally {
    loading.value = false;
  }
}

async function save() {
  error.value = "";
  notice.value = "";
	if (!canManage.value) {
		error.value = "当前 Claude Code 设置或恢复备份位置无法安全验证，因此禁止保存或改写。可在恢复安全状态后重新打开此页面。";
		return;
	}
  if (!readyToSave.value) {
    error.value = helperMode.value
      ? "请填写 API 根地址、密钥脚本与模型名称。XIASS Tools 不会执行或读取已有脚本。"
      : "请填写 API 根地址、凭据与模型名称。保存时需要重新输入凭据；本工具不会读取已有令牌。";
    return;
  }

  saving.value = true;
  const request = {
    baseUrl: draft.value.baseUrl,
    credentialMode: draft.value.credentialMode,
    credential: draft.value.credential,
    apiKeyHelper: draft.value.apiKeyHelper,
    enableGatewayModelDiscovery: draft.value.enableGatewayModelDiscovery,
    model: draft.value.model,
  };
  // The reactive field is cleared before any native result is rendered. The
  // request object remains local to this function and is discarded afterwards.
  resetVisibleCredentials();
  try {
    const result = await applyClaudeCodeConfiguration(request);
    request.credential = "";
    request.apiKeyHelper = "";
    applyStatus(result);
    if (!result?.ok) {
      error.value = "未保存 Claude Code 用户设置。请检查填写内容与当前 settings.json 后重试。";
      return;
    }
    notice.value = "Claude Code 用户设置已安全保存，并创建可恢复备份。";
    emit("changed");
  } catch {
    request.credential = "";
    request.apiKeyHelper = "";
    error.value = "未保存 Claude Code 用户设置。请检查填写内容后重试。";
  } finally {
    request.credential = "";
    request.apiKeyHelper = "";
    saving.value = false;
  }
}

function gatewayRequest() {
  return {
    baseUrl: draft.value.baseUrl,
    credentialMode: draft.value.credentialMode,
    credential: draft.value.credential,
    model: draft.value.model,
  };
}

function clearGatewayRequest(request) {
  request.credential = "";
}

async function discoverModels() {
  error.value = "";
  notice.value = "";
  gatewayTestStatus.value = null;
  if (!readyToDiscover.value) {
    error.value = helperMode.value
      ? "密钥脚本仅由 Claude Code 执行。请先保存设置，再在 Claude Code 中使用 /model 验证网关模型目录。"
      : "请填写 API 根地址和本次用于检查的凭据。";
    return;
  }
  const request = gatewayRequest();
  actionID.value = "discover-gateway-models";
  try {
    const result = await discoverClaudeCodeGatewayModels(request);
    clearGatewayRequest(request);
    gatewayModelStatus.value = result || null;
    gatewayModels.value = Array.isArray(result?.models) ? result.models : [];
    if (!result?.ok) {
      error.value = "未获取到 Claude Code 网关模型目录。请检查本次填写的地址、凭据和 Anthropic Messages 兼容性。";
      return;
    }
    notice.value = gatewayModels.value.length
      ? "网关模型目录已获取。选择一个模型后可保存到 Claude Code。"
      : "网关已响应，但没有返回可被 Claude Code 选择的 Claude / Anthropic 模型。";
  } catch {
    clearGatewayRequest(request);
    gatewayModels.value = [];
    gatewayModelStatus.value = null;
    error.value = "未获取到 Claude Code 网关模型目录。";
  } finally {
    clearGatewayRequest(request);
    // A direct gateway check is finished now, so remove the credential from
    // Vue's reactive draft as well as from the short-lived request object.
    // Saving after a check deliberately requires a fresh explicit entry.
    resetVisibleCredentials();
    actionID.value = "";
  }
}

async function testGatewayConnection() {
  error.value = "";
  notice.value = "";
  if (!readyToTest.value) {
    error.value = helperMode.value
      ? "密钥脚本仅由 Claude Code 执行。XIASS Tools 不会运行任意本地脚本进行远程测试。"
      : "请填写 API 根地址、本次凭据和要测试的模型。";
    return;
  }
  const request = gatewayRequest();
  actionID.value = "test-gateway-messages";
  try {
    const result = await testClaudeCodeGateway(request);
    clearGatewayRequest(request);
    gatewayTestStatus.value = result || null;
    if (!result?.ok) {
      error.value = "Claude Messages 实际请求未通过。请核对模型、凭据与网关协议。";
      return;
    }
    notice.value = "Claude Messages 实际请求成功；保存后 Claude Code 会使用同一配置。";
  } catch {
    clearGatewayRequest(request);
    gatewayTestStatus.value = null;
    error.value = "Claude Messages 实际请求未通过。";
  } finally {
    clearGatewayRequest(request);
    // See discoverModels: never keep a completed test credential in the
    // reactive component state until the modal is closed.
    resetVisibleCredentials();
    actionID.value = "";
  }
}

function chooseGatewayModel(model) {
  if (!model?.id || busy.value) return;
  draft.value.model = model.id;
  gatewayTestStatus.value = null;
}

async function runBackupAction(action, backupID) {
  error.value = "";
  notice.value = "";
  actionID.value = `${action}:${backupID}`;
  try {
    const result = action === "restore"
      ? await restoreClaudeCodeConfiguration(backupID)
      : await deleteClaudeCodeConfigurationBackup(backupID);
    applyStatus(result, { updateDraft: action === "restore" });
    if (!result?.ok) {
      error.value = "Claude Code 用户设置备份操作未完成。请确认备份仍可安全验证。";
      return;
    }
    notice.value = action === "restore" ? "已恢复用户设置备份。" : "已删除用户设置备份。";
    emit("changed");
  } catch {
    error.value = "Claude Code 用户设置备份操作未完成。";
  } finally {
    actionID.value = "";
  }
}

function confirmBackupAction(action, backupID) {
  const message = action === "restore"
    ? "将恢复这份 Claude Code 用户设置备份。当前 settings.json 会先自动备份；不会影响登录、OAuth、会话、MCP 或项目设置。是否继续？"
    : "确定删除这份 Claude Code 用户设置备份吗？该操作不可恢复。";
  if (!window.confirm(message)) return;
  void runBackupAction(action, backupID);
}

async function migrateLegacyBackup(backup) {
  if (!window.confirm("将这份旧版备份复制为新的 XIASS Tools 恢复点。不会恢复、修改或删除旧备份，是否继续？")) return;
  error.value = "";
  notice.value = "";
  actionID.value = `migrate:${backup.source}:${backup.id}`;
  try {
    const result = await migrateClaudeCodeLegacyBackup(backup.source, backup.id);
    applyStatus(result, { updateDraft: false });
    if (!result?.ok) {
      error.value = "旧版 Claude Code 用户设置备份未导入。该备份可能未通过安全校验。";
      return;
    }
    notice.value = "旧版备份已复制为新的恢复点。";
    emit("changed");
  } catch {
    error.value = "旧版 Claude Code 用户设置备份未导入。";
  } finally {
    actionID.value = "";
  }
}

function close() {
  if (busy.value) return;
  resetVisibleCredentials();
  resetGatewayResults();
  clearSavedAccountSelection({ clearCandidates: true });
  emit("close");
}

async function focusRequestedSection() {
  const section = ["gateway", "model-test", "backups"].includes(props.section)
    ? props.section
    : "";
  if (!section) return;
  await nextTick();
  const target = configRoot.value?.querySelector(`[data-section="${section}"]`);
  if (!target) return;
  if (target instanceof HTMLDetailsElement) target.open = true;
  await nextTick();
  target.scrollIntoView({ block: "start", behavior: "auto" });
}

watch(() => [props.open, props.section], ([open], [wasOpen]) => {
  if (open && !wasOpen) {
    void Promise.all([refresh(), refreshSavedAccountCandidates()]).then(focusRequestedSection);
    return;
  }
  if (open) {
    void focusRequestedSection();
    return;
  }
  resetVisibleCredentials();
  resetGatewayResults();
  clearSavedAccountSelection({ clearCandidates: true });
  error.value = "";
  notice.value = "";
});

// Changing the credential mechanism must not retain a now-hidden credential
// or helper command. It also invalidates previous direct gateway results,
// which were performed with the former mode.
watch(() => draft.value.credentialMode, () => {
  resetVisibleCredentials();
  resetGatewayResults();
});
</script>

<template>
  <Modal :open="open" title="配置 Claude Code" wide persistent :inline="inline" :closable="!busy" @close="close">
    <div ref="configRoot" class="claude-config">
      <p class="intro">
        XIASS Tools 仅管理 Claude Code 用户 <code>settings.json</code> 中的 API 根地址、一个显式凭据方式、模型和网关模型目录开关。
        不读取或管理登录、OAuth、账号额度、会话、MCP、项目配置或托管设置。
      </p>

      <div v-if="loading" class="state-block">正在读取本机 Claude Code 用户设置…</div>

      <template v-else>
        <section class="status-card" :class="{ invalid: !canManage }">
          <div>
            <strong>{{ canManage ? (snapshot.managed ? "XIASS Tools 已配置" : "已发现用户设置") : (valid ? "当前为只读检查模式" : "用户设置需要处理") }}</strong>
            <span>{{ snapshot.exists ? "settings.json 已存在" : "尚未创建 settings.json" }}</span>
          </div>
          <span class="status-pill" :class="canManage ? 'ok' : 'warn'">{{ canManage ? "可安全管理" : "不可写入" }}</span>
        </section>

        <div v-if="snapshot.baseUrl || snapshot.model || snapshot.credentialConfigured" class="current-state">
          <div v-if="snapshot.baseUrl"><span>当前 API 根地址</span><code>{{ snapshot.baseUrl }}</code></div>
          <div v-if="snapshot.model"><span>当前模型</span><code>{{ snapshot.model }}</code></div>
          <div><span>凭据方式</span><strong :class="snapshot.credentialConfigured ? 'configured' : 'missing'">{{ currentCredentialLabel }}</strong></div>
          <div><span>网关模型目录</span><strong :class="gatewayDiscoveryClass">{{ gatewayDiscoveryLabel }}</strong></div>
        </div>

        <section class="saved-account-section" :aria-busy="savedAccountCandidatesLoading ? 'true' : 'false'">
          <div class="section-heading">
            <div>
              <strong>使用已保存的上游账户</strong>
              <span>仅列出已启用、Claude Messages 兼容且可无损映射的账户。界面只接收名称、认证方式与已绑定模型；密钥、地址、请求头和 OAuth 凭据始终留在本机原生层。</span>
            </div>
            <button type="button" class="saved-account-refresh" :disabled="savedAccountCandidatesLoading || busy" @click="refreshSavedAccountCandidates">{{ savedAccountCandidatesLoading ? "读取中…" : "刷新账户" }}</button>
          </div>

          <p v-if="savedAccountCandidatesMessage" class="saved-account-message">{{ savedAccountCandidatesMessage }}</p>

          <div v-if="savedAccountCandidates.length" class="saved-account-grid" role="group" aria-label="可复用 Claude Code 账户">
            <button
              v-for="candidate in savedAccountCandidates"
              :key="candidate.id"
              type="button"
              class="saved-account-card"
              :class="{ selected: selectedSavedAccountID === candidate.id }"
              :aria-pressed="selectedSavedAccountID === candidate.id"
              :disabled="busy"
              @click="selectSavedAccount(candidate)"
            >
              <strong>{{ candidate.label }}</strong>
              <span>{{ candidate.credentialMode }}</span>
              <small>{{ candidate.models.length ? `${candidate.models.length} 个已绑定模型` : "暂无已绑定模型" }}</small>
            </button>
          </div>

          <div v-if="selectedSavedAccount" class="saved-account-selection">
            <div class="saved-account-selection-copy">
              <strong>选择 {{ selectedSavedAccount.label }} 的模型</strong>
              <span>仅可选用该账户已绑定的 Claude / Anthropic 模型；“启动时从网关发现模型”使用下方的同一开关。</span>
            </div>
            <div v-if="selectedSavedAccountModels.length" class="saved-account-models" role="group" aria-label="已绑定 Claude 模型">
              <button
                v-for="model in selectedSavedAccountModels"
                :key="model.id"
                type="button"
                :class="{ selected: selectedSavedAccountModel === model.id }"
                :aria-pressed="selectedSavedAccountModel === model.id"
                :disabled="busy"
                @click="chooseSavedAccountModel(model)"
              >
                <strong>{{ model.displayName }}</strong>
                <span v-if="model.displayName !== model.id">{{ model.id }}</span>
              </button>
            </div>
            <p v-else class="saved-account-empty">该账户尚未绑定 Claude 模型。请先在“模型”页面为它添加模型，或使用下方手动配置。</p>
            <div class="saved-account-actions">
              <span>应用时会先创建可恢复的 <code>settings.json</code> 备份，不会触碰 Claude Code 登录、会话、MCP 或项目配置。</span>
              <Button variant="filled" size="sm" :disabled="!canApplySavedAccount" :loading="applyingSavedAccount" @click="applySavedAccount">应用到 Claude Code</Button>
            </div>
          </div>
        </section>

        <p v-if="notice" class="notice" role="status">{{ notice }}</p>
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <p v-if="valid && !canManage" class="gateway-compatibility-warning" role="status">
          当前用户设置可读取，但 XIASS Tools 无法安全验证可恢复备份位置。为保护现有配置，保存与修改已禁用；使用本次手动输入凭据的模型目录获取和 Claude Messages 单次检查仍不会写入设置。
        </p>

        <section class="configuration-section" data-section="gateway" :aria-disabled="!valid">
          <div class="section-heading">
            <div>
              <strong>用户设置</strong>
              <span>保存时只写入一个凭据方式；旧的冲突凭据会被安全移除，避免 Claude Code 使用了错误优先级。</span>
            </div>
          </div>

          <label class="field">
            <span>API 根地址</span>
            <input v-model="draft.baseUrl" autocomplete="url" inputmode="url" spellcheck="false" placeholder="https://gateway.example.com" :disabled="!valid || busy" />
            <small>远程地址必须使用 HTTPS；本机 localhost 或回环地址可使用 HTTP。检查会兼容根地址与已包含 <code>/v1</code> 的地址。</small>
          </label>

          <label class="field">
            <span>凭据方式</span>
            <select v-model="draft.credentialMode" :disabled="!valid || busy">
              <option v-for="mode in credentialModes" :key="mode.value" :value="mode.value">{{ mode.label }}</option>
            </select>
            <small>{{ selectedCredentialMode.hint }}</small>
          </label>

          <label v-if="!helperMode" class="field">
            <span>{{ draft.credentialMode === "api_key" ? "API Key" : "Bearer 令牌" }}</span>
            <input v-model="draft.credential" type="password" autocomplete="new-password" spellcheck="false" placeholder="仅用于此次安全保存、获取模型和连接测试" :disabled="!valid || busy" />
            <small>每次保存和测试都需要主动输入；完成一次获取模型或连接测试后会立即清空，保存前需重新填写。</small>
          </label>

          <label v-else class="field helper-field">
            <span>apiKeyHelper 命令</span>
            <input v-model="draft.apiKeyHelper" autocomplete="off" spellcheck="false" placeholder="例如 ~/bin/get-claude-key" :disabled="!valid || busy" />
            <small>Claude Code 会在自己的环境中调用它。只有在你信任该命令时才保存；XIASS Tools 不执行此命令，也不会用它进行远程测试。</small>
          </label>

          <label class="field">
            <span>模型</span>
            <input v-model="draft.model" autocomplete="off" spellcheck="false" placeholder="claude-sonnet-4-5" :disabled="!valid || busy" />
            <small>这是 Claude Code 的显式用户模型设置。可通过下方网关目录选择，或手动填写已知模型 ID。</small>
          </label>

          <label class="discovery-toggle">
            <input v-model="draft.enableGatewayModelDiscovery" type="checkbox" :disabled="!valid || busy" />
            <span>
              <strong>启动时从网关发现模型</strong>
              <small>保存 <code>CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1</code>；在未配置其他 Claude Code Provider 路由且未禁用非必要流量时，Claude Code 会在启动时请求网关的 <code>/v1/models?limit=1000</code>。</small>
            </span>
          </label>

          <p v-if="snapshot.gatewayModelDiscoveryBlocked" class="gateway-compatibility-warning" role="status">
            已保存标准网关模型发现开关，但检测到本机 Claude Code 的 Provider 路由或非必要流量限制；Claude Code 当前不会执行标准网关模型发现。XIASS Tools 不会自动删除这些用户管理的设置。
          </p>

          <section class="gateway-checks" data-section="model-test" :class="{ unavailable: helperMode }">
            <div class="gateway-heading">
              <div>
                <strong>网关实际检查</strong>
                <span>仅使用本次填写的凭据；不会读取已保存的凭据，也不会自动重试。</span>
              </div>
              <div class="gateway-actions">
                <button type="button" :disabled="!valid || !readyToDiscover || busy" @click="discoverModels">获取模型</button>
                <button type="button" :disabled="!valid || !readyToTest || busy" @click="testGatewayConnection">测试 Messages</button>
              </div>
            </div>

            <p v-if="helperMode" class="gateway-note">密钥脚本模式下，XIASS Tools 不执行任意本机命令。保存后请在 Claude Code 中使用 <code>/model</code> 与 <code>/status</code> 验证网关。</p>

            <div v-if="gatewayModelStatus" class="gateway-result" :class="gatewayModelStatus.ok ? 'success' : 'failed'">
              <strong>{{ gatewayModelStatus.ok ? `模型目录响应 · ${gatewayModels.length} 个可用项` : "模型目录检查未通过" }}</strong>
              <span>HTTP {{ gatewayModelStatus.httpStatus || "—" }} · {{ gatewayModelStatus.durationMs ?? 0 }} ms</span>
            </div>

            <div v-if="gatewayModels.length" class="gateway-models" aria-label="网关模型目录">
              <button v-for="model in gatewayModels" :key="model.id" type="button" :class="{ selected: draft.model === model.id }" :disabled="busy" @click="chooseGatewayModel(model)">
                <strong>{{ model.displayName || model.id }}</strong>
                <span v-if="model.displayName">{{ model.id }}</span>
              </button>
            </div>

            <div v-if="gatewayTestStatus" class="gateway-result" :class="gatewayTestStatus.ok ? 'success' : 'failed'">
              <strong>{{ gatewayTestStatus.ok ? "Claude Messages 实际请求成功" : "Claude Messages 实际请求未通过" }}</strong>
              <span>HTTP {{ gatewayTestStatus.httpStatus || "—" }} · {{ gatewayTestStatus.durationMs ?? 0 }} ms · 单次最小请求</span>
            </div>
          </section>
        </section>

        <details class="backup-section" data-section="backups">
          <summary>可恢复备份 <span>{{ backups.length }}</span></summary>
          <p>备份只包含这一个用户 settings.json 的校验副本，不包含账号、OAuth、会话或项目数据。</p>
          <div v-if="!backups.length" class="empty-backups">尚无可恢复备份。</div>
          <article v-for="backup in backups" :key="backup.id" class="backup-row">
            <div>
              <strong>{{ backup.reason || "用户设置备份" }}</strong>
              <span>{{ formatBackupTime(backup) }}</span>
            </div>
            <div>
              <button type="button" :disabled="busy" @click="confirmBackupAction('restore', backup.id)">恢复</button>
              <button type="button" :disabled="busy" @click="confirmBackupAction('delete', backup.id)">删除</button>
            </div>
          </article>
        </details>

        <details v-if="legacyBackups.length || data?.legacyBackupWarning" class="backup-section legacy-section">
          <summary>旧版备份迁移 <span>{{ legacyBackups.length }}</span></summary>
          <p v-if="data?.legacyBackupWarning" class="warning-copy">{{ data.legacyBackupWarning }}</p>
          <p v-else>仅可复制已校验的旧版备份为新的恢复点；旧备份保持不变。</p>
          <article v-for="backup in legacyBackups" :key="`${backup.source}:${backup.id}`" class="backup-row">
            <div>
              <strong>{{ backup.reason || "旧版用户设置备份" }}</strong>
              <span>{{ formatBackupTime(backup) }}</span>
            </div>
            <div><button type="button" :disabled="busy" @click="migrateLegacyBackup(backup)">复制为恢复点</button></div>
          </article>
        </details>
      </template>
    </div>

    <template #footer>
      <Button variant="plain" :disabled="busy" @click="close">关闭</Button>
      <Button variant="filled" :disabled="!canManage || !readyToSave || busy" :loading="saving" @click="save">保存用户设置</Button>
    </template>
  </Modal>
</template>

<style scoped>
.claude-config [data-section] { scroll-margin-top: 16px; }
.claude-config { display: grid; min-width: 0; gap: 13px; overflow-wrap: anywhere; }
.intro { margin: 0; color: var(--text-secondary); font-size: 12px; line-height: 1.65; }
.intro code, .current-state code { color: var(--accent-strong); font-family: var(--font-num); font-size: .95em; }
.state-block { min-height: 110px; display: grid; place-items: center; border: 1px dashed var(--separator-strong); border-radius: 10px; color: var(--text-tertiary); font-size: 12px; }
.status-card { display: flex; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid color-mix(in srgb, var(--green) 42%, var(--separator)); border-left: 3px solid var(--green); border-radius: 10px; background: color-mix(in srgb, var(--green) 6%, var(--bg-inset)); padding: 11px 12px; }
.status-card.invalid { border-color: color-mix(in srgb, var(--orange) 52%, var(--separator)); border-left-color: var(--orange); background: color-mix(in srgb, var(--orange) 7%, var(--bg-inset)); }
.status-card > div { display: grid; gap: 3px; min-width: 0; }
.status-card strong { color: var(--text-primary); font-size: 13px; }
.status-card span { color: var(--text-tertiary); font-size: 11px; }
.status-pill { flex: 0 0 auto; border-radius: 999px; font-size: 10px; font-weight: 720; padding: 4px 7px; }
.status-pill.ok { background: color-mix(in srgb, var(--green) 15%, transparent); color: var(--green); }
.status-pill.warn { background: color-mix(in srgb, var(--orange) 16%, transparent); color: var(--orange); }
.current-state { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; }
.current-state > div { display: grid; min-width: 0; gap: 3px; border: 1px solid var(--separator); border-radius: 8px; background: var(--bg-inset); padding: 8px 9px; }
.current-state span { color: var(--text-tertiary); font-size: 10px; }
.current-state code { overflow: hidden; color: var(--text-secondary); text-overflow: ellipsis; white-space: nowrap; }
.current-state strong { font-size: 11px; }
.configured { color: var(--green); }
.blocked { color: var(--orange); }
.missing { color: var(--text-tertiary); }
.saved-account-section { display: grid; gap: 9px; border: 1px solid color-mix(in srgb, var(--accent-strong) 30%, var(--separator)); border-left: 3px solid var(--accent-strong); border-radius: 10px; background: color-mix(in srgb, var(--accent-soft) 42%, var(--bg-inset)); padding: 10px 11px; }
.saved-account-refresh { flex: 0 0 auto; border: 1px solid var(--accent-border); border-radius: 7px; background: var(--bg-base); color: var(--accent-strong); font: inherit; font-size: 10px; font-weight: 700; padding: 6px 8px; }
.saved-account-refresh:hover:not(:disabled) { border-color: var(--accent-strong); background: var(--accent-soft); }
.saved-account-refresh:disabled { cursor: wait; opacity: .5; }
.saved-account-message { margin: 0; color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.saved-account-grid, .saved-account-models { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 6px; }
.saved-account-card, .saved-account-models button { display: grid; min-width: 0; gap: 2px; border: 1px solid var(--separator); border-radius: 8px; background: var(--bg-base); color: var(--text-secondary); text-align: left; padding: 8px 9px; }
.saved-account-card:hover:not(:disabled), .saved-account-card.selected, .saved-account-models button:hover:not(:disabled), .saved-account-models button.selected { border-color: var(--accent-strong); background: var(--accent-soft); color: var(--accent-strong); }
.saved-account-card:disabled, .saved-account-models button:disabled { cursor: not-allowed; opacity: .5; }
.saved-account-card strong, .saved-account-card span, .saved-account-card small, .saved-account-models strong, .saved-account-models span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.saved-account-card strong, .saved-account-models strong { font-size: 11px; }
.saved-account-card span, .saved-account-card small, .saved-account-models span { color: var(--text-tertiary); font-family: var(--font-num); font-size: 9px; }
.saved-account-selection { display: grid; gap: 8px; border-top: 1px solid var(--separator); margin-top: 1px; padding-top: 9px; }
.saved-account-selection-copy { display: grid; gap: 3px; }
.saved-account-selection-copy strong { color: var(--text-primary); font-size: 11px; }
.saved-account-selection-copy span, .saved-account-empty { margin: 0; color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.saved-account-actions { display: flex; align-items: center; justify-content: space-between; gap: 9px; border: 1px solid var(--separator); border-radius: 8px; background: var(--bg-base); padding: 8px 9px; }
.saved-account-actions span { color: var(--text-tertiary); font-size: 9.5px; line-height: 1.4; }
.saved-account-actions code { color: var(--accent-strong); font-family: var(--font-num); font-size: .95em; }
.notice, .error { margin: 0; border-radius: 8px; font-size: 11px; line-height: 1.5; padding: 8px 10px; }
.notice { border: 1px solid color-mix(in srgb, var(--green) 35%, var(--separator)); background: color-mix(in srgb, var(--green) 7%, transparent); color: var(--green); }
.error { border: 1px solid color-mix(in srgb, var(--red) 42%, var(--separator)); background: color-mix(in srgb, var(--red) 8%, transparent); color: var(--red); }
.configuration-section { display: grid; min-width: 0; gap: 10px; border-top: 1px solid var(--separator); padding-top: 12px; }
.section-heading { display: flex; justify-content: space-between; gap: 12px; }
.section-heading > div { display: grid; gap: 2px; }
.section-heading strong { color: var(--text-primary); font-size: 12px; }
.section-heading span { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.field { display: grid; gap: 5px; }
.field > span { color: var(--text-secondary); font-size: 11px; }
.field input, .field select { width: 100%; min-width: 0; border: 1px solid var(--separator-strong); border-radius: 8px; outline: none; background: var(--bg-base); color: var(--text-primary); font: inherit; font-family: var(--font-num); font-size: 12px; padding: 9px 10px; }
.field select { cursor: pointer; appearance: auto; }
.field input:focus, .field select:focus { border-color: var(--accent-strong); box-shadow: 0 0 0 3px var(--accent-soft); }
.field small { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.field small code, .discovery-toggle code, .gateway-note code { color: var(--accent-strong); font-family: var(--font-num); font-size: .95em; }
.helper-field { border-left: 2px solid color-mix(in srgb, var(--orange) 65%, var(--separator)); padding-left: 9px; }
.discovery-toggle { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: start; gap: 9px; border: 1px solid var(--separator); border-radius: 9px; background: var(--bg-inset); padding: 10px; cursor: pointer; }
.discovery-toggle input { width: 14px; height: 14px; margin: 2px 0 0; accent-color: var(--accent-strong); }
.discovery-toggle > span { display: grid; min-width: 0; gap: 3px; }
.discovery-toggle strong { color: var(--text-primary); font-size: 11px; }
.discovery-toggle small { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.gateway-compatibility-warning { margin: -2px 0 0; border: 1px solid color-mix(in srgb, var(--orange) 46%, var(--separator)); border-left: 3px solid var(--orange); border-radius: 8px; background: color-mix(in srgb, var(--orange) 7%, var(--bg-inset)); color: var(--text-secondary); font-size: 10px; line-height: 1.5; padding: 8px 10px; }
.gateway-checks { display: grid; gap: 9px; border: 1px solid color-mix(in srgb, var(--accent-strong) 30%, var(--separator)); border-left: 3px solid var(--accent-strong); border-radius: 10px; background: color-mix(in srgb, var(--accent-soft) 50%, var(--bg-inset)); padding: 10px 11px; }
.gateway-checks.unavailable { border-left-color: var(--orange); background: color-mix(in srgb, var(--orange) 5%, var(--bg-inset)); }
.gateway-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.gateway-heading > div:first-child { display: grid; gap: 3px; min-width: 0; }
.gateway-heading strong { color: var(--text-primary); font-size: 12px; }
.gateway-heading span { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.gateway-actions { display: flex; flex: 0 0 auto; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }
.gateway-actions button { border: 1px solid var(--accent-border); border-radius: 7px; background: var(--bg-base); color: var(--accent-strong); font: inherit; font-size: 10px; font-weight: 700; padding: 6px 8px; }
.gateway-actions button:hover:not(:disabled) { border-color: var(--accent-strong); background: var(--accent-soft); }
.gateway-actions button:disabled { cursor: not-allowed; opacity: .46; }
.gateway-note { margin: 0; color: var(--orange); font-size: 10px; line-height: 1.5; }
.gateway-result { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; border-radius: 7px; padding: 7px 8px; }
.gateway-result.success { border: 1px solid color-mix(in srgb, var(--green) 40%, var(--separator)); background: color-mix(in srgb, var(--green) 7%, transparent); }
.gateway-result.failed { border: 1px solid color-mix(in srgb, var(--red) 42%, var(--separator)); background: color-mix(in srgb, var(--red) 7%, transparent); }
.gateway-result strong { color: var(--text-secondary); font-size: 10px; }
.gateway-result.success strong { color: var(--green); }
.gateway-result.failed strong { color: var(--red); }
.gateway-result span { color: var(--text-tertiary); font-family: var(--font-num); font-size: 10px; white-space: nowrap; }
.gateway-models { display: grid; grid-template-columns: repeat(auto-fit, minmax(145px, 1fr)); gap: 6px; }
.gateway-models button { display: grid; min-width: 0; gap: 2px; border: 1px solid var(--separator); border-radius: 7px; background: var(--bg-base); color: var(--text-secondary); text-align: left; padding: 7px 8px; }
.gateway-models button:hover:not(:disabled), .gateway-models button.selected { border-color: var(--accent-strong); background: var(--accent-soft); color: var(--accent-strong); }
.gateway-models button:disabled { opacity: .5; }
.gateway-models strong, .gateway-models span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.gateway-models strong { font-size: 10px; }
.gateway-models span { color: var(--text-tertiary); font-family: var(--font-num); font-size: 9px; }
.backup-section { border-top: 1px solid var(--separator); padding-top: 12px; }
.backup-section summary { cursor: pointer; color: var(--text-primary); font-size: 12px; font-weight: 700; }
.backup-section summary span { display: inline-grid; place-items: center; min-width: 17px; min-height: 17px; border-radius: 999px; background: var(--bg-fill); color: var(--text-tertiary); font-family: var(--font-num); font-size: 10px; }
.backup-section p { margin: 8px 0 7px; color: var(--text-tertiary); font-size: 10px; line-height: 1.5; }
.warning-copy { color: var(--orange) !important; }
.empty-backups { border: 1px dashed var(--separator-strong); border-radius: 8px; color: var(--text-tertiary); font-size: 11px; padding: 9px 10px; }
.backup-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-top: 1px solid var(--separator); padding: 9px 0; }
.backup-row > div:first-child { display: grid; min-width: 0; gap: 2px; }
.backup-row strong { overflow: hidden; color: var(--text-secondary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.backup-row span { color: var(--text-tertiary); font-size: 10px; }
.backup-row > div:last-child { display: flex; flex: 0 0 auto; gap: 6px; }
.backup-row button { border: 1px solid var(--separator); border-radius: 6px; color: var(--text-secondary); font: inherit; font-size: 10px; padding: 4px 7px; }
.backup-row button:hover:not(:disabled) { border-color: var(--accent-border); color: var(--accent-strong); }
.backup-row button:disabled { cursor: wait; opacity: .5; }
@media (max-width: 640px) { .current-state { grid-template-columns: repeat(2, minmax(0, 1fr)); } .gateway-heading { align-items: flex-start; flex-direction: column; } .gateway-actions { justify-content: flex-start; } .saved-account-actions { align-items: flex-start; flex-direction: column; } }
@media (max-width: 560px) { .current-state { grid-template-columns: 1fr; } .gateway-result { align-items: flex-start; flex-direction: column; } .gateway-result span { white-space: normal; } .backup-row { align-items: flex-start; flex-direction: column; } .backup-row > div:last-child { align-self: stretch; } .backup-row button { flex: 1; } }
</style>
