import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

const [modalSource, appStateSource] = await Promise.all([
  readFile(new URL("../src/components/CodexConfigurationModal.vue", import.meta.url), "utf8"),
  readFile(new URL("../src/state/appState.js", import.meta.url), "utf8"),
]);

test("Codex can select, discover and apply a redacted saved account through the native bridge", () => {
  assert.match(appStateSource, /call\("GetCodexAccountCandidates"\)/);
  assert.match(appStateSource, /call\("DiscoverCodexAccountModels", id\)/);
  assert.match(appStateSource, /call\("ApplyCodexConfigurationFromAccount", input\)/);
  assert.match(appStateSource, /ApplyCodexConfigurationFromAccountWithLifecycle/);
  assert.match(modalSource, /getCodexAccountCandidates/);
  assert.match(modalSource, /discoverCodexAccountModels/);
  assert.match(modalSource, /applyCodexConfigurationFromAccount/);
  assert.match(modalSource, /applyCodexConfigurationFromAccountWithLifecycle/);
  assert.match(modalSource, /accountId:\s*candidate\.id/);
  assert.match(modalSource, /clearSavedCodexAccountRequest\(accountRequest\)/);
});

test("Codex saved-account UI keeps only allowlisted public account metadata", () => {
  assert.match(modalSource, /function normalizeSavedCodexAccountCandidate/);
  assert.match(modalSource, /label:\s*cleanSavedCodexAccountText\(value\?\.label\)/);
  assert.match(modalSource, /credentialMode:\s*cleanSavedCodexAccountText\(value\?\.credentialMode\)/);
  assert.match(modalSource, /models,/);
  assert.doesNotMatch(modalSource, /candidate\.(?:apiKey|apiUrl|headers|oauth|refreshToken)\b/);
  assert.doesNotMatch(modalSource, /localStorage|sessionStorage/);
  assert.match(modalSource, /clearSavedCodexAccountSelection\(\{ clearCandidates: true \}\)/);
});

test("Codex account selection stays mutually exclusive with one-time and manual credentials", () => {
  assert.match(modalSource, /cancelXIASSKeySelection\(\);\n\s*draft\.value\.api_key = "";\n\s*selectedSavedCodexAccountID\.value = candidate\.id/);
  assert.match(modalSource, /:disabled="usingSavedCodexAccount \|\| xiassSelectionReady"/);
  assert.match(modalSource, /v-if="!usingSavedCodexAccount && !xiassSelectionReady"/);
  assert.match(modalSource, /每次保存前都会在原生层重新验证/);
  assert.match(modalSource, /OAuth 与刷新令牌不会进入界面/);
});
