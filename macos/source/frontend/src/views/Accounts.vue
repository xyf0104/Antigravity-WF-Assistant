<script setup>
import { computed, onMounted, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Field from "@/components/ui/Field.vue";
import Modal from "@/components/ui/Modal.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import {
  addDiscoveredModels,
  defaultUpstreamAccount,
  deleteUpstreamAccount,
  discoverAccountModels,
  importUpstreamAccounts,
  loadAccounts,
  saveUpstreamAccount,
  setUpstreamAccountEnabled,
  state,
  testUpstreamAccount,
} from "@/state/appState";

const DEFAULT_XIASS_URL = "https://api.xiass.com";
const editorOpen = ref(false);
const importOpen = ref(false);
const confirmDelete = ref(null);
const saving = ref(false);
const testing = ref(false);
const discovering = ref(false);
const importing = ref(false);
const editorError = ref("");
const editorNotice = ref("");
const importError = ref("");
const importNotice = ref("");
const importText = ref("");
const discoveredModels = ref([]);
const selectedModelIds = ref([]);

const providerOptions = [
  { label: "OpenAI", value: "openai" },
  { label: "Claude", value: "anthropic" },
  { label: "Grok", value: "grok" },
  { label: "兼容接口", value: "custom" },
];
const endpointModeOptions = [
  { label: "智能补全", value: "auto" },
  { label: "完整路径（手动）", value: "manual" },
];
const anthropicPathOptions = [
  { label: "自动", value: "auto" },
  { label: "标准 Messages", value: "standard" },
  { label: "兼容 Chat Messages", value: "compat" },
];
const authOptions = [
  { label: "Bearer", value: "bearer" },
  { label: "x-api-key", value: "x_api_key" },
  { label: "自定义头", value: "custom_header" },
];
const accountTypeOptions = [
  { label: "API Key", value: "api_key" },
  { label: "OAuth / Bearer Token", value: "oauth" },
  { label: "x-api-key", value: "x_api_key" },
  { label: "Setup Token", value: "setup_token" },
  { label: "自定义认证头", value: "custom_header" },
];

function emptyForm() {
  return {
    id: "",
    name: "",
    notes: "",
    provider: "openai",
    type: "api_key",
    apiUrl: DEFAULT_XIASS_URL,
    endpointMode: "auto",
    apiStyle: "auto",
    messagePathMode: "auto",
    authMode: "bearer",
    authHeader: "",
    headersText: "{}",
    apiKey: "",
    enabled: true,
    priority: 50,
    maxConcurrency: 2,
  };
}

const form = ref(emptyForm());
const isExisting = computed(() => Boolean(form.value.id));
const selectedCount = computed(() => selectedModelIds.value.length);
const allSelected = computed(() => discoveredModels.value.length > 0 && selectedModelIds.value.length === discoveredModels.value.length);
const apiURLLabel = computed(() => form.value.endpointMode === "manual" ? "完整 API 地址" : "基础域名 / 基础路径");
const apiURLHint = computed(() => {
  if (form.value.endpointMode === "manual") return "严格原样使用，不会自动补全或替换路径。";
  if (form.value.provider === "anthropic") return "只填域名即可；Claude 自动使用 Messages 路径。";
  return "只填域名或基础路径即可；WF 自动补全对应的 /v1 接口。";
});
const apiURLPlaceholder = computed(() => form.value.endpointMode === "manual"
  ? (form.value.provider === "anthropic" ? "https://api.xiass.com/v1/messages" : "https://api.xiass.com/v1/chat/completions")
  : DEFAULT_XIASS_URL);

function providerLabel(provider) {
  return provider === "anthropic" ? "Claude" : provider === "grok" ? "Grok" : provider === "custom" ? "兼容" : "OpenAI";
}

function providerTone(provider) {
  return provider === "anthropic" ? "warn" : provider === "openai" ? "info" : "neutral";
}

function health(account) {
  if (!account.enabled) return { label: "已暂停", tone: "neutral" };
  const until = Date.parse(account.cooldownUntil || "");
  if (Number.isFinite(until) && until > Date.now()) {
    const seconds = Math.max(1, Math.ceil((until - Date.now()) / 1000));
    return { label: `冷却 ${seconds}s`, tone: "warn" };
  }
  if (account.lastError) return { label: "待恢复", tone: "warn" };
  return { label: "可调度", tone: "ok" };
}

function formatTime(value) {
  const time = Date.parse(value || "");
  if (!Number.isFinite(time)) return "—";
  return new Intl.DateTimeFormat("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(time);
}

function accountToForm(account) {
  return {
    ...emptyForm(),
    ...account,
    endpointMode: account.endpointMode || (account.messagePathMode === "manual" ? "manual" : "auto"),
    messagePathMode: account.messagePathMode === "manual" ? "auto" : (account.messagePathMode || "auto"),
    headersText: JSON.stringify(account.headers || {}, null, 2),
    apiKey: "",
  };
}

async function openNew() {
  let defaults = {};
  try {
    defaults = await defaultUpstreamAccount();
  } catch {
    // The static defaults keep the editor useful while Wails is starting.
  }
  form.value = { ...emptyForm(), ...defaults, headersText: JSON.stringify(defaults?.headers || {}, null, 2), apiKey: "" };
  editorError.value = "";
  editorNotice.value = "地址默认只需填写域名。保存后可获取模型并默认全选导入；完整接口地址可随时切换为手动模式。";
  discoveredModels.value = [];
  selectedModelIds.value = [];
  editorOpen.value = true;
}

function openEdit(account) {
  form.value = accountToForm(account);
  editorError.value = "";
  editorNotice.value = "为保护已保存的凭据，令牌栏不会回显；留空保存将保留原有凭据。";
  discoveredModels.value = [];
  selectedModelIds.value = [];
  editorOpen.value = true;
}

function onProviderChange(provider) {
  form.value.provider = provider;
  if (provider === "anthropic") {
    form.value.authMode = "x_api_key";
    form.value.apiStyle = "messages";
  } else if (form.value.authMode === "x_api_key") {
    form.value.authMode = "bearer";
    if (form.value.apiStyle === "messages") form.value.apiStyle = "auto";
  }
}

function onTypeChange(type) {
  form.value.type = type;
  if (type === "x_api_key") form.value.authMode = "x_api_key";
  else if (type === "custom_header") form.value.authMode = "custom_header";
  else if (type === "api_key") form.value.authMode = form.value.provider === "anthropic" ? "x_api_key" : "bearer";
  else form.value.authMode = "bearer";
}

function onEndpointModeChange(mode) {
  form.value.endpointMode = mode;
  editorNotice.value = mode === "manual"
    ? "手动完整路径已启用：WF 会严格使用上方地址。"
    : "智能补全已启用：WF 会根据协议补全请求路径。";
}

function parseHeaders() {
  const raw = String(form.value.headersText || "{}").trim();
  if (!raw) return {};
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("附加请求头必须是 JSON 对象，例如 {\"X-Client\": \"WF\"}");
  }
  if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") throw new Error("附加请求头必须是 JSON 对象");
  const headers = {};
  for (const [name, value] of Object.entries(parsed)) {
    if (!["string", "number", "boolean"].includes(typeof value)) throw new Error(`请求头 ${name} 的值必须是文本、数字或布尔值`);
    headers[name] = String(value);
  }
  return headers;
}

function accountPayload() {
  const payload = {
    ...form.value,
    apiUrl: form.value.apiUrl?.trim(),
    apiKey: form.value.apiKey?.trim(),
    authHeader: form.value.authHeader?.trim(),
    headers: parseHeaders(),
    priority: Number(form.value.priority),
    maxConcurrency: Number(form.value.maxConcurrency),
  };
  delete payload.headersText;
  return payload;
}

async function saveAccount() {
  editorError.value = "";
  let account;
  try {
    account = accountPayload();
    if (!account.apiUrl) throw new Error("请填写 API 地址");
    if (!account.apiKey && !account.id) throw new Error("请填写 API Key、访问令牌，或使用 JSON 导入账户");
  } catch (error) {
    editorError.value = error.message;
    return;
  }
  saving.value = true;
  try {
    const result = await saveUpstreamAccount(account);
    if (result?.ok) {
      editorOpen.value = false;
    } else {
      editorError.value = result?.message || "账户保存失败";
    }
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    saving.value = false;
  }
}

async function discoverModels() {
  if (!form.value.id) {
    editorError.value = "请先保存账户，再获取模型并将其绑定到该账户。";
    return;
  }
  editorError.value = "";
  editorNotice.value = "";
  discovering.value = true;
  try {
    const result = await discoverAccountModels(form.value.id);
    if (!result?.ok) {
      editorError.value = result?.message || "无法获取上游模型列表";
      return;
    }
    discoveredModels.value = result.models || [];
    selectedModelIds.value = discoveredModels.value.map((model) => model.id);
    editorNotice.value = result.message || `已发现 ${selectedModelIds.value.length} 个模型，默认已全选。`;
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    discovering.value = false;
  }
}

async function testAccount() {
  const model = selectedModelIds.value[0];
  if (!form.value.id) {
    editorError.value = "请先保存账户后再测试。";
    return;
  }
  if (!model) {
    editorError.value = "请先获取模型列表并选择一个模型。";
    return;
  }
  editorError.value = "";
  testing.value = true;
  try {
    const result = await testUpstreamAccount(form.value.id, model);
    if (result?.ok) editorNotice.value = `${result.message} · ${result.endpoint}`;
    else editorError.value = result?.message || "账户测试失败";
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    testing.value = false;
  }
}

function toggleAllModels() {
  selectedModelIds.value = allSelected.value ? [] : discoveredModels.value.map((model) => model.id);
}

function toggleModel(id, checked) {
  if (checked && !selectedModelIds.value.includes(id)) selectedModelIds.value.push(id);
  if (!checked) selectedModelIds.value = selectedModelIds.value.filter((value) => value !== id);
}

async function addSelectedModels() {
  if (!form.value.id || !selectedModelIds.value.length) return;
  saving.value = true;
  editorError.value = "";
  try {
    const result = await addDiscoveredModels({ accountId: form.value.id }, selectedModelIds.value);
    if (result?.ok) editorNotice.value = result.message;
    else editorError.value = result?.message || "模型导入失败";
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    saving.value = false;
  }
}

async function toggleAccount(account) {
  const result = await setUpstreamAccountEnabled(account.id, !account.enabled);
  if (!result?.ok) editorError.value = result?.message || "账户状态更新失败";
}

async function removeAccount() {
  const account = confirmDelete.value;
  if (!account) return;
  const result = await deleteUpstreamAccount(account.id);
  if (!result?.ok) editorError.value = result?.message || "删除账户失败";
  confirmDelete.value = null;
}

async function importAccounts() {
  importError.value = "";
  importNotice.value = "";
  if (!importText.value.trim()) {
    importError.value = "请粘贴账户 JSON。";
    return;
  }
  importing.value = true;
  try {
    const result = await importUpstreamAccounts(importText.value);
    if (result?.ok) {
      importNotice.value = result.message;
      importText.value = "";
    } else {
      importError.value = result?.message || "账户导入失败";
    }
  } catch (error) {
    importError.value = String(error?.message || error);
  } finally {
    importing.value = false;
  }
}

onMounted(() => loadAccounts());
</script>

<template>
  <div class="page fade-up">
    <div class="row between page-head" style="gap: 12px">
      <div class="col" style="gap: 2px">
        <div class="t-title">上游账户池</div>
        <div class="t-caption">API Key、OAuth/Bearer Token、x-api-key、Setup Token 与账户 JSON 导入；凭据仅保存在本机。</div>
      </div>
      <div class="row" style="gap: 7px">
        <Button variant="plain" @click="importOpen = true">导入 JSON</Button>
        <Button variant="filled" @click="openNew">添加账户</Button>
      </div>
    </div>

    <div v-if="!state.accounts.length && !state.accountsLoading" class="empty">
      <div class="empty-icon">◌</div>
      <div class="t-headline">还没有上游账户</div>
      <div class="t-caption" style="margin-top: 5px">添加一次凭据后，可把多个模型绑定到同一账户或账户池。</div>
      <div class="row" style="gap: 8px; margin-top: 14px">
        <Button variant="tinted" @click="importOpen = true">导入账户 JSON</Button>
        <Button variant="filled" @click="openNew">添加账户</Button>
      </div>
    </div>

    <div v-else class="grid">
      <article v-for="account in state.accounts" :key="account.id" class="account-card" :class="{ paused: !account.enabled }">
        <div class="row between" style="align-items: flex-start; gap: 10px">
          <div class="grow col" style="gap: 3px; min-width: 0">
            <div class="t-headline truncate">{{ account.name || providerLabel(account.provider) + ' 账户' }}</div>
            <div class="mono truncate">{{ account.apiUrl }}</div>
          </div>
          <Badge :tone="providerTone(account.provider)" :label="providerLabel(account.provider)" />
        </div>
        <div class="status-row">
          <Badge :tone="health(account).tone" :label="health(account).label" />
          <span>优先级 {{ account.priority }}</span>
          <span>并发 {{ account.activeRequests || 0 }}/{{ account.maxConcurrency || 2 }}</span>
        </div>
        <div class="inset-group" style="margin-top: 11px">
          <div class="inset-row"><span>类型</span><code>{{ account.type || 'api_key' }}</code></div>
          <div class="inset-row"><span>上次成功</span><code>{{ formatTime(account.lastSuccessAt) }}</code></div>
          <div v-if="account.lastError" class="inset-row error-row"><span>状态</span><code class="truncate">{{ account.lastError }}</code></div>
        </div>
        <div class="row" style="gap: 6px; margin-top: 12px; justify-content: flex-end">
          <Button variant="plain" size="sm" @click="toggleAccount(account)">{{ account.enabled ? '暂停' : '恢复' }}</Button>
          <Button variant="plain" size="sm" @click="openEdit(account)">编辑</Button>
          <Button variant="danger" size="sm" @click="confirmDelete = account">删除</Button>
        </div>
      </article>
    </div>

    <Modal :open="editorOpen" :title="isExisting ? '编辑上游账户' : '添加上游账户'" wide persistent @close="editorOpen = false">
      <div class="col editor" style="gap: 15px">
        <section class="section">
          <span class="t-footnote">账户类型与协议</span>
          <SegmentedControl :options="providerOptions" :model-value="form.provider" @update:model-value="onProviderChange" />
          <label class="select-field">
            <span class="t-footnote">凭据类型</span>
            <select :value="form.type" @change="onTypeChange($event.target.value)">
              <option v-for="option in accountTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>
          <div class="compact-label">接口地址输入方式</div>
          <SegmentedControl :options="endpointModeOptions" :model-value="form.endpointMode" @update:model-value="onEndpointModeChange" />
          <div class="two-col">
            <Field :label="apiURLLabel" :hint="apiURLHint" v-model="form.apiUrl" :placeholder="apiURLPlaceholder" mono />
            <Field label="账户名称" hint="可选；用于模型绑定时识别" v-model="form.name" placeholder="例如 XIASS 主账户" />
          </div>
          <div v-if="form.provider === 'anthropic' && form.endpointMode !== 'manual'" class="claude-path-control">
            <div class="compact-label">Claude 路径</div>
            <SegmentedControl :options="anthropicPathOptions" :model-value="form.messagePathMode" @update:model-value="form.messagePathMode = $event" />
            <div class="t-caption">自动时先走 <code>/v1/messages</code>，接口不存在才改试 <code>/v1/chat/messages</code>。</div>
          </div>
          <div v-else-if="form.provider === 'anthropic'" class="t-caption">手动模式下会严格使用完整地址，例如 <code>/v1/messages</code> 或 <code>/v1/chat/messages</code>。</div>
        </section>

        <section class="section">
          <div class="t-headline">认证与调度</div>
          <Field
            :label="isExisting ? '新 API Key / Token（留空保留原凭据）' : 'API Key / Token'"
            type="password"
            v-model="form.apiKey"
            :placeholder="form.type === 'setup_token' ? 'setup token…' : form.type === 'oauth' ? 'access token…' : 'sk-…'"
            mono
          />
          <div class="compact-label">认证方式</div>
          <SegmentedControl :options="authOptions" :model-value="form.authMode" @update:model-value="form.authMode = $event" />
          <Field v-if="form.authMode === 'custom_header'" label="认证请求头名称" hint="例如 X-API-Token" v-model="form.authHeader" placeholder="X-API-Token" mono />
          <label class="text-field">
            <span class="t-footnote">附加请求头（可选 JSON）</span>
            <textarea v-model="form.headersText" spellcheck="false" placeholder='{"X-Client":"Antigravity-WF"}'></textarea>
          </label>
          <div class="two-col">
            <Field label="优先级" hint="数值越小越先调度" type="number" v-model="form.priority" />
            <Field label="最大并发" hint="每个账户 1–32" type="number" v-model="form.maxConcurrency" />
          </div>
          <label class="enabled-check"><input v-model="form.enabled" type="checkbox" /> 保存后立即参与调度</label>
        </section>

        <section v-if="isExisting" class="section discover-box">
          <div class="row between" style="gap: 8px">
            <div>
              <div class="t-headline">获取、测试并批量添加模型</div>
              <div class="t-caption">读取该账户的 <code>/models</code>；结果默认全选，添加后自动绑定当前账户。</div>
            </div>
            <Button variant="tinted" :loading="discovering" @click="discoverModels">获取全部模型</Button>
          </div>
          <div v-if="discoveredModels.length" class="selection-list">
            <label class="select-all"><input type="checkbox" :checked="allSelected" @change="toggleAllModels" /> 全选 {{ discoveredModels.length }} 个模型</label>
            <label v-for="model in discoveredModels" :key="model.id" class="select-row">
              <input type="checkbox" :checked="selectedModelIds.includes(model.id)" @change="toggleModel(model.id, $event.target.checked)" />
              <span class="truncate">{{ model.name || model.id }}</span>
              <code>{{ model.id }}</code>
            </label>
          </div>
          <div class="row" style="justify-content: flex-end; gap: 7px">
            <Button variant="plain" size="sm" :disabled="!selectedModelIds.length" :loading="testing" @click="testAccount">测试已选模型</Button>
            <Button variant="filled" size="sm" :disabled="!selectedModelIds.length" :loading="saving" @click="addSelectedModels">添加已选 {{ selectedCount }} 个</Button>
          </div>
        </section>

        <section v-else class="section hint-box">
          <div class="t-headline">下一步</div>
          <div class="t-caption">保存账户后，重新编辑它即可获取全部上游模型、默认全选并一键绑定到 Antigravity。</div>
        </section>

        <div v-if="editorNotice" class="notice-box">{{ editorNotice }}</div>
        <div v-if="editorError" class="err-box">{{ editorError }}</div>
      </div>
      <template #footer>
        <Button variant="plain" @click="editorOpen = false">取消</Button>
        <Button variant="filled" :loading="saving" @click="saveAccount">{{ isExisting ? '保存账户' : '保存账户' }}</Button>
      </template>
    </Modal>

    <Modal :open="importOpen" title="导入账户 JSON" wide persistent @close="importOpen = false">
      <div class="col" style="gap: 10px">
        <div class="t-body">支持单账户对象、数组、<code>{"accounts": [...]}</code>、<code>{"data": [...]}</code>，以及 XIASS 风格的 <code>credentials</code>、<code>access_token</code>、<code>api_key</code> 字段。</div>
        <div class="t-caption">导入的 OAuth/Token 凭据仅写入本机私有存储；不会回显到界面或日志。</div>
        <textarea v-model="importText" class="import-text" spellcheck="false" placeholder='{"provider":"openai","api_key":"sk-...","base_url":"https://api.xiass.com"}'></textarea>
        <div v-if="importNotice" class="notice-box">{{ importNotice }}</div>
        <div v-if="importError" class="err-box">{{ importError }}</div>
      </div>
      <template #footer>
        <Button variant="plain" @click="importOpen = false">取消</Button>
        <Button variant="filled" :loading="importing" @click="importAccounts">导入账户</Button>
      </template>
    </Modal>

    <Modal :open="!!confirmDelete" title="确认删除账户" @close="confirmDelete = null">
      <div class="t-body">删除后，已绑定该账户的模型不会自动删除，但会因没有可用账户而停止调度。确定继续吗？</div>
      <template #footer>
        <Button variant="plain" @click="confirmDelete = null">取消</Button>
        <Button variant="danger" @click="removeAccount">删除账户</Button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 14px; padding: 18px 20px 28px; height: 100%; overflow-y: auto; }
.page > * { flex-shrink: 0; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(286px, 1fr)); gap: 12px; }
.account-card { background: var(--bg-card); border: 1px solid var(--separator); border-radius: var(--r-lg); padding: 14px; box-shadow: var(--shadow-card); backdrop-filter: blur(16px); }
.account-card.paused { opacity: .68; }
.mono { color: var(--text-tertiary); font-size: 11px; }
.status-row { display: flex; flex-wrap: wrap; align-items: center; gap: 7px; margin-top: 10px; color: var(--text-tertiary); font-size: 11px; }
.inset-row > span { color: var(--text-tertiary); font-size: 10px; width: 48px; letter-spacing: .04em; }
.inset-row code { min-width: 0; color: var(--text-secondary); font-size: 11px; }
.error-row code { color: var(--orange); }
.empty { flex: 1; min-height: 270px; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 40px 20px; text-align: center; border: 1px dashed var(--separator-strong); border-radius: var(--r-lg); }
.empty-icon { width: 48px; height: 48px; display: grid; place-items: center; border-radius: 15px; font-size: 28px; color: var(--accent-strong); background: var(--accent-soft); margin-bottom: 13px; }
.editor { padding-bottom: 2px; }
.section { display: flex; flex-direction: column; gap: 9px; padding: 12px; border: 1px solid var(--separator); border-radius: var(--r-md); background: color-mix(in srgb, var(--bg-inset) 58%, transparent); }
.discover-box { background: color-mix(in srgb, var(--accent-soft) 28%, var(--bg-card)); }
.hint-box { background: color-mix(in srgb, var(--blue-soft) 28%, var(--bg-card)); }
.two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.compact-label { color: var(--text-tertiary); font-size: 10px; letter-spacing: .06em; text-transform: uppercase; }
.claude-path-control { display: flex; flex-direction: column; gap: 7px; padding: 9px; border: 1px dashed var(--separator-strong); border-radius: var(--r-sm); background: var(--bg-card); }
.select-field, .text-field { display: flex; flex-direction: column; gap: 6px; }
select, textarea { width: 100%; border: 1px solid var(--separator-strong); border-radius: var(--r-sm); background: var(--bg-inset); color: var(--text-primary); outline: none; }
select { height: 34px; padding: 0 10px; font: 13px var(--font-ui); }
textarea { min-height: 66px; padding: 9px 10px; resize: vertical; font: 12px var(--font-num); }
select:focus, textarea:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.enabled-check { display: flex; align-items: center; gap: 7px; color: var(--text-secondary); font-size: 12px; }
.selection-list { max-height: 196px; overflow-y: auto; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-card); }
.select-all, .select-row { display: grid; grid-template-columns: 16px minmax(0, 1fr) minmax(90px, .75fr); align-items: center; gap: 8px; min-height: 34px; padding: 0 10px; border-bottom: 1px solid var(--separator); font-size: 12px; }
.select-all { grid-template-columns: 16px 1fr; color: var(--text-secondary); background: var(--bg-fill); position: sticky; top: 0; }
.select-row:last-child { border-bottom: 0; }
.select-row code { color: var(--text-tertiary); font-size: 10px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.notice-box, .err-box { padding: 10px 11px; border-radius: var(--r-sm); font-size: 12px; line-height: 1.45; }
.notice-box { color: var(--accent-strong); background: var(--accent-soft); border: 1px solid var(--accent-border); }
.err-box { color: var(--red); background: rgba(255,69,58,.1); border: 1px solid rgba(255,69,58,.25); }
.import-text { min-height: 220px; }
@media (max-width: 620px) { .two-col { grid-template-columns: 1fr; } .select-row { grid-template-columns: 16px minmax(0, 1fr); } .select-row code { display: none; } .page-head { align-items: flex-start; flex-direction: column; } }
</style>
