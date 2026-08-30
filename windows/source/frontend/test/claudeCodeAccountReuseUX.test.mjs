import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

const [modalSource, appStateSource] = await Promise.all([
  readFile(new URL("../src/components/ClaudeCodeConfigurationModal.vue", import.meta.url), "utf8"),
  readFile(new URL("../src/state/appState.js", import.meta.url), "utf8"),
]);

test("Claude Code can apply a redacted saved account through the native bridge", () => {
  assert.match(appStateSource, /call\("GetClaudeCodeAccountCandidates"\)/);
  assert.match(appStateSource, /call\("ApplyClaudeCodeConfigurationFromAccount", input\)/);
  assert.match(modalSource, /getClaudeCodeAccountCandidates/);
  assert.match(modalSource, /applyClaudeCodeConfigurationFromAccount/);
  assert.match(modalSource, /accountId:\s*candidate\.id/);
  assert.match(modalSource, /enableGatewayModelDiscovery:\s*Boolean\(draft\.value\.enableGatewayModelDiscovery\)/);
  assert.match(modalSource, /request\.accountId\s*=\s*""/);
  assert.match(modalSource, /request\.model\s*=\s*""/);
});

test("Claude saved-account UI retains only redacted candidate fields", () => {
  assert.match(modalSource, /function normalizeSavedAccountCandidate/);
  assert.match(modalSource, /label:\s*cleanText\(value\?\.label\)/);
  assert.match(modalSource, /credentialMode:\s*cleanText\(value\?\.credentialMode\)/);
  assert.match(modalSource, /models,/);
  assert.doesNotMatch(modalSource, /candidate\.(?:apiKey|apiUrl|headers|oauth|refreshToken)\b/);
  assert.doesNotMatch(modalSource, /localStorage|sessionStorage/);
  assert.match(modalSource, /clearSavedAccountSelection\(\{ clearCandidates: true \}\)/);
});
