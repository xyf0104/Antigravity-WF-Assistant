import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  DEFAULT_OAUTH_PROFILE_ID,
  canStartOAuthLogin,
  chooseOAuthProfileID,
	isOAuthLoginProfileSupported,
	oauthLoginProfileMode,
  usesSimplifiedOAuthLogin,
} from "../src/state/oauthLoginUX.js";

const [accountsSource, storageSettingsSource] = await Promise.all([
  readFile(new URL("../src/views/Accounts.vue", import.meta.url), "utf8"),
  readFile(new URL("../../internal/storage/settings.go", import.meta.url), "utf8"),
]);

const profiles = [
  { id: "claude-code", available: "manual" },
  { id: DEFAULT_OAUTH_PROFILE_ID, available: "ready" },
  { id: "grok-cli", available: "ready" },
];

test("OAuth chooses OpenAI / Codex by default without replacing an explicit profile", () => {
  assert.equal(chooseOAuthProfileID(profiles), DEFAULT_OAUTH_PROFILE_ID);
  assert.equal(chooseOAuthProfileID(profiles, { selectedProfileID: "claude-code" }), "claude-code");
});

test("custom OAuth mode deliberately does not choose a provider preset", () => {
  assert.equal(chooseOAuthProfileID(profiles, { advancedCustomOpen: true }), "");
  assert.equal(chooseOAuthProfileID([{ id: "claude-code", available: "manual" }]), "");
});

test("simplified OAuth is exclusive to the standard OAuth credential type", () => {
  assert.equal(usesSimplifiedOAuthLogin("oauth", false), true);
  assert.equal(usesSimplifiedOAuthLogin("oauth", true), false);
  assert.equal(usesSimplifiedOAuthLogin("api_key", false), false);
});

test("OAuth start is available only for a loaded preset or explicit custom mode", () => {
  assert.equal(canStartOAuthLogin(null, false), false);
  assert.equal(canStartOAuthLogin({ id: "openai-codex", available: "ready" }, false), true);
  assert.equal(canStartOAuthLogin({ id: "gemini-google", available: "requires_client_id" }, false), false);
  assert.equal(canStartOAuthLogin({ id: "gemini-google", available: "requires_client_id" }, false, "desktop-client-id"), true);
  assert.equal(canStartOAuthLogin({ id: "antigravity", available: "custom_only" }, false), false);
  assert.equal(canStartOAuthLogin(null, true), true);
});

test("OAuth presentation distinguishes automatic, manual, and user-client routes", () => {
	assert.equal(oauthLoginProfileMode(null), "unavailable");
	assert.equal(oauthLoginProfileMode({ id: "openai-codex", available: "ready", automaticCallback: true }), "automatic-callback");
	assert.equal(oauthLoginProfileMode({ id: "claude-code", available: "manual", manualCompletionRequired: true }), "manual-callback");
	assert.equal(oauthLoginProfileMode({ id: "gemini-google", available: "requires_client_id", requiresClientId: true }), "bring-your-own-client");
	assert.equal(oauthLoginProfileMode({ id: "claude-code", available: "manual" }), "manual-callback");
	assert.equal(oauthLoginProfileMode({ id: "antigravity", available: "custom_only" }), "unavailable");
});

test("OAuth chooser refuses unavailable, custom-only, and unreviewed profiles", () => {
  const unsafeProfiles = [
    { id: DEFAULT_OAUTH_PROFILE_ID, available: "custom_only" },
    { id: "antigravity", available: "unavailable" },
		{ id: "experimental-provider", available: "experimental" },
		{ id: "missing-mode" },
  ];
  assert.equal(chooseOAuthProfileID(unsafeProfiles), "");
  assert.equal(chooseOAuthProfileID(unsafeProfiles, { selectedProfileID: "antigravity" }), "");
	for (const profile of unsafeProfiles) assert.equal(isOAuthLoginProfileSupported(profile), false);
  assert.equal(isOAuthLoginProfileSupported({ id: "openai-codex", available: "ready" }), true);
  assert.equal(isOAuthLoginProfileSupported({ id: "claude-code", available: "manual" }), true);
  assert.equal(isOAuthLoginProfileSupported({ id: "gemini-google", available: "requires_client_id" }), true);
});

test("account selector filters unsupported preset records before they reach the UI", () => {
	assert.match(accountsSource, /isOAuthLoginProfileSupported/);
	assert.match(accountsSource, /\.filter\(\(profile\) => profile && isOAuthLoginProfileSupported\(profile\)\)/);
	assert.match(accountsSource, /OAuth 账户连接/);
});

test("bring-your-own-client OAuth exposes only the public Client ID quick path", () => {
  assert.match(accountsSource, /const profileRequiresClientID = computed/);
  assert.match(accountsSource, /canStartOAuthLogin\(selectedOAuthProfile\.value, oauthAdvancedOpen\.value, form\.value\.oauth\?\.clientId\)/);
  assert.match(accountsSource, /v-if="profileRequiresClientID"/);
  assert.match(accountsSource, /label="我的公开 Client ID"/);
  assert.match(accountsSource, /不需要 Client Secret/);
	assert.match(accountsSource, /state\.settings\?\.oauth\?\.googleDesktopClientId/);
	assert.match(accountsSource, /首次填写你自己注册的 Desktop Client ID 后会保存在本机/);
  assert.match(storageSettingsSource, /type OAuthSettings struct \{[\s\S]*GoogleDesktopClientID string `json:"googleDesktopClientId,omitempty"`/);
  assert.match(storageSettingsSource, /type AppSettings struct \{[\s\S]*OAuth\s+OAuthSettings\s+`json:"oauth"`/);
  assert.doesNotMatch(accountsSource, /请主动点击“高级自定义 OAuth”后填写/);
});

test("reviewed OAuth presets clear hidden custom transport state while retaining the saved Google public client", () => {
  assert.match(accountsSource, /function resetReviewedOAuthPresetDraft\(profile\)/);
  assert.match(accountsSource, /form\.value\.apiUrl = emptyForm\(\)\.apiUrl/);
  assert.match(accountsSource, /form\.value\.authHeader = ""/);
  assert.match(accountsSource, /form\.value\.headersText = ""/);
  assert.match(accountsSource, /form\.value\.quotaUrl = ""/);
  assert.match(accountsSource, /headersTouched\.value = true/);
  assert.match(accountsSource, /authorizationUrl: ""/);
  assert.match(accountsSource, /tokenUrl: ""/);
  assert.match(accountsSource, /clientId: profile\?\.id === "gemini-google" \? googleClientID : ""/);
  assert.match(accountsSource, /resetReviewedOAuthPresetDraft\(profile\)/);
});

test("simple OAuth keeps local account labels separate from authorization data", () => {
  assert.match(accountsSource, /class="oauth-account-draft"/);
  assert.match(accountsSource, /label="账户显示名称（可选）"/);
  assert.match(accountsSource, /v-model="form\.name"/);
  assert.match(accountsSource, /label="备注（可选）"/);
  assert.match(accountsSource, /v-model="form\.notes"/);
  assert.match(accountsSource, /仅保存在本机账户池，不会发送到 OAuth 或模型上游/);
});

test("account center exposes a direct OAuth entry and preserves default-selection intent during profile loading", () => {
  assert.match(accountsSource, /XIASS 上游账户中心/);
  assert.match(accountsSource, /这不是外部客户端的原生账号登录或会话导入/);
  assert.match(accountsSource, /不会扫描、接管或导入其他客户端的登录会话/);
  assert.match(accountsSource, /async function openOAuthNew\(\)/);
  assert.match(accountsSource, /await openNew\(\);/);
  assert.match(accountsSource, /onTypeChange\("oauth"\);/);
  assert.match(accountsSource, /const oauthProfilesAutoSelectRequested = ref\(false\);/);
  assert.match(accountsSource, /if \(autoSelectDefault\) oauthProfilesAutoSelectRequested\.value = true;/);
  assert.match(accountsSource, /@click="openOAuthNew">OAuth 登录<\/Button>/);
});

test("OAuth chooser copy does not advertise provider presets that the native profile list hides", () => {
  assert.match(accountsSource, /当前内置一键登录仅开放 OpenAI \/ Codex/);
  assert.doesNotMatch(accountsSource, /也可选择 Claude、Grok 等安全预设/);
});

test("account editor keeps OAuth, API credentials and JSON import as explicit peer entry modes", () => {
  assert.match(accountsSource, /const credentialEntryOptions = \[/);
  assert.match(accountsSource, /\{ label: "OAuth 授权", value: "oauth" \}/);
  assert.match(accountsSource, /\{ label: "API Key \/ Token", value: "token" \}/);
  assert.match(accountsSource, /\{ label: "账户 JSON", value: "json" \}/);
  assert.match(accountsSource, /const credentialEntryMode = computed/);
  assert.match(accountsSource, /function selectCredentialEntryMode\(mode\)/);
  assert.match(accountsSource, /onTypeChange\("oauth"\)/);
  assert.match(accountsSource, /onTypeChange\("auth_json"\)/);
  assert.match(accountsSource, /onTypeChange\("api_key"\)/);
  assert.match(accountsSource, /class="credential-entry-selector"/);
});

test("OAuth guidance remains readable in light and dark themes", () => {
  assert.match(accountsSource, /\.oauth-profile-description \{[^}]*color: var\(--text-secondary\);[^}]*font-size: 12px;/s);
  assert.match(accountsSource, /\.oauth-profile-empty, \.oauth-profile-selected, \.oauth-auto-callback \{[^}]*font-size: 12\.5px;/s);
  assert.match(accountsSource, /\.oauth-boundary-note \{[^}]*color: var\(--text-secondary\);[^}]*font-size: 12px;/s);
  assert.match(accountsSource, /\.oauth-boundary-note strong \{[^}]*color: var\(--text-primary\);[^}]*font-size: 12px;/s);
  assert.doesNotMatch(accountsSource, /\.oauth-profile-description \{[^}]*font-size: (?:9|10|10\.5|11)px;/s);
});
