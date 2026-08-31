<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Field from "@/components/ui/Field.vue";
import Modal from "@/components/ui/Modal.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import AccountTestModal from "@/components/accounts/AccountTestModal.vue";
import AccountQuotaWindows from "@/components/accounts/AccountQuotaWindows.vue";
import OAuthTOTPQuickPicker from "@/components/OAuthTOTPQuickPicker.vue";
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
  ACCOUNT_SORT_OPTIONS,
  ACCOUNT_STATUS_OPTIONS,
  accountIsEnabled,
  accountProviderFilterOptions,
  accountProviderLabel,
  selectDirectoryAccounts,
} from "@/state/accountDirectory";
import {
  canStartOAuthLogin,
  chooseOAuthProfileID,
	isOAuthLoginProfileSupported,
	oauthLoginProfileMode,
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
const accountUsageDetailsAccount = ref(null);
// Directory controls are intentionally local render state. They never change
// an account until the user invokes an explicit single or bulk state action.
const accountDirectorySearch = ref("");
const accountDirectoryProvider = ref("all");
const accountDirectoryStatus = ref("all");
const accountDirectorySort = ref("priority");
const selectedAccountIDs = ref([]);
const accountBulkBusy = ref(false);
const accountBulkAction = ref("");
const accountBulkMessage = ref(null);
let accountTestGeneration = 0;
let accountTestRequestSerial = 0;
const oauthSession = ref(null);
const oauthCallback = ref("");
const oauthProfiles = ref([]);
const oauthProfilesLoading = ref(false);
const oauthProfilesLoaded = ref(false);
// An OAuth entry action can happen while the initial profile request is still
// in flight. Preserve that intent so the newly loaded list always selects the
// safe default instead of leaving the user on a blank editor.
const oauthProfilesAutoSelectRequested = ref(false);
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
	{ label: "账户凭据 JSON", value: "auth_json" },
	{ label: "Refresh Token / Mobile RT", value: "refresh_token" },
	{ label: "Codex PAT", value: "codex_pat" },
  { label: "自定义认证头", value: "custom_header" },
];
// Keep the first decision deliberately small. OAuth, request credentials and
// JSON import follow materially different security flows, so the account
// editor exposes them as peer entry modes instead of hiding them behind a
// provider-specific form. The detailed selector remains available for Token
// users who need a particular header or token format.
const credentialEntryOptions = [
  { label: "OAuth 授权", value: "oauth" },
  { label: "API Key / Token", value: "token" },
  { label: "账户 JSON", value: "json" },
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
  return entries
    .map(normalizeOAuthProfile)
    .filter((profile) => profile && isOAuthLoginProfileSupported(profile));
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
    apiStyle: "chat_completions",
    messagePathMode: "auto",
    authMode: "bearer",
    authHeader: "",
    // Stored headers are deliberately never rendered back into this form.
    // An empty editor is distinct from an explicit user change (see
    // headersTouched below), so editing a redacted account cannot erase them.
    headersText: "",
    hasPrivateHeaders: false,
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
const headersTouched = ref(false);
const isExisting = computed(() => Boolean(form.value.id));
const isJSONImportTypeSelected = computed(() => isJSONImportType(form.value.type));
const isRefreshTokenTypeSelected = computed(() => isRefreshTokenType(form.value.type));
const isOAuthAuthorizationLogin = computed(() => form.value.type === "oauth");
const credentialEntryMode = computed(() => {
  if (isJSONImportType(form.value.type)) return "json";
  if (form.value.type === "oauth") return "oauth";
  return "token";
});
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
  return canStartOAuthLogin(selectedOAuthProfile.value, oauthAdvancedOpen.value, form.value.oauth?.clientId);
});
const profileRequiresClientID = computed(() => (
  isOAuthAuthorizationLogin.value
  && !oauthAdvancedOpen.value
  && oauthLoginProfileMode(selectedOAuthProfile.value) === "bring-your-own-client"
));
const oauthLoginTitle = computed(() => oauthAdvancedOpen.value
  ? "高级自定义 OAuth"
  : (selectedOAuthProfile.value?.label || "OpenAI / Codex"));
const oauthLoginDescription = computed(() => {
  if (selectedOAuthProfile.value?.description) return selectedOAuthProfile.value.description;
  if (oauthProfilesLoading.value) return "正在读取可用 OAuth 登录方式…";
  if (oauthAdvancedOpen.value) return "使用你自己注册的公开 OAuth 客户端；不会保存客户端密钥。";
  return "未读取到默认登录预设。可刷新预设，或主动切换到高级自定义 OAuth。";
});
const oauthLoginButtonLabel = computed(() => {
  if (oauthAdvancedOpen.value) return "打开授权页";
	if (profileRequiresClientID.value) {
		return readText(form.value.oauth?.clientId) ? `${selectedOAuthProfile.value.label} 授权` : "填写我的 Client ID";
	}
	if (selectedOAuthProfile.value && oauthLoginProfileMode(selectedOAuthProfile.value) === "manual-callback") {
		return `${selectedOAuthProfile.value.label} 授权`;
	}
  return selectedOAuthProfile.value ? `${selectedOAuthProfile.value.label} 一键登录` : "等待可用预设";
});
const oauthManualCompletionRequired = computed(() => {
  const session = oauthSession.value;
  return Boolean(session && (session.manualCompletionRequired === true || session.automaticCallback === false));
});
const showOAuthManualFallback = computed(() => canManuallyCompleteOAuthSession(oauthSession.value));
const selectedCount = computed(() => selectedModelIds.value.length);
const allSelected = computed(() => discoveredModels.value.length > 0 && selectedModelIds.value.length === discoveredModels.value.length);
const accountDirectoryProviderOptions = computed(() => accountProviderFilterOptions(state.accounts));
const visibleAccounts = computed(() => selectDirectoryAccounts(state.accounts, {
  search: accountDirectorySearch.value,
  provider: accountDirectoryProvider.value,
  status: accountDirectoryStatus.value,
  sort: accountDirectorySort.value,
}));
const selectedAccountIDSet = computed(() => new Set(selectedAccountIDs.value));
const selectedAccounts = computed(() => {
  const selected = selectedAccountIDSet.value;
  return (Array.isArray(state.accounts) ? state.accounts : [])
    .filter((account) => selected.has(readText(account?.id)));
});
const selectedAccountCount = computed(() => selectedAccounts.value.length);
const selectedPausedAccountCount = computed(() => selectedAccounts.value.filter((account) => !accountIsEnabled(account)).length);
const selectedEnabledAccountCount = computed(() => selectedAccounts.value.filter((account) => accountIsEnabled(account)).length);
const selectedVisibleAccountCount = computed(() => (
  visibleAccounts.value.filter((account) => selectedAccountIDSet.value.has(readText(account?.id))).length
));
const allVisibleAccountsSelected = computed(() => (
  visibleAccounts.value.length > 0
  && visibleAccounts.value.every((account) => selectedAccountIDSet.value.has(readText(account?.id)))
));
const accountDirectoryFiltersActive = computed(() => (
  Boolean(accountDirectorySearch.value)
  || accountDirectoryProvider.value !== "all"
  || accountDirectoryStatus.value !== "all"
  || accountDirectorySort.value !== "priority"
));
const accountDirectorySummary = computed(() => {
  const total = Array.isArray(state.accounts) ? state.accounts.length : 0;
  const visible = visibleAccounts.value.length;
  return visible === total ? `共 ${total} 个账户` : `显示 ${visible} / ${total} 个账户`;
});
const apiURLLabel = computed(() => form.value.endpointMode === "manual" ? "完整 API 地址" : "基础域名 / 基础路径");
const apiURLHint = computed(() => {
  if (form.value.endpointMode === "manual") return "严格原样使用，不会自动补全或替换路径。";
  if (form.value.provider === "anthropic") return "只填域名即可；Claude 自动使用 Messages 路径。";
  return "只填域名或基础路径即可；XIASS Tools 会自动补全对应的 /v1 接口。";
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
  return accountProviderLabel(provider);
}

function providerTone(provider) {
  const key = readText(provider).toLowerCase();
  return key === "anthropic" ? "warn" : key === "openai" ? "info" : "neutral";
}

function oauthProfileModeLabel(profile) {
	return {
		"automatic-callback": "一键授权",
		"manual-callback": "手动回调",
		"bring-your-own-client": "使用我的 Client ID",
		unavailable: "不可用",
	}[oauthLoginProfileMode(profile)] || "不可用";
}

function health(account) {
  if (!accountIsEnabled(account)) return { label: "已暂停", tone: "neutral" };
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
    headersText: "",
    hasPrivateHeaders: account.hasPrivateHeaders === true,
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
		headersText: "",
		hasPrivateHeaders: false,
		oauth: { ...emptyForm().oauth, ...(defaults?.oauth || {}) },
	apiKey: "",
	};
	headersTouched.value = false;
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

// This is the direct equivalent of the account-center "OAuth 授权" entry:
// users do not need to first create an empty API-key account and then discover
// the credential-type selector. The existing profile chooser determines which
// provider-specific fields, if any, are actually required.
async function openOAuthNew() {
	await openNew();
	onTypeChange("oauth");
}

function openEdit(account) {
  clearOAuthSession();
  form.value = accountToForm(account);
	headersTouched.value = false;
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
    if (form.value.apiStyle === "messages") form.value.apiStyle = "chat_completions";
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
		editorNotice.value = "账户凭据 JSON 需要通过 JSON 导入解析，不能作为 API Key 保存。";
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
		editorNotice.value = "正在准备 OpenAI / Codex OAuth 登录…";
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

function selectCredentialEntryMode(mode) {
  switch (mode) {
    case "oauth":
      onTypeChange("oauth");
      return;
    case "json":
      onTypeChange("auth_json");
      return;
    default:
      // API Key is the safe, least-surprising default in the Token entry
      // route. The detailed credential selector remains available afterwards
      // for Bearer, x-api-key, Setup Token, Codex PAT and custom headers.
      onTypeChange("api_key");
  }
}

function onEndpointModeChange(mode) {
  form.value.endpointMode = mode;
  if (mode === "auto") form.value.apiUrl = smartBaseAPIURL(form.value.apiUrl);
  editorNotice.value = mode === "manual"
    ? "手动完整路径已启用：XIASS Tools 会严格使用上方地址。"
    : "智能补全已启用：XIASS Tools 会根据协议补全请求路径。";
}

function onAPIURLChange(value) {
  const raw = typeof value === "string" ? value : "";
  form.value.apiUrl = form.value.endpointMode === "auto" ? smartBaseAPIURL(raw) : raw;
}

async function loadOAuthLoginProfiles({ force = false, autoSelectDefault = false } = {}) {
  if (autoSelectDefault) oauthProfilesAutoSelectRequested.value = true;
  if (oauthProfilesLoading.value) return;
  if (oauthProfilesLoaded.value && !force) {
    if (oauthProfilesAutoSelectRequested.value) {
      oauthProfilesAutoSelectRequested.value = false;
      ensureDefaultOAuthProfile({ silent: true });
    }
    return;
  }
  oauthProfilesLoading.value = true;
  oauthProfilesError.value = "";
  try {
    const result = await getOAuthLoginProfiles();
    oauthProfiles.value = normalizeOAuthProfiles(result);
    oauthProfilesLoaded.value = true;
    if (oauthProfilesAutoSelectRequested.value) ensureDefaultOAuthProfile({ silent: true });
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
    oauthProfilesAutoSelectRequested.value = false;
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

function resetReviewedOAuthPresetDraft(profile) {
  const previousProfileID = selectedOAuthProfileID.value;
  const currentGoogleClientID = previousProfileID === "gemini-google"
    ? readText(form.value.oauth?.clientId)
    : "";
  const savedGoogleClientID = readText(state.settings?.oauth?.googleDesktopClientId);
  const googleClientID = currentGoogleClientID || savedGoogleClientID;

  // The simplified preset UI hides transport details. Clear every hidden value
  // before starting a reviewed profile so a previous Custom OAuth/edited account
  // cannot silently retain its token endpoint, inference route or request header.
  form.value.apiUrl = emptyForm().apiUrl;
  form.value.endpointMode = "auto";
  form.value.messagePathMode = profile?.provider === "anthropic" ? "standard" : "auto";
  form.value.authHeader = "";
  form.value.headersText = "";
  form.value.hasPrivateHeaders = false;
  form.value.quotaUrl = "";
  headersTouched.value = true;
  form.value.oauth = {
    authorizationUrl: "",
    tokenUrl: "",
    clientId: profile?.id === "gemini-google" ? googleClientID : "",
    redirectUri: "",
    scopes: "",
    refreshScopes: "",
    upstream: "",
  };
}

function selectOAuthProfile(profile, { silent = false } = {}) {
  if (!profile?.id) return;
  clearOAuthSession();
  if (oauthLoginProfileMode(profile) === "unavailable") {
    selectedOAuthProfileID.value = "";
    form.value.type = "oauth";
    oauthAdvancedOpen.value = true;
    editorError.value = "";
    editorNotice.value = profile.message || "该登录方式需要使用高级自定义 OAuth。";
    return;
  }
  resetReviewedOAuthPresetDraft(profile);
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
		const mode = oauthLoginProfileMode(profile);
		editorNotice.value = mode === "bring-your-own-client"
			? (readText(form.value.oauth?.clientId)
				? `已选择 ${profile.label}，并已使用本机保存的公开 Client ID；现在可直接开始授权。`
				: `已选择 ${profile.label}。首次填写你自己的公开 Client ID 后会保存在本机，之后可直接授权；不需要 Client Secret。`)
			: mode === "manual-callback"
				? `已选择 ${profile.label}。浏览器授权后，请粘贴完整回调地址或授权码完成保存。`
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
		editorNotice.value = "已返回 OAuth 登录预设，可切换其他预设或直接在浏览器授权。";
		return;
	}
	editorNotice.value = "正在读取可用 OAuth 登录方式…";
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
    throw new Error("附加请求头必须是 JSON 对象，例如 {\"X-Client\": \"XIASS Tools\"}");
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
    priority: Number(form.value.priority),
    maxConcurrency: Number(form.value.maxConcurrency),
  };
  if (!form.value.id || headersTouched.value) {
    payload.headers = parseHeaders();
  }
  delete payload.headersText;
	delete payload.hasPrivateHeaders;
	delete payload.refreshToken;
  return payload;
}

async function saveAccount() {
  editorError.value = "";
  let account;
  try {
		account = accountPayload();
		if (!account.apiUrl) throw new Error("请填写 API 地址");
		if (isJSONImportType(account.type)) throw new Error("账户凭据 JSON 请通过“导入 JSON”导入，不能作为 API Key 直接保存。");
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
      throw new Error("暂未读取到可用 OAuth 登录预设。请刷新预设，或主动点击“高级自定义 OAuth”。");
    }
    if (profile && !canStartOAuthLogin(profile, false, account.oauth?.clientId)) {
      throw new Error(oauthLoginProfileMode(profile) === "bring-your-own-client"
        ? "请先填写你自己的公开 Client ID；不需要 Client Secret。"
        : "该 OAuth 登录预设当前不可用，请改用高级自定义 OAuth。");
    }
    if (profile?.requiresClientId && !readText(account.oauth?.clientId)) {
      throw new Error("请先填写上方的公开 Client ID；不需要 Client Secret。");
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

function openAccountUsageDetails(account) {
	if (!account?.id) return;
	accountUsageDetailsAccount.value = account;
}

function localUsageMetric(account, keys, format = formatTokens) {
	const usage = account?.localUsage || account?.local_usage || {};
	for (const key of keys) {
		if (usage[key] !== undefined && usage[key] !== null) return format(usage[key]);
	}
	return format(0);
}

function formatRequestCount(value) {
	const number = Number(value || 0);
	if (!Number.isFinite(number) || number <= 0) return "0";
	return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 0 }).format(Math.round(number));
}

const accountUsageDetailRows = computed(() => {
	const account = accountUsageDetailsAccount.value;
	if (!account) return [];
	return [
		{ label: "已完成请求", value: localUsageMetric(account, ["requestCount", "requests", "request_count"], formatRequestCount) },
		{ label: "总 Token", value: localUsageMetric(account, ["totalTokens", "tokens", "total_tokens"]) },
		{ label: "输入 Token", value: localUsageMetric(account, ["promptTokens", "inputTokens", "input_tokens"]) },
		{ label: "输出 Token", value: localUsageMetric(account, ["completionTokens", "outputTokens", "output_tokens"]) },
		{ label: "缓存读取", value: localUsageMetric(account, ["cacheReadTokens", "cache_read_tokens"]) },
		{ label: "缓存写入", value: localUsageMetric(account, ["cacheWriteTokens", "cache_write_tokens"]) },
	];
});

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
		.map((step) => ({ type: readText(step?.type), text: readText(step?.text), tone: readText(step?.tone) || "muted" }))
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

function accountID(account) {
  return readText(account?.id);
}

function accountSelectionName(account) {
  return readText(account?.name) || `${providerLabel(account?.provider)} 账户`;
}

function isAccountSelected(account) {
  const id = accountID(account);
  return Boolean(id && selectedAccountIDSet.value.has(id));
}

function setAccountSelected(account, selected) {
  const id = accountID(account);
  if (!id || accountBulkBusy.value) return;
  const ids = new Set(selectedAccountIDs.value);
  if (selected) ids.add(id);
  else ids.delete(id);
  selectedAccountIDs.value = [...ids];
}

function setVisibleAccountsSelected(selected) {
  if (accountBulkBusy.value) return;
  const ids = new Set(selectedAccountIDs.value);
  for (const account of visibleAccounts.value) {
    const id = accountID(account);
    if (!id) continue;
    if (selected) ids.add(id);
    else ids.delete(id);
  }
  selectedAccountIDs.value = [...ids];
}

function clearAccountSelection() {
  if (accountBulkBusy.value) return;
  selectedAccountIDs.value = [];
}

function clearAccountSearch() {
  accountDirectorySearch.value = "";
}

function clearAccountDirectoryFilters() {
  accountDirectorySearch.value = "";
  accountDirectoryProvider.value = "all";
  accountDirectoryStatus.value = "all";
  accountDirectorySort.value = "priority";
}

async function updateSelectedAccountsEnabled(enabled) {
  if (accountBulkBusy.value) return;
  const targets = selectedAccounts.value.filter((account) => accountIsEnabled(account) !== enabled);
  if (!targets.length) return;

  const action = enabled ? "启用" : "暂停";
  const failures = [];
  let succeeded = 0;
  accountBulkBusy.value = true;
  accountBulkAction.value = enabled ? "enable" : "pause";
  accountBulkMessage.value = { tone: "progress", message: `正在${action} ${targets.length} 个已选账户…` };
  try {
    // Use the established single-account bridge serially. It preserves native
    // read-after-write refreshes instead of introducing a second, unverified
    // batch persistence API solely for this local directory.
    for (let index = 0; index < targets.length; index += 1) {
      const account = targets[index];
      accountBulkMessage.value = { tone: "progress", message: `正在${action} ${index + 1} / ${targets.length}：${accountSelectionName(account)}` };
      try {
        const result = await setUpstreamAccountEnabled(account.id, enabled);
        if (result?.ok) succeeded += 1;
        else failures.push(accountID(account));
      } catch {
        failures.push(accountID(account));
      }
    }
    await loadAccounts();
    selectedAccountIDs.value = failures.filter(Boolean);
    accountBulkMessage.value = failures.length
      ? { tone: "warning", message: `已${action} ${succeeded} 个账户；${failures.length} 个未更新，仍保留为已选状态，可检查后重试。` }
      : { tone: "success", message: `已${action} ${succeeded} 个账户。` };
  } finally {
    accountBulkBusy.value = false;
    accountBulkAction.value = "";
  }
}

async function toggleAccount(account) {
  try {
    const result = await setUpstreamAccountEnabled(account.id, !accountIsEnabled(account));
    if (!result?.ok) accountBulkMessage.value = { tone: "error", message: result?.message || "账户状态更新失败" };
  } catch {
    accountBulkMessage.value = { tone: "error", message: "账户状态更新失败，请检查本地代理后重试。" };
  }
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
        <div class="t-title">XIASS 上游账户中心</div>
		<div class="t-caption">统一管理 OAuth、令牌和 API Key。账户可驱动 Antigravity 的本地代理与模型账户池；Claude Code 仅可复用已启用且 Messages 兼容的账户。这不是外部客户端的原生账号登录或会话导入。</div>
      </div>
      <div class="row" style="gap: 7px">
        <Button variant="tinted" @click="openOAuthNew">OAuth 登录</Button>
        <Button variant="plain" @click="openJSONImport()">导入 JSON</Button>
        <Button variant="filled" @click="openNew">添加账户</Button>
      </div>
    </div>

    <div v-if="!state.accounts.length && !state.accountsLoading" class="empty">
      <div class="empty-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="3.5" /><path d="M5 19c.7-4 3.1-6 7-6s6.3 2 7 6" /></svg>
      </div>
      <div class="t-headline">还没有 XIASS 上游账户</div>
      <div class="t-caption" style="margin-top: 5px">可先通过 OAuth 授权，也可添加 API Key / 令牌或导入账户凭据；之后把模型绑定到该账户或账户池。不会扫描、接管或导入其他客户端的登录会话。</div>
      <div class="row" style="gap: 8px; margin-top: 14px">
        <Button variant="tinted" @click="openOAuthNew">OAuth 登录</Button>
        <Button variant="tinted" @click="openJSONImport()">导入账户 JSON</Button>
        <Button variant="filled" @click="openNew">添加账户</Button>
      </div>
    </div>

    <template v-else>
      <section class="account-directory" aria-label="账户筛选与批量操作">
        <div class="account-directory-toolbar">
          <div class="account-directory-filters" role="search" aria-label="筛选账户">
            <label class="directory-field directory-search-field">
              <span class="t-footnote">搜索账户</span>
              <input
                v-model="accountDirectorySearch"
                type="search"
                autocomplete="off"
                spellcheck="false"
                placeholder="名称、身份、地址或备注"
                aria-describedby="account-directory-summary"
                @keydown.esc.prevent="clearAccountSearch"
              />
            </label>
            <label class="directory-field">
              <span class="t-footnote">提供商</span>
              <select v-model="accountDirectoryProvider">
                <option v-for="option in accountDirectoryProviderOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </label>
            <label class="directory-field">
              <span class="t-footnote">状态</span>
              <select v-model="accountDirectoryStatus">
                <option v-for="option in ACCOUNT_STATUS_OPTIONS" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </label>
            <label class="directory-field">
              <span class="t-footnote">排序</span>
              <select v-model="accountDirectorySort">
                <option v-for="option in ACCOUNT_SORT_OPTIONS" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </label>
          </div>
          <div class="account-directory-meta">
            <span id="account-directory-summary" class="directory-summary" role="status" aria-live="polite">{{ accountDirectorySummary }}</span>
            <Button v-if="accountDirectoryFiltersActive" variant="plain" size="sm" @click="clearAccountDirectoryFilters">清除筛选</Button>
          </div>
        </div>

        <div class="account-bulk-toolbar" role="group" aria-label="批量更新账户状态">
          <label class="directory-select-all">
            <input
              type="checkbox"
              :checked="allVisibleAccountsSelected"
              :indeterminate="selectedVisibleAccountCount > 0 && !allVisibleAccountsSelected"
              :disabled="!visibleAccounts.length || accountBulkBusy"
              @change="setVisibleAccountsSelected($event.target.checked)"
            />
            <span>选择当前结果</span>
          </label>
          <span class="directory-selection-copy">{{ selectedAccountCount ? `已选 ${selectedAccountCount} 个账户` : "选择账户后可批量更新状态" }}</span>
          <div class="account-bulk-actions">
            <Button variant="tinted" size="sm" :loading="accountBulkAction === 'enable'" :disabled="!selectedPausedAccountCount || accountBulkBusy" @click="updateSelectedAccountsEnabled(true)">批量启用<span v-if="selectedPausedAccountCount"> {{ selectedPausedAccountCount }}</span></Button>
            <Button variant="plain" size="sm" :loading="accountBulkAction === 'pause'" :disabled="!selectedEnabledAccountCount || accountBulkBusy" @click="updateSelectedAccountsEnabled(false)">批量暂停<span v-if="selectedEnabledAccountCount"> {{ selectedEnabledAccountCount }}</span></Button>
            <Button v-if="selectedAccountCount" variant="plain" size="sm" :disabled="accountBulkBusy" @click="clearAccountSelection">清除选择</Button>
          </div>
        </div>
        <div
          v-if="accountBulkMessage"
          class="account-bulk-feedback"
          :class="accountBulkMessage.tone"
          role="status"
          aria-live="polite"
        >{{ accountBulkMessage.message }}</div>
      </section>

      <div v-if="state.accountsLoading && !state.accounts.length" class="account-directory-loading" role="status" aria-live="polite">正在读取账户…</div>
      <div v-else-if="!visibleAccounts.length" class="empty filter-empty" role="status">
        <div class="t-headline">没有找到匹配账户</div>
        <div class="t-caption" style="margin-top: 5px">可调整搜索、提供商或状态筛选；这些条件只在当前窗口本地生效。</div>
        <div class="row" style="gap: 8px; margin-top: 14px">
          <Button variant="tinted" @click="clearAccountDirectoryFilters">清除筛选</Button>
          <Button variant="filled" @click="openNew">添加账户</Button>
        </div>
      </div>

      <div v-else class="grid">
      <article v-for="account in visibleAccounts" :key="account.id" class="account-card" :class="{ paused: !accountIsEnabled(account), selected: isAccountSelected(account) }">
        <div class="row between" style="align-items: flex-start; gap: 10px">
          <div class="row grow" style="align-items: flex-start; gap: 9px; min-width: 0">
            <label class="account-select-control">
              <input
                type="checkbox"
                :checked="isAccountSelected(account)"
                :disabled="accountBulkBusy"
                :aria-label="`选择账户：${accountSelectionName(account)}`"
                @change="setAccountSelected(account, $event.target.checked)"
              />
              <span class="sr-only">选择账户：{{ accountSelectionName(account) }}</span>
            </label>
            <div class="grow col" style="gap: 3px; min-width: 0">
              <div class="t-headline truncate">{{ account.name || providerLabel(account.provider) + ' 账户' }}</div>
              <div class="mono truncate">{{ accountDisplayAPIURL(account) }}</div>
            </div>
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
		  @view-requests="openAccountUsageDetails(account)"
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
          <Button variant="plain" size="sm" @click="toggleAccount(account)">{{ accountIsEnabled(account) ? '暂停' : '恢复' }}</Button>
          <Button variant="plain" size="sm" @click="openEdit(account)">编辑</Button>
          <Button variant="danger" size="sm" @click="confirmDelete = account">删除</Button>
        </div>
      </article>
      </div>
    </template>

    <Modal :open="editorOpen" :title="isExisting ? '编辑上游账户' : '添加上游账户'" wide persistent @close="closeEditor">
      <div class="col editor" style="gap: 15px">
		<section class="section">
          <div class="credential-entry-selector">
            <span class="t-footnote">添加方式</span>
            <SegmentedControl
              :options="credentialEntryOptions"
              :model-value="credentialEntryMode"
              @update:model-value="selectCredentialEntryMode"
            />
          </div>
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
              <Button variant="plain" size="sm" @click="oauthCredentialSwitchOpen = false">返回 OAuth 预设</Button>
            </div>
          </template>
          <div v-else class="oauth-entry-summary">
            <div>
              <div class="t-headline">OAuth 账户连接</div>
              <div class="t-caption">从安全预设开始；每项会明确标注一键回调、手动回调或需使用自己的公开 Client ID。授权成功后会自动保存为可调度账户。</div>
            </div>
            <Button variant="plain" size="sm" @click="oauthCredentialSwitchOpen = true">高级凭据方式</Button>
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
		  <div class="t-caption">仅导入账户级 API Key、Access / Refresh Token、ID Token 与公开 OAuth Client ID。客户端密钥、Cookie、浏览器或桌面会话以及未知私有字段会被忽略。</div>
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
            <span v-if="isExisting && form.hasPrivateHeaders && !headersTouched" class="t-caption">已保存的附加请求头不会显示；不编辑此框时保存会保留原值。编辑后将以新内容替换，清空并保存可删除。</span>
            <textarea v-model="form.headersText" spellcheck="false" placeholder='{"X-Client":"XIASS-Tools"}' @input="headersTouched = true"></textarea>
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

		  <div v-if="isOAuthAuthorizationLogin && !oauthAdvancedOpen" class="oauth-account-draft">
			<Field
			  label="账户显示名称（可选）"
			  hint="授权成功后会显示上游返回的身份；这里的名称仅用于本地账户池区分。"
			  v-model="form.name"
			  placeholder="例如 开发账号"
			/>
			<Field
			  label="备注（可选）"
			  hint="仅保存在本机账户池，不会发送到 OAuth 或模型上游。"
			  v-model="form.notes"
			  placeholder="例如 个人订阅"
			/>
		  </div>

			<div v-if="isOAuthAuthorizationLogin" class="oauth-profiles">
			  <div class="row between" style="gap: 8px">
				<div>
				  <div class="compact-label">切换 OAuth 登录方式</div>
				  <div class="t-caption">当前内置一键登录仅开放 OpenAI / Codex；其他提供方需在专用运行时完成验证后才会显示。</div>
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
				  <span class="oauth-profile-head">
					<span class="oauth-profile-title">{{ profile.label }}</span>
					<span class="oauth-profile-mode" :class="oauthLoginProfileMode(profile)">{{ oauthProfileModeLabel(profile) }}</span>
				  </span>
				  <span v-if="profile.description" class="oauth-profile-description">{{ profile.description }}</span>
				  <span v-else class="oauth-profile-description">使用该提供方的安全 OAuth 登录流程</span>
				</button>
			  </div>
			  <div v-else class="oauth-profile-empty">{{ oauthProfilesError }}</div>
			  <div class="row" style="flex-wrap: wrap; gap: 7px">
				<Button variant="plain" size="sm" @click="useCustomOAuth">高级自定义 OAuth</Button>
				<Button variant="plain" size="sm" @click="openJSONImport('请粘贴你有权使用的账户凭据 JSON；导入器只保留账户级令牌与公开 OAuth 配置。')">导入账户凭据 JSON</Button>
				<Button variant="plain" size="sm" @click="onTypeChange('refresh_token')">使用 Refresh Token</Button>
			  </div>
			  <div v-if="selectedOAuthProfile" class="oauth-profile-selected">
				<template v-if="oauthLoginProfileMode(selectedOAuthProfile) === 'bring-your-own-client'">
				  <span>已选择 <b>{{ selectedOAuthProfile.label }}</b>。只需填写你自己的公开 Client ID；不会要求或保存 Client Secret。</span>
				</template>
				<template v-else-if="oauthLoginProfileMode(selectedOAuthProfile) === 'manual-callback'">已选择 <b>{{ selectedOAuthProfile.label }}</b>。浏览器授权后，请粘贴完整回调地址或授权码完成保存。</template>
				<template v-else>已选择 <b>{{ selectedOAuthProfile.label }}</b>。授权成功后会自动保存为可调度账户。</template>
			  </div>
			  <div class="oauth-boundary-note">
				<strong>授权范围</strong>
				<span>这里管理的是 XIASS Tools 的上游账户，不会读取、替换或导出 Codex、Claude Code、Cursor、Windsurf 或 Antigravity 的原生登录会话。</span>
			  </div>
			  <Field
				v-if="profileRequiresClientID"
				label="我的公开 Client ID"
				hint="首次填写你自己注册的 Desktop Client ID 后会保存在本机，后续可直接授权；不需要 Client Secret。"
				v-model="form.oauth.clientId"
				placeholder="Desktop OAuth Client ID"
				mono
			  />
			</div>

			<div v-if="showOAuthCustomFields" class="oauth-custom-fields">
			  <div class="row between" style="gap: 8px">
				<div>
				  <div class="t-headline">高级自定义 OAuth</div>
				  <div class="t-caption">仅填写你自己注册的公开客户端；不需要也不应填写客户端密钥。</div>
				</div>
				<Button v-if="isOAuthAuthorizationLogin && oauthAdvancedOpen" variant="plain" size="sm" @click="returnToSimpleOAuthLogin">返回 OAuth 预设</Button>
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
				<OAuthTOTPQuickPicker :open="Boolean(oauthSession)" />
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
	  :show-prompt="false"
      @test="runAccountTest"
		@cancel="cancelAccountTest"
      @close="closeAccountTest"
      @preview-image="previewAccountTestImage"
    />

	<Modal :open="Boolean(accountUsageDetailsAccount)" title="本机转发统计" @close="accountUsageDetailsAccount = null">
	  <div v-if="accountUsageDetailsAccount" class="usage-details-modal">
		<div class="usage-details-heading">
		  <div class="t-headline truncate">{{ accountUsageDetailsAccount.name || accountIdentity(accountUsageDetailsAccount) }}</div>
		  <div class="t-caption">仅显示 XIASS Tools 在此账户上的聚合转发统计；不会保存或展示请求文本、文件、图片、凭据或聊天内容。</div>
		</div>
		<dl class="usage-detail-grid">
		  <div v-for="row in accountUsageDetailRows" :key="row.label"><dt>{{ row.label }}</dt><dd>{{ row.value }}</dd></div>
		  <div><dt>上次成功</dt><dd>{{ formatTime(accountUsageDetailsAccount.lastSuccessAt) }}</dd></div>
		</dl>
	  </div>
	  <template #footer><Button variant="filled" @click="accountUsageDetailsAccount = null">关闭</Button></template>
	</Modal>

    <Modal :open="importOpen" title="导入账户 JSON" wide persistent @close="importOpen = false">
      <div class="col" style="gap: 10px">
        <div class="t-body">支持单账户对象、数组、<code>{"accounts": [...]}</code>、<code>{"data": [...]}</code>，以及 XIASS 风格的 <code>credentials</code>、<code>access_token</code>、<code>api_key</code> 字段。</div>
        <div class="t-caption">仅粘贴你有权使用的账户级 API / OAuth 凭据。客户端密钥、Cookie、浏览器或桌面会话以及未知私有字段不会导入；令牌不会回显到界面或日志。</div>
        <textarea v-model="importText" class="import-text" aria-label="账户 JSON" spellcheck="false" placeholder='{"provider":"openai","api_key":"sk-...","base_url":"https://api.xiass.com"}'></textarea>
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
.account-directory { display: flex; flex-direction: column; gap: 9px; padding: 11px; border: 1px solid var(--separator); border-radius: var(--r-lg); background: color-mix(in srgb, var(--bg-card) 86%, var(--bg-inset)); box-shadow: var(--shadow-card); }
.account-directory-toolbar { display: flex; align-items: flex-end; justify-content: space-between; gap: 10px; }
.account-directory-filters { flex: 1 1 auto; display: grid; grid-template-columns: minmax(190px, 1.5fr) repeat(3, minmax(114px, .72fr)); gap: 8px; min-width: 0; }
.directory-field { display: grid; min-width: 0; gap: 5px; }
.directory-field input, .directory-field select { width: 100%; height: 34px; min-width: 0; padding: 0 10px; border: 1px solid var(--separator-strong); border-radius: var(--r-sm); color: var(--text-primary); background: var(--bg-inset); font: 12px var(--font-ui); outline: none; }
.directory-field input { padding-right: 8px; }
.directory-field input::placeholder { color: var(--text-tertiary); }
.directory-field input:focus-visible, .directory-field select:focus-visible, .account-select-control input:focus-visible, .directory-select-all input:focus-visible { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.account-directory :deep(.btn:focus-visible) { outline: 2px solid var(--accent); outline-offset: 2px; }
.account-directory-meta { flex: 0 0 auto; display: flex; align-items: center; justify-content: flex-end; gap: 7px; min-height: 34px; }
.directory-summary, .directory-selection-copy { color: var(--text-tertiary); font-size: 11.5px; white-space: nowrap; }
.account-bulk-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; min-height: 37px; padding: 7px 9px; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-inset); }
.directory-select-all { display: inline-flex; align-items: center; gap: 7px; color: var(--text-secondary); font-size: 12px; cursor: pointer; user-select: none; }
.directory-select-all input, .account-select-control input { width: 15px; height: 15px; margin: 0; accent-color: var(--accent); cursor: pointer; }
.directory-select-all input:disabled, .account-select-control input:disabled { cursor: not-allowed; }
.account-bulk-actions { display: flex; flex: 1 1 auto; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }
.account-bulk-feedback { padding: 8px 10px; border: 1px solid var(--separator); border-radius: var(--r-sm); color: var(--text-secondary); background: var(--bg-inset); font-size: 11.5px; line-height: 1.45; }
.account-bulk-feedback.progress { border-color: var(--accent-border); color: var(--accent-strong); background: var(--accent-soft); }
.account-bulk-feedback.success { border-color: color-mix(in srgb, var(--green) 45%, var(--separator)); color: var(--green); background: color-mix(in srgb, var(--green) 10%, var(--bg-inset)); }
.account-bulk-feedback.warning { border-color: color-mix(in srgb, var(--orange) 45%, var(--separator)); color: var(--orange); background: color-mix(in srgb, var(--orange) 10%, var(--bg-inset)); }
.account-bulk-feedback.error { border-color: color-mix(in srgb, var(--red) 45%, var(--separator)); color: var(--red); background: color-mix(in srgb, var(--red) 10%, var(--bg-inset)); }
.account-directory-loading { display: grid; min-height: 172px; place-items: center; border: 1px dashed var(--separator-strong); border-radius: var(--r-lg); color: var(--text-secondary); background: var(--bg-card); font-size: 13px; }
.filter-empty { min-height: 200px; }
.credential-entry-selector { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 11px; }
.credential-entry-selector :deep(.seg) { max-width: 100%; overflow-x: auto; }
.account-card { background: var(--bg-card); border: 1px solid var(--separator); border-radius: var(--r-lg); padding: 14px; box-shadow: var(--shadow-card); backdrop-filter: blur(16px); }
.account-card.paused { opacity: .68; }
.account-card.selected { border-color: var(--accent-border); box-shadow: 0 0 0 2px var(--accent-soft), var(--shadow-card); }
.account-select-control { display: inline-flex; flex: 0 0 auto; align-items: center; min-height: 18px; padding-top: 2px; cursor: pointer; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
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
.usage-details-modal { display: grid; gap: 13px; }
.usage-details-heading { display: grid; gap: 4px; }
.usage-detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; margin: 0; }
.usage-detail-grid > div { min-width: 0; display: grid; gap: 4px; padding: 10px; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-inset); }
.usage-detail-grid dt { color: var(--text-tertiary); font-size: 10.5px; }
.usage-detail-grid dd { overflow: hidden; margin: 0; color: var(--text-primary); font: 650 14px var(--font-num); text-overflow: ellipsis; white-space: nowrap; }
.empty { flex: 1; min-height: 270px; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 40px 20px; text-align: center; border: 1px dashed var(--separator-strong); border-radius: var(--r-lg); }
.empty-icon { width: 48px; height: 48px; display: grid; place-items: center; border-radius: 15px; color: var(--accent-strong); background: var(--accent-soft); margin-bottom: 13px; }
.empty-icon svg { width: 25px; height: 25px; fill: none; stroke: currentColor; stroke-width: 1.7; stroke-linecap: round; stroke-linejoin: round; }
.editor { padding-bottom: 2px; }
.section { display: flex; flex-direction: column; gap: 9px; padding: 12px; border: 1px solid var(--separator); border-radius: var(--r-md); background: color-mix(in srgb, var(--bg-inset) 58%, transparent); }
.discover-box { background: color-mix(in srgb, var(--accent-soft) 28%, var(--bg-card)); }
.hint-box { background: color-mix(in srgb, var(--blue-soft) 28%, var(--bg-card)); }
.oauth-box { background: color-mix(in srgb, var(--blue-soft) 24%, var(--bg-card)); }
.oauth-entry-summary { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px; border: 1px solid var(--accent-border); border-radius: var(--r-sm); background: color-mix(in srgb, var(--accent-soft) 36%, var(--bg-card)); }
.oauth-profiles, .oauth-custom-fields, .oauth-account-draft { display: flex; flex-direction: column; gap: 9px; padding: 10px; border: 1px solid var(--separator); border-radius: var(--r-sm); background: var(--bg-card); }
.oauth-account-draft { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; }
@media (max-width: 620px) { .oauth-account-draft { grid-template-columns: 1fr; } }
.oauth-profile-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(145px, 1fr)); gap: 7px; }
.oauth-profile { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; min-height: 74px; padding: 9px 10px; border: 1px solid var(--separator-strong); border-radius: var(--r-sm); color: var(--text-primary); background: var(--bg-inset); text-align: left; transition: border-color .16s var(--ease), background .16s var(--ease), transform .14s var(--spring); }
.oauth-profile:hover { border-color: var(--accent-border); background: var(--accent-soft); }
.oauth-profile:active { transform: scale(.985); }
.oauth-profile.active { border-color: var(--accent); background: var(--accent-soft); box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 16%, transparent); }
.oauth-profile-head { display: flex; width: 100%; align-items: center; justify-content: space-between; gap: 7px; }
.oauth-profile-title { font-size: 12.5px; font-weight: 650; }
.oauth-profile-mode { flex: 0 0 auto; border: 1px solid var(--separator); border-radius: 999px; color: var(--text-tertiary); padding: 3px 6px; font-size: 10.5px; font-weight: 680; line-height: 1.15; }
.oauth-profile-mode.automatic-callback { border-color: color-mix(in srgb, var(--green) 42%, var(--separator)); color: var(--green); background: color-mix(in srgb, var(--green) 9%, transparent); }
.oauth-profile-mode.manual-callback { border-color: color-mix(in srgb, var(--orange) 42%, var(--separator)); color: var(--orange); background: color-mix(in srgb, var(--orange) 9%, transparent); }
.oauth-profile-mode.bring-your-own-client { border-color: color-mix(in srgb, var(--blue) 42%, var(--separator)); color: var(--blue); background: color-mix(in srgb, var(--blue) 9%, transparent); }
.oauth-profile-description { color: var(--text-secondary); font-size: 12px; line-height: 1.5; }
.oauth-profile-empty, .oauth-profile-selected, .oauth-auto-callback { padding: 9px 10px; border: 1px dashed var(--separator-strong); border-radius: var(--r-sm); color: var(--text-secondary); font-size: 12.5px; line-height: 1.5; background: var(--bg-inset); }
.oauth-profile-selected { display: flex; align-items: center; justify-content: space-between; gap: 8px; border-style: solid; border-color: var(--accent-border); color: var(--accent-strong); background: var(--accent-soft); }
.oauth-boundary-note { display: grid; gap: 3px; border-left: 2px solid var(--separator-strong); color: var(--text-secondary); padding: 6px 0 6px 9px; font-size: 12px; line-height: 1.5; }
.oauth-boundary-note strong { color: var(--text-primary); font-size: 12px; }
.oauth-auto-callback { display: flex; flex-direction: column; gap: 4px; border-style: solid; border-color: var(--accent-border); background: color-mix(in srgb, var(--accent-soft) 56%, var(--bg-card)); }
.oauth-result { display: flex; flex-direction: column; gap: 8px; padding: 10px; border: 1px solid var(--accent-border); border-radius: var(--r-sm); background: var(--bg-card); }
.oauth-link { display: block; max-height: 86px; overflow: auto; padding: 9px; border: 1px solid var(--separator); border-radius: var(--r-sm); color: var(--text-secondary); font-size: 12px; line-height: 1.5; word-break: break-all; background: var(--bg-inset); }
.two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.compact-label { color: var(--text-secondary); font-size: 11.5px; letter-spacing: .05em; text-transform: uppercase; }
.fixed-auth-row { display: flex; align-items: center; justify-content: space-between; min-height: 36px; padding: 0 10px; border: 1px solid var(--separator); border-radius: var(--r-sm); color: var(--text-secondary); background: var(--bg-inset); font-size: 12px; }.fixed-auth-row code { color: var(--text-primary); font-size: 12px; }
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
@media (max-width: 900px) { .account-directory-toolbar { align-items: stretch; flex-direction: column; } .account-directory-meta { justify-content: space-between; } }
@media (max-width: 700px) { .account-directory-filters { grid-template-columns: repeat(2, minmax(0, 1fr)); } .directory-search-field { grid-column: span 2; } }
@media (max-width: 900px) {
  .page-head { align-items: flex-start; flex-direction: column; }
  .page-head > .row:last-child { width: 100%; flex-wrap: wrap; }
  .page-head > .row:last-child :deep(.btn) { flex: 1 1 auto; white-space: nowrap; }
}
@media (max-width: 620px) { .two-col, .usage-detail-grid { grid-template-columns: 1fr; } .select-row { grid-template-columns: 16px minmax(0, 1fr) minmax(112px, auto); } .select-row code { display: none; } .account-actions { justify-content: flex-start; } .account-directory { padding: 10px; } .account-directory-filters { grid-template-columns: 1fr; } .directory-search-field { grid-column: auto; } .account-directory-meta, .account-bulk-actions { justify-content: flex-start; } .directory-selection-copy { flex-basis: 100%; white-space: normal; } }
</style>
