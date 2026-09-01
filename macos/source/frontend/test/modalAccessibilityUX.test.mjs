import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("../src/components/ui/Modal.vue", import.meta.url), "utf8");
const appSource = readFileSync(new URL("../src/App.vue", import.meta.url), "utf8");
const buttonSource = readFileSync(new URL("../src/components/ui/Button.vue", import.meta.url), "utf8");
const totpSource = readFileSync(new URL("../src/components/TOTPSettingsCard.vue", import.meta.url), "utf8");
const codexSource = readFileSync(new URL("../src/components/CodexConfigurationModal.vue", import.meta.url), "utf8");
const accountTestSource = readFileSync(new URL("../src/components/accounts/AccountTestModal.vue", import.meta.url), "utf8");
const accountsSource = readFileSync(new URL("../src/views/Accounts.vue", import.meta.url), "utf8");
const permissionsSource = readFileSync(new URL("../src/views/Permissions.vue", import.meta.url), "utf8");
const settingsSource = readFileSync(new URL("../src/views/Settings.vue", import.meta.url), "utf8");

test("shared modal traps keyboard focus and restores the previous control", () => {
  assert.match(source, /function handleDocumentKeydown\(event\)/);
  assert.match(source, /event\.key !== "Tab"/);
  assert.match(source, /previousFocus = document\.activeElement/);
  assert.match(source, /previousFocus\?\.isConnected/);
  assert.match(source, /tabindex="-1"/);
});

test("persistent editors ignore backdrop clicks while ordinary dialogs remain dismissible", () => {
  assert.match(source, /if \(!props\.persistent && props\.closable\) emit\("close"\)/);
  assert.match(source, /@click\.self="handleBackdropClick"/);
});

test("inline workbench sheets do not fade through the parent canvas", () => {
  assert.match(source, /<Transition name="mask" :css="!inline">/);
  assert.match(source, /<Transition name="sheet" appear :css="!inline">/);
});

test("inline workbench sheets participate in the host page scroll instead of creating a nested viewport", () => {
  assert.match(source, /\.mask\.inline \{[\s\S]*?position: static;[\s\S]*?height: auto;[\s\S]*?min-height: 0;/);
  assert.match(source, /\.sheet\.inline \{[\s\S]*?height: auto;[\s\S]*?overflow: visible;[\s\S]*?display: block;/);
  assert.match(source, /\.sheet\.inline > \.body \{[\s\S]*?overflow: visible;/);
});

test("inline workbenches relay wheel input to the host unless a local control can consume it", () => {
  assert.match(appSource, /function embeddedScrollableAncestorCanConsumeWheel\(target, deltaY\)/);
  assert.match(appSource, /function relayEmbeddedWheel\(event\)/);
  assert.match(appSource, /if \(embeddedScrollableAncestorCanConsumeWheel\(event\.target, deltaY\)\) return;/);
  assert.match(appSource, /type: "xiass-wf-scroll"/);
  assert.match(appSource, /window\.addEventListener\("wheel", relayEmbeddedWheel, \{ capture: true, passive: false \}\)/);
  assert.match(appSource, /window\.removeEventListener\("wheel", relayEmbeddedWheel, true\)/);
});

test("2FA page exposes one element root so page transitions do not drop animations", () => {
  assert.match(totpSource, /<template>\s*<div class="totp-page">/);
  assert.match(totpSource, /\.totp-page \{ min-width:0; \}/);
});

test("loading buttons keep their text name and announce busy state", () => {
  assert.match(buttonSource, /:aria-busy="loading \|\| undefined"/);
  assert.match(buttonSource, /class="loader spin" aria-hidden="true"/);
  assert.match(buttonSource, /<span class="btn-label"><slot \/><\/span>/);
  assert.doesNotMatch(buttonSource, /<slot v-else\s*\/>/);
});

test("one-shot credential and recovery fields expose explicit accessible names", () => {
  assert.match(codexSource, /aria-label="Codex Desktop 应用路径"/);
  assert.match(codexSource, /aria-label="XIASS API Key 选择回调地址"/);
  assert.match(accountTestSource, /aria-label="搜索测试模型"/);
  assert.match(accountsSource, /aria-label="账户 JSON"/);
  assert.match(permissionsSource, /aria-label="自定义命令规则"/);
});

test("account tests use vector status marks instead of structural Unicode glyphs", () => {
  assert.doesNotMatch(accountTestSource, /[▶↻▦◉◌]/);
  assert.match(accountTestSource, /class="terminal-result-icon"/);
  assert.match(accountTestSource, /class="test-action-icon"/);
});

test("settings and permissions announce asynchronous results", () => {
  assert.match(permissionsSource, /class="result-box error" role="alert"/);
  assert.match(permissionsSource, /class="result-box success" role="status" aria-live="polite"/);
  assert.match(settingsSource, /class="result-box error" role="alert"/);
  assert.match(settingsSource, /class="result-box success" role="status" aria-live="polite"/);
});
