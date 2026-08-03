<script setup>
import { computed, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Field from "@/components/ui/Field.vue";
import Modal from "@/components/ui/Modal.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import {
  state,
  saveModel,
  deleteModel,
  discoverUpstreamModels,
  testUpstreamModel,
  addDiscoveredModels,
} from "@/state/appState";

const DEFAULT_XIASS_URL = "https://api.xiass.com";

const editorOpen = ref(false);
const editorError = ref("");
const editorNotice = ref("");
const saving = ref(false);
const discovering = ref(false);
const testing = ref(false);
const isNew = ref(false);
const confirmDelete = ref(null);
const discoveredModels = ref([]);
const selectedModelIds = ref([]);

function defaultCapabilities(provider = "openai", modelName = "") {
  const name = String(modelName).toLowerCase();
  const nonChat = /embedding|whisper|tts/.test(name);
  return {
    configured: true,
    supportsImages: !nonChat,
    supportsFiles: !nonChat,
    supportsAudio: false,
    supportsVideo: false,
    supportsToolCalls: !nonChat,
    supportsWebSearch: false,
    supportsImageGeneration: /gpt-image|image-1/.test(name),
    supportsThinking: !nonChat,
  };
}

function emptyForm() {
  return {
    name: "",
    displayName: "",
    description: "",
    provider: "openai",
    apiKey: "",
    apiUrl: DEFAULT_XIASS_URL,
    endpointMode: "auto",
    accountIds: [],
    externalModelName: "",
    reasoningEffort: "auto",
    apiStyle: "auto",
    messagePathMode: "auto",
    authMode: "bearer",
    authHeader: "",
    headersText: "{}",
    capabilities: defaultCapabilities(),
  };
}

const form = ref(emptyForm());

const providerOptions = [
  { label: "OpenAI", value: "openai" },
  { label: "Claude", value: "anthropic" },
  { label: "Grok", value: "grok" },
  { label: "兼容接口", value: "custom" },
];
const authOptions = [
  { label: "Bearer API Key", value: "bearer" },
  { label: "x-api-key", value: "x_api_key" },
  { label: "自定义请求头", value: "custom_header" },
];
const apiStyleOptions = [
  { label: "自动", value: "auto" },
  { label: "Chat", value: "chat_completions" },
  { label: "Responses", value: "responses" },
  { label: "Messages", value: "messages" },
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
const openAIReasoningOptions = [
  { label: "自动", value: "auto" },
  { label: "无", value: "none" },
  { label: "最小", value: "minimal" },
  { label: "低", value: "low" },
  { label: "中", value: "medium" },
  { label: "高", value: "high" },
  { label: "超高", value: "xhigh" },
  { label: "最大", value: "max" },
];
const anthropicReasoningOptions = openAIReasoningOptions.filter((option) =>
  ["auto", "low", "medium", "high"].includes(option.value)
);
const reasoningOptions = computed(() =>
  form.value.provider === "anthropic" ? anthropicReasoningOptions : openAIReasoningOptions
);
const testTarget = computed(() => selectedModelIds.value[0] || form.value.externalModelName?.trim());
const allDiscoveredSelected = computed(() =>
  discoveredModels.value.length > 0 && selectedModelIds.value.length === discoveredModels.value.length
);
const selectedCount = computed(() => selectedModelIds.value.length);
const apiURLLabel = computed(() =>
  form.value.endpointMode === "manual" ? "完整 API 地址" : "基础域名 / 基础路径"
);
const apiURLHint = computed(() => {
  if (form.value.endpointMode === "manual") {
    return "原样使用，不会自动改写；可填任意完整接口路径或带参数的地址。";
  }
  if (form.value.provider === "anthropic") {
    return "只填域名即可；会自动使用 Claude Messages 路径。";
  }
  return "只填域名或基础路径即可；WF 会自动补全对应的 /v1 接口。";
});
const apiURLPlaceholder = computed(() =>
  form.value.endpointMode === "manual"
    ? (form.value.provider === "anthropic" ? "https://api.xiass.com/v1/messages" : "https://api.xiass.com/v1/chat/completions")
    : "https://api.xiass.com"
);

function providerTone(provider) {
  return provider === "anthropic" ? "warn" : provider === "openai" ? "info" : "neutral";
}

function providerLabel(provider) {
  return provider === "anthropic" ? "Claude" : provider === "grok" ? "Grok" : provider === "openai" ? "OpenAI" : "兼容";
}

function reasoningLabel(value) {
  return openAIReasoningOptions.find((option) => option.value === value)?.label || "自动";
}

function maskKey(key) {
  const value = String(key || "").trim();
  if (!value) return "—";
  if (value.length <= 10) return "•".repeat(value.length);
  return `${value.slice(0, 5)}••••${value.slice(-4)}`;
}

function hostOf(url) {
  try {
    return new URL(url).host;
  } catch {
    return String(url || "").replace(/^https?:\/\//, "").split("/")[0] || "—";
  }
}

function capabilityLabels(model) {
  const caps = model.capabilities || defaultCapabilities(model.provider, model.externalModelName);
  const labels = [];
  if (caps.supportsImages) labels.push("识图");
  if (caps.supportsFiles) labels.push("文件");
  if (caps.supportsToolCalls) labels.push("工具");
  if (caps.supportsWebSearch) labels.push("联网");
  if (caps.supportsImageGeneration) labels.push("生图");
  return labels;
}

function modelToForm(model) {
  const capabilities = { ...defaultCapabilities(model.provider, model.externalModelName), ...(model.capabilities || {}), configured: true };
  return {
    ...emptyForm(),
    ...model,
    endpointMode: model.endpointMode || (model.messagePathMode === "manual" ? "manual" : "auto"),
    accountIds: Array.isArray(model.accountIds) ? [...model.accountIds] : [],
    reasoningEffort: model.reasoningEffort || "auto",
    apiStyle: model.apiStyle || (model.provider === "anthropic" ? "messages" : "auto"),
    messagePathMode: model.messagePathMode === "manual" ? "auto" : (model.messagePathMode || "auto"),
    authMode: model.authMode || (model.provider === "anthropic" ? "x_api_key" : "bearer"),
    authHeader: model.authHeader || "",
    headersText: JSON.stringify(model.headers || {}, null, 2),
    capabilities,
  };
}

function openNew() {
  form.value = emptyForm();
  isNew.value = true;
  editorError.value = "";
  editorNotice.value = "默认只需填写 XIASS 域名；如上游要求完整接口地址，可随时切换到“完整路径（手动）”。";
  discoveredModels.value = [];
  selectedModelIds.value = [];
  editorOpen.value = true;
}

function openEdit(model) {
  form.value = modelToForm(model);
  isNew.value = false;
  editorError.value = "";
  editorNotice.value = "";
  discoveredModels.value = [];
  selectedModelIds.value = [];
  editorOpen.value = true;
}

function onProviderChange(provider) {
  form.value.provider = provider;
  if (provider === "anthropic") {
    form.value.authMode = "x_api_key";
    form.value.apiStyle = "messages";
    if (!anthropicReasoningOptions.some((option) => option.value === form.value.reasoningEffort)) {
      form.value.reasoningEffort = "auto";
    }
  } else if (form.value.authMode === "x_api_key") {
    form.value.authMode = "bearer";
    if (form.value.apiStyle === "messages") form.value.apiStyle = "auto";
  }
  form.value.capabilities = { ...defaultCapabilities(provider, form.value.externalModelName), ...form.value.capabilities, configured: true };
}

function onEndpointModeChange(mode) {
  form.value.endpointMode = mode;
  if (mode === "manual") {
    editorNotice.value = "手动完整路径已启用：WF 会原样使用你填写的 API 地址，不再补全或替换路径。";
  } else {
    editorNotice.value = "智能补全已启用：只需填写域名或基础路径，WF 会按当前协议补全请求地址。";
  }
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
  if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") {
    throw new Error("附加请求头必须是 JSON 对象");
  }
  const headers = {};
  for (const [name, value] of Object.entries(parsed)) {
    if (typeof value !== "string" && typeof value !== "number" && typeof value !== "boolean") {
      throw new Error(`请求头 ${name} 的值必须是文本、数字或布尔值`);
    }
    headers[name] = String(value);
  }
  return headers;
}

function upstreamConfig() {
  return {
    provider: form.value.provider,
    apiUrl: form.value.apiUrl?.trim(),
    endpointMode: form.value.endpointMode,
    apiKey: form.value.apiKey?.trim(),
    apiStyle: form.value.apiStyle,
    messagePathMode: form.value.messagePathMode,
    authMode: form.value.authMode,
    authHeader: form.value.authHeader?.trim(),
    headers: parseHeaders(),
  };
}

function validateConnection() {
  if (!form.value.apiUrl?.trim()) throw new Error("请填写 API 地址");
  if (!form.value.apiKey?.trim()) throw new Error("请填写 API Key 或访问令牌");
  return upstreamConfig();
}

async function fetchModels() {
  editorError.value = "";
  editorNotice.value = "";
  let config;
  try {
    config = validateConnection();
  } catch (error) {
    editorError.value = error.message;
    return;
  }
  discovering.value = true;
  try {
    const result = await discoverUpstreamModels(config);
    if (!result?.ok) {
      editorError.value = result?.message || "无法获取上游模型列表";
      return;
    }
    discoveredModels.value = result.models || [];
    selectedModelIds.value = discoveredModels.value.map((model) => model.id);
    editorNotice.value = result.message || `已发现 ${selectedModelIds.value.length} 个模型，默认已全部选中。`;
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    discovering.value = false;
  }
}

function toggleModel(modelID, checked) {
  if (checked) {
    if (!selectedModelIds.value.includes(modelID)) selectedModelIds.value.push(modelID);
  } else {
    selectedModelIds.value = selectedModelIds.value.filter((id) => id !== modelID);
  }
}

function toggleAllModels() {
  selectedModelIds.value = allDiscoveredSelected.value ? [] : discoveredModels.value.map((model) => model.id);
}

async function testSelectedModel() {
  editorError.value = "";
  editorNotice.value = "";
  let config;
  try {
    config = validateConnection();
  } catch (error) {
    editorError.value = error.message;
    return;
  }
  if (!testTarget.value) {
    editorError.value = "请先获取模型列表并选择一个模型，或填写上游模型名。";
    return;
  }
  testing.value = true;
  try {
    const result = await testUpstreamModel(config, testTarget.value);
    if (result?.ok) editorNotice.value = `${result.message} · ${result.apiStyle}`;
    else editorError.value = result?.message || "模型测试失败";
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    testing.value = false;
  }
}

async function saveManualModel() {
  let config;
  try {
    config = validateConnection();
  } catch (error) {
    editorError.value = error.message;
    return;
  }
  const externalModelName = form.value.externalModelName?.trim();
  if (!externalModelName) {
    editorError.value = "请填写上游模型名，或先点击“获取全部模型”。";
    return;
  }
  const model = {
    ...form.value,
    ...config,
    externalModelName,
    displayName: form.value.displayName?.trim() || externalModelName,
    name: form.value.name?.trim() || `models/${form.value.provider}-${externalModelName.replace(/[^a-zA-Z0-9.-]+/g, "-")}`,
    capabilities: { ...form.value.capabilities, configured: true },
  };
  delete model.headersText;
  if (model.accountIds?.length) model.apiKey = "";
  saving.value = true;
  try {
    const result = await saveModel(model);
    if (result?.ok) {
      editorOpen.value = false;
    } else {
      editorError.value = result?.message || "保存失败";
    }
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    saving.value = false;
  }
}

async function addSelectedModels() {
  editorError.value = "";
  let config;
  try {
    config = validateConnection();
  } catch (error) {
    editorError.value = error.message;
    return;
  }
  if (!selectedModelIds.value.length) {
    editorError.value = "请至少勾选一个模型。";
    return;
  }
  saving.value = true;
  try {
    const result = await addDiscoveredModels(config, selectedModelIds.value);
    if (result?.ok) {
      editorNotice.value = result.message;
      editorOpen.value = false;
    } else {
      editorError.value = result?.message || "批量添加失败";
    }
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    saving.value = false;
  }
}

async function handleDelete() {
  const target = confirmDelete.value;
  if (!target) return;
  await deleteModel(target.name);
  confirmDelete.value = null;
}
</script>

<template>
  <div class="page fade-up">
    <div class="row between" style="gap: 12px">
      <div class="col" style="gap: 2px">
        <div class="t-title">自定义上游模型</div>
        <div class="t-caption">批量发现并导入模型；图片、文件与工具能力会随模型配置注入 Antigravity</div>
      </div>
      <Button variant="filled" @click="openNew">添加上游</Button>
    </div>

    <div v-if="state.models.length === 0" class="empty">
      <div class="empty-icon">＋</div>
      <div class="t-headline">还没有配置模型</div>
      <div class="t-caption" style="margin-top: 5px">默认连接 XIASS API；填写 Key 后可获取、全选并添加全部可用模型。</div>
      <Button variant="tinted" style="margin-top: 14px" @click="openNew">开始配置</Button>
    </div>

    <div v-else class="grid">
      <article v-for="model in state.models" :key="model.name" class="model-card">
        <div class="row between" style="align-items: flex-start; gap: 10px">
          <div class="grow col" style="gap: 2px; min-width: 0">
            <div class="t-headline truncate">{{ model.displayName || model.externalModelName || model.name }}</div>
            <div class="mono truncate model-id">{{ model.externalModelName }}</div>
          </div>
          <Badge :tone="providerTone(model.provider)" :label="providerLabel(model.provider)" />
        </div>
        <div class="cap-row">
          <span v-for="label in capabilityLabels(model)" :key="label" class="cap">{{ label }}</span>
          <span v-if="!capabilityLabels(model).length" class="cap muted">文本</span>
        </div>
        <div class="inset-group" style="margin-top: 11px">
          <div class="inset-row"><span>HOST</span><code class="truncate">{{ hostOf(model.apiUrl) }}</code></div>
          <div class="inset-row"><span>模式</span><code>{{ model.apiStyle || "兼容" }}</code></div>
          <div class="inset-row"><span>KEY</span><code class="truncate">{{ maskKey(model.apiKey) }}</code></div>
        </div>
        <div class="row" style="gap: 6px; margin-top: 12px; justify-content: flex-end">
          <Button variant="plain" size="sm" @click="openEdit(model)">编辑</Button>
          <Button variant="danger" size="sm" @click="confirmDelete = model">删除</Button>
        </div>
      </article>
    </div>

    <Modal :open="editorOpen" :title="isNew ? '添加上游模型' : '编辑上游模型'" wide persistent @close="editorOpen = false">
      <div class="col editor" style="gap: 15px">
        <section class="section">
          <span class="t-footnote">供应商与协议</span>
          <SegmentedControl :options="providerOptions" :model-value="form.provider" @update:model-value="onProviderChange" />
          <div class="compact-label">接口地址输入方式</div>
          <SegmentedControl :options="endpointModeOptions" :model-value="form.endpointMode" @update:model-value="onEndpointModeChange" />
          <div class="two-col">
            <Field :label="apiURLLabel" :hint="apiURLHint" v-model="form.apiUrl" :placeholder="apiURLPlaceholder" mono />
            <Field label="API Key / 访问令牌" type="password" v-model="form.apiKey" placeholder="sk-..." mono />
          </div>
          <div v-if="form.provider === 'anthropic' && form.endpointMode !== 'manual'" class="claude-path-control">
            <div class="compact-label">Claude 路径</div>
            <SegmentedControl :options="anthropicPathOptions" :model-value="form.messagePathMode" @update:model-value="form.messagePathMode = $event" />
            <div class="t-caption">自动：先尝试 <code>/v1/messages</code>，仅在接口不存在时改试 <code>/v1/chat/messages</code>；也可强制选择任一路径。</div>
          </div>
          <div v-else-if="form.provider === 'anthropic'" class="t-caption">手动模式下，Claude 请求会严格发送到上方完整地址；例如 <code>/v1/messages</code> 或 <code>/v1/chat/messages</code>。</div>
          <div class="compact-label">认证方式</div>
          <SegmentedControl :options="authOptions" :model-value="form.authMode" @update:model-value="form.authMode = $event" />
          <Field v-if="form.authMode === 'custom_header'" label="认证请求头名称" hint="例如 X-API-Token" v-model="form.authHeader" placeholder="X-API-Token" mono />
          <label class="text-field">
            <span class="t-footnote">附加请求头（可选 JSON）</span>
            <textarea v-model="form.headersText" spellcheck="false" placeholder='{"X-Client": "Antigravity-WF"}'></textarea>
          </label>
          <div class="compact-label">上游 API 模式</div>
          <SegmentedControl :options="apiStyleOptions" :model-value="form.apiStyle" @update:model-value="form.apiStyle = $event" />
          <div class="t-caption">自动模式：图片/文本附件可兼容 Chat；PDF、通用文件、联网与生图会优先走 Responses，并仅在该端点不存在时降级。</div>
        </section>

        <section class="section discover-box">
          <div class="row between" style="gap: 8px">
            <div>
              <div class="t-headline">发现并批量导入</div>
              <div class="t-caption">点击后读取上游 <code>/models</code>；发现结果默认全选，不会自动保存。</div>
            </div>
            <Button variant="tinted" :loading="discovering" @click="fetchModels">获取全部模型</Button>
          </div>
          <div v-if="discoveredModels.length" class="selection-list">
            <label class="select-all"><input type="checkbox" :checked="allDiscoveredSelected" @change="toggleAllModels" /> 全选 {{ discoveredModels.length }} 个模型</label>
            <label v-for="model in discoveredModels" :key="model.id" class="select-row">
              <input type="checkbox" :checked="selectedModelIds.includes(model.id)" @change="toggleModel(model.id, $event.target.checked)" />
              <span class="truncate">{{ model.name || model.id }}</span>
              <code>{{ model.id }}</code>
            </label>
          </div>
          <div class="row" style="justify-content: flex-end; gap: 7px">
            <Button variant="plain" size="sm" :disabled="!testTarget" :loading="testing" @click="testSelectedModel">测试 {{ testTarget || '模型' }}</Button>
          </div>
        </section>

        <section v-if="!discoveredModels.length" class="section manual-box">
          <div class="t-headline">手动添加单个模型</div>
          <Field label="上游模型名" hint="例如 gpt-5.6 / claude-sonnet / grok-4" v-model="form.externalModelName" placeholder="gpt-5.6" mono />
          <Field label="显示名称" hint="留空时使用上游模型名" v-model="form.displayName" :placeholder="form.externalModelName || '自动使用上游模型名'" />
          <Field label="描述（可选）" v-model="form.description" placeholder="模型用途或上游说明" />
        </section>

        <section class="section account-binding">
          <div class="t-headline">账户池绑定（可选）</div>
          <div class="t-caption">绑定后请求会从账户池按优先级、并发和健康状态调度；额度不足或连接中断时自动切换下一个账户。</div>
          <div v-if="!state.accounts.length" class="t-caption">尚未添加账户。可先在“账户池”中添加 API Key、Token 或导入账户 JSON；未绑定时继续使用本页直接填写的 API Key。</div>
          <div v-else class="account-binding-list">
            <label v-for="account in state.accounts" :key="account.id" class="account-binding-row" :class="{ paused: !account.enabled }">
              <input v-model="form.accountIds" type="checkbox" :value="account.id" />
              <span class="grow truncate">{{ account.name || account.provider || '上游账户' }}</span>
              <code>{{ account.provider }}</code>
              <span v-if="!account.enabled" class="paused-label">已暂停</span>
            </label>
          </div>
        </section>

        <section class="section">
          <div class="t-headline">能力声明</div>
          <div class="t-caption">只勾选你确认上游支持的能力；它会控制 Antigravity 是否允许拖入对应内容。</div>
          <div class="capability-grid">
            <label><input type="checkbox" v-model="form.capabilities.supportsImages" /> 图片/截图</label>
            <label><input type="checkbox" v-model="form.capabilities.supportsFiles" /> 文件/PDF</label>
            <label><input type="checkbox" v-model="form.capabilities.supportsToolCalls" /> 原生工具调用</label>
            <label><input type="checkbox" v-model="form.capabilities.supportsThinking" /> 推理强度</label>
            <label><input type="checkbox" v-model="form.capabilities.supportsWebSearch" /> 上游联网搜索</label>
            <label><input type="checkbox" v-model="form.capabilities.supportsImageGeneration" /> 上游图片生成</label>
          </div>
        </section>

        <section class="section">
          <div class="compact-label">思考强度</div>
          <div class="reasoning-grid">
            <button v-for="option in reasoningOptions" :key="option.value" type="button" :class="{ active: form.reasoningEffort === option.value }" @click="form.reasoningEffort = option.value">{{ option.label }}</button>
          </div>
        </section>

        <div v-if="editorNotice" class="notice-box">{{ editorNotice }}</div>
        <div v-if="editorError" class="err-box">{{ editorError }}</div>
      </div>
      <template #footer>
        <Button variant="plain" @click="editorOpen = false">取消</Button>
        <Button v-if="discoveredModels.length" variant="filled" :loading="saving" @click="addSelectedModels">添加已选 {{ selectedCount }} 个</Button>
        <Button v-else variant="filled" :loading="saving" @click="saveManualModel">{{ isNew ? '添加模型' : '保存' }}</Button>
      </template>
    </Modal>

    <Modal :open="!!confirmDelete" title="确认删除" @close="confirmDelete = null">
      <div class="t-body">确定删除 <strong>{{ confirmDelete?.displayName || confirmDelete?.name }}</strong> 吗？此操作不可撤销。</div>
      <template #footer>
        <Button variant="plain" @click="confirmDelete = null">取消</Button>
        <Button variant="danger" @click="handleDelete">删除</Button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 14px; padding: 18px 20px 28px; height: 100%; overflow-y: auto; }
.page > * { flex-shrink: 0; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(272px, 1fr)); gap: 12px; }
.model-card { background: var(--bg-card); border: 1px solid var(--separator); border-radius: var(--r-lg); padding: 14px; box-shadow: var(--shadow-card); backdrop-filter: blur(16px); transition: transform .18s var(--spring), border-color .18s var(--ease); }
.model-card:hover { transform: translateY(-1px); border-color: var(--separator-strong); }
.model-id { color: var(--text-tertiary); font-size: 11px; }
.cap-row { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 10px; }
.cap { font-size: 10px; padding: 3px 6px; color: var(--accent-strong); background: var(--accent-soft); border-radius: 999px; }
.cap.muted { color: var(--text-tertiary); background: var(--bg-fill); }
.inset-group { border: 1px solid var(--separator); border-radius: var(--r-sm); overflow: hidden; }
.inset-row { display: flex; min-height: 32px; align-items: center; gap: 9px; padding: 6px 10px; border-bottom: 1px solid var(--separator); }
.inset-row:last-child { border-bottom: 0; }
.inset-row > span { color: var(--text-tertiary); font-size: 10px; width: 39px; letter-spacing: .04em; }
.inset-row code { min-width: 0; color: var(--text-secondary); font-size: 11px; }
.empty { flex: 1; min-height: 270px; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 40px 20px; text-align: center; border: 1px dashed var(--separator-strong); border-radius: var(--r-lg); }
.empty-icon { width: 48px; height: 48px; display: grid; place-items: center; border-radius: 15px; font-size: 28px; color: var(--accent-strong); background: var(--accent-soft); margin-bottom: 13px; }
.editor { padding-bottom: 2px; }
.section { display: flex; flex-direction: column; gap: 9px; padding: 12px; border: 1px solid var(--separator); border-radius: var(--r-md); background: color-mix(in srgb, var(--bg-inset) 58%, transparent); }
.two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.compact-label { color: var(--text-tertiary); font-size: 10px; letter-spacing: .06em; text-transform: uppercase; }
.claude-path-control { display: flex; flex-direction: column; gap: 7px; padding: 9px; border: 1px dashed var(--separator-strong); border-radius: var(--r-sm); background: var(--bg-card); }
.account-binding { background: color-mix(in srgb, var(--blue-soft) 30%, var(--bg-card)); }
.account-binding-list { max-height: 144px; overflow-y: auto; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-card); }
.account-binding-row { display: grid; grid-template-columns: 16px minmax(0, 1fr) auto auto; align-items: center; gap: 8px; min-height: 34px; padding: 0 10px; border-bottom: 1px solid var(--separator); color: var(--text-secondary); font-size: 12px; }
.account-binding-row:last-child { border-bottom: 0; }
.account-binding-row code { font-size: 10px; color: var(--text-tertiary); }
.account-binding-row.paused { opacity: .55; }
.paused-label { color: var(--orange); font-size: 10px; }
.text-field { display: flex; flex-direction: column; gap: 6px; }
textarea { width: 100%; min-height: 64px; padding: 9px 10px; resize: vertical; border: 1px solid var(--separator-strong); border-radius: var(--r-sm); background: var(--bg-inset); color: var(--text-primary); font: 11.5px var(--font-num); }
textarea:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); outline: none; }
.discover-box { background: color-mix(in srgb, var(--accent-soft) 28%, var(--bg-card)); }
.selection-list { max-height: 196px; overflow-y: auto; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-card); }
.select-all, .select-row { display: grid; grid-template-columns: 16px minmax(0, 1fr) minmax(90px, .75fr); align-items: center; gap: 8px; min-height: 34px; padding: 0 10px; border-bottom: 1px solid var(--separator); font-size: 12px; }
.select-all { grid-template-columns: 16px 1fr; color: var(--text-secondary); background: var(--bg-fill); position: sticky; top: 0; }
.select-row:last-child { border-bottom: 0; }
.select-row code { color: var(--text-tertiary); font-size: 10px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.capability-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 7px; }
.capability-grid label { display: flex; gap: 7px; align-items: center; min-height: 30px; padding: 0 8px; font-size: 12px; color: var(--text-secondary); border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-card); }
.reasoning-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 5px; }
.reasoning-grid button { height: 30px; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-card); color: var(--text-secondary); font-size: 11px; }
.reasoning-grid button.active { border-color: var(--accent); color: var(--accent-strong); background: var(--accent-soft); }
.notice-box, .err-box { padding: 10px 11px; border-radius: var(--r-sm); font-size: 12px; line-height: 1.45; }
.notice-box { color: var(--accent-strong); background: var(--accent-soft); border: 1px solid var(--accent-border); }
.err-box { color: var(--red); background: rgba(255,69,58,.1); border: 1px solid rgba(255,69,58,.25); }
@media (max-width: 620px) { .two-col { grid-template-columns: 1fr; } .capability-grid { grid-template-columns: 1fr; } .select-row { grid-template-columns: 16px minmax(0, 1fr); } .select-row code { display: none; } }
</style>
