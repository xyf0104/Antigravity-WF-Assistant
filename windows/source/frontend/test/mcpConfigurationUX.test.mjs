import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modalSource = await readFile(new URL("../src/components/MCPConfigurationModal.vue", import.meta.url), "utf8");
const appStateSource = await readFile(new URL("../src/state/appState.js", import.meta.url), "utf8");
const appSource = await readFile(new URL("../src/App.vue", import.meta.url), "utf8");
const toolsSource = await readFile(new URL("../src/views/Tools.vue", import.meta.url), "utf8");

function sourceSlice(source, startText, endText) {
  const start = source.indexOf(startText);
  assert.notEqual(start, -1, `missing ${startText}`);
  const end = source.indexOf(endText, start);
  assert.notEqual(end, -1, `missing ${endText}`);
  return source.slice(start, end);
}

test("MCP bridge retains generic compatibility while preferring fixed target-scoped native calls", () => {
  assert.match(appStateSource, /export async function getMCPConfiguration\(target\) \{\s*return call\("GetMCPConfiguration", target\);\s*\}/);
  assert.match(appStateSource, /export async function applyMCPConfiguration\(input\) \{\s*return call\("ApplyMCPConfiguration", input\);\s*\}/);
  const scoped = sourceSlice(appStateSource, "const mcpTargetMethods", "let dashboardRefreshPromise");
  for (const method of [
    "GetCursorMCPConfiguration",
    "ApplyCursorMCPConfiguration",
    "ListCursorMCPBackups",
    "RestoreCursorMCPBackup",
    "DeleteCursorMCPBackup",
    "GetWindsurfMCPConfiguration",
    "ApplyWindsurfMCPConfiguration",
    "ListWindsurfMCPBackups",
    "RestoreWindsurfMCPBackup",
    "DeleteWindsurfMCPBackup",
  ]) {
    assert.match(scoped, new RegExp(method));
  }
  assert.match(scoped, /export async function getTargetMCPConfiguration\(target\)/);
  assert.match(scoped, /export async function applyTargetMCPConfiguration\(target, remoteURL\)/);
  assert.match(scoped, /return getMCPConfiguration\(normalized\);/);
  assert.match(scoped, /return applyMCPConfiguration\(\{ target: normalized, remoteUrl: scopedInput\.remoteUrl \}\);/);
  assert.match(scoped, /export async function listTargetMCPBackups\(target\)/);
  assert.match(scoped, /export async function restoreTargetMCPBackup\(target, backupID\)/);
  assert.match(scoped, /export async function deleteTargetMCPBackup\(target, backupID\)/);
  assert.doesNotMatch(appStateSource, /mcpRemoteUrl|mcpEndpoint|mcpConfiguration:\s*\{/);
});

test("MCP modal keeps the endpoint local, clears it before native result rendering, and is persistent", () => {
  assert.match(modalSource, /const remoteURL = ref\(""\)/);
  assert.match(modalSource, /const request = \{ remoteUrl: remoteURL\.value \}/);
  assert.match(modalSource, /clearEndpoint\(\);\s*try \{\s*const result = await applyTargetMCPConfiguration\(props\.target, request\.remoteUrl\)/);
  assert.match(modalSource, /request\.remoteUrl = ""/);
  assert.match(modalSource, /persistent :closable="!busy"/);
  assert.doesNotMatch(modalSource, /\blocalStorage\s*\.|state\.[A-Za-z]+\s*=\s*remoteURL|snapshot\.remoteUrl|snapshot\.endpoint|data\?\.message|result\?\.message/);
});

test("MCP UI describes only the documented global configuration boundary", () => {
  assert.match(modalSource, /不读取或改写账号、Cookie、令牌、聊天记录、数据库或其他 MCP 条目/);
  assert.match(modalSource, /只支持 HTTPS，或无凭据的本机 localhost\/回环 HTTP 地址/);
  assert.match(modalSource, /含有环境变量、请求头、认证信息或其他敏感字段/);
  assert.doesNotMatch(modalSource, /OAuth 登录|账户额度|账号池/);
});

test("Tools routes Cursor and Windsurf configuration into the dedicated MCP modal", () => {
  assert.match(appSource, /import MCPConfigurationModal/);
  assert.match(appSource, /agentID === "cursor" \|\| agentID === "windsurf"/);
  assert.match(appSource, /mcpConfigurationTarget\.value = agentID/);
  assert.match(appSource, /<MCPConfigurationModal/);
});

test("a verified-but-protected configuration is labelled as unavailable rather than unimplemented", () => {
  assert.match(toolsSource, /function configurationActionLabel\(platform\)/);
  assert.match(toolsSource, /item\.available \? "配置" : "暂不可配置"/);
  assert.match(toolsSource, /return "等待接入"/);
});

test("MCP modal presents only checked recovery-point metadata", () => {
  const recoveryStart = modalSource.indexOf('<details class="recovery-points"');
  assert.notEqual(recoveryStart, -1, "recovery point panel is missing");
  const recoveryEnd = modalSource.indexOf("</details>", recoveryStart);
  const recoveryTemplate = modalSource.slice(recoveryStart, recoveryEnd + "</details>".length);
  assert.match(recoveryTemplate, /经过校验的恢复点/);
  assert.match(recoveryTemplate, /formatRecoveryTime\(backup\.createdAt\)/);
  assert.match(recoveryTemplate, /backup\.originalExisted/);
  assert.match(recoveryTemplate, /recoveryReasonLabel\(backup\.reason\)/);
  assert.doesNotMatch(recoveryTemplate, /\{\{\s*backup\.id\s*\}\}|backup\.(?:path|url|remoteUrl|raw|json|headers|env|token|key)/i);

  const sanitizer = sourceSlice(modalSource, "function safeRecoveryPoint", "function applyBackupList");
  assert.match(sanitizer, /backup\.id/);
  assert.match(sanitizer, /backup\.createdAt/);
  assert.match(sanitizer, /backup\.reason/);
  assert.match(sanitizer, /backup\.originalExisted/);
  assert.doesNotMatch(sanitizer, /backup\.(?:path|url|remoteUrl|raw|json|headers|env|token|key)/i);
});

test("recovery restore and deletion require separate explicit confirmation", () => {
  assert.match(modalSource, /function confirmRecoveryRestore\(backupID\)/);
  assert.match(modalSource, /function confirmRecoveryDelete\(backupID\)/);
  assert.match(modalSource, /window\.confirm\(`将恢复选定的/);
  assert.match(modalSource, /window\.confirm\(`将删除选定的/);
  assert.match(modalSource, /只有这次确认的操作会执行/);
  assert.match(modalSource, /void runRecoveryAction\("restore", backupID\)/);
  assert.match(modalSource, /void runRecoveryAction\("delete", backupID\)/);
});

test("successful MCP mutations refresh both status and recovery points before reporting an agent change", () => {
  const save = sourceSlice(modalSource, "async function save", "async function runRecoveryAction");
  const recovery = sourceSlice(modalSource, "async function runRecoveryAction", "function confirmRecoveryRestore");
  assert.match(save, /await refreshAfterMutation\(\)/);
  assert.match(save, /emit\("changed"\)/);
  assert.match(recovery, /await refreshAfterMutation\(\)/);
  assert.match(recovery, /emit\("changed"\)/);
  assert.match(modalSource, /getTargetMCPConfiguration\(props\.target\)/);
  assert.match(modalSource, /refreshBackups\(\{ quiet: true \}\)/);
});

test("MCP recovery UI has theme-safe keyboard focus and fixed generic error copy", () => {
  assert.match(modalSource, /\.recovery-points summary:focus-visible/);
  assert.match(modalSource, /\.recovery-actions :deep\(\.btn:focus-visible\), \.recovery-refresh:focus-visible/);
  assert.match(modalSource, /var\(--teal\)/);
  assert.match(modalSource, /var\(--bg-inset\)/);
  const recovery = sourceSlice(modalSource, "async function runRecoveryAction", "function confirmRecoveryRestore");
  assert.match(recovery, /当前设置已保持不变/);
  assert.match(recovery, /现有设置未被修改/);
  assert.doesNotMatch(recovery, /result\?\.message|result\.message|cause\.message|console\.(?:log|error|warn)/);
});
