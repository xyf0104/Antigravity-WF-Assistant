import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const [pickerSource, accountsSource] = await Promise.all([
  readFile(new URL("../src/components/OAuthTOTPQuickPicker.vue", import.meta.url), "utf8"),
  readFile(new URL("../src/views/Accounts.vue", import.meta.url), "utf8"),
]);

test("OAuth TOTP quick picker uses only the existing credential-vault bridge", () => {
  assert.match(pickerSource, /import \{ generateTOTPCode, getTOTPEntries \} from "@\/state\/appState"/);
  assert.match(pickerSource, /await getTOTPEntries\(\)/);
  assert.match(pickerSource, /await generateTOTPCode\(entry\.id\)/);
  assert.match(pickerSource, /navigator\.clipboard\?\.writeText/);
  assert.match(pickerSource, /密钥不进入授权链接、账户、日志或诊断文件/);
  assert.doesNotMatch(pickerSource, /AddTOTPEntry|DeleteTOTPEntry|ExportTOTPEncrypted|ImportTOTPEncrypted|localStorage|sessionStorage|console\.(?:log|error|warn)|otpauth:\/\//i);
});

test("OAuth TOTP quick picker clears transient codes whenever the OAuth session closes", () => {
  assert.match(pickerSource, /function clearSensitiveViewState\(\) \{\s*entries\.value = \[\];\s*clearCodes\(\);\s*resetFeedback\(\);\s*\}/);
  assert.match(pickerSource, /watch\(\(\) => props\.open, \(open\) => \{[\s\S]*?clearSensitiveViewState\(\);/);
  assert.match(pickerSource, /onBeforeUnmount\(\(\) => \{[\s\S]*?clearSensitiveViewState\(\);/);
  assert.match(pickerSource, /<details v-if="open" class="oauth-totp-picker">/);
  assert.match(pickerSource, /取码并复制/);
});

test("Accounts renders the picker only while a live OAuth session is visible", () => {
  assert.match(accountsSource, /import OAuthTOTPQuickPicker from "@\/components\/OAuthTOTPQuickPicker\.vue"/);
  assert.match(accountsSource, /<OAuthTOTPQuickPicker :open="Boolean\(oauthSession\)" \/>/);
  const pickerIndex = accountsSource.indexOf("<OAuthTOTPQuickPicker");
  const callbackIndex = accountsSource.indexOf("v-if=\"showOAuthManualFallback\"");
  assert.ok(pickerIndex !== -1 && callbackIndex !== -1 && pickerIndex < callbackIndex, "quick picker should be available before a manual callback is pasted");
});
