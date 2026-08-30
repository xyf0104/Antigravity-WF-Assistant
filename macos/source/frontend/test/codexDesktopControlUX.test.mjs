import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modalSource = await readFile(new URL("../src/components/CodexConfigurationModal.vue", import.meta.url), "utf8");
const appStateSource = await readFile(new URL("../src/state/appState.js", import.meta.url), "utf8");

function sourceSlice(source, startText, endText) {
  const start = source.indexOf(startText);
  assert.notEqual(start, -1, `missing ${startText}`);
  const end = source.indexOf(endText, start);
  assert.notEqual(end, -1, `missing ${endText}`);
  return source.slice(start, end);
}

function desktopTemplate() {
  const start = modalSource.indexOf('<section class="desktop-control-section"');
  assert.notEqual(start, -1, "Codex Desktop control section is missing");
  const end = modalSource.indexOf("</section>", start);
  assert.notEqual(end, -1, "Codex Desktop control section is incomplete");
  return modalSource.slice(start, end + "</section>".length);
}

test("Codex Desktop bridge feature-detects current and compatibility method names", () => {
  const bridge = sourceSlice(appStateSource, "const codexDesktopMethodAliases", "// ─── Claude Code user settings");
  assert.match(bridge, /GetCodexDesktopControlStatus/);
  assert.match(bridge, /GetCodexDesktopStatus/);
  assert.match(bridge, /SelectCodexDesktopInstallation/);
  assert.match(bridge, /SelectCodexDesktopApp/);
  assert.match(bridge, /SelectCodexDesktopInstallationPath/);
  assert.match(bridge, /manualPath/);
  assert.match(bridge, /LaunchCodexDesktop/);
  assert.match(bridge, /StopCodexDesktop/);
  assert.match(bridge, /RestartCodexDesktop/);
  assert.match(bridge, /codexDesktopConfirmationPhrase/);
  assert.match(bridge, /CONFIRM_CODEX_DESKTOP_LIFECYCLE/);
  assert.match(bridge, /codexDesktopUnavailableResult/);
  assert.doesNotMatch(bridge, /console\.(?:log|error|warn)/);
});

test("Codex Desktop modal requires direct user actions and lifecycle confirmation", () => {
  const template = desktopTemplate();
  assert.match(template, /Codex Desktop 协作/);
  assert.match(template, /选择并验证 App/);
  assert.match(template, /自动检测不到？粘贴本机 App 路径/);
  assert.match(template, /manualCodexDesktopPath/);
  assert.match(template, /useManualCodexDesktopPath/);
  assert.match(template, /打开 Codex/);
  assert.match(template, /退出 Codex/);
  assert.match(template, /重新启动/);
  assert.match(template, /confirmDesktopAction\('stop'\)/);
  assert.match(template, /confirmDesktopAction\('restart'\)/);
  assert.match(template, /aria-label="Codex Desktop 控制"/);
  assert.match(template, /刷新 Codex Desktop 状态/);
  assert.match(modalSource, /function confirmDesktopAction\(action\)/);
  assert.match(modalSource, /window\.confirm/);
});

test("Codex Desktop renderer keeps the native response redacted", () => {
  const template = desktopTemplate();
  const normalizer = sourceSlice(modalSource, "function normalizeDesktopStatus", "function applyDesktopStatus");
  for (const unsafeProperty of [
    "installation.path",
    "installation.executablePath",
    "installation.root",
    "raw.path",
    "raw.pid",
    "raw.processId",
    "raw.arguments",
    "raw.credentials",
    "raw.apiKey",
    "raw.token",
  ]) {
    assert.doesNotMatch(normalizer, new RegExp(unsafeProperty.replace(/[.]/g, "\\.")));
  }
  assert.doesNotMatch(template, /desktopStatus\.(?:path|pid|processId|credentials|apiKey|token)/);
  assert.match(modalSource, /safeDesktopVersion/);
  assert.match(modalSource, /不会写入状态、日志或诊断/);
});

test("pasted Codex Desktop paths are one-shot and never enter shared renderer state", () => {
  const action = sourceSlice(modalSource, "async function useManualCodexDesktopPath", "async function runDesktopAction");
  assert.match(action, /selectCodexDesktopPath\(selectedPath\)/);
  assert.match(action, /manualCodexDesktopPath\.value = ""/);
  assert.match(action, /selectedPath = ""/);
  assert.doesNotMatch(action, /localStorage|sessionStorage|state\.|console\.(?:log|error|warn)/);
  assert.match(modalSource, /onBeforeUnmount\(\(\) => \{\s*manualCodexDesktopPath\.value = ""/s);
});

test("provider changes guide a fresh safe history check instead of replaying history automatically", () => {
  const historyStart = modalSource.indexOf('<section class="history-section">');
  assert.notEqual(historyStart, -1, "history section is missing");
  const historyEnd = modalSource.indexOf("</section>", historyStart);
  const history = modalSource.slice(historyStart, historyEnd + "</section>".length);
  assert.match(history, /安全顺序/);
  assert.match(history, /historyCompatibilityState/);
  assert.match(history, /historyRepairBlockedByDesktop/);
  assert.match(history, /不会自动扫描或重写会话/);
  assert.match(modalSource, /markProviderChange\(previousProvider, "xiass_tools"\)/);
  assert.match(modalSource, /const desktop = await refreshCodexDesktopStatus\(\)/);
  assert.match(modalSource, /desktop\.bridgeAvailable && desktop\.running/);
  assert.match(modalSource, /if \(providerChangePending\.value\) historyCompatibilityChecked\.value = true/);
  const historyRestore = sourceSlice(modalSource, "async function runHistoryBackupAction", "async function refreshAfterHistoryAction");
  assert.match(historyRestore, /action === "restore"/);
  assert.match(historyRestore, /await refreshCodexDesktopStatus\(\)/);
  assert.match(historyRestore, /desktop\.bridgeAvailable && desktop\.running/);
});

test("Codex Desktop controls retain visible keyboard focus in every theme", () => {
  assert.match(modalSource, /\.desktop-refresh:focus-visible, \.desktop-actions :deep\(\.btn:focus-visible\)/);
  assert.match(modalSource, /var\(--accent-strong\)/);
  assert.match(modalSource, /var\(--bg-inset\)/);
});

test("advanced lifecycle bridge stays opt-in and keeps credentials request-local", () => {
  const lifecycleBridge = sourceSlice(appStateSource, "const codexLifecycleMethodAliases", "// ─── Claude Code user settings");
  assert.match(lifecycleBridge, /ApplyCodexConfigurationWithLifecycle/);
  assert.match(lifecycleBridge, /ApplyCodexXIASSSelectionWithLifecycle/);
  assert.match(lifecycleBridge, /lifecycleInputWithConfirmation/);
  assert.match(lifecycleBridge, /confirmation: confirmed \? codexDesktopConfirmationPhrase : ""/);
  assert.match(lifecycleBridge, /callCodexLifecycle/);
  assert.doesNotMatch(lifecycleBridge, /localStorage|state\.|console\.(?:log|error|warn)/);
});

test("advanced lifecycle UI requires acknowledgement and a second confirmation", () => {
  const start = modalSource.indexOf('<section class="lifecycle-section"');
  assert.notEqual(start, -1, "advanced lifecycle section is missing");
  const end = modalSource.indexOf("</section>", start);
  const template = modalSource.slice(start, end + "</section>".length);
  assert.match(template, /高级：安全保存、检查历史并启动 Codex/);
  assert.match(template, /检查 Provider 兼容历史/);
  assert.match(template, /完成后启动 Codex Desktop/);
  assert.match(template, /lifecycleAcknowledged/);
  assert.match(template, /lifecycleActionEnabled/);
  assert.match(template, /confirmLifecycleApply/);
  assert.match(modalSource, /function confirmLifecycleApply\(\)/);
  assert.match(modalSource, /window\.confirm\(`\$\{desktopDetail\}/);
  assert.match(modalSource, /未保存的工作可能丢失/);
});

test("advanced lifecycle makes provider history repair optional and skips scans when the provider is unchanged", () => {
  const templateStart = modalSource.indexOf('<section class="lifecycle-section"');
  const templateEnd = modalSource.indexOf("</section>", templateStart);
  const template = modalSource.slice(templateStart, templateEnd + "</section>".length);
  assert.match(template, /v-if="providerWillChange"/);
  assert.match(template, /v-model="lifecycleRepairHistory"/);
  assert.match(template, /Provider 未变更。本高级操作不会触发全量历史扫描/);
  assert.match(modalSource, /repairHistoryOnProviderChange: providerChanged && lifecycleRepairHistory\.value/);
  assert.match(modalSource, /Provider 未变更，未触发全量历史扫描/);
});

test("advanced lifecycle reduces the result to safe transaction facts and clears the local API key on success", () => {
  const reducer = sourceSlice(modalSource, "function normalizeLifecycleStatus", "function providerBaseURL");
  assert.match(reducer, /raw\.configuration/);
  assert.match(reducer, /raw\.desktop/);
  assert.doesNotMatch(reducer, /raw\.(?:message|path|pid|processId|apiKey|token|callback)/);

  const action = sourceSlice(modalSource, "async function applyLifecycleConfiguration", "async function runConfigurationBackupAction");
  assert.match(action, /applyCodexXIASSSelectionWithLifecycle/);
  assert.match(action, /applyCodexConfigurationWithLifecycle/);
  assert.match(action, /draft\.value\.api_key = ""/);
  assert.match(action, /lifecycleInput\.config\.api_key = ""/);
  assert.match(action, /refreshAfterHistoryAction\(\)/);
  assert.match(action, /refreshCodexDesktopStatus\(\)/);
  assert.doesNotMatch(action, /result\?\.message|result\.message|raw\.message|console\.(?:log|error|warn)/);
});

test("advanced lifecycle outcome communicates rollback and fail-closed desktop states without native diagnostics", () => {
  const copy = sourceSlice(modalSource, "function lifecycleOutcomeCopy", "function applyLifecycleStatus");
  assert.match(copy, /已回滚/);
  assert.match(copy, /Codex 已保持关闭/);
  assert.match(copy, /手动恢复/);
  assert.doesNotMatch(copy, /message|path|pid|token|key|callback/i);
});
