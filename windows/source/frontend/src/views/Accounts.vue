<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Field from "@/components/ui/Field.vue";
import Modal from "@/components/ui/Modal.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import AccountTestModal from "@/components/accounts/AccountTestModal.vue";
import AccountQuotaWindows from "@/components/accounts/AccountQuotaWindows.vue";
import {
  normalizeReasoningEffort,
  resolveReasoningProfile,
} from "@/state/reasoningCapabilities";
import {
  addDiscoveredModels,
	cancelUpstreamAccountTest,
  completeOAuthAuthorization,
  defaultUpstreamAccount,
  deleteUpstreamAccount,
  discoverAccountModels,
  importOAuthRefreshToken,
  importUpstreamAccounts,
  getOAuthAuthorizationStatus,
  getOAuthLoginProfiles,
  loadAccounts,
  refreshUpstreamAccountQuota,
  refreshUpstreamOAuthAccount,
  saveUpstreamAccount,
  saveModel,
  setUpstreamAccountEnabled,
  startOAuthAuthorization,
  startOAuthProviderAuthorization,
  state,
  syncUpstreamAccountModels,
  testUpstreamAccountDetailed,
} from "@/state/appState";
import {
  canManuallyCompleteOAuthSession,
  isAutomaticOAuthPendingSession,
  isTerminalOAuthAuthorizationState,
  redactOAuthAuthorizationStatus,
} from "@/state/oauthAuthorizationStatus";
import {
  canStartOAuthLogin,
  chooseOAuthProfileID,
  usesSimplifiedOAuthLogin,
} from "@/state/oauthLoginUX";

const DEFAULT_XIASS_URL = "https://api.xiass.com";
const editorOpen = ref(false);
const importOpen = ref(false);
const confirmDelete = ref(null);
const saving = ref(false);
const discovering = ref(false);
const importing = ref(false);
const oauthBusy = ref(false);
const tokenRefreshBusy = ref("");
const quotaRefreshBusy = ref("");
const syncingAccountID = ref("");
const accountSyncMessages = ref({});
const editorError = ref("");
const editorNotice = ref("");
const importError = ref("");
const importNotice = ref("");
const importText = ref("");
const discoveredModels = ref([]);
const selectedModelIds = ref([]);
const discoveredReasoningEfforts = ref({});
const discoveredReasoningTouched = ref({});
const accountTestOpen = ref(false);
const accountTestAccount = ref(null);
const accountTestModels = ref([]);
const accountTestLoadingModels = ref(false);
const accountTestStatus = ref("idle");
const accountTestOutputLines = ref([]);
const accountTestContent = ref("");
const accountTestError = ref("");
const accountTestImages = ref([]);
const accountTestDefaultModelID = ref("");
const accountTestRequestID = ref("");
let accountTestGeneration = 0;
let accountTestRequestSerial = 0;
const oauthSession = ref(null);
const oauthCallback = ref("");
const oauthProfiles = ref([]);
const oauthProfilesLoading = ref(false);
const oauthProfilesLoaded = ref(false);
const oauthProfilesError = ref("");
const selectedOAuthProfileID = ref("");
const oauthAdvancedOpen = ref(false);
const oauthCredentialSwitchOpen = ref(false);
let removeOAuthCompletionListener = null;
const OAUTH_STATUS_POLL_INTERVAL_MS = 2000;
let oauthStatusPollTimer = null;
let oauthStatusPollSessionID = "";
let oauthStatusPollInFlight = false;
let completingOAuthSessionID = "";
let oauthStartGeneration = 0;

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
  { label: "OAuth 授权登录", value: "oauth" },
  { label: "Bearer / Access Token", value: "bearer_token" },
  { label: "x-api-key", value: "x_api_key" },
  { label: "Setup Token", value: "setup_token" },
  { label: "auth.json / OAuth JSON", value: "auth_json" },
  { label: "Refresh Token / Mobile RT", value: "refresh_token" },
  { label: "Codex PAT", value: "codex_pat" },
  { label: "自定义认证头", value: "custom_header" },
];
const JSON_IMPORT_TYPE_KEYS = new Set([
  "auth_json",
  "oauth_json",
  "credential_json",
  "credentials_json",
  "json_import",
  "import_json",
  "account_json",
]);
const REFRESH_TOKEN_TYPE_KEYS = new Set([
  "refresh_token",
  "mobile_rt",
  "mobile_refresh_token",
]);

function credentialTypeKey(type) {
  return String(type || "").trim().toLowerCase().replace(/[.\s-]+/g, "_");
}

function isJSONImportType(type) {
  return JSON_IMPORT_TYPE_KEYS.has(credentialTypeKey(type));
}

function isRefreshTokenType(type) {
  return REFRESH_TOKEN_TYPE_KEYS.has(credentialTypeKey(type));
}

function editorCredentialType(type) {
  if (isJSONImportType(type)) return "auth_json";
  if (isRefreshTokenType(type)) return "refresh_token";
  return type || "api_key";
}

function readText(value) {
  return typeof value === "string" ? value.trim() : "";
}

function knownProvider(value) {
  const provider = readText(value).toLowerCase();
  return providerOptions.some((option) => option.value === provider) ? provider : "";
}

// OAuth profiles are deliberately supplied by GetOAuthLoginProfiles(). Do not
// add provider-owned client IDs, secrets, or auth endpoints to this renderer.
// This sanitised shape is the only profile data the UI retains or displays.
function normalizeOAuthProfile(profile) {
  const id = readText(profile?.id ?? profile?.profileId ?? profile?.profileID ?? profile?.profile_id);
  if (!id) return null;
  const automaticCallback = typeof profile?.automaticCallback === "boolean"
    ? profile.automaticCallback
    : typeof profile?.autoLoopback === "boolean"
      ? profile.autoLoopback
      : undefined;
  return {
    id,
    label: readText(profile?.label ?? profile?.displayName ?? profile?.display_name ?? profile?.name ?? profile?.provider) || "OAuth 登录",
    description: readText(profile?.description ?? profile?.hint),
    provider: knownProvider(profile?.provider),
    available: readText(profile?.available),
    message: readText(profile?.message),
    requiresClientId: profile?.requiresClientId === true || profile?.requiresClientID === true,
    manualCompletionRequired: profile?.manualCompletionRequired === true,
    automaticCallback,
  };
}

function normalizeOAuthProfiles(result) {
  const entries = Array.isArray(result)
    ? result
    : Array.isArray(result?.profiles)
      ? result.profiles
      : Array.isArray(result?.items)
        ? result.items
        : [];
  return entries.map(normalizeOAuthProfile).filter(Boolean);
}

// Smart endpoint mode owns the protocol suffix. Old saved configurations may
// still contain a request endpoint, so only strip a closed set of endpoint
// tails here. Manual mode deliberately bypasses this helper unchanged.
const SMART_API_ENDPOINT_SUFFIXES = [
  "/v1/chat/completions",
  "/v1/chat/messages",
  "/v1/responses",
  "/v1/messages",
  "/v1/models",
  "/chat/completions",
  "/chat/messages",
  "/responses",
  "/messages",
  "/models",
];

function smartBaseAPIURL(value) {
  let raw = readText(value);
  if (!raw) return "";
  // Smart mode owns the request path. A pasted endpoint often also carries a
  // copied query/hash; neither belongs to a reusable base URL. Manual mode
  // bypasses this helper entirely and continues to preserve those bytes.
  try {
    const parsed = new URL(raw);
    if (parsed.protocol === "https:" || parsed.protocol === "http:") {
      parsed.search = "";
      parsed.hash = "";
      raw = parsed.toString();
    }
  } catch {
    // Leave an incomplete address untouched while the user is still typing;
    // the backend supplies the final validation on save.
  }
  const withoutTrailingSlashes = raw.replace(/\/+$/, "");
  const lower = withoutTrailingSlashes.toLowerCase();
  for (const suffix of SMART_API_ENDPOINT_SUFFIXES) {
    if (!lower.endsWith(suffix)) continue;
    const base = withoutTrailingSlashes.slice(0, -suffix.length).replace(/\/+$/, "");
    return base || raw;
  }
  return raw;
}

function accountEndpointMode(account) {
  return account?.endpointMode || (account?.messagePathMode === "manual" ? "manual" : "auto");
}

function accountDisplayAPIURL(account) {
  const value = readText(account?.apiUrl);
  return accountEndpointMode(account) === "auto" ? smartBaseAPIURL(value) : value;
}

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
    quotaUrl: "",
    apiKey: "",
    refreshToken: "",
    oauth: {
      authorizationUrl: "",
      tokenUrl: "",
      clientId: "",
      redirectUri: "http://localhost:1455/auth/callback",
      scopes: "openid profile email offline_access",
    },
    enabled: true,
    priority: 50,
    maxConcurrency: 2,
  };
}

const form = ref(emptyForm());
const isExisting = computed(() => Boolean(form.value.id));
const isJSONImportTypeSelected = computed(() => isJSONImportType(form.value.type));
const isRefreshTokenTypeSelected = computed(() => isRefreshTokenType(form.value.type));
const isOAuthAuthorizationLogin = computed(() => form.value.type === "oauth");
const selectedOAuthProfile = computed(() => oauthProfiles.value.find((profile) => profile.id === selectedOAuthProfileID.value) || null);
const usesSimpleOAuthLogin = computed(() => usesSimplifiedOAuthLogin(form.value.type, oauthAdvancedOpen.value));
const showOAuthCredentialTypeChooser = computed(() => !usesSimpleOAuthLogin.value || oauthCredentialSwitchOpen.value);
const showOAuthAccountFields = computed(() => !isOAuthAuthorizationLogin.value || oauthAdvancedOpen.value);
const showOAuthCustomFields = computed(() => isRefreshTokenTypeSelected.value || (isOAuthAuthorizationLogin.value && oauthAdvancedOpen.value));
const showQuotaURLField = computed(() => showOAuthAccountFields.value && !isOAuthAuthorizationLogin.value && !isRefreshTokenTypeSelected.value);
const showCredentialSecretField = computed(() => showOAuthAccountFields.value && !isOAuthAuthorizationLogin.value && !isRefreshTokenTypeSelected.value && !isJSONImportTypeSelected.value);
const showCustomHeaderName = computed(() => form.value.type === "custom_header");
const showAdditionalHeaders = computed(() => form.value.type === "custom_header" || oauthAdvancedOpen.value);
const showSchedulingControls = computed(() => showOAuthAccountFields.value && !isJSONImportTypeSelected.value);
const canStartSelectedOAuth = computed(() => {
  const profileNeedsCustomClient = selectedOAuthProfile.value?.requiresClientId === true && !oauthAdvancedOpen.value;
  return !profileNeedsCustomClient && canStartOAuthLogin(selectedOAuthProfile.value, oauthAdvancedOpen.value);
});
const oauthLoginTitle = computed(() => oauthAdvancedOpen.value
  ? "高级自定义 OAuth"
  : (selectedOAuthProfile.value?.label || "OpenAI / Codex"));
const oauthLoginDescription = computed(() => {
  if (selectedOAuthProfile.value?.description) return selectedOAuthProfile.value.description;
  if (oauthProfilesLoading.value) return "正在读取可用的一键登录方式…";
  if (oauthAdvancedOpen.value) return "使用你自己注册的公开 OAuth 客户端；不会保存客户端密钥。";
  return "未读取到默认登录预设。可刷新预设，或主动切换到高级自定义 OAuth。";
});
const oauthLoginButtonLabel = computed(() => {
  if (oauthAdvancedOpen.value) return "打开授权页";
  if (selectedOAuthProfile.value?.requiresClientId) return "需要高级配置";
  return selectedOAuthProfile.value ? `${selectedOAuthProfile.value.label} 一键登录` : "等待可用预设";
});
const oauthManualCompletionRequired = computed(() => {
  const session = oauthSession.value;
  return Boolean(session && (session.manualCompletionRequired === true || session.automaticCallback === false));
});
const showOAuthManualFallback = computed(() => canManuallyCompleteOAuthSession(oauthSession.value));
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
const credentialLabel = computed(() => {
  const prefix = isExisting.value ? "新的" : "";
  const retained = isExisting.value ? "（留空保留原凭据）" : "";
  switch (form.value.type) {
    case "bearer_token": return `${prefix}Bearer / Access Token${retained}`;
    case "x_api_key": return `${prefix}x-api-key${retained}`;
    case "setup_token": return `${prefix}Setup Token${retained}`;
    case "codex_pat": return `${prefix}Codex PAT${retained}`;
    case "custom_header": return `${prefix}认证头值${retained}`;
    default: return `${prefix}API Key${retained}`;
  }
});
const credentialHint = computed(() => {
  switch (form.value.type) {
    case "bearer_token": return "将以 Authorization: Bearer 发送。";
    case "x_api_key": return "将以 x-api-key 请求头发送。";
    case "setup_token": return "将以 Bearer 令牌方式发送。";
    case "codex_pat": return "将以 Bearer 令牌方式发送。";
    case "custom_header": return "仅发送到下方配置的自定义认证请求头。";
    default: return form.value.provider === "anthropic" ? "Claude 默认以 x-api-key 发送。" : "OpenAI 兼容接口默认以 Bearer 发送。";
  }
});
const credentialPlaceholder = computed(() => {
  switch (form.value.type) {
    case "setup_token": return "setup token…";
    case "codex_pat": return "pat…";
    case "bearer_token": return "access token…";
    default: return form.value.type === "custom_header" ? "认证头值…" : "sk-…";
  }
});
const fixedAuthModeLabel = computed(() => {
  if (form.value.type === "x_api_key" || (form.value.type === "api_key" && form.value.provider === "anthropic")) return "x-api-key";
  if (form.value.type === "custom_header") return form.value.authHeader?.trim() || "自定义请求头";
  return "Bearer";
});

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
  const defaults = emptyForm();
  const endpointMode = accountEndpointMode(account);
  return {
    ...defaults,
    ...account,
    type: editorCredentialType(account.type),
    apiUrl: endpointMode === "auto" ? smartBaseAPIURL(account.apiUrl || defaults.apiUrl) : account.apiUrl,
    endpointMode,
    messagePathMode: account.messagePathMode === "manual" ? "auto" : (account.messagePathMode || "auto"),
    headersText: JSON.stringify(account.headers || {}, null, 2),
    oauth: { ...defaults.oauth, ...(account.oauth || {}) },
    apiKey: "",
    refreshToken: "",
  };
}

function discoveredModelID(model) {
  return readText(model?.id ?? model?.externalModelName ?? model?.name).replace(/^models\//, "");
}

function discoveredReasoningProfile(model) {
  return resolveReasoningProfile({
    ...(model && typeof model === "object" ? model : {}),
    provider: form.value.provider,
    model: discoveredModelID(model),
    apiStyle: form.value.apiStyle,
  });
}

function resetDiscoveredModelSelection(models = []) {
  const list = Array.isArray(models) ? models : [];
  const efforts = {};
  for (const model of list) {
    const id = discoveredModelID(model);
    if (!id) continue;
    efforts[id] = normalizeReasoningEffort(model?.reasoningEffort, discoveredReasoningProfile(model));
  }
  discoveredModels.value = list;
  selectedModelIds.value = list.map(discoveredModelID).filter(Boolean);
  discoveredReasoningEfforts.value = efforts;
  discoveredReasoningTouched.value = {};
}

function discoveredReasoningEffort(model) {
  const id = discoveredModelID(model);
  return normalizeReasoningEffort(discoveredReasoningEfforts.value[id], discoveredReasoningProfile(model));
}

function setDiscoveredReasoningEffort(model, value) {
  const id = discoveredModelID(model);
  if (!id) return;
  discoveredReasoningEfforts.value = {
    ...discoveredReasoningEfforts.value,
    [id]: normalizeReasoningEffort(value, discoveredReasoningProfile(model)),
  };
  discoveredReasoningTouched.value = { ...discoveredReasoningTouched.value, [id]: true };
}

function clearOAuthStatusPolling(sessionID = "") {
  // A late completion from an older browser tab must not stop a newer login.
  if (sessionID && oauthStatusPollSessionID && oauthStatusPollSessionID !== sessionID) return;
  if (oauthStatusPollTimer !== null) window.clearTimeout(oauthStatusPollTimer);
  oauthStatusPollTimer = null;
  oauthStatusPollSessionID = "";
  oauthStatusPollInFlight = false;
}

function clearOAuthSession() {
  // Invalidate an in-flight authorization-start call before clearing local
  // state. A browser result that arrives after close/profile switching must
  // never re-open polling or restore a stale session in the renderer.
  oauthStartGeneration += 1;
  clearOAuthStatusPolling();
  oauthSession.value = null;
  oauthCallback.value = "";
  oauthBusy.value = false;
}

function closeEditor() {
  clearOAuthSession();
  editorOpen.value = false;
}

function shouldPollOAuthStatus(session = oauthSession.value) {
  return isAutomaticOAuthPendingSession(session, editorOpen.value);
}

function scheduleOAuthStatusPoll(sessionID, delay = OAUTH_STATUS_POLL_INTERVAL_MS) {
  if (!sessionID || oauthStatusPollSessionID !== sessionID || oauthStatusPollTimer !== null || oauthStatusPollInFlight) return;
  oauthStatusPollTimer = window.setTimeout(() => {
    oauthStatusPollTimer = null;
    void pollOAuthAuthorizationStatus(sessionID);
  }, delay);
}

async function pollOAuthAuthorizationStatus(sessionID) {
  if (oauthStatusPollSessionID !== sessionID || oauthStatusPollInFlight) return;
  if (!shouldPollOAuthStatus() || oauthSession.value?.sessionId !== sessionID) {
    clearOAuthStatusPolling(sessionID);
    return;
  }

  oauthStatusPollInFlight = true;
  let shouldContinue = true;
  try {
    const status = redactOAuthAuthorizationStatus(await getOAuthAuthorizationStatus(sessionID));
    if (!status || status.sessionId !== sessionID || oauthStatusPollSessionID !== sessionID) return;

    if (isTerminalOAuthAuthorizationState(status.state) && (status.state === "completed" || status.state === "failed")) {
      shouldContinue = false;
      await handleOAuthCompletionEvent(status.completion);
      return;
    }

    if (isTerminalOAuthAuthorizationState(status.state)) {
      shouldContinue = false;
      clearOAuthStatusPolling(sessionID);
      if (oauthSession.value?.sessionId === sessionID) {
        oauthBusy.value = false;
        oauthSession.value = null;
        oauthCallback.value = "";
        editorError.value = status.message || "OAuth 授权会话已失效，请重新开始授权。";
      }
    }
    // A transient native bridge failure intentionally stays pending. The
    // existing completion event can still arrive, and the next low-frequency
    // status query provides a reliable fallback without touching credentials.
  } catch {
    // Keep waiting for the native completion event and retry on the interval.
  } finally {
    if (oauthStatusPollSessionID === sessionID) {
      oauthStatusPollInFlight = false;
      if (shouldContinue && shouldPollOAuthStatus() && oauthSession.value?.sessionId === sessionID) {
        scheduleOAuthStatusPoll(sessionID);
      }
    }
  }
}

function startOAuthStatusPolling() {
  const session = oauthSession.value;
  clearOAuthStatusPolling();
  if (!shouldPollOAuthStatus(session)) return;
  oauthStatusPollSessionID = session.sessionId;
  void pollOAuthAuthorizationStatus(session.sessionId);
}

async function openNew() {
  clearOAuthSession();
  let defaults = {};
  try {
    defaults = await defaultUpstreamAccount();
  } catch {
    // The static defaults keep the editor useful while Wails is starting.
  }
  form.value = {
    ...emptyForm(),
    ...defaults,
    headersText: JSON.stringify(defaults?.headers || {}, null, 2),
    oauth: { ...emptyForm().oauth, ...(defaults?.oauth || {}) },
    apiKey: "",
  };
	if (form.value.endpointMode === "auto") form.value.apiUrl = smartBaseAPIURL(form.value.apiUrl);
  editorError.value = "";
  editorNotice.value = "地址默认只需填写域名。保存后可获取模型并默认全选导入；完整接口地址可随时切换为手动模式。";
  resetDiscoveredModelSelection();
	selectedOAuthProfileID.value = "";
	oauthAdvancedOpen.value = false;
	oauthCredentialSwitchOpen.value = false;
	void loadOAuthLoginProfiles();
  editorOpen.value = true;
}

function openEdit(account) {
  clearOAuthSession();
  form.value = accountToForm(account);
  editorError.value = "";
  editorNotice.value = "为保护已保存的凭据，令牌栏不会回显；留空保存将保留原有凭据。";
  resetDiscoveredModelSelection();
	selectedOAuthProfileID.value = "";
	oauthAdvancedOpen.value = false;
	oauthCredentialSwitchOpen.value = false;
	void loadOAuthLoginProfiles();
  editorOpen.value = true;
}

function onProviderChange(provider) {
  if (provider !== form.value.provider) clearOAuthSession();
  if (selectedOAuthProfile.value?.provider && selectedOAuthProfile.value.provider !== provider) {
    selectedOAuthProfileID.value = "";
    oauthAdvancedOpen.value = true;
  }
  form.value.provider = provider;
  if (form.value.endpointMode === "auto") form.value.apiUrl = smartBaseAPIURL(form.value.apiUrl);
  if (provider === "anthropic") {
    form.value.authMode = "x_api_key";
    form.value.apiStyle = "messages";
  } else if (form.value.authMode === "x_api_key") {
    form.value.authMode = "bearer";
    if (form.value.apiStyle === "messages") form.value.apiStyle = "auto";
  }
}

function onTypeChange(type) {
  if (isJSONImportType(type)) {
    clearOAuthSession();
    selectedOAuthProfileID.value = "";
    form.value.type = "auth_json";
    form.value.apiKey = "";
    form.value.refreshToken = "";
    editorError.value = "";
    editorNotice.value = "auth.json / OAuth JSON 需要通过 JSON 导入解析，不能作为 API Key 保存。";
    closeEditor();
    openJSONImport("请粘贴完整的账户 JSON；它将按凭据字段安全导入，而不会被当作普通 API Key。");
    return;
  }

  const nextType = isRefreshTokenType(type) ? "refresh_token" : type;
  clearOAuthSession();
	form.value.type = nextType;
	if (form.value.type === "oauth") {
		selectedOAuthProfileID.value = "";
		oauthAdvancedOpen.value = false;
		oauthCredentialSwitchOpen.value = false;
    editorError.value = "";
    editorNotice.value = "正在准备 OpenAI / Codex 一键登录…";
    void loadOAuthLoginProfiles({ autoSelectDefault: true });
    return;
	}
	selectedOAuthProfileID.value = "";
	oauthCredentialSwitchOpen.value = false;
  if (isRefreshTokenType(form.value.type)) {
    form.value.apiKey = "";
  }
  if (form.value.type === "x_api_key") form.value.authMode = "x_api_key";
  else if (form.value.type === "custom_header") form.value.authMode = "custom_header";
  else if (form.value.type === "api_key") form.value.authMode = form.value.provider === "anthropic" ? "x_api_key" : "bearer";
  else form.value.authMode = "bearer";
}

function onEndpointModeChange(mode) {
  form.value.endpointMode = mode;
  if (mode === "auto") form.value.apiUrl = smartBaseAPIURL(form.value.apiUrl);
  editorNotice.value = mode === "manual"
    ? "手动完整路径已启用：WF 会严格使用上方地址。"
    : "智能补全已启用：WF 会根据协议补全请求路径。";
}

function onAPIURLChange(value) {
  const raw = typeof value === "string" ? value : "";
  form.value.apiUrl = form.value.endpointMode === "auto" ? smartBaseAPIURL(raw) : raw;
}

async function loadOAuthLoginProfiles({ force = false, autoSelectDefault = false } = {}) {
  if (oauthProfilesLoading.value) return;
  if (oauthProfilesLoaded.value && !force) {
    if (autoSelectDefault) ensureDefaultOAuthProfile({ silent: true });
    return;
  }
  oauthProfilesLoading.value = true;
  oauthProfilesError.value = "";
  try {
    const result = await getOAuthLoginProfiles();
    oauthProfiles.value = normalizeOAuthProfiles(result);
    oauthProfilesLoaded.value = true;
    if (autoSelectDefault) ensureDefaultOAuthProfile({ silent: true });
    if (!oauthProfiles.value.length) {
      oauthProfilesError.value = "当前没有可用的安全 OAuth 登录预设；仍可使用高级自定义 OAuth。";
    }
  } catch {
    // Older builds may not expose profiles yet. Custom PKCE OAuth remains a
    // complete fallback, so intentionally do not surface an internal error.
    oauthProfiles.value = [];
    oauthProfilesLoaded.value = true;
    oauthProfilesError.value = "安全 OAuth 登录预设暂不可用；仍可使用高级自定义 OAuth。";
  } finally {
    oauthProfilesLoading.value = false;
  }
}

function refreshOAuthLoginProfiles() {
  return loadOAuthLoginProfiles({
    force: true,
    autoSelectDefault: isOAuthAuthorizationLogin.value && !oauthAdvancedOpen.value,
  });
}

function ensureDefaultOAuthProfile({ silent = false } = {}) {
  if (!isOAuthAuthorizationLogin.value || oauthAdvancedOpen.value) return false;
  const profileID = chooseOAuthProfileID(oauthProfiles.value, {
    selectedProfileID: selectedOAuthProfileID.value,
    advancedCustomOpen: oauthAdvancedOpen.value,
  });
  if (!profileID) {
    selectedOAuthProfileID.value = "";
    return false;
  }
  if (profileID === selectedOAuthProfileID.value) return true;
  const profile = oauthProfiles.value.find((item) => item.id === profileID);
  if (!profile) return false;
  selectOAuthProfile(profile, { silent });
  return true;
}

function selectOAuthProfile(profile, { silent = false } = {}) {
  if (!profile?.id) return;
  clearOAuthSession();
  if (profile.available === "custom_only") {
    selectedOAuthProfileID.value = "";
    form.value.type = "oauth";
    oauthAdvancedOpen.value = true;
    editorError.value = "";
    editorNotice.value = profile.message || "该登录方式需要使用高级自定义 OAuth。";
    return;
  }
  if (profile.provider) {
    form.value.provider = profile.provider;
    form.value.authMode = "bearer";
    form.value.apiStyle = profile.provider === "anthropic" ? "messages" : "auto";
    form.value.messagePathMode = profile.provider === "anthropic" ? "standard" : "auto";
  }
  form.value.type = "oauth";
	selectedOAuthProfileID.value = profile.id;
	oauthAdvancedOpen.value = false;
	oauthCredentialSwitchOpen.value = false;
  editorError.value = "";
  if (!silent) {
    editorNotice.value = profile.requiresClientId
      ? `已选择 ${profile.label}。如需自有客户端，请主动点击“高级自定义 OAuth”填写公开 Client ID。`
      : (profile.message || `已选择 ${profile.label} 安全登录。将由本机后端提供已审核的 OAuth 配置。`);
  }
}

function useCustomOAuth() {
  clearOAuthSession();
	selectedOAuthProfileID.value = "";
	form.value.type = "oauth";
	oauthAdvancedOpen.value = true;
	oauthCredentialSwitchOpen.value = false;
	editorError.value = "";
	editorNotice.value = "高级自定义 OAuth 已启用：请填写你自己注册的公开客户端信息。";
}

function returnToSimpleOAuthLogin() {
	clearOAuthSession();
	oauthAdvancedOpen.value = false;
	oauthCredentialSwitchOpen.value = false;
	editorError.value = "";
	if (ensureDefaultOAuthProfile({ silent: true })) {
		editorNotice.value = "已返回一键 OAuth 登录，可切换其他预设或直接在浏览器授权。";
		return;
	}
	editorNotice.value = "正在读取可用的一键登录方式…";
	void loadOAuthLoginProfiles({ force: true, autoSelectDefault: true });
}

function missingOAuthConfiguration() {
  const oauth = form.value.oauth || {};
  const labels = [
    [oauth.authorizationUrl, "授权地址"],
    [oauth.tokenUrl, "令牌地址"],
    [oauth.clientId, "公开客户端 ID"],
    [oauth.redirectUri, "已注册回调地址"],
  ];
  return labels.filter(([value]) => !readText(value)).map(([, label]) => label);
}

function requireCustomOAuthConfiguration() {
  const missing = missingOAuthConfiguration();
  if (!missing.length) return;
  oauthAdvancedOpen.value = true;
  throw new Error(`请先填写高级自定义 OAuth：${missing.join("、")}`);
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

function fixedAuthMode(type, provider) {
  if (type === "x_api_key" || (type === "api_key" && provider === "anthropic")) return "x_api_key";
  if (type === "custom_header") return "custom_header";
  return "bearer";
}

function accountPayload() {
  const rawAPIURL = form.value.apiUrl?.trim();
  const payload = {
    ...form.value,
    apiUrl: form.value.endpointMode === "auto" ? smartBaseAPIURL(rawAPIURL) : rawAPIURL,
    apiKey: form.value.apiKey?.trim(),
    authMode: fixedAuthMode(form.value.type, form.value.provider),
    authHeader: form.value.authHeader?.trim(),
    quotaUrl: form.value.quotaUrl?.trim(),
		oauth: {
			upstream: form.value.oauth?.upstream?.trim(),
			authorizationUrl: form.value.oauth?.authorizationUrl?.trim(),
      tokenUrl: form.value.oauth?.tokenUrl?.trim(),
      clientId: form.value.oauth?.clientId?.trim(),
      redirectUri: form.value.oauth?.redirectUri?.trim(),
      scopes: form.value.oauth?.scopes?.trim(),
    },
    headers: parseHeaders(),
    priority: Number(form.value.priority),
    maxConcurrency: Number(form.value.maxConcurrency),
  };
  delete payload.headersText;
  delete payload.refreshToken;
  return payload;
}

async function saveAccount() {
  editorError.value = "";
  let account;
  try {
    account = accountPayload();
    if (!account.apiUrl) throw new Error("请填写 API 地址");
    if (isJSONImportType(account.type)) throw new Error("auth.json / OAuth JSON 请通过“导入 JSON”导入，不能作为 API Key 直接保存。");
    if (isRefreshTokenType(account.type)) throw new Error("Refresh Token / Mobile RT 必须先兑换为 OAuth 访问令牌，不能作为 API Key 直接保存。");
    if (!account.apiKey && !account.id && account.type !== "oauth") {
      throw new Error("请填写 API Key、访问令牌，或使用 OAuth 授权/JSON 导入账户");
    }
  } catch (error) {
    editorError.value = error.message;
    return;
  }
  saving.value = true;
  try {
    const result = await saveUpstreamAccount(account);
    if (result?.ok) {
      closeEditor();
    } else {
      editorError.value = result?.message || "账户保存失败";
    }
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    saving.value = false;
  }
}

async function importRefreshToken() {
  editorError.value = "";
  editorNotice.value = "";
  let account;
  const refreshToken = String(form.value.refreshToken || "").trim();
  try {
    account = accountPayload();
    if (!account.apiUrl) throw new Error("请填写 API 地址");
    requireCustomOAuthConfiguration();
    if (!refreshToken) throw new Error("请填写 Refresh Token 或 Mobile RT。");
  } catch (error) {
    editorError.value = error.message;
    return;
  }
  oauthBusy.value = true;
  try {
    const result = await importOAuthRefreshToken(account, refreshToken);
    if (!result?.ok) {
      editorError.value = result?.message || "刷新令牌兑换失败";
      return;
    }
    form.value.refreshToken = "";
    editorNotice.value = result.message || "刷新令牌已兑换为 OAuth 账户。";
    closeEditor();
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    oauthBusy.value = false;
  }
}

// Keep only the normal authorization result fields in renderer state. In
// particular, do not spread a native result because it might later gain token
// fields that must never be held by or logged from the WebView.
function oauthSessionFromResult(result, { manualFallback = false, profile = null } = {}) {
  const resultAutomaticCallback = result?.automaticCallback === true
    ? true
    : result?.automaticCallback === false
      ? false
      : undefined;
  const automaticCallback = resultAutomaticCallback ?? profile?.automaticCallback;
  return {
    sessionId: readText(result?.sessionId ?? result?.sessionID),
    authorizationUrl: readText(result?.authorizationUrl ?? result?.authorizationURL),
    expiresAt: readText(result?.expiresAt),
    manualCompletionRequired: manualFallback
      || result?.manualCompletionRequired === true
      || profile?.manualCompletionRequired === true
      || automaticCallback === false,
    automaticCallback,
  };
}

async function startOAuth() {
  editorError.value = "";
  editorNotice.value = "";
  let account;
  const profile = selectedOAuthProfile.value;
  try {
    account = accountPayload();
    if (!account.apiUrl) throw new Error("请填写 API 地址");
    if (!profile && !oauthAdvancedOpen.value) {
      throw new Error("暂未读取到可用的一键登录预设。请刷新预设，或主动点击“高级自定义 OAuth”。");
    }
    if (profile?.requiresClientId && !readText(account.oauth?.clientId)) {
      throw new Error("该预设需要自有公开 Client ID。请主动点击“高级自定义 OAuth”后填写；无需 Client Secret。");
    }
    if (!profile && oauthAdvancedOpen.value) requireCustomOAuthConfiguration();
  } catch (error) {
    editorError.value = error.message;
    return;
  }
  clearOAuthSession();
  const startGeneration = oauthStartGeneration;
  oauthBusy.value = true;
  try {
    const result = profile
      ? await startOAuthProviderAuthorization(profile.id, account)
      : await startOAuthAuthorization(account);
    if (startGeneration !== oauthStartGeneration || !editorOpen.value) return;
    if (!result?.ok) {
      editorError.value = result?.message || "无法生成 OAuth 授权链接";
      return;
    }
    // Generic custom OAuth is intentionally a copy-and-paste completion flow.
    // Provider profiles declare whether their callback completes automatically.
    const session = oauthSessionFromResult(result, { manualFallback: !profile, profile });
    if (!session.sessionId) {
      editorError.value = "OAuth 授权会话创建失败，请重新开始授权。";
      return;
    }
    oauthSession.value = session;
    oauthCallback.value = "";
    editorNotice.value = result.message || (profile ? "已打开浏览器，等待授权完成。" : "授权链接已生成。");
    startOAuthStatusPolling();
  } catch (error) {
    if (startGeneration === oauthStartGeneration && editorOpen.value) {
      editorError.value = String(error?.message || error);
    }
  } finally {
    if (startGeneration === oauthStartGeneration) oauthBusy.value = false;
  }
}

function redactedOAuthIdentity(identity) {
  if (!identity || typeof identity !== "object") return null;
  const safe = {
    email: readText(identity.email),
    subject: readText(identity.subject ?? identity.sub),
    plan: readText(identity.plan ?? identity.planType ?? identity.plan_type),
  };
  return Object.values(safe).some(Boolean) ? safe : null;
}

// wf:oauth-completed is a redacted native event contract. Deliberately copy
// only sessionId, ok, message, accountId and display-only identity fields;
// never retain arbitrary payload properties in the renderer.
function redactedOAuthCompletionEvent(payload) {
  if (!payload || typeof payload !== "object") return null;
  const sessionId = readText(payload.sessionId ?? payload.sessionID);
  if (!sessionId) return null;
  return {
    sessionId,
    ok: payload.ok === true,
    message: readText(payload.message),
    accountId: readText(payload.accountId ?? payload.accountID),
    identity: redactedOAuthIdentity(payload.identity),
  };
}

function completedIdentityLabel(identity) {
  return identity?.email || identity?.subject || identity?.plan || "";
}

async function handleOAuthCompletionEvent(payload) {
  const completion = redactedOAuthCompletionEvent(payload);
  if (!completion || completion.sessionId !== oauthSession.value?.sessionId) return;
  if (completingOAuthSessionID === completion.sessionId) return;
  completingOAuthSessionID = completion.sessionId;
  clearOAuthStatusPolling(completion.sessionId);
  oauthBusy.value = false;
  oauthSession.value = null;
  oauthCallback.value = "";
  try {
    if (!completion.ok) {
      editorError.value = completion.message || "浏览器授权未完成，请重试或改用高级自定义 OAuth。";
      return;
    }
    const identity = completedIdentityLabel(completion.identity);
    editorError.value = "";
    editorNotice.value = completion.message || (identity ? `已完成授权：${identity}` : "OAuth 授权已完成。");
    await loadAccounts();
    closeEditor();
  } finally {
    completingOAuthSessionID = "";
  }
}

function bindOAuthCompletionEvents() {
  if (removeOAuthCompletionListener) return;
  const runtime = window.runtime;
  if (typeof runtime?.EventsOn !== "function") return;
  const unsubscribe = runtime.EventsOn("wf:oauth-completed", (payload) => {
    void handleOAuthCompletionEvent(payload);
  });
  // Wails runtime builds differ: newer generated bindings return an unsubscribe
  // callback, while some bundled runtimes return void. In the latter case use
  // EventsOff during unmount so returning to this route cannot stack listeners
  // and process the same OAuth completion more than once.
  if (typeof unsubscribe === "function") {
    removeOAuthCompletionListener = unsubscribe;
  } else if (typeof runtime.EventsOff === "function") {
    removeOAuthCompletionListener = () => runtime.EventsOff("wf:oauth-completed");
  }
}

async function completeOAuth() {
  if (!oauthSession.value?.sessionId) return;
  if (!oauthCallback.value.trim()) {
    editorError.value = "请粘贴完整回调 URL 或授权码。";
    return;
  }
  editorError.value = "";
  const sessionID = oauthSession.value.sessionId;
  oauthBusy.value = true;
  try {
    const result = await completeOAuthAuthorization(sessionID, oauthCallback.value.trim());
    if (oauthSession.value?.sessionId !== sessionID) return;
    if (!result?.ok) {
      editorError.value = result?.message || "OAuth 授权兑换失败";
      return;
    }
    editorNotice.value = result.message || "OAuth 授权完成。";
    closeEditor();
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    oauthBusy.value = false;
  }
}

async function copyText(value) {
  const text = String(value || "").trim();
  if (!text) return;
  try {
    if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
    await navigator.clipboard.writeText(text);
    editorNotice.value = "已复制到剪贴板。";
  } catch {
    editorError.value = "无法写入剪贴板，请手动复制。";
  }
}

async function refreshOAuthToken(account) {
  if (!account?.id) return;
  tokenRefreshBusy.value = account.id;
  editorError.value = "";
  try {
    const result = await refreshUpstreamOAuthAccount(account.id);
    if (result?.ok) editorNotice.value = result.message || "访问令牌已刷新。";
    else editorError.value = result?.message || "访问令牌刷新失败";
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    tokenRefreshBusy.value = "";
  }
}

async function refreshQuota(account) {
  if (!account?.id) return;
  quotaRefreshBusy.value = account.id;
  editorError.value = "";
  try {
    const result = await refreshUpstreamAccountQuota(account.id);
    if (result?.ok) editorNotice.value = result.message || "上游额度快照已更新。";
    else editorError.value = result?.message || "上游额度查询失败";
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    quotaRefreshBusy.value = "";
  }
}

function formatTokens(value) {
  const number = Number(value || 0);
  if (!Number.isFinite(number) || number <= 0) return "0";
  if (number >= 1_000_000) return `${(number / 1_000_000).toFixed(1)}M`;
  if (number >= 1_000) return `${(number / 1_000).toFixed(1)}K`;
  return String(Math.round(number));
}

function accountIdentity(account) {
  const identity = account?.identity || {};
  if (identity.email) return identity.email;
  if (identity.subject) return identity.subject;
  return "未提供身份资料";
}

function quotaSummary(account) {
  const quota = account?.quota || {};
  if (!quota.available) return "上游未返回额度";
  if (Array.isArray(quota.windows) && quota.windows.length) {
    return quota.windows.map((window) => {
      const label = readText(window?.label) || "额度";
      const used = Number(window?.usedPercent);
      return Number.isFinite(used) ? `${label} 已用 ${Math.max(0, Math.min(100, Math.round(used)))}%` : label;
    }).join(" · ");
  }
  const values = [];
  if (quota.requestsRemaining !== undefined && quota.requestsRemaining !== null) {
    values.push(`请求余量 ${quota.requestsRemaining}`);
  }
  if (quota.tokensRemaining !== undefined && quota.tokensRemaining !== null) {
    values.push(`Token 余量 ${quota.tokensRemaining}`);
  }
  if (quota.retryAfter) values.push(`重试 ${quota.retryAfter}`);
  return values.join(" · ") || "上游已返回额度快照";
}

function canRefreshAccountQuota(account) {
  if (readText(account?.quotaUrl)) return true;
  return readText(account?.oauth?.upstream).toLowerCase() === "openai_codex";
}

function modelID(model) {
  if (typeof model === "string") return model.trim();
  return String(model?.id ?? model?.externalModelName ?? model?.name ?? "").trim();
}

function modelLabel(model) {
  if (typeof model === "string") return model;
  return String(model?.displayName ?? model?.display_name ?? model?.name ?? modelID(model)).trim();
}

function uniqueTestModels(...groups) {
  const seen = new Set();
  const result = [];
  for (const group of groups) {
    for (const model of Array.isArray(group) ? group : []) {
      const id = modelID(model);
      if (!id || seen.has(id)) continue;
      seen.add(id);
      result.push({ id, name: modelLabel(model) || id });
    }
  }
  return result;
}

function defaultTestModelForAccount(account) {
  const provider = String(account?.provider || "openai").toLowerCase();
  if (provider === "anthropic") return "claude-sonnet-4-5";
  if (provider === "grok") return "grok-4";
  return "gpt-5.4";
}

function modelsBoundToAccount(accountID) {
  return state.models.filter((model) => Array.isArray(model?.accountIds) && model.accountIds.includes(accountID));
}

function accountTestSteps(steps) {
  if (!Array.isArray(steps)) return [];
  return steps
    .map((step) => ({ text: readText(step?.text), tone: readText(step?.tone) || "muted" }))
    .filter((step) => step.text);
}

function nextAccountTestRequestID() {
  const nativeID = globalThis.crypto?.randomUUID?.();
  if (typeof nativeID === "string" && nativeID) return `account-test-${nativeID}`;
  accountTestRequestSerial += 1;
  return `account-test-${Date.now().toString(36)}-${accountTestRequestSerial}-${Math.random().toString(36).slice(2, 10)}`;
}

function cancelActiveAccountTest() {
  const requestID = accountTestRequestID.value;
  accountTestRequestID.value = "";
  if (!requestID) return;
  // The native side owns the HTTP context. Treat a late/finished result as a
  // harmless no-op, while still cancelling a slow upstream stream promptly.
  void cancelUpstreamAccountTest(requestID).catch(() => {});
}

async function openAccountTest(account) {
  if (!account?.id) return;
	cancelActiveAccountTest();
  const generation = ++accountTestGeneration;
  accountTestAccount.value = account;
  accountTestOpen.value = true;
  accountTestStatus.value = "idle";
  accountTestContent.value = "";
  accountTestError.value = "";
  accountTestImages.value = [];
  accountTestModels.value = uniqueTestModels(modelsBoundToAccount(account.id));
  accountTestDefaultModelID.value = accountTestModels.value[0]?.id || defaultTestModelForAccount(account);
  accountTestOutputLines.value = [{ text: "正在读取该账户可用模型…", tone: "info" }];
  accountTestLoadingModels.value = true;
  try {
    const result = await discoverAccountModels(account.id);
    if (generation !== accountTestGeneration || accountTestAccount.value?.id !== account.id) return;
    if (result?.ok) {
      accountTestModels.value = uniqueTestModels(result.models, modelsBoundToAccount(account.id));
      accountTestDefaultModelID.value = accountTestModels.value[0]?.id || defaultTestModelForAccount(account);
      accountTestOutputLines.value = [{ text: result.message || `已获取 ${accountTestModels.value.length} 个可测试模型。`, tone: "success" }];
      return;
    }
    if (!accountTestModels.value.length) {
      accountTestModels.value = [{ id: defaultTestModelForAccount(account), name: defaultTestModelForAccount(account) }];
      accountTestDefaultModelID.value = accountTestModels.value[0].id;
    }
    accountTestOutputLines.value = [{ text: `未能获取模型列表：${readText(result?.message) || "可直接使用默认模型测试。"}`, tone: "warning" }];
  } catch {
    if (generation !== accountTestGeneration || accountTestAccount.value?.id !== account.id) return;
    if (!accountTestModels.value.length) {
      accountTestModels.value = [{ id: defaultTestModelForAccount(account), name: defaultTestModelForAccount(account) }];
      accountTestDefaultModelID.value = accountTestModels.value[0].id;
    }
    accountTestOutputLines.value = [{ text: "模型列表读取失败；可直接使用默认模型测试。", tone: "warning" }];
  } finally {
    if (generation === accountTestGeneration) accountTestLoadingModels.value = false;
  }
}

async function runAccountTest(payload) {
  const account = accountTestAccount.value;
  if (!account?.id || !payload?.modelId || accountTestStatus.value === "connecting") return;
  const generation = ++accountTestGeneration;
	const requestID = nextAccountTestRequestID();
	accountTestRequestID.value = requestID;
  accountTestStatus.value = "connecting";
  accountTestError.value = "";
  accountTestContent.value = "";
  accountTestImages.value = [];
  accountTestOutputLines.value = [{ text: "正在发送账户测试请求…", tone: "info" }];
  try {
    const result = await testUpstreamAccountDetailed({
      accountId: account.id,
		requestId: requestID,
      model: payload.modelId,
      prompt: String(payload.prompt || "hi").trim() || "hi",
      mode: String(payload.mode || "default"),
    });
    if (generation !== accountTestGeneration || accountTestAccount.value?.id !== account.id) return;
    accountTestOutputLines.value = accountTestSteps(result?.steps);
    accountTestContent.value = readText(result?.content);
    accountTestImages.value = Array.isArray(result?.images) ? result.images : [];
    if (result?.ok) {
      accountTestStatus.value = "success";
      if (!accountTestOutputLines.value.length) accountTestOutputLines.value = [{ text: result.message || "模型可用", tone: "success" }];
    } else {
      accountTestStatus.value = "error";
      accountTestError.value = readText(result?.message) || "账户测试失败";
      if (!accountTestOutputLines.value.length) accountTestOutputLines.value = [{ text: accountTestError.value, tone: "error" }];
    }
    await loadAccounts();
  } catch {
    if (generation !== accountTestGeneration || accountTestAccount.value?.id !== account.id) return;
    accountTestStatus.value = "error";
    accountTestError.value = "账户测试请求失败";
    accountTestOutputLines.value = [{ text: accountTestError.value, tone: "error" }];
	} finally {
		if (accountTestRequestID.value === requestID) accountTestRequestID.value = "";
  }
}

function cancelAccountTest() {
  accountTestGeneration += 1;
	cancelActiveAccountTest();
}

function closeAccountTest() {
	cancelAccountTest();
  accountTestOpen.value = false;
}

function previewAccountTestImage(image) {
  const url = readText(typeof image === "string" ? image : image?.url);
  if (!url) return;
  window.open(url, "_blank", "noopener,noreferrer");
}

function setAccountSyncMessage(accountID, tone, message) {
  accountSyncMessages.value = {
    ...accountSyncMessages.value,
    [accountID]: { tone, message },
  };
}

async function syncAllAccountModels(account) {
  if (!account?.id || syncingAccountID.value) return;
  syncingAccountID.value = account.id;
  setAccountSyncMessage(account.id, "progress", "正在读取该账户的全部模型并同步到 Antigravity…");
  try {
    const result = await syncUpstreamAccountModels(account.id);
    if (result?.ok && !result?.refreshFailed) {
      setAccountSyncMessage(account.id, "success", result.message || "该账户的模型已同步到 Antigravity。");
    } else if (result?.ok) {
      setAccountSyncMessage(account.id, "warning", result.message || "模型已同步，但账户池列表尚未刷新。");
    } else {
      setAccountSyncMessage(account.id, "error", result?.message || "模型同步失败，请检查账户和上游接口后重试。");
    }
  } catch (error) {
    setAccountSyncMessage(account.id, "error", String(error?.message || error || "模型同步失败"));
  } finally {
    syncingAccountID.value = "";
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
    resetDiscoveredModelSelection(result.models || []);
    editorNotice.value = result.message || `已发现 ${selectedModelIds.value.length} 个模型，默认已全选。`;
  } catch (error) {
    editorError.value = String(error?.message || error);
  } finally {
    discovering.value = false;
  }
}

function openEditorAccountTest() {
  const accountID = readText(form.value.id);
  if (!accountID) {
    editorError.value = "请先保存账户；保存后可从该账户卡片打开完整测试。";
    return;
  }
  const account = state.accounts.find((item) => item?.id === accountID);
  if (!account) {
    editorError.value = "未找到该账户，请关闭编辑器后从账户卡片重新打开测试。";
    return;
  }
  // The editor only manages discovery/import. Every real probe goes through
  // the XIASS-style modal bound to exactly this saved account, so selecting a
  // model here can never silently test another account or its first model.
  closeEditor();
  void openAccountTest(account);
}

function toggleAllModels() {
  selectedModelIds.value = allSelected.value ? [] : discoveredModels.value.map(discoveredModelID).filter(Boolean);
}

function toggleModel(id, checked) {
  if (checked && !selectedModelIds.value.includes(id)) selectedModelIds.value.push(id);
  if (!checked) selectedModelIds.value = selectedModelIds.value.filter((value) => value !== id);
}

// Account discovery uses the same per-model profile resolver as the Models
// page. AddDiscoveredModels remains a compact native binding, so only a row
// the user actually changed is saved again with its selected effort after the
// account-bound batch import completes. This preserves existing settings for
// the same model in a shared account pool.
async function persistTouchedDiscoveredReasoning() {
  const accountID = readText(form.value.id);
  const touchedIDs = selectedModelIds.value.filter((id) => discoveredReasoningTouched.value[id]);
  if (!accountID || !touchedIDs.length) return { saved: 0, failures: [] };

  let saved = 0;
  const failures = [];
  for (const modelID of touchedIDs) {
    const discovered = discoveredModels.value.find((model) => discoveredModelID(model) === modelID);
    const target = state.models.find((model) =>
      readText(model?.externalModelName).replace(/^models\//, "") === modelID
      && Array.isArray(model?.accountIds)
      && model.accountIds.map(readText).includes(accountID)
    );
    if (!discovered || !target) {
      failures.push(`${modelID} 未在账户绑定的模型列表中找到`);
      continue;
    }
    const profile = resolveReasoningProfile({
      ...target,
      provider: target.provider || form.value.provider,
      model: target.externalModelName,
      apiStyle: target.apiStyle || form.value.apiStyle,
    });
    const effort = normalizeReasoningEffort(discoveredReasoningEfforts.value[modelID], profile);
    if (normalizeReasoningEffort(target.reasoningEffort, profile) === effort) continue;
    try {
      const result = await saveModel({ ...target, reasoningEffort: effort });
      if (result?.ok) saved += 1;
      else failures.push(`${modelID}：${readText(result?.message) || "保存失败"}`);
    } catch (error) {
      failures.push(`${modelID}：${readText(error?.message) || "保存失败"}`);
    }
  }
  return { saved, failures };
}

async function addSelectedModels() {
  if (!form.value.id || !selectedModelIds.value.length) return;
  saving.value = true;
  editorError.value = "";
  try {
    const result = await addDiscoveredModels({ accountId: form.value.id }, selectedModelIds.value);
    if (result?.ok) {
      const persisted = await persistTouchedDiscoveredReasoning();
      if (persisted.failures.length) {
        editorError.value = `模型已添加，但思考强度未完全保存：${persisted.failures.join("；")}`;
        return;
      }
      editorNotice.value = persisted.saved
        ? `${result.message || "模型已添加。"} 已保存 ${persisted.saved} 个模型的思考强度。`
        : result.message;
    }
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

function openJSONImport(notice = "") {
  if (editorOpen.value) closeEditor();
  importError.value = "";
  importNotice.value = notice;
  importOpen.value = true;
}

onMounted(() => {
  loadAccounts();
  void loadOAuthLoginProfiles();
  bindOAuthCompletionEvents();
});

onBeforeUnmount(() => {
	cancelAccountTest();
  clearOAuthSession();
  if (typeof removeOAuthCompletionListener === "function") removeOAuthCompletionListener();
  removeOAuthCompletionListener = null;
});
</script>

<template>
  <div class="page fade-up">
    <div class="row between page-head" style="gap: 12px">
      <div class="col" style="gap: 2px">
        <div class="t-title">上游账户池</div>
        <div class="t-caption">每张账户卡点“同步全部模型”即可加入 Antigravity；同一上游的同名模型会自动组成账户池。</div>
      </div>
      <div class="row" style="gap: 7px">
        <Button variant="plain" @click="openJSONImport()">导入 JSON</Button>
        <Button variant="filled" @click="openNew">添加账户</Button>
      </div>
    </div>

    <div v-if="!state.accounts.length && !state.accountsLoading" class="empty">
      <div class="empty-icon">◌</div>
      <div class="t-headline">还没有上游账户</div>
      <div class="t-caption" style="margin-top: 5px">添加一次凭据后，可把多个模型绑定到同一账户或账户池。</div>
      <div class="row" style="gap: 8px; margin-top: 14px">
        <Button variant="tinted" @click="openJSONImport()">导入账户 JSON</Button>
        <Button variant="filled" @click="openNew">添加账户</Button>
      </div>
    </div>

    <div v-else class="grid">
      <article v-for="account in state.accounts" :key="account.id" class="account-card" :class="{ paused: !account.enabled }">
        <div class="row between" style="align-items: flex-start; gap: 10px">
          <div class="grow col" style="gap: 3px; min-width: 0">
            <div class="t-headline truncate">{{ account.name || providerLabel(account.provider) + ' 账户' }}</div>
            <div class="mono truncate">{{ accountDisplayAPIURL(account) }}</div>
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
          <div class="inset-row"><span>身份</span><code class="truncate">{{ accountIdentity(account) }}</code></div>
          <div v-if="account.identity?.plan" class="inset-row"><span>套餐</span><code>{{ account.identity.plan }}</code></div>
          <div v-if="account.authExpiresAt" class="inset-row"><span>令牌到期</span><code>{{ formatTime(account.authExpiresAt) }}</code></div>
          <div class="inset-row"><span>上次成功</span><code>{{ formatTime(account.lastSuccessAt) }}</code></div>
          <div v-if="account.lastError" class="inset-row error-row"><span>状态</span><code class="truncate">{{ account.lastError }}</code></div>
        </div>
        <div class="usage-band">
          <div class="usage-label">本机转发用量</div>
          <div class="usage-values">
            <span><b>{{ account.localUsage?.requestCount || 0 }}</b> 请求</span>
            <span><b>{{ formatTokens(account.localUsage?.totalTokens) }}</b> Token</span>
            <span>缓存 {{ formatTokens(account.localUsage?.cacheReadTokens) }}</span>
          </div>
        </div>
        <AccountQuotaWindows
          v-if="canRefreshAccountQuota(account)"
          :account="account"
          :loading="quotaRefreshBusy === account.id"
          :show-identity="false"
          @refresh="refreshQuota(account)"
        />
        <div v-else class="quota-band" :class="{ available: account.quota?.available }">
          <div class="quota-copy">
            <span class="usage-label">上游额度</span>
            <span class="quota-text">{{ quotaSummary(account) }}</span>
            <span v-if="account.quota?.updatedAt" class="quota-meta">{{ account.quota.source }} · {{ formatTime(account.quota.updatedAt) }}</span>
          </div>
          <span class="quota-meta">未配置可查询的上游额度接口</span>
        </div>
        <div
          v-if="accountSyncMessages[account.id]"
          class="account-sync-feedback"
          :class="accountSyncMessages[account.id].tone"
          role="status"
        >{{ accountSyncMessages[account.id].message }}</div>
        <div class="account-actions">
          <Button variant="filled" size="sm" :loading="syncingAccountID === account.id" :disabled="Boolean(syncingAccountID) && syncingAccountID !== account.id" :title="syncingAccountID && syncingAccountID !== account.id ? '请等待当前账户同步完成' : ''" @click="syncAllAccountModels(account)">同步全部模型</Button>
          <Button variant="tinted" size="sm" @click="openAccountTest(account)">测试连接</Button>
          <Button v-if="account.type === 'oauth'" variant="plain" size="sm" :loading="tokenRefreshBusy === account.id" @click="refreshOAuthToken(account)">刷新令牌</Button>
          <Button variant="plain" size="sm" @click="toggleAccount(account)">{{ account.enabled ? '暂停' : '恢复' }}</Button>
          <Button variant="plain" size="sm" @click="openEdit(account)">编辑</Button>
          <Button variant="danger" size="sm" @click="confirmDelete = account">删除</Button>
        </div>
      </article>
    </div>

    <Modal :open="editorOpen" :title="isExisting ? '编辑上游账户' : '添加上游账户'" wide persistent @close="closeEditor">
      <div class="col editor" style="gap: 15px">
		<section class="section">
          <span class="t-footnote">账户类型与协议</span>
          <template v-if="showOAuthCredentialTypeChooser">
            <SegmentedControl :options="providerOptions" :model-value="form.provider" @update:model-value="onProviderChange" />
            <label class="select-field">
              <span class="t-footnote">凭据类型</span>
              <select :value="form.type" @change="onTypeChange($event.target.value)">
                <option v-for="option in accountTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </label>
            <div v-if="isOAuthAuthorizationLogin" class="row" style="justify-content: flex-end">
              <Button variant="plain" size="sm" @click="oauthCredentialSwitchOpen = false">返回一键登录</Button>
            </div>
          </template>
          <div v-else class="oauth-entry-summary">
            <div>
              <div class="t-headline">一键 OAuth 登录</div>
              <div class="t-caption">无需填写 API 地址、名称或 API Key；授权成功后会自动保存为可调度账户。</div>
            </div>
            <Button variant="plain" size="sm" @click="oauthCredentialSwitchOpen = true">切换其他登录/凭据方式</Button>
          </div>

          <template v-if="showOAuthAccountFields">
            <div class="compact-label">接口地址输入方式</div>
            <SegmentedControl :options="endpointModeOptions" :model-value="form.endpointMode" @update:model-value="onEndpointModeChange" />
            <div class="two-col">
              <Field :label="apiURLLabel" :hint="apiURLHint" :model-value="form.apiUrl" :placeholder="apiURLPlaceholder" mono @update:model-value="onAPIURLChange" />
              <Field label="账户名称" hint="可选；用于模型绑定时识别" v-model="form.name" placeholder="例如 XIASS 主账户" />
            </div>
			<Field v-if="showQuotaURLField" label="上游额度接口（可选）" hint="仅在点击“刷新额度”时请求；填写上游文档提供的完整 URL。" v-model="form.quotaUrl" placeholder="https://provider.example.com/v1/usage" mono />
            <div v-if="form.provider === 'anthropic' && form.endpointMode !== 'manual'" class="claude-path-control">
              <div class="compact-label">Claude 路径</div>
              <SegmentedControl :options="anthropicPathOptions" :model-value="form.messagePathMode" @update:model-value="form.messagePathMode = $event" />
              <div class="t-caption">自动时先走 <code>/v1/messages</code>，接口不存在才改试 <code>/v1/chat/messages</code>。</div>
            </div>
            <div v-else-if="form.provider === 'anthropic'" class="t-caption">手动模式下会严格使用完整地址，例如 <code>/v1/messages</code> 或 <code>/v1/chat/messages</code>。</div>
          </template>
        </section>

        <section v-if="isJSONImportTypeSelected" class="section json-import-box">
          <div class="t-headline">请通过 JSON 导入凭据</div>
          <div class="t-caption">auth.json、OAuth JSON 与账户凭据 JSON 会由导入器识别并安全保存，不能作为普通 API Key 直接提交。</div>
          <div class="row" style="justify-content: flex-end; gap: 7px">
            <Button variant="tinted" @click="openJSONImport('请粘贴完整的账户 JSON；它将按凭据字段安全导入，而不会被当作普通 API Key。')">打开 JSON 导入</Button>
          </div>
        </section>

		<section v-else-if="showCredentialSecretField" class="section">
          <div class="t-headline">认证凭据</div>
		  <Field
            :label="credentialLabel"
            :hint="credentialHint"
            type="password"
            v-model="form.apiKey"
            :placeholder="credentialPlaceholder"
            mono
          />
          <div class="fixed-auth-row"><span>认证方式</span><code>{{ fixedAuthModeLabel }}</code></div>
          <Field v-if="showCustomHeaderName" label="认证请求头名称" hint="例如 X-API-Token" v-model="form.authHeader" placeholder="X-API-Token" mono />
          <label v-if="showAdditionalHeaders" class="text-field">
            <span class="t-footnote">附加请求头（可选 JSON）</span>
            <textarea v-model="form.headersText" spellcheck="false" placeholder='{"X-Client":"Antigravity-WF"}'></textarea>
          </label>
        </section>
        <section v-if="showSchedulingControls" class="section">
          <div class="t-headline">调度设置</div>
          <div class="two-col">
            <Field label="优先级" hint="数值越小越先调度" type="number" v-model="form.priority" />
            <Field label="最大并发" hint="每个账户 1–32" type="number" v-model="form.maxConcurrency" />
          </div>
          <label class="enabled-check"><input v-model="form.enabled" type="checkbox" /> 保存后立即参与调度</label>
        </section>

		<section v-if="isOAuthAuthorizationLogin || isRefreshTokenTypeSelected" class="section oauth-box">
		  <div class="row between" style="gap: 8px">
			<div>
			  <div class="t-headline">{{ isRefreshTokenTypeSelected ? '刷新令牌 / Mobile RT 导入' : oauthLoginTitle }}</div>
			  <div class="t-caption">{{ isRefreshTokenTypeSelected ? '使用已注册的公开客户端与回调地址；刷新令牌不会作为 API Key 保存。' : oauthLoginDescription }}</div>
			</div>
			<Button
				v-if="isOAuthAuthorizationLogin"
				variant="tinted"
				:loading="oauthBusy"
				:disabled="!canStartSelectedOAuth"
				@click="startOAuth"
			>{{ oauthLoginButtonLabel }}</Button>
		  </div>

			<div v-if="isOAuthAuthorizationLogin" class="oauth-profiles">
			  <div class="row between" style="gap: 8px">
				<div>
				  <div class="compact-label">切换 OAuth 登录方式</div>
				  <div class="t-caption">默认使用 OpenAI / Codex；也可选择 Claude、Grok 等安全预设。</div>
				</div>
				<Button variant="plain" size="sm" :loading="oauthProfilesLoading" @click="refreshOAuthLoginProfiles">刷新预设</Button>
			  </div>
			  <div v-if="oauthProfilesLoading && !oauthProfiles.length" class="oauth-profile-empty">正在读取可用登录方式…</div>
			  <div v-else-if="oauthProfiles.length" class="oauth-profile-grid" role="group" aria-label="OAuth 登录方式">
				<button
				  v-for="profile in oauthProfiles"
				  :key="profile.id"
				  type="button"
				  class="oauth-profile"
				  :class="{ active: selectedOAuthProfileID === profile.id }"
				  :aria-pressed="selectedOAuthProfileID === profile.id"
				  @click="selectOAuthProfile(profile)"
				>
				  <span class="oauth-profile-title">{{ profile.label }}</span>
				  <span v-if="profile.description" class="oauth-profile-description">{{ profile.description }}</span>
				  <span v-else class="oauth-profile-description">使用该提供方的安全 OAuth 登录流程</span>
				</button>
			  </div>
			  <div v-else class="oauth-profile-empty">{{ oauthProfilesError }}</div>
			  <div class="row" style="flex-wrap: wrap; gap: 7px">
				<Button variant="plain" size="sm" @click="useCustomOAuth">高级自定义 OAuth</Button>
				<Button variant="plain" size="sm" @click="openJSONImport('请粘贴完整的 auth.json / OAuth JSON；导入器会安全提取令牌与账户信息。')">导入 OAuth JSON</Button>
				<Button variant="plain" size="sm" @click="onTypeChange('refresh_token')">使用 Refresh Token</Button>
			  </div>
			  <div v-if="selectedOAuthProfile" class="oauth-profile-selected">
				<template v-if="selectedOAuthProfile.requiresClientId">已选择 <b>{{ selectedOAuthProfile.label }}</b>。该预设需要你的公开 Client ID；请主动打开“高级自定义 OAuth”后填写。</template>
				<template v-else>已选择 <b>{{ selectedOAuthProfile.label }}</b>。授权成功后会自动保存为可调度账户。</template>
			  </div>
			</div>

			<div v-if="showOAuthCustomFields" class="oauth-custom-fields">
			  <div class="row between" style="gap: 8px">
				<div>
				  <div class="t-headline">高级自定义 OAuth</div>
				  <div class="t-caption">仅填写你自己注册的公开客户端；不需要也不应填写客户端密钥。</div>
				</div>
				<Button v-if="isOAuthAuthorizationLogin && oauthAdvancedOpen" variant="plain" size="sm" @click="returnToSimpleOAuthLogin">返回一键登录</Button>
			  </div>
			  <div class="two-col">
				<Field label="授权地址" v-model="form.oauth.authorizationUrl" placeholder="https://provider.example.com/oauth/authorize" mono />
				<Field label="令牌地址" v-model="form.oauth.tokenUrl" placeholder="https://provider.example.com/oauth/token" mono />
				<Field label="公开客户端 ID" v-model="form.oauth.clientId" placeholder="OAuth public client ID" mono />
				<Field label="已注册回调地址" v-model="form.oauth.redirectUri" placeholder="http://localhost:1455/auth/callback" mono />
			  </div>
			  <Field label="Scopes（可选）" v-model="form.oauth.scopes" placeholder="openid profile email offline_access" mono />
			</div>

          <div v-if="isRefreshTokenTypeSelected" class="oauth-result">
            <Field label="Refresh Token / Mobile RT" hint="仅发送至上方 OAuth 令牌地址；不会作为 API Key 保存或发往模型接口。" type="password" v-model="form.refreshToken" placeholder="粘贴刷新令牌" mono />
            <div class="t-caption">该值不会作为 API Key 保存或发往模型接口；兑换成功后仅保留 OAuth 访问令牌和刷新凭据。</div>
            <div class="row" style="justify-content: flex-end; gap: 7px">
              <Button variant="filled" size="sm" :loading="oauthBusy" @click="importRefreshToken">兑换并保存 OAuth 账户</Button>
            </div>
          </div>
          <div v-else-if="oauthSession" class="oauth-result">
            <div class="t-footnote">授权链接</div>
            <code class="oauth-link">{{ oauthSession.authorizationUrl }}</code>
            <div class="row" style="justify-content: flex-end; gap: 7px">
              <Button variant="plain" size="sm" @click="copyText(oauthSession.authorizationUrl)">复制链接</Button>
            </div>
            <div v-if="!oauthManualCompletionRequired" class="oauth-auto-callback">
              <div class="t-footnote">正在等待浏览器授权完成</div>
              <div class="t-caption">自动回调优先。请保持此窗口打开；若浏览器没有自动返回，可在下方粘贴完整回调地址或 code 继续完成。</div>
            </div>
            <label v-if="showOAuthManualFallback" class="text-field">
              <span class="t-footnote">{{ oauthManualCompletionRequired ? '回调 URL 或授权码' : '手动兜底：回调 URL 或授权码' }}</span>
              <textarea v-model="oauthCallback" spellcheck="false" placeholder="粘贴浏览器跳转后的完整地址，或仅粘贴 code"></textarea>
            </label>
            <div v-if="showOAuthManualFallback" class="row" style="justify-content: flex-end; gap: 7px">
              <Button variant="filled" size="sm" :loading="oauthBusy" @click="completeOAuth">{{ oauthManualCompletionRequired ? '兑换并保存账户' : '手动兑换并保存账户' }}</Button>
            </div>
          </div>
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
          <div class="row" style="justify-content: flex-end; gap: 7px">
            <Button variant="plain" size="sm" @click="openEditorAccountTest">在账户卡测试</Button>
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
        <Button variant="plain" @click="closeEditor">取消</Button>
		<Button v-if="!isOAuthAuthorizationLogin && !isRefreshTokenTypeSelected && !isJSONImportTypeSelected" variant="filled" :loading="saving" @click="saveAccount">保存账户</Button>
      </template>
    </Modal>

    <AccountTestModal
      :open="accountTestOpen"
      :account="accountTestAccount"
      :models="accountTestModels"
      :loading-models="accountTestLoadingModels"
      :status="accountTestStatus"
      :output-lines="accountTestOutputLines"
      :error-message="accountTestError"
      :generated-images="accountTestImages"
      :default-model-id="accountTestDefaultModelID"
      @test="runAccountTest"
		@cancel="cancelAccountTest"
      @close="closeAccountTest"
      @preview-image="previewAccountTestImage"
    />

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
.account-actions { display: flex; min-width: 0; flex-wrap: wrap; justify-content: flex-end; gap: 6px; margin-top: 12px; }
.account-actions :deep(.btn) { max-width: 100%; min-width: 0; }
.mono { color: var(--text-tertiary); font-size: 11px; }
.status-row { display: flex; flex-wrap: wrap; align-items: center; gap: 7px; margin-top: 10px; color: var(--text-tertiary); font-size: 11px; }
.inset-row > span { color: var(--text-tertiary); font-size: 10px; width: 48px; letter-spacing: .04em; }
.inset-row code { min-width: 0; color: var(--text-secondary); font-size: 11px; }
.error-row code { color: var(--orange); }
.usage-band, .quota-band { margin-top: 10px; padding: 9px 10px; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-inset); }
.usage-band { display: flex; flex-direction: column; gap: 5px; }
.account-sync-feedback { margin-top: 10px; padding: 8px 10px; border: 1px solid var(--separator); border-radius: var(--r-sm); color: var(--text-secondary); background: var(--bg-inset); font-size: 11.5px; line-height: 1.45; }
.account-sync-feedback.progress { color: var(--text-secondary); border-color: var(--accent-border); background: var(--accent-soft); }
.account-sync-feedback.success { color: #20a968; border-color: color-mix(in srgb, #20a968 48%, var(--separator)); background: color-mix(in srgb, #20a968 10%, var(--bg-inset)); }
.account-sync-feedback.warning { color: var(--orange); border-color: color-mix(in srgb, var(--orange) 46%, var(--separator)); background: color-mix(in srgb, var(--orange) 10%, var(--bg-inset)); }
.account-sync-feedback.error { color: var(--red); border-color: rgba(255,69,58,.28); background: rgba(255,69,58,.09); }
.usage-label { color: var(--text-tertiary); font-size: 10px; letter-spacing: .05em; text-transform: uppercase; }
.usage-values { display: flex; flex-wrap: wrap; gap: 8px 12px; color: var(--text-secondary); font-size: 11px; }
.usage-values b { color: var(--text-primary); font-weight: 700; }
.quota-band { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.quota-band.available { border-color: var(--accent-border); background: color-mix(in srgb, var(--accent-soft) 34%, var(--bg-inset)); }
.quota-copy { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.quota-text { color: var(--text-secondary); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.quota-meta { color: var(--text-tertiary); font-size: 10px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.empty { flex: 1; min-height: 270px; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 40px 20px; text-align: center; border: 1px dashed var(--separator-strong); border-radius: var(--r-lg); }
.empty-icon { width: 48px; height: 48px; display: grid; place-items: center; border-radius: 15px; font-size: 28px; color: var(--accent-strong); background: var(--accent-soft); margin-bottom: 13px; }
.editor { padding-bottom: 2px; }
.section { display: flex; flex-direction: column; gap: 9px; padding: 12px; border: 1px solid var(--separator); border-radius: var(--r-md); background: color-mix(in srgb, var(--bg-inset) 58%, transparent); }
.discover-box { background: color-mix(in srgb, var(--accent-soft) 28%, var(--bg-card)); }
.hint-box { background: color-mix(in srgb, var(--blue-soft) 28%, var(--bg-card)); }
.oauth-box { background: color-mix(in srgb, var(--blue-soft) 24%, var(--bg-card)); }
.oauth-entry-summary { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px; border: 1px solid var(--accent-border); border-radius: var(--r-sm); background: color-mix(in srgb, var(--accent-soft) 36%, var(--bg-card)); }
.oauth-profiles, .oauth-custom-fields { display: flex; flex-direction: column; gap: 9px; padding: 10px; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-card); }
.oauth-profile-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(145px, 1fr)); gap: 7px; }
.oauth-profile { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; min-height: 68px; padding: 9px 10px; border: 1px solid var(--separator-strong); border-radius: var(--r-sm); color: var(--text-primary); background: var(--bg-inset); text-align: left; transition: border-color .16s var(--ease), background .16s var(--ease), transform .14s var(--spring); }
.oauth-profile:hover { border-color: var(--accent-border); background: var(--accent-soft); }
.oauth-profile:active { transform: scale(.985); }
.oauth-profile.active { border-color: var(--accent); background: var(--accent-soft); box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 16%, transparent); }
.oauth-profile-title { font-size: 12.5px; font-weight: 650; }
.oauth-profile-description { color: var(--text-tertiary); font-size: 10.5px; line-height: 1.35; }
.oauth-profile-empty, .oauth-profile-selected, .oauth-auto-callback { padding: 8px 9px; border: 1px dashed var(--separator-strong); border-radius: var(--r-sm); color: var(--text-secondary); font-size: 11.5px; line-height: 1.45; background: var(--bg-inset); }
.oauth-profile-selected { border-style: solid; border-color: var(--accent-border); color: var(--accent-strong); background: var(--accent-soft); }
.oauth-auto-callback { display: flex; flex-direction: column; gap: 4px; border-style: solid; border-color: var(--accent-border); background: color-mix(in srgb, var(--accent-soft) 56%, var(--bg-card)); }
.oauth-result { display: flex; flex-direction: column; gap: 8px; padding: 10px; border: 1px solid var(--accent-border); border-radius: var(--r-sm); background: var(--bg-card); }
.oauth-link { display: block; max-height: 74px; overflow: auto; padding: 8px; border: 1px solid var(--separator); border-radius: var(--r-sm); color: var(--text-secondary); font-size: 11px; line-height: 1.45; word-break: break-all; background: var(--bg-inset); }
.two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.compact-label { color: var(--text-tertiary); font-size: 10px; letter-spacing: .06em; text-transform: uppercase; }
.fixed-auth-row { display: flex; align-items: center; justify-content: space-between; min-height: 34px; padding: 0 10px; border: 1px solid var(--separator); border-radius: var(--r-sm); color: var(--text-tertiary); background: var(--bg-inset); font-size: 11px; }.fixed-auth-row code { color: var(--text-secondary); font-size: 11px; }
.claude-path-control { display: flex; flex-direction: column; gap: 7px; padding: 9px; border: 1px dashed var(--separator-strong); border-radius: var(--r-sm); background: var(--bg-card); }
.select-field, .text-field { display: flex; flex-direction: column; gap: 6px; }
select, textarea { width: 100%; border: 1px solid var(--separator-strong); border-radius: var(--r-sm); background: var(--bg-inset); color: var(--text-primary); outline: none; }
select { height: 34px; padding: 0 10px; font: 13px var(--font-ui); }
textarea { min-height: 66px; padding: 9px 10px; resize: vertical; font: 12px var(--font-num); }
select:focus, textarea:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.enabled-check { display: flex; align-items: center; gap: 7px; color: var(--text-secondary); font-size: 12px; }
.selection-list { max-height: 196px; overflow-y: auto; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-card); }
.select-all, .select-row { display: grid; grid-template-columns: 16px minmax(0, 1fr) minmax(90px, .75fr) minmax(128px, .85fr); align-items: center; gap: 8px; min-height: 38px; padding: 0 10px; border-bottom: 1px solid var(--separator); font-size: 12px; }
.select-all { grid-template-columns: 16px 1fr; color: var(--text-secondary); background: var(--bg-fill); position: sticky; top: 0; }
.select-row:last-child { border-bottom: 0; }
.select-row code { color: var(--text-tertiary); font-size: 10px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.discovery-reasoning { display: flex; align-items: center; justify-content: flex-end; gap: 5px; min-width: 0; color: var(--text-tertiary); font-size: 10px; }
.discovery-reasoning select { width: auto; min-width: 0; height: 28px; padding: 0 23px 0 8px; color: var(--text-primary); background: var(--bg-card); font: 11px var(--font-num); }
.discovery-reasoning select:disabled { color: var(--text-tertiary); border-color: var(--separator); opacity: .78; cursor: default; }
.notice-box, .err-box { padding: 10px 11px; border-radius: var(--r-sm); font-size: 12px; line-height: 1.45; }
.notice-box { color: var(--accent-strong); background: var(--accent-soft); border: 1px solid var(--accent-border); }
.err-box { color: var(--red); background: rgba(255,69,58,.1); border: 1px solid rgba(255,69,58,.25); }
.import-text { min-height: 220px; }
@media (max-width: 620px) { .two-col { grid-template-columns: 1fr; } .select-row { grid-template-columns: 16px minmax(0, 1fr) minmax(112px, auto); } .select-row code { display: none; } .page-head { align-items: flex-start; flex-direction: column; } .account-actions { justify-content: flex-start; } }
</style>
