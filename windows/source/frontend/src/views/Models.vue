<script setup>
import { computed, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Field from "@/components/ui/Field.vue";
import Modal from "@/components/ui/Modal.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import AccountTestModal from "@/components/accounts/AccountTestModal.vue";
import { groupModelsByUpstream, modelIsEnabled } from "@/state/modelGroups";
import {
  normalizeReasoningEffort,
  reasoningEffortLabel,
  resolveReasoningProfile,
} from "@/state/reasoningCapabilities";
import {
  state,
  saveModel,
  discoverUpstreamModels,
  testUpstreamModelDetailed,
  cancelUpstreamAccountTest,
  addDiscoveredModels,
} from "@/state/appState";

const DEFAULT_XIASS_URL = "https://api.xiass.com";
const AUTO_ENDPOINT_SUFFIXES = [
  "/chat/completions",
  "/chat/messages",
  "/responses",
  "/messages",
  "/models",
];

const editorOpen = ref(false);
const editorError = ref("");
const editorNotice = ref("");
const saving = ref(false);
const discovering = ref(false);
const isNew = ref(false);
const editingGroup = ref(null);
const reasoningEditorOpen = ref(false);
const reasoningEditorModel = ref(null);
const reasoningEditorEffort = ref("auto");
const reasoningEditorSaving = ref(false);
const reasoningEditorError = ref("");
const discoveredModels = ref([]);
const selectedModelIds = ref([]);
const discoveredReasoningEfforts = ref({});
const discoveredReasoningTouched = ref({});
const modelEnableBusy = ref({});
const groupEnableBusy = ref({});
const modelActionMessage = ref("");
const modelActionError = ref("");
const expandedGroupKeys = ref(new Set());
const modelTestOpen = ref(false);
const modelTestConfig = ref(null);
const modelTestConfigByID = ref({});
const modelTestModelsOverride = ref([]);
const modelTestDefaultModelIDOverride = ref("");
const modelTestAccountOverride = ref(null);
const modelTestTitle = ref("测试账号连接");
const modelTestStatus = ref("idle");
const modelTestOutputLines = ref([]);
const modelTestContent = ref("");
const modelTestError = ref("");
const modelTestImages = ref([]);
const modelTestRequestID = ref("");
let modelTestGeneration = 0;
let modelTestRequestSerial = 0;

function defaultCapabilities(provider = "openai", modelName = "", apiStyle = "chat_completions") {
  const name = String(modelName).toLowerCase();
  const nonChat = /embedding|whisper|tts/.test(name);
  const normalizedProvider = String(provider || "openai").toLowerCase();
  const normalizedStyle = String(apiStyle || "chat_completions").toLowerCase();
  const supportsHostedWebSearch = !nonChat && normalizedProvider === "openai" && normalizedStyle === "responses";
  const supportsDirectImageGeneration = !nonChat && normalizedStyle !== "messages" && ["openai", "grok", "custom"].includes(normalizedProvider);
  return {
    configured: true,
    supportsImages: !nonChat,
    supportsFiles: !nonChat,
    supportsAudio: false,
    supportsVideo: false,
    supportsToolCalls: !nonChat,
    supportsWebSearch: supportsHostedWebSearch,
    supportsImageGeneration: supportsDirectImageGeneration,
    supportsThinking: !nonChat,
  };
}

// Models are exposed to Antigravity as full chat-capable by default.  The proxy
// downgrades an individual request when an upstream endpoint does not implement
// an optional feature, so users never need to guess capability checkboxes.
function automaticCapabilities(provider = "openai", modelName = "", previous = {}, apiStyle = "chat_completions") {
  const saved = previous && typeof previous === "object" ? previous : {};
  return {
    ...saved,
    ...defaultCapabilities(provider, modelName, apiStyle),
    configured: true,
  };
}

function emptyForm() {
  return {
    name: "",
    displayName: "",
    upstreamName: "",
    description: "",
    provider: "openai",
    apiKey: "",
    apiUrl: DEFAULT_XIASS_URL,
    endpointMode: "auto",
    accountIds: [],
    externalModelName: "",
    reasoningEffort: "auto",
    apiStyle: "chat_completions",
    messagePathMode: "auto",
    authMode: "bearer",
    authHeader: "",
    headersText: "{}",
    enabled: true,
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
  { label: "Chat（推荐）", value: "chat_completions" },
  { label: "自动（按 Chat）", value: "auto" },
  { label: "Responses", value: "responses" },
  { label: "Messages", value: "messages" },
];
function apiStyleLabel(value) {
  return apiStyleOptions.find((option) => option.value === value)?.label || "Chat（推荐）";
}
const endpointModeOptions = [
  { label: "智能补全", value: "auto" },
  { label: "完整路径（手动）", value: "manual" },
];
const anthropicPathOptions = [
  { label: "自动", value: "auto" },
  { label: "标准 Messages", value: "standard" },
  { label: "兼容 Chat Messages", value: "compat" },
];
const formReasoningProfile = computed(() => resolveReasoningProfile({
  provider: form.value.provider,
  model: form.value.externalModelName,
  apiStyle: form.value.apiStyle,
  capabilities: form.value.capabilities,
}));
const reasoningOptions = computed(() => formReasoningProfile.value.options);
const selectedFormReasoningEffort = computed(() =>
  normalizeReasoningEffort(form.value.reasoningEffort, formReasoningProfile.value)
);
const testTarget = computed(() => selectedModelIds.value[0] || form.value.externalModelName?.trim());
const selectedAccounts = computed(() => {
  const selected = new Set(form.value.accountIds || []);
  return state.accounts.filter((account) => selected.has(account.id));
});
const accountBindingError = computed(() => {
  const selectedIDs = form.value.accountIds || [];
  if (!selectedIDs.length) return "";
  if (selectedAccounts.value.length !== selectedIDs.length) return "所选账户已不存在，请重新选择。";
  if (selectedAccounts.value.some((account) => !account.enabled)) return "账户池中包含已暂停账户，请启用或取消选择。";
  const providers = new Set(selectedAccounts.value.map((account) => String(account.provider || "openai").toLowerCase()));
  if (providers.size > 1) return "同一个模型只能绑定同一种上游协议的账户。";
  const accountProvider = [...providers][0];
  if (accountProvider && accountProvider !== form.value.provider) return `所选账户使用 ${providerLabel(accountProvider)} 协议，请先切换供应商或重新选择账户。`;
  return "";
});
const automaticCapabilityItems = computed(() => {
  const capabilities = automaticCapabilities(
    form.value.provider,
    form.value.externalModelName,
    form.value.capabilities,
    form.value.apiStyle
  );
  return [
    { label: "图片/截图", enabled: capabilities.supportsImages },
    { label: form.value.provider === "anthropic" || ["messages", "responses"].includes(form.value.apiStyle) ? "文件/PDF" : "文本文件", enabled: capabilities.supportsFiles },
    { label: "原生工具调用", enabled: capabilities.supportsToolCalls },
    { label: "推理强度", enabled: capabilities.supportsThinking },
    { label: "上游联网搜索", enabled: capabilities.supportsWebSearch },
    { label: "上游图片生成", enabled: capabilities.supportsImageGeneration },
  ];
});
const allDiscoveredSelected = computed(() =>
  discoveredModels.value.length > 0 && selectedModelIds.value.length === discoveredModels.value.length
);
const selectedCount = computed(() => selectedModelIds.value.length);
const modelGroups = computed(() => groupModelsByUpstream(state.models));
const editorTitle = computed(() => editingGroup.value ? "编辑上游 / 代理商" : (isNew.value ? "添加上游模型" : "编辑上游模型"));
function uniqueTestModels(models) {
  const seen = new Set();
  const unique = [];
  for (const model of models) {
    const id = String(model?.id || model?.name || "").trim();
    if (!id || seen.has(id)) continue;
    seen.add(id);
    unique.push({ id, name: String(model?.name || model?.id || "").trim() || id });
  }
  return unique;
}
const testModalModels = computed(() => {
  if (modelTestModelsOverride.value.length) return uniqueTestModels(modelTestModelsOverride.value);
  const models = discoveredModels.value.length
    ? discoveredModels.value
    : testTarget.value
      ? [{ id: testTarget.value, name: testTarget.value }]
      : [];
  return uniqueTestModels(models);
});
const testModalDefaultModelID = computed(() => modelTestDefaultModelIDOverride.value || testTarget.value || testModalModels.value[0]?.id || "");
const testModalAccount = computed(() => modelTestAccountOverride.value || ({
  // Deliberately omit API key, headers, and endpoint details. AccountTestModal
  // only needs neutral display metadata; the sensitive config remains local to
  // the native test call and is never rendered.
  name: form.value.upstreamName?.trim() || form.value.displayName?.trim() || form.value.externalModelName?.trim() || `${providerLabel(form.value.provider)} 临时上游`,
  provider: form.value.provider,
  type: "api_key",
  enabled: true,
}));
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
  return "只填域名或基础路径即可；XIASS Tools 会自动补全对应的 /v1 接口。";
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

function groupDisplayName(group) {
  return String(group?.upstreamName || "").trim() || `${providerLabel(group?.provider)} 上游`;
}

function modelReasoningProfile(model, fallbackProvider = form.value.provider, fallbackAPIStyle = form.value.apiStyle) {
  return resolveReasoningProfile({
    ...(model && typeof model === "object" ? model : {}),
    provider: model?.provider || fallbackProvider,
    model: model?.externalModelName || model?.external_model_name || model?.id || model?.name || "",
    apiStyle: model?.apiStyle || fallbackAPIStyle,
  });
}

function modelReasoningLabel(model) {
  const profile = modelReasoningProfile(model, model?.provider, model?.apiStyle);
  return reasoningEffortLabel(model?.reasoningEffort, profile);
}

function discoveredModelID(model) {
  return String(model?.id || model?.externalModelName || model?.name || "")
    .trim()
    .replace(/^models\//, "");
}

function resetDiscoveredModelSelection(models = [], selectedIDs = null) {
  const list = Array.isArray(models) ? models : [];
  const efforts = {};
  for (const model of list) {
    const id = discoveredModelID(model);
    if (!id) continue;
    efforts[id] = normalizeReasoningEffort(model?.reasoningEffort, modelReasoningProfile(model));
  }
  discoveredModels.value = list;
  const availableIDs = list.map(discoveredModelID).filter(Boolean);
  selectedModelIds.value = selectedIDs === null
    ? availableIDs
    : availableIDs.filter((id) => new Set(selectedIDs).has(id));
  discoveredReasoningEfforts.value = efforts;
  discoveredReasoningTouched.value = {};
}

function discoveredReasoningProfile(model) {
  return modelReasoningProfile(model);
}

function discoveredReasoningEffort(model) {
  const id = discoveredModelID(model);
  return normalizeReasoningEffort(
    discoveredReasoningEfforts.value[id],
    discoveredReasoningProfile(model)
  );
}

function setDiscoveredReasoningEffort(model, value) {
  const id = discoveredModelID(model);
  if (!id) return;
  const profile = discoveredReasoningProfile(model);
  discoveredReasoningEfforts.value = {
    ...discoveredReasoningEfforts.value,
    [id]: normalizeReasoningEffort(value, profile),
  };
  discoveredReasoningTouched.value = { ...discoveredReasoningTouched.value, [id]: true };
}

function setFormReasoningEffort(value) {
  form.value.reasoningEffort = normalizeReasoningEffort(value, formReasoningProfile.value);
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

function groupHost(group) {
  return hostOf(String(group?.apiUrl || "").replace(/^manual:/, ""));
}

function groupBindingLabel(group) {
  const accountIDs = new Set();
  for (const model of group?.models || []) {
    for (const accountID of Array.isArray(model?.accountIds) ? model.accountIds : []) accountIDs.add(accountID);
  }
  return accountIDs.size ? `账户池 ${accountIDs.size} 个` : "直接 API 凭据";
}

function isGroupExpanded(group) {
  return Boolean(group?.key) && expandedGroupKeys.value.has(group.key);
}

function toggleGroupExpanded(group) {
  if (!group?.key) return;
  const next = new Set(expandedGroupKeys.value);
  if (next.has(group.key)) next.delete(group.key);
  else next.add(group.key);
  expandedGroupKeys.value = next;
}

function storedModelID(model) {
  return String(model?.externalModelName || model?.id || model?.name || "")
    .trim()
    .replace(/^models\//i, "");
}

function storedModelTestConfig(model) {
  const accountIds = [...new Set((Array.isArray(model?.accountIds) ? model.accountIds : [])
    .map((id) => String(id || "").trim())
    .filter(Boolean))];
  return {
    provider: model?.provider || "openai",
    upstreamName: String(model?.upstreamName || "").trim(),
    apiUrl: model?.apiUrl || "",
    endpointMode: model?.endpointMode || (model?.messagePathMode === "manual" ? "manual" : "auto"),
    apiKey: model?.apiKey || "",
    apiStyle: model?.apiStyle || "chat_completions",
    messagePathMode: model?.messagePathMode || "auto",
    authMode: model?.authMode || "bearer",
    authHeader: model?.authHeader || "",
    headers: model?.headers && typeof model.headers === "object" ? { ...model.headers } : {},
    accountIds,
    accountId: accountIds[0] || "",
  };
}

function groupTestModels(group) {
  return (group?.models || [])
    .map((model) => ({
      id: storedModelID(model),
      name: String(model?.displayName || model?.externalModelName || model?.name || "").trim(),
      config: storedModelTestConfig(model),
    }))
    .filter((model) => model.id);
}

function openGroupModelTest(group) {
  const models = groupTestModels(group);
  const preferred = models.find((entry) => modelIsEnabled(
    (group?.models || []).find((model) => storedModelID(model) === entry.id)
  )) || models[0];
  const configByID = Object.fromEntries(models.map((model) => [model.id, model.config]));
  openModelTest({
    config: preferred?.config,
    models,
    defaultModelID: preferred?.id || "",
    configByID,
    title: `${groupDisplayName(group)} 测试 · ${groupHost(group)}`,
    account: {
      name: `${groupDisplayName(group)} · ${groupHost(group)}`,
      provider: group?.provider || "openai",
      type: "upstream",
      enabled: true,
    },
  });
}

function enabledModelCount(group) {
  return (group?.models || []).filter(modelIsEnabled).length;
}

function isGroupEnableBusy(group) {
  return Boolean(groupEnableBusy.value[group?.key]);
}

function isModelEnableBusy(model) {
  return Boolean(modelEnableBusy.value[model?.name]);
}

function sanitizeModelMessage(value, config = modelTestConfig.value) {
  let message = String(value?.message || value || "操作失败");
  const secrets = [
    config?.apiKey,
    ...Object.values(config?.headers || {}),
    form.value.apiKey,
  ].filter((secret) => typeof secret === "string" && secret.length >= 4);
  for (const secret of secrets) {
    message = message.split(secret).join("[已隐藏]");
  }
  return message;
}

// Older releases stored a full inference endpoint even when endpointMode was
// "auto". In that mode the proxy owns the protocol suffix, so showing the
// full path is both confusing and makes a later provider switch error-prone.
// Preserve a non-standard base path and query string; strip only recognised
// endpoint leaves. Manual mode never calls this function.
function normalizeAutoBaseURL(value) {
  const raw = String(value || "").trim();
  if (!raw) return DEFAULT_XIASS_URL;
  try {
    const parsed = new URL(raw);
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") return raw;
    let path = parsed.pathname.replace(/\/+$/, "");
    const lowered = path.toLowerCase();
    for (const suffix of AUTO_ENDPOINT_SUFFIXES) {
      if (lowered.endsWith(suffix)) {
        path = path.slice(0, -suffix.length);
        break;
      }
    }
    // XIASS's default is intentionally just the domain. Its resolver adds
    // /v1 itself, whereas a third-party /v1 base path remains intact.
    if (parsed.hostname.toLowerCase() === "api.xiass.com" && (path === "" || path === "/v1")) {
      path = "";
    }
    return `${parsed.protocol}//${parsed.host}${path}${parsed.search}`;
  } catch {
    return raw;
  }
}

function capabilityLabels(model) {
  const caps = automaticCapabilities(model.provider, model.externalModelName, model.capabilities, model.apiStyle);
  const labels = [];
  if (caps.supportsImages) labels.push("识图");
  if (caps.supportsFiles) labels.push("文件");
  if (caps.supportsToolCalls) labels.push("工具");
  if (caps.supportsWebSearch) labels.push("联网");
  if (caps.supportsImageGeneration) labels.push("生图");
  return labels;
}

function visibleModelName(model) {
  const base = String(model?.displayName || model?.externalModelName || model?.name || "").trim();
  const upstream = String(model?.upstreamName || "").trim();
  const pool = String(model?.accountPoolLabel || "").trim();
  const label = pool || upstream;
  return label ? `${base} · ${label}` : base;
}

function modelToForm(model) {
  const capabilities = automaticCapabilities(model.provider, model.externalModelName, model.capabilities, model.apiStyle);
  const endpointMode = model.endpointMode || (model.messagePathMode === "manual" ? "manual" : "auto");
  const next = {
    ...emptyForm(),
    ...model,
    apiUrl: endpointMode === "auto" ? normalizeAutoBaseURL(model.apiUrl) : model.apiUrl,
    endpointMode,
    accountIds: Array.isArray(model.accountIds) ? [...model.accountIds] : [],
    apiStyle: model.apiStyle || (model.provider === "anthropic" ? "messages" : "chat_completions"),
    messagePathMode: model.messagePathMode === "manual" ? "auto" : (model.messagePathMode || "auto"),
    authMode: model.authMode || (model.provider === "anthropic" ? "x_api_key" : "bearer"),
    authHeader: model.authHeader || "",
    headersText: JSON.stringify(model.headers || {}, null, 2),
    enabled: model.enabled !== false,
    capabilities,
  };
  next.reasoningEffort = normalizeReasoningEffort(
    model.reasoningEffort,
    modelReasoningProfile({ ...model, ...next }, next.provider, next.apiStyle)
  );
  // accountPoolLabel is runtime-only display metadata. The editable model
  // name and the real upstream model ID must remain independent from it.
  delete next.accountPoolLabel;
  return next;
}

function openNew() {
  form.value = emptyForm();
  isNew.value = true;
  editingGroup.value = null;
  editorError.value = "";
  editorNotice.value = "默认只需填写 XIASS 域名；如上游要求完整接口地址，可随时切换到“完整路径（手动）”。";
  resetDiscoveredModelSelection();
  editorOpen.value = true;
}

function openModelReasoningEditor(model) {
  reasoningEditorModel.value = model;
  reasoningEditorEffort.value = normalizeReasoningEffort(model?.reasoningEffort, modelReasoningProfile(model));
  reasoningEditorError.value = "";
  reasoningEditorOpen.value = true;
}

async function saveModelReasoning() {
  const model = reasoningEditorModel.value;
  if (!model?.name || reasoningEditorSaving.value) return;
  const profile = modelReasoningProfile(model);
  reasoningEditorSaving.value = true;
  reasoningEditorError.value = "";
  try {
    const result = await saveModel({
      ...model,
      reasoningEffort: normalizeReasoningEffort(reasoningEditorEffort.value, profile),
    });
    if (!result?.ok) throw new Error(result?.message || "保存推理强度失败");
    reasoningEditorOpen.value = false;
    reasoningEditorModel.value = null;
  } catch (error) {
    reasoningEditorError.value = sanitizeModelMessage(error);
  } finally {
    reasoningEditorSaving.value = false;
  }
}

function openEditGroup(group) {
  const firstModel = group?.models?.[0];
  if (!group?.key || !firstModel) return;
  form.value = modelToForm(firstModel);
  isNew.value = false;
  editingGroup.value = group;
  editorError.value = "";
  editorNotice.value = `保存后会同步更新此代理商下 ${group.models.length} 个模型的名称、协议、地址、认证、账户绑定和 API 模式；各模型的模型名、启用状态与推理强度会保留。`;
  const currentModels = (group.models || []).map((model) => ({
    id: storedModelID(model),
    name: model.externalModelName || model.displayName || model.name,
    provider: model.provider,
    apiStyle: model.apiStyle,
    reasoningEffort: model.reasoningEffort,
  }));
  const enabledIDs = (group.models || []).filter(modelIsEnabled).map(storedModelID);
  resetDiscoveredModelSelection(currentModels, enabledIDs);
  editorOpen.value = true;
}

function onProviderChange(provider) {
  form.value.provider = provider;
  if (provider === "anthropic") {
    form.value.authMode = "x_api_key";
    form.value.apiStyle = "messages";
  } else if (form.value.authMode === "x_api_key") {
    form.value.authMode = "bearer";
    if (form.value.apiStyle === "messages") form.value.apiStyle = "chat_completions";
  }
  form.value.capabilities = automaticCapabilities(provider, form.value.externalModelName, form.value.capabilities, form.value.apiStyle);
  form.value.reasoningEffort = normalizeReasoningEffort(
    form.value.reasoningEffort,
    resolveReasoningProfile({
      provider: form.value.provider,
      model: form.value.externalModelName,
      apiStyle: form.value.apiStyle,
      capabilities: form.value.capabilities,
    })
  );
}

function onEndpointModeChange(mode) {
  form.value.endpointMode = mode;
  if (mode === "manual") {
    editorNotice.value = "手动完整路径已启用：XIASS Tools 会原样使用你填写的 API 地址，不再补全或替换路径。";
  } else {
    const originalURL = String(form.value.apiUrl || "").trim();
    form.value.apiUrl = normalizeAutoBaseURL(originalURL);
    editorNotice.value = form.value.apiUrl !== originalURL
      ? "智能补全已启用：已将完整接口尾缀收敛为基础地址，XIASS Tools 会按当前协议补全请求路径。"
      : "智能补全已启用：只需填写域名或基础路径，XIASS Tools 会按当前协议补全请求地址。";
  }
}

// Keep the model editor consistent with the account editor: automatic mode
// owns the protocol leaf, so a pasted legacy /v1/chat/completions-style URL is
// immediately displayed as its base address. Manual mode deliberately leaves
// every character untouched so users can override any endpoint themselves.
function onAPIURLChange(value) {
  const raw = typeof value === "string" ? value : "";
  form.value.apiUrl = form.value.endpointMode === "auto" ? normalizeAutoBaseURL(raw) : raw;
}

function parseHeaders() {
  const raw = String(form.value.headersText || "{}").trim();
  if (!raw) return {};
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("附加请求头必须是 JSON 对象，例如 {\"X-Client\": \"XIASS Tools\"}");
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
  const accountIds = selectedAccounts.value.map((account) => account.id);
  const endpointMode = form.value.endpointMode;
  const apiUrl = endpointMode === "auto"
    ? normalizeAutoBaseURL(form.value.apiUrl)
    : form.value.apiUrl?.trim();
  return {
    provider: form.value.provider,
    upstreamName: form.value.upstreamName?.trim(),
    apiUrl,
    endpointMode,
    apiKey: form.value.apiKey?.trim(),
    apiStyle: form.value.apiStyle,
    messagePathMode: form.value.messagePathMode,
    authMode: form.value.authMode,
    authHeader: form.value.authHeader?.trim(),
    headers: parseHeaders(),
    accountIds,
    accountId: accountIds[0] || "",
  };
}

function validateConnection() {
  if (accountBindingError.value) throw new Error(accountBindingError.value);
  const config = upstreamConfig();
  if (config.accountIds.length) return config;
  if (!form.value.apiUrl?.trim()) throw new Error("请填写 API 地址");
  if (!form.value.apiKey?.trim()) throw new Error("请填写 API Key 或访问令牌");
  return config;
}

async function fetchModels() {
  editorError.value = "";
  editorNotice.value = "";
  let config;
  try {
    config = validateConnection();
  } catch (error) {
    editorError.value = sanitizeModelMessage(error);
    return;
  }
  discovering.value = true;
  try {
    const result = await discoverUpstreamModels(config);
    if (!result?.ok) {
      editorError.value = sanitizeModelMessage(result?.message || "无法获取上游模型列表", config);
      return;
    }
    const models = result.models || [];
    if (editingGroup.value) {
      const existing = new Map((editingGroup.value.models || []).map((model) => [storedModelID(model), model]));
      const selected = models
        .map(discoveredModelID)
        .filter((id) => !existing.has(id) || modelIsEnabled(existing.get(id)));
      resetDiscoveredModelSelection(models, selected);
    } else {
      resetDiscoveredModelSelection(models);
    }
    editorNotice.value = result.message || `已发现 ${selectedModelIds.value.length} 个模型，默认已全部选中。`;
  } catch (error) {
    editorError.value = sanitizeModelMessage(error, config);
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
  selectedModelIds.value = allDiscoveredSelected.value ? [] : discoveredModels.value.map(discoveredModelID).filter(Boolean);
}

function modelTestSteps(steps, config) {
  if (!Array.isArray(steps)) return [];
  return steps
    .filter((step) => String(step?.type || "").toLowerCase() !== "complete")
    .map((step) => ({
      text: sanitizeModelMessage(step?.text, config),
      tone: String(step?.tone || "muted"),
    }))
    .filter((step) => step.text);
}

function nextModelTestRequestID() {
  const nativeID = globalThis.crypto?.randomUUID?.();
  if (typeof nativeID === "string" && nativeID) return `model-test-${nativeID}`;
  modelTestRequestSerial += 1;
  return `model-test-${Date.now().toString(36)}-${modelTestRequestSerial}-${Math.random().toString(36).slice(2, 10)}`;
}

function cancelActiveModelTest() {
  const requestID = modelTestRequestID.value;
  modelTestRequestID.value = "";
  if (!requestID) return;
  void cancelUpstreamAccountTest(requestID).catch(() => {});
}

function openModelTest({ config, models, defaultModelID, configByID = {}, account = null, title = "测试账号连接" }) {
  const uniqueModels = uniqueTestModels(models || []);
  if (!config || !uniqueModels.length) {
    modelActionError.value = "没有可测试的模型配置，请先编辑或重新添加该上游。";
    return;
  }
  cancelActiveModelTest();
  modelTestGeneration += 1;
  modelTestConfig.value = config;
  modelTestConfigByID.value = { ...configByID };
  modelTestModelsOverride.value = uniqueModels;
  modelTestDefaultModelIDOverride.value = uniqueModels.some((model) => model.id === defaultModelID)
    ? defaultModelID
    : (uniqueModels[0]?.id || "");
  modelTestAccountOverride.value = account;
  modelTestTitle.value = String(title || "测试账号连接");
  modelTestStatus.value = "idle";
  modelTestError.value = "";
  modelTestContent.value = "";
  modelTestImages.value = [];
  modelTestOutputLines.value = [{ text: "已准备详细测试；可选择模型与测试提示词。", tone: "info" }];
  modelTestOpen.value = true;
}

function openDetailedModelTest() {
  editorError.value = "";
  let config;
  try {
    config = validateConnection();
  } catch (error) {
    editorError.value = sanitizeModelMessage(error);
    return;
  }
  if (!testTarget.value) {
    editorError.value = "请先获取模型列表并选择一个模型，或填写上游模型名。";
    return;
  }
  openModelTest({
    config,
    models: testModalModels.value,
    defaultModelID: testTarget.value,
    account: testModalAccount.value,
  });
}

async function runDetailedModelTest(payload) {
  const config = modelTestConfigByID.value[String(payload?.modelId || "")] || modelTestConfig.value;
  if (!config || !payload?.modelId || modelTestStatus.value === "connecting") return;
  const generation = ++modelTestGeneration;
  const requestID = nextModelTestRequestID();
  modelTestRequestID.value = requestID;
  modelTestStatus.value = "connecting";
  modelTestError.value = "";
  modelTestContent.value = "";
  modelTestImages.value = [];
  modelTestOutputLines.value = [{ text: "正在发送详细模型测试请求…", tone: "info" }];
  try {
    const result = await testUpstreamModelDetailed(config, {
      requestId: requestID,
      model: String(payload.modelId || "").trim(),
      prompt: String(payload.prompt || "hi").trim() || "hi",
      mode: String(payload.mode || "default"),
    });
    if (generation !== modelTestGeneration) return;
    modelTestOutputLines.value = modelTestSteps(result?.steps, config);
    const content = sanitizeModelMessage(result?.content || "", config);
    modelTestContent.value = content && !modelTestOutputLines.value.some((line) => line.text.includes(content)) ? content : "";
    modelTestImages.value = Array.isArray(result?.images) ? result.images : [];
    if (result?.ok) {
      modelTestStatus.value = "success";
      if (!modelTestOutputLines.value.length) {
        modelTestOutputLines.value = [{ text: sanitizeModelMessage(result?.message || "模型可用", config), tone: "success" }];
      }
      return;
    }
    modelTestStatus.value = "error";
    modelTestError.value = sanitizeModelMessage(result?.message || "模型测试失败", config);
    if (!modelTestOutputLines.value.length) modelTestOutputLines.value = [{ text: modelTestError.value, tone: "error" }];
  } catch (error) {
    if (generation !== modelTestGeneration) return;
    modelTestStatus.value = "error";
    modelTestError.value = sanitizeModelMessage(error, config);
    modelTestOutputLines.value = [{ text: modelTestError.value, tone: "error" }];
  } finally {
    if (modelTestRequestID.value === requestID) modelTestRequestID.value = "";
  }
}

function cancelDetailedModelTest() {
  modelTestGeneration += 1;
  cancelActiveModelTest();
}

function closeDetailedModelTest() {
  cancelDetailedModelTest();
  modelTestOpen.value = false;
  modelTestConfig.value = null;
  modelTestConfigByID.value = {};
  modelTestModelsOverride.value = [];
  modelTestDefaultModelIDOverride.value = "";
  modelTestAccountOverride.value = null;
  modelTestTitle.value = "测试账号连接";
}

function previewModelTestImage(image) {
  const url = typeof image === "string" ? image : String(image?.url || "");
  if (url) window.open(url, "_blank", "noopener,noreferrer");
}

async function setModelEnabled(model, enabled, { quiet = false } = {}) {
  if (!model?.name || modelIsEnabled(model) === enabled || isModelEnableBusy(model)) return true;
  if (!quiet) {
    modelActionError.value = "";
    modelActionMessage.value = "";
  }
  modelEnableBusy.value = { ...modelEnableBusy.value, [model.name]: true };
  try {
    const payload = { ...model, enabled };
    delete payload.accountPoolLabel;
    const result = await saveModel(payload);
    if (!result?.ok) {
      modelActionError.value = sanitizeModelMessage(result?.message || "模型状态保存失败");
      return false;
    }
    if (!quiet) modelActionMessage.value = `${visibleModelName(model) || "模型"} 已${enabled ? "启用" : "停用"}。`;
    return true;
  } catch (error) {
    modelActionError.value = sanitizeModelMessage(error);
    return false;
  } finally {
    const next = { ...modelEnableBusy.value };
    delete next[model.name];
    modelEnableBusy.value = next;
  }
}

async function setGroupEnabled(group, enabled) {
  if (!group?.key || isGroupEnableBusy(group)) return;
  const targets = (group.models || []).filter((model) => modelIsEnabled(model) !== enabled);
  if (!targets.length) return;
  modelActionError.value = "";
  modelActionMessage.value = "";
  groupEnableBusy.value = { ...groupEnableBusy.value, [group.key]: true };
  try {
    let completed = 0;
    for (const model of targets) {
      if (await setModelEnabled(model, enabled, { quiet: true })) completed += 1;
    }
    if (!modelActionError.value) {
      modelActionMessage.value = `已${enabled ? "启用" : "停用"}该上游的 ${completed} 个模型。`;
    }
  } finally {
    const next = { ...groupEnableBusy.value };
    delete next[group.key];
    groupEnableBusy.value = next;
  }
}

function modelWithUpdatedUpstream(model, config) {
  const accountIds = [...new Set((Array.isArray(config?.accountIds) ? config.accountIds : [])
    .map((id) => String(id || "").trim())
    .filter(Boolean))];
  const updated = {
    ...model,
    provider: config.provider,
    upstreamName: String(config.upstreamName || "").trim(),
    apiUrl: config.apiUrl,
    endpointMode: config.endpointMode,
    apiKey: accountIds.length ? "" : config.apiKey,
    apiStyle: config.apiStyle,
    messagePathMode: config.messagePathMode,
    authMode: config.authMode,
    authHeader: config.authHeader,
    headers: config.headers,
    accountIds,
    capabilities: automaticCapabilities(config.provider, model.externalModelName, model.capabilities, config.apiStyle),
  };
  delete updated.accountPoolLabel;
  return updated;
}

async function saveUpstreamGroup() {
  const group = editingGroup.value;
  if (!group?.key || saving.value) return;
  let config;
  try {
    config = validateConnection();
  } catch (error) {
    editorError.value = sanitizeModelMessage(error);
    return;
  }
  const models = [...(group.models || [])];
  if (!models.length) {
    editorError.value = "此代理商下没有可更新的模型。";
    return;
  }
  saving.value = true;
  editorError.value = "";
  try {
    const selected = new Set(selectedModelIds.value);
    const discoveredIDs = new Set(discoveredModels.value.map(discoveredModelID).filter(Boolean));
    const existingIDs = new Set(models.map(storedModelID));
    for (const model of models) {
      const modelID = storedModelID(model);
      const updated = modelWithUpdatedUpstream(model, config);
      if (discoveredIDs.has(modelID)) updated.enabled = selected.has(modelID);
      const result = await saveModel(updated);
      if (!result?.ok) throw new Error(sanitizeModelMessage(result?.message || "保存代理商信息失败", config));
    }
    const newSelectedIDs = selectedModelIds.value.filter((id) => !existingIDs.has(id));
    if (newSelectedIDs.length) {
      const imported = await addDiscoveredModels(config, newSelectedIDs);
      if (!imported?.ok) throw new Error(sanitizeModelMessage(imported?.message || "导入新模型失败", config));
      const persisted = await persistTouchedDiscoveredReasoning(config);
      if (persisted.failures.length) throw new Error(`模型已导入，但推理强度未完全保存：${persisted.failures.join("；")}`);
    }
    const name = String(config.upstreamName || "").trim() || `${providerLabel(config.provider)} 上游`;
    modelActionMessage.value = `已更新“${name}”的代理商信息；启用 ${selected.size} 个模型${newSelectedIDs.length ? `，新增 ${newSelectedIDs.length} 个` : ""}。`;
    modelActionError.value = "";
    editorOpen.value = false;
    editingGroup.value = null;
  } catch (error) {
    editorError.value = sanitizeModelMessage(error, config);
  } finally {
    saving.value = false;
  }
}

async function saveManualModel() {
  let config;
  try {
    config = validateConnection();
  } catch (error) {
    editorError.value = sanitizeModelMessage(error);
    return;
  }
  const externalModelName = form.value.externalModelName?.trim();
  if (!externalModelName) {
    editorError.value = "请填写上游模型名，或先点击“获取全部模型”。";
    return;
  }
  form.value.reasoningEffort = normalizeReasoningEffort(
    form.value.reasoningEffort,
    formReasoningProfile.value
  );
  const model = {
    ...form.value,
    ...config,
    externalModelName,
    displayName: form.value.displayName?.trim() || externalModelName,
    name: form.value.name?.trim() || `models/${form.value.provider}-${externalModelName.replace(/[^a-zA-Z0-9.-]+/g, "-")}`,
    enabled: form.value.enabled !== false,
    capabilities: automaticCapabilities(form.value.provider, externalModelName, form.value.capabilities, form.value.apiStyle),
  };
  delete model.headersText;
  if (model.accountIds?.length) model.apiKey = "";
  saving.value = true;
  try {
    const result = await saveModel(model);
    if (result?.ok) {
      editorOpen.value = false;
    } else {
      editorError.value = sanitizeModelMessage(result?.message || "保存失败", config);
    }
  } catch (error) {
    editorError.value = sanitizeModelMessage(error, config);
  } finally {
    saving.value = false;
  }
}

function accountIDsForConfig(config) {
  return [...new Set((Array.isArray(config?.accountIds) ? config.accountIds : [])
    .map((id) => String(id || "").trim())
    .filter(Boolean))];
}

function matchesImportedModel(model, config, externalModelName) {
  const expectedName = String(externalModelName || "").trim().replace(/^models\//, "");
  const actualName = String(model?.externalModelName || "").trim().replace(/^models\//, "");
  if (!expectedName || actualName !== expectedName) return false;
  if (String(model?.provider || "").toLowerCase() !== String(config?.provider || "").toLowerCase()) return false;

  const expectedAccountIDs = accountIDsForConfig(config);
  if (expectedAccountIDs.length) {
    const actualAccountIDs = new Set((Array.isArray(model?.accountIds) ? model.accountIds : [])
      .map((id) => String(id || "").trim()));
    return expectedAccountIDs.every((id) => actualAccountIDs.has(id));
  }

  const expectedManual = config?.endpointMode === "manual";
  const actualManual = model?.endpointMode === "manual" || model?.messagePathMode === "manual";
  if (expectedManual || actualManual) return String(model?.apiUrl || "").trim() === String(config?.apiUrl || "").trim();
  return normalizeAutoBaseURL(model?.apiUrl) === normalizeAutoBaseURL(config?.apiUrl);
}

// AddDiscoveredModels intentionally keeps its native signature small and
// credential-safe.  The selected per-model effort is therefore persisted in a
// second local save after the batch import has completed and reloaded models.
// Only a row the user changed is written, so an "auto" default never erases a
// reasoning setting on an already configured model from the same upstream.
async function persistTouchedDiscoveredReasoning(config) {
  const touchedIDs = selectedModelIds.value.filter((id) => discoveredReasoningTouched.value[id]);
  if (!touchedIDs.length) return { saved: 0, failures: [] };

  let saved = 0;
  const failures = [];
  for (const modelID of touchedIDs) {
    const discovered = discoveredModels.value.find((model) => discoveredModelID(model) === modelID);
    const target = state.models.find((model) => matchesImportedModel(model, config, modelID));
    if (!discovered || !target) {
      failures.push(`${modelID} 未在本机模型列表中找到`);
      continue;
    }
    const effort = normalizeReasoningEffort(
      discoveredReasoningEfforts.value[modelID],
      modelReasoningProfile(target, config.provider, config.apiStyle)
    );
    if (normalizeReasoningEffort(target.reasoningEffort, modelReasoningProfile(target)) === effort) continue;
    try {
      const result = await saveModel({ ...target, reasoningEffort: effort });
      if (result?.ok) saved += 1;
      else failures.push(`${modelID}：${sanitizeModelMessage(result?.message || "保存失败", config)}`);
    } catch (error) {
      failures.push(`${modelID}：${sanitizeModelMessage(error, config)}`);
    }
  }
  return { saved, failures };
}

async function addSelectedModels() {
  editorError.value = "";
  let config;
  try {
    config = validateConnection();
  } catch (error) {
    editorError.value = sanitizeModelMessage(error);
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
      const persisted = await persistTouchedDiscoveredReasoning(config);
      if (persisted.failures.length) {
        editorError.value = `模型已导入，但思考强度未完全保存：${persisted.failures.join("；")}`;
        return;
      }
      editorNotice.value = persisted.saved
        ? `${result.message || "模型已导入。"} 已保存 ${persisted.saved} 个模型的思考强度。`
        : result.message;
      editorOpen.value = false;
    } else {
      editorError.value = sanitizeModelMessage(result?.message || "批量添加失败", config);
    }
  } catch (error) {
    editorError.value = sanitizeModelMessage(error, config);
  } finally {
    saving.value = false;
  }
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

    <div v-if="modelActionMessage || modelActionError" class="model-action-status" :class="{ error: modelActionError }" role="status">
      {{ modelActionError || modelActionMessage }}
    </div>

    <div v-if="state.models.length > 0" class="grid model-groups">
      <article v-for="group in modelGroups" :key="group.key" class="model-card model-group-card">
        <div class="row between group-heading">
          <div class="grow col" style="gap: 2px; min-width: 0">
            <div class="t-headline truncate">{{ groupDisplayName(group) }}</div>
            <div class="mono truncate model-id">{{ groupHost(group) }} · {{ group.models.length }} 个模型 · {{ groupBindingLabel(group) }}</div>
          </div>
          <Badge :tone="providerTone(group.provider)" :label="providerLabel(group.provider)" />
        </div>
        <div class="group-toolbar">
          <div class="group-summary">
            <span class="group-count">{{ enabledModelCount(group) }}/{{ group.models.length }} 个模型已启用</span>
            <span class="group-summary-note">{{ isGroupExpanded(group) ? '正在显示模型明细' : `已折叠 ${group.models.length} 个模型` }}</span>
          </div>
          <div class="group-actions">
            <Button class="group-test-button" variant="tinted" size="sm" @click="openGroupModelTest(group)">测试连接</Button>
            <div class="group-utility-actions">
              <div class="group-enable-actions">
                <Button variant="tinted" size="sm" :disabled="isGroupEnableBusy(group)" @click="setGroupEnabled(group, true)">全部启用</Button>
                <Button variant="plain" size="sm" :disabled="isGroupEnableBusy(group)" @click="setGroupEnabled(group, false)">全部停用</Button>
                <Button variant="plain" size="sm" @click="openEditGroup(group)">编辑代理商</Button>
              </div>
              <button
                type="button"
                class="group-expand-toggle"
                :aria-expanded="isGroupExpanded(group)"
                :title="isGroupExpanded(group) ? '收起该上游的模型明细' : '展开查看该上游的模型明细'"
                @click="toggleGroupExpanded(group)"
              >{{ isGroupExpanded(group) ? '收起模型' : '展开模型' }}</button>
            </div>
          </div>
        </div>
        <div v-if="isGroupExpanded(group)" class="group-model-list">
          <div v-for="model in group.models" :key="model.name" class="group-model-row" :class="{ disabled: !modelIsEnabled(model) }">
            <div class="model-row-main">
              <label class="model-enable-control" :title="modelIsEnabled(model) ? '停用该模型' : '启用该模型'">
                <input
                  type="checkbox"
                  :checked="modelIsEnabled(model)"
                  :disabled="isModelEnableBusy(model) || isGroupEnableBusy(group)"
                  @change="setModelEnabled(model, $event.target.checked)"
                />
                <span class="model-enable-text">{{ modelIsEnabled(model) ? '启用' : '停用' }}</span>
              </label>
              <div class="grow col model-copy" style="gap: 1px; min-width: 0">
                <div class="t-body truncate">{{ visibleModelName(model) }}</div>
                <div class="mono truncate model-id">{{ model.externalModelName || model.name }}</div>
              </div>
              <div class="model-row-actions">
                <Button variant="plain" size="sm" @click="openModelReasoningEditor(model)">编辑推理</Button>
              </div>
            </div>
            <div class="cap-row model-cap-row">
              <span v-for="label in capabilityLabels(model)" :key="label" class="cap">{{ label }}</span>
              <span v-if="!capabilityLabels(model).length" class="cap muted">文本</span>
              <span class="model-style">{{ apiStyleLabel(model.apiStyle || "chat_completions") }} · {{ modelReasoningLabel(model) }}</span>
            </div>
          </div>
        </div>
      </article>
    </div>

    <Modal :open="editorOpen" :title="editorTitle" wide persistent @close="editorOpen = false">
      <div class="col editor" style="gap: 15px">
        <section class="section">
          <div v-if="editingGroup" class="t-caption">正在编辑 {{ editingGroup.models.length }} 个模型共用的代理商连接信息；模型名、启用状态与推理强度不会被覆盖。</div>
          <span class="t-footnote">供应商与协议</span>
          <SegmentedControl :options="providerOptions" :model-value="form.provider" @update:model-value="onProviderChange" />
          <Field label="上游 / 代理商名称" hint="仅用于本页上游卡片显示；右侧仍会保留 OpenAI、Claude 等平台标识。" v-model="form.upstreamName" placeholder="例如 XIASS、公司代理或自定义名称" />
          <div class="compact-label">接口地址输入方式</div>
          <SegmentedControl :options="endpointModeOptions" :model-value="form.endpointMode" @update:model-value="onEndpointModeChange" />
          <div class="two-col">
            <Field :label="apiURLLabel" :hint="apiURLHint" :model-value="form.apiUrl" :placeholder="apiURLPlaceholder" mono @update:model-value="onAPIURLChange" />
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
            <textarea v-model="form.headersText" spellcheck="false" placeholder='{"X-Client": "XIASS-Tools"}'></textarea>
          </label>
          <div class="compact-label">上游 API 模式</div>
          <SegmentedControl :options="apiStyleOptions" :model-value="form.apiStyle" @update:model-value="form.apiStyle = $event" />
          <div class="t-caption">默认使用 <code>/v1/chat/completions</code>；自动模式也按 Chat 处理。识图、文本附件和函数工具走 Chat，生图单独调用已配置的 Images API。只有手动选择 Responses 或使用 Codex OAuth 才会请求 <code>/v1/responses</code>。</div>
        </section>

        <section class="section discover-box">
          <div class="row between" style="gap: 8px">
            <div>
              <div class="t-headline">{{ editingGroup ? '获取并管理代理商模型' : '发现并批量导入' }}</div>
              <div class="t-caption">
                {{ editingGroup
                  ? '勾选决定哪些模型注入 Antigravity；取消勾选只会停用，不会删除本地配置。点击按钮可重新读取上游 /models。'
                  : '点击后读取上游 /models；发现结果默认全选，不会自动保存。' }}
              </div>
            </div>
            <Button variant="tinted" :loading="discovering" @click="fetchModels">{{ editingGroup ? '刷新上游模型' : '获取全部模型' }}</Button>
          </div>
          <div v-if="discoveredModels.length" class="selection-list">
            <label class="select-all"><input type="checkbox" :checked="allDiscoveredSelected" @change="toggleAllModels" /> 全选 {{ discoveredModels.length }} 个模型</label>
            <label v-for="model in discoveredModels" :key="discoveredModelID(model)" class="select-row">
              <input type="checkbox" :checked="selectedModelIds.includes(discoveredModelID(model))" @change="toggleModel(discoveredModelID(model), $event.target.checked)" />
              <span class="truncate">{{ model.name || model.id }}</span>
              <code>{{ model.id }}</code>
              <span class="discovery-reasoning" :title="discoveredReasoningProfile(model).note" @click.stop>
                <span>思考</span>
                <select
                  :value="discoveredReasoningEffort(model)"
                  :disabled="discoveredReasoningProfile(model).options.length <= 1"
                  @click.stop
                  @change="setDiscoveredReasoningEffort(model, $event.target.value)"
                >
                  <option v-for="option in discoveredReasoningProfile(model).options" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </span>
            </label>
          </div>
          <div class="test-action-row">
            <span class="t-caption">测试会打开完整的账号测试流程，可验证文本、图片与生图链路。</span>
            <Button variant="plain" size="sm" :disabled="!testTarget" @click="openDetailedModelTest">详细测试 {{ testTarget || '模型' }}</Button>
          </div>
        </section>

        <section v-if="!editingGroup && !discoveredModels.length" class="section manual-box">
          <div class="t-headline">手动添加单个模型</div>
          <Field label="上游模型名" hint="例如 gpt-5.6 / claude-sonnet / grok-4" v-model="form.externalModelName" placeholder="gpt-5.6" mono />
          <Field label="显示名称" hint="留空时使用上游模型名" v-model="form.displayName" :placeholder="form.externalModelName || '自动使用上游模型名'" />
          <Field label="描述（可选）" v-model="form.description" placeholder="模型用途或上游说明" />
        </section>

        <section class="section account-binding">
          <div class="t-headline">账户池绑定（可选）</div>
          <div class="t-caption">绑定后，发现与测试使用首个账户；每个新请求按优先级和并发选择一次账户，同一次请求遇到瞬时波动只重试当前账户，不会自动切换其他账户。</div>
          <div v-if="!state.accounts.length" class="t-caption">尚未添加账户。可先在“账户池”中添加 API Key、Token 或导入账户 JSON；未绑定时继续使用本页直接填写的 API Key。</div>
          <div v-else class="account-binding-list">
            <label v-for="account in state.accounts" :key="account.id" class="account-binding-row" :class="{ paused: !account.enabled }">
              <input v-model="form.accountIds" type="checkbox" :value="account.id" :disabled="!account.enabled" />
              <span class="grow truncate">{{ account.name || account.provider || '上游账户' }}</span>
              <code>{{ account.provider }}</code>
              <span v-if="!account.enabled" class="paused-label">已暂停</span>
            </label>
          </div>
        </section>

        <section v-if="!editingGroup" class="section">
          <div class="t-headline">全能力自动适配</div>
          <div class="t-caption">能力按协议和 API 模式自动声明，不需要手动勾选。图片、文本文件、函数工具和推理走 Chat/Claude 转换；生图使用独立 Images API；只有上游明确选择 Responses 时才声明其托管联网能力。</div>
          <div class="capability-grid">
            <span v-for="capability in automaticCapabilityItems" :key="capability.label" class="capability-item" :class="{ disabled: !capability.enabled }">
              <span class="capability-mark">{{ capability.enabled ? '✓' : '—' }}</span>
              {{ capability.label }}
            </span>
          </div>
          <div v-if="automaticCapabilityItems.some((capability) => !capability.enabled)" class="t-caption">嵌入、语音等非聊天模型会保守关闭不适用的能力。</div>
        </section>

        <section v-if="!editingGroup" class="section">
          <label class="select-field reasoning-select-field">
            <span class="t-footnote">思考强度</span>
            <select :value="selectedFormReasoningEffort" @change="setFormReasoningEffort($event.target.value)">
              <option v-for="option in reasoningOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
            <span class="t-caption">{{ formReasoningProfile.note }}</span>
          </label>
        </section>

        <div v-if="editorNotice" class="notice-box">{{ editorNotice }}</div>
        <div v-if="editorError" class="err-box">{{ editorError }}</div>
        <div v-if="accountBindingError" class="err-box">{{ accountBindingError }}</div>
      </div>
      <template #footer>
        <Button variant="plain" @click="editorOpen = false">取消</Button>
        <Button v-if="editingGroup" variant="filled" :loading="saving" @click="saveUpstreamGroup">保存代理商信息</Button>
        <Button v-else-if="discoveredModels.length" variant="filled" :loading="saving" @click="addSelectedModels">添加已选 {{ selectedCount }} 个</Button>
        <Button v-else variant="filled" :loading="saving" @click="saveManualModel">{{ isNew ? '添加模型' : '保存' }}</Button>
      </template>
    </Modal>

    <Modal
      :open="reasoningEditorOpen"
      title="调整模型推理强度"
      @close="reasoningEditorOpen = false"
    >
      <div class="col" style="gap: 13px">
        <div class="reasoning-model-summary">
          <div class="t-headline">{{ visibleModelName(reasoningEditorModel) }}</div>
          <div class="mono model-id">{{ reasoningEditorModel?.externalModelName || reasoningEditorModel?.name }}</div>
        </div>
        <label class="select-field reasoning-select-field">
          <span class="t-footnote">注入时使用的推理强度</span>
          <select v-model="reasoningEditorEffort">
            <option
              v-for="option in modelReasoningProfile(reasoningEditorModel, reasoningEditorModel?.provider, reasoningEditorModel?.apiStyle).options"
              :key="option.value"
              :value="option.value"
            >{{ option.label }}</option>
          </select>
          <span class="t-caption">{{ modelReasoningProfile(reasoningEditorModel, reasoningEditorModel?.provider, reasoningEditorModel?.apiStyle).note }}</span>
        </label>
        <div v-if="reasoningEditorError" class="err-box">{{ reasoningEditorError }}</div>
      </div>
      <template #footer>
        <Button variant="plain" @click="reasoningEditorOpen = false">取消</Button>
        <Button variant="filled" :loading="reasoningEditorSaving" @click="saveModelReasoning">保存推理强度</Button>
      </template>
    </Modal>

    <AccountTestModal
      :open="modelTestOpen"
      :title="modelTestTitle"
      :account="testModalAccount"
      :models="testModalModels"
      :status="modelTestStatus"
      :output-lines="modelTestOutputLines"
      :streaming-content="modelTestContent"
      :error-message="modelTestError"
      :generated-images="modelTestImages"
      :default-model-id="testModalDefaultModelID"
      @test="runDetailedModelTest"
      @cancel="cancelDetailedModelTest"
      @close="closeDetailedModelTest"
      @preview-image="previewModelTestImage"
    />

  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 14px; padding: 18px 20px 28px; height: 100%; overflow-y: auto; }
.page > * { flex-shrink: 0; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(272px, 1fr)); gap: 12px; }
.model-card { background: var(--bg-card); border: 1px solid var(--separator); border-radius: var(--r-lg); padding: 14px; box-shadow: var(--shadow-card); backdrop-filter: blur(16px); transition: transform .18s var(--spring), border-color .18s var(--ease); }
.model-card:hover { transform: translateY(-1px); border-color: var(--separator-strong); }
.model-group-card { min-width: 0; }
.group-heading { align-items: flex-start; gap: 10px; }
.group-toolbar { display: flex; flex-direction: column; gap: 9px; margin-top: 12px; padding: 9px; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-inset); }
.group-summary { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 6px 10px; }
.group-count { color: var(--text-secondary); font-size: 11px; }
.group-summary-note { color: var(--text-tertiary); font-size: 10px; }
.group-actions { display: flex; flex-direction: column; align-items: stretch; gap: 8px; }
.group-test-button { align-self: flex-start; }
.group-utility-actions { width: 100%; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 7px; }
.group-enable-actions { display: contents; }
.group-utility-actions :deep(button) { width: 100%; justify-content: center; }
.group-expand-toggle { min-height: 28px; padding: 0 9px; border: 1px solid var(--separator-strong); border-radius: var(--r-sm); color: var(--text-secondary); background: var(--bg-card); font-size: 11px; cursor: pointer; }
.group-expand-toggle:hover { color: var(--text-primary); border-color: var(--accent-border); background: var(--accent-soft); }
.group-expand-toggle[aria-expanded="true"] { color: var(--accent-strong); border-color: var(--accent-border); }
.group-model-list { display: flex; flex-direction: column; margin-top: 10px; border: 1px solid var(--separator); border-radius: var(--r-sm); overflow: hidden; background: var(--bg-inset); }
.group-model-row { min-width: 0; padding: 10px; border-bottom: 1px solid var(--separator); transition: background .16s var(--ease), opacity .16s var(--ease); }
.group-model-row:last-child { border-bottom: 0; }
.group-model-row:hover { background: var(--bg-fill); }
.group-model-row.disabled { opacity: .58; }
.model-row-main { display: flex; align-items: center; gap: 8px; min-width: 0; }
.model-copy { min-width: 0; }
.model-enable-control { display: inline-flex; align-items: center; gap: 5px; flex: 0 0 auto; color: var(--green); font-size: 10px; cursor: pointer; }
.model-enable-control input { width: 15px; height: 15px; margin: 0; accent-color: var(--green); }
.model-enable-control input:disabled { cursor: wait; opacity: .55; }
.model-enable-text { min-width: 24px; }
.model-row-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 5px; flex: 0 0 auto; }
.model-cap-row { align-items: center; margin-top: 8px; }
.model-style { margin-left: auto; color: var(--text-tertiary); font: 10px var(--font-num); }
.model-action-status { padding: 9px 11px; border: 1px solid var(--accent-border); border-radius: var(--r-sm); color: var(--accent-strong); background: var(--accent-soft); font-size: 12px; line-height: 1.4; }
.model-action-status.error { border-color: rgba(255,69,58,.25); color: var(--red); background: rgba(255,69,58,.1); }
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
.select-all, .select-row { display: grid; grid-template-columns: 16px minmax(0, 1fr) minmax(90px, .75fr) minmax(128px, .85fr); align-items: center; gap: 8px; min-height: 38px; padding: 0 10px; border-bottom: 1px solid var(--separator); font-size: 12px; }
.select-all { grid-template-columns: 16px 1fr; color: var(--text-secondary); background: var(--bg-fill); position: sticky; top: 0; }
.select-row:last-child { border-bottom: 0; }
.select-row code { color: var(--text-tertiary); font-size: 10px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.discovery-reasoning { display: flex; align-items: center; justify-content: flex-end; gap: 5px; min-width: 0; color: var(--text-tertiary); font-size: 10px; }
.discovery-reasoning select, .reasoning-select-field select { min-width: 0; height: 28px; padding: 0 23px 0 8px; border: 1px solid var(--separator-strong); border-radius: var(--r-sm); color: var(--text-primary); background: var(--bg-card); font: 11px var(--font-num); }
.discovery-reasoning select:focus, .reasoning-select-field select:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); outline: none; }
.discovery-reasoning select:disabled { color: var(--text-tertiary); border-color: var(--separator); opacity: .78; cursor: default; }
.test-action-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 9px; min-height: 30px; }
.capability-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 7px; }
.capability-item { display: flex; gap: 7px; align-items: center; min-height: 30px; padding: 0 8px; font-size: 12px; color: var(--text-secondary); border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-card); }
.capability-item.disabled { color: var(--text-tertiary); opacity: .7; }
.capability-mark { color: var(--green); font-weight: 800; }
.capability-item.disabled .capability-mark { color: var(--text-tertiary); }
.select-field { display: flex; flex-direction: column; gap: 6px; }
.reasoning-select-field select { width: min(100%, 260px); height: 34px; font-size: 12px; }
.reasoning-model-summary { padding: 11px 12px; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-inset); }
.notice-box, .err-box { padding: 10px 11px; border-radius: var(--r-sm); font-size: 12px; line-height: 1.45; }
.notice-box { color: var(--accent-strong); background: var(--accent-soft); border: 1px solid var(--accent-border); }
.err-box { color: var(--red); background: rgba(255,69,58,.1); border: 1px solid rgba(255,69,58,.25); }
@media (max-width: 620px) { .two-col { grid-template-columns: 1fr; } .capability-grid { grid-template-columns: 1fr; } .select-row { grid-template-columns: 16px minmax(0, 1fr) minmax(112px, auto); } .select-row code { display: none; } .model-row-main { align-items: flex-start; flex-wrap: wrap; } .model-copy { flex: 1 1 calc(100% - 78px); } .model-row-actions { width: 100%; justify-content: flex-end; padding-left: 24px; } .model-style { margin-left: 0; } .test-action-row { align-items: stretch; flex-direction: column; } }
</style>
