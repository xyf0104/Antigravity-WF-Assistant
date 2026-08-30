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
    "RemoveCursorMCPConfiguration",
    "ListCursorMCPBackups",
    "RestoreCursorMCPBackup",
    "DeleteCursorMCPBackup",
    "GetWindsurfMCPConfiguration",
    "ApplyWindsurfMCPConfiguration",
    "RemoveWindsurfMCPConfiguration",
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
  const remove = sourceSlice(scoped, "export async function removeTargetMCPConfiguration", "export async function listTargetMCPBackups");
  assert.match(remove, /const result = await callTargetScopedMCP\(normalized, "remove"\);/);
  assert.match(remove, /return result \?\? mcpTargetScopedUnavailable\("remove"\);/);
  assert.doesNotMatch(remove, /call\("(?:GetMCPConfiguration|ApplyMCPConfiguration|RemoveMCPConfiguration)"/);
  assert.match(scoped, /export async function listTargetMCPBackups\(target\)/);
  assert.match(scoped, /export async function restoreTargetMCPBackup\(target, backupID\)/);
  assert.match(scoped, /export async function deleteTargetMCPBackup\(target, backupID\)/);
  assert.doesNotMatch(appStateSource, /mcpRemoteUrl|mcpEndpoint|mcpConfiguration:\s*\{/);
});

test("MCP modal keeps the endpoint local, clears it before native result rendering, and is persistent", () => {
  assert.match(modalSource, /const remoteURL = ref\(""\)/);
  assert.match(modalSource, /const request = \{ remoteUrl: remoteURL\.value \}/);
  const save = sourceSlice(modalSource, "async function save", "async function removeManagedConnection");
  assert.match(save, /clearEndpoint\(\);\s*try \{\s*const result = isProjectScope\.value/);
  assert.match(save, /applyCursorProjectMCPConfiguration\(activeSelectionID\(\), request\.remoteUrl\)/);
  assert.match(save, /applyTargetMCPConfiguration\(props\.target, request\.remoteUrl\)/);
  assert.match(modalSource, /request\.remoteUrl = ""/);
  const modalOpenTag = modalSource.match(/<Modal\s[^>]*>/)?.[0] || "";
  assert.match(modalOpenTag, /\bpersistent\b/);
  assert.match(modalOpenTag, /:inline="inline"/);
  assert.match(modalOpenTag, /:closable="!busy"/);
  assert.doesNotMatch(modalSource, /\blocalStorage\s*\.|state\.[A-Za-z]+\s*=\s*remoteURL|snapshot\.remoteUrl|snapshot\.endpoint|data\?\.message|result\?\.message/);
});

test("MCP removal is target-scoped, explicitly confirmed, and only presents the safe managed result", () => {
  assert.match(modalSource, /removeTargetMCPConfiguration/);
  assert.match(modalSource, /removeCursorProjectMCPConfiguration/);
  assert.match(modalSource, /const removing = ref\(false\)/);
  assert.match(modalSource, /const canRemove = computed\(\(\) => Boolean\(configurationEligible\.value && snapshot\.value\.managedServerConfigured\)\)/);
  assert.match(modalSource, /<Button v-if="snapshot\.managedServerConfigured" variant="danger"/);
  assert.match(modalSource, /移除 XIASS 连接/);
  assert.match(modalSource, /function confirmManagedRemoval\(\)/);
  assert.match(modalSource, /const location = isProjectScope\.value \? `项目“\$\{selectedProjectName\.value\}”` : `\$\{displayName\.value\} 全局 MCP 设置`;/);
  assert.match(modalSource, /window\.confirm\(`将只移除 \$\{location\}中名为 xiass-tools 的 XIASS Tools 保留条目；不会删除其他 MCP 条目。操作前会创建一个经过校验的恢复点，是否继续？`\)/);
  const removal = sourceSlice(modalSource, "async function removeManagedConnection", "async function runRecoveryAction");
  assert.match(removal, /clearEndpoint\(\);\s*removing\.value = true;/);
  assert.match(removal, /removeCursorProjectMCPConfiguration\(activeSelectionID\(\)\)/);
  assert.match(removal, /await removeTargetMCPConfiguration\(props\.target\)/);
  assert.match(removal, /await refreshAfterMutation\(\)/);
  assert.match(removal, /result\?\.result\?\.removed === true/);
  assert.match(removal, /其他 MCP 条目保持不变/);
  assert.doesNotMatch(removal, /result\?\.message|result\.message|data\?\.message|remoteURL\.value\s*=/);
});

test("MCP UI describes global and Cursor project configuration boundaries without advertising account access", () => {
  assert.match(modalSource, /不读取或改写账号、Cookie、令牌、聊天记录、数据库或其他 MCP 条目/);
  assert.match(modalSource, /只支持 HTTPS，或无凭据的本机 localhost\/回环 HTTP 地址/);
  assert.match(modalSource, /含有环境变量、请求头、认证信息或其他敏感字段/);
  assert.match(modalSource, /全局 MCP 面向本机所有 Cursor 工作区；项目 MCP 只作用于你明确选择的一个项目/);
  assert.match(modalSource, /只会写入该项目的 \.cursor\/mcp\.json；完整本机路径不会显示、保存或导出/);
  assert.match(modalSource, /v-if="isCursor" class="scope-ribbon"/);
  assert.doesNotMatch(modalSource, /OAuth 登录|账户额度|账号池/);
});

test("MCP writes require a verified recovery-point state and do not claim remote health", () => {
  assert.match(modalSource, /const recoveryPointsVerified = computed\(\(\) => !backupsLoading\.value && !backupUnavailable\.value && !backupError\.value\)/);
  assert.match(modalSource, /const configurationEligible = computed\(\(\) => Boolean\(\(!isProjectScope\.value \|\| hasProjectSelection\.value\) && data\.value\?\.canApply && recoveryPointsVerified\.value\)\)/);
  assert.match(modalSource, /const canApply = computed\(\(\) => Boolean\(configurationEligible\.value && remoteURL\.value\.trim\(\)\)\)/);
  assert.match(modalSource, /无法安全验证 MCP 恢复点；当前全局 MCP 设置保持只读/);
  assert.match(modalSource, /尚未测试远端 MCP 服务/);
  assert.doesNotMatch(modalSource, /MCP 远程连接已验证/);
});

test("Cursor project MCP uses only an opaque native selection and keeps Windsurf global-only", () => {
  const projectBridge = sourceSlice(appStateSource, "function cursorProjectMCPUnavailable", "let dashboardRefreshPromise");
  for (const method of [
    "ChooseCursorProjectMCPConfiguration",
    "GetCursorProjectMCPConfiguration",
    "ApplyCursorProjectMCPConfiguration",
    "RemoveCursorProjectMCPConfiguration",
    "ListCursorProjectMCPBackups",
    "RestoreCursorProjectMCPBackup",
    "DeleteCursorProjectMCPBackup",
  ]) {
    assert.match(projectBridge, new RegExp(`call\\("${method}"`));
  }
  assert.match(projectBridge, /selectionId: cursorProjectMCPSelectionID\(selectionID\)/);
  assert.doesNotMatch(projectBridge, /projectRoot|projectPath|directoryPath|filePath|headers|token|cookie|command|args|env/i);

  assert.match(modalSource, /const mcpScope = ref\("global"\)/);
  assert.match(modalSource, /const projectSelection = ref\(\{ id: "", name: "", expiresAt: "" \}\)/);
  assert.match(modalSource, /async function chooseProjectDirectory\(\)/);
  assert.match(modalSource, /await chooseCursorProjectMCPConfiguration\(\)/);
  assert.match(modalSource, /getCursorProjectMCPConfiguration\(activeSelectionID\(\)\)/);
  assert.match(modalSource, /listCursorProjectMCPBackups\(activeSelectionID\(\)\)/);
  assert.match(modalSource, /v-if="isCursor" class="scope-ribbon"/);
  assert.doesNotMatch(modalSource, /Windsurf[^\n]{0,80}项目 MCP|windsurf[^\n]{0,80}project/i);
});

test("Tools routes Cursor and Windsurf configuration into the dedicated MCP modal", () => {
  assert.match(appSource, /import MCPConfigurationModal/);
  assert.match(appSource, /agentID === "cursor" \|\| agentID === "windsurf"/);
  assert.match(appSource, /mcpConfigurationTarget\.value = agentID/);
  assert.match(appSource, /<MCPConfigurationModal/);
});

test("a protected configuration remains reachable for verified recovery actions", () => {
  assert.match(toolsSource, /label: "全局 MCP"[\s\S]*event: "configure"/);
  assert.match(toolsSource, /label: "项目 MCP"[\s\S]*event: "configure"/);
  assert.match(toolsSource, /label: "恢复点"[\s\S]*event: "configure"/);
  assert.match(toolsSource, /function functionActionDisabled\(action, platform\)/);
  assert.doesNotMatch(toolsSource, /!isAvailable\(.*configuration/);
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
  assert.match(sanitizer, /backup\.reason === "apply" \|\| backup\.reason === "remove" \|\| backup\.reason === "restore"/);
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

test("MCP removal recovery points are labelled without exposing configuration data", () => {
  const labeler = sourceSlice(modalSource, "function recoveryReasonLabel", "function formatRecoveryTime");
  assert.match(labeler, /reason === "remove"/);
  assert.match(labeler, /移除 XIASS 连接前的恢复点/);
  assert.doesNotMatch(labeler, /backup\.|url|remote|path|header|token|secret/i);
});
