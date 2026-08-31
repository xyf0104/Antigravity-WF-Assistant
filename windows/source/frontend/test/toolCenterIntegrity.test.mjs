import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

const toolsSource = await readFile(new URL("../src/views/Tools.vue", import.meta.url), "utf8");
const appSource = await readFile(new URL("../src/App.vue", import.meta.url), "utf8");
const appStateSource = await readFile(new URL("../src/state/appState.js", import.meta.url), "utf8");
const codexModalSource = await readFile(new URL("../src/components/CodexConfigurationModal.vue", import.meta.url), "utf8");
const claudeModalSource = await readFile(new URL("../src/components/ClaudeCodeConfigurationModal.vue", import.meta.url), "utf8");
const mcpModalSource = await readFile(new URL("../src/components/MCPConfigurationModal.vue", import.meta.url), "utf8");
const modalSource = await readFile(new URL("../src/components/ui/Modal.vue", import.meta.url), "utf8");
const segmentedSource = await readFile(new URL("../src/components/ui/SegmentedControl.vue", import.meta.url), "utf8");
const agentIconSource = await readFile(new URL("../src/components/AgentIcon.vue", import.meta.url), "utf8");

test("tools center exposes only a guarded Cursor/Windsurf launch action", () => {
  assert.doesNotMatch(toolsSource, /platform\.launchable/);
  assert.match(toolsSource, /function canLaunch\(platform\)/);
  assert.match(toolsSource, /\["cursor", "windsurf"\]/);
	assert.match(toolsSource, /const macTarget/);
	assert.match(toolsSource, /const windowsTarget/);
  assert.match(toolsSource, /event: "launch"/);
  assert.match(toolsSource, /emit\(action\.event, platform\.agentId, action\.id\)/);
  assert.match(appSource, /handleAgentLaunch/);
  assert.match(appSource, /@launch="handleAgentLaunch"/);
  assert.match(appSource, /launchDetectedAgent/);
});

test("Cursor and Windsurf use a native-only verified application chooser", () => {
  assert.match(toolsSource, /manualSelections:/);
  assert.match(toolsSource, /label: "选择应用"[\s\S]*event: "choose"/);
  assert.match(toolsSource, /emit\(action\.event, platform\.agentId, action\.id\)/);
  assert.match(toolsSource, /选择应用/);
  assert.match(appSource, /handleAgentChoose/);
  assert.match(appSource, /@choose="handleAgentChoose"/);
  assert.match(appSource, /:manual-selections="state\.agents\.manualSelections"/);

  const start = appStateSource.indexOf("export async function selectAgentDesktopInstallation");
  assert.notEqual(start, -1, "native application selection bridge is missing");
  const end = appStateSource.indexOf("// ─── Codex local configuration", start);
  assert.notEqual(end, -1, "selection bridge boundary is missing");
  const action = appStateSource.slice(start, end);
  assert.match(action, /call\("SelectAgentDesktopInstallation", identifier\)/);
  assert.match(action, /\["cursor", "windsurf"\]\.includes\(identifier\)/);
  assert.doesNotMatch(action, /\bpath\b|OpenFileDialog|manual.*path/i);
  assert.doesNotMatch(action, /localStorage|sessionStorage|console\.(?:log|error|warn)/);
});

test("Claude configuration reduces successful native responses to fixed safe copy", () => {
  assert.doesNotMatch(claudeModalSource, /notice\.value\s*=\s*result\.message/);
  assert.match(claudeModalSource, /已安全保存，并创建可恢复备份/);
  assert.match(claudeModalSource, /已恢复用户设置备份/);
  assert.match(claudeModalSource, /backup\?\.created_at \|\| backup\?\.createdAt/);
  assert.doesNotMatch(claudeModalSource, /new Date\(backup\.createdAt\)/);
});

test("Claude saves require a verified rollback location while one-shot checks remain non-mutating", () => {
  assert.match(claudeModalSource, /const canManage = computed\(\(\) => valid\.value && data\.value\?\.ok === true\)/);
  assert.match(claudeModalSource, /canManage\.value\s*&&\s*draft\.value\.baseUrl\.trim\(\)/);
  assert.match(claudeModalSource, /当前 Claude Code 设置或恢复备份位置无法安全验证，因此禁止保存或改写/);
  assert.match(claudeModalSource, /:disabled="!canManage \|\| !readyToSave \|\| busy"/);
  assert.match(claudeModalSource, /模型目录获取和 Claude Messages 单次检查仍不会写入设置/);
});

test("tools distinguish available local actions from fetched models or proxy health", () => {
  assert.match(toolsSource, /<dt>可用本机操作<\/dt>/);
  assert.match(toolsSource, /"model-catalog": "手动模型获取"/);
  assert.match(toolsSource, /"local-proxy": "本地代理健康检查"/);
});

test("tools expose only implemented or bindable capabilities while Antigravity accounts remain isolated", () => {
  assert.match(toolsSource, /function visibleCapabilities\(platform\)/);
  assert.match(toolsSource, /"requires-binding": "待连接"/);
  assert.match(toolsSource, /\["not-implemented", "not-applicable"\]\.includes\(item\?\.availability\)/);
  assert.doesNotMatch(toolsSource, /oauth: "客户端原生 OAuth/);
  assert.doesNotMatch(toolsSource, /"two-factor-authentication": "双重验证"/);
  assert.match(toolsSource, /class="tool-capabilities"/);
  assert.match(toolsSource, /:title="item\.reason \|\| ''"/);
  assert.doesNotMatch(toolsSource, /usesUpstreamAccountPool|accountActionDescription|上游账户/);
  assert.match(appSource, /function handleAgentAccounts\(agentID\)/);
  assert.match(appSource, /activeModuleID\.value = "antigravity";\s*antigravityTab\.value = "accounts";/s);
  assert.match(appSource, /@accounts="handleAgentAccounts"/);
  assert.match(claudeModalSource, /使用已保存的上游账户/);
  assert.match(claudeModalSource, /Claude Messages 兼容/);
});

test("every Agent uses the shared local vector icon and only implemented actions are exposed", () => {
  assert.match(appSource, /import AgentIcon/);
  assert.match(toolsSource, /import AgentIcon/);
  assert.match(appSource, /<AgentIcon :agent-id="item\.id"/);
  assert.match(toolsSource, /<AgentIcon :agent-id="platform\.agentId"/);
  assert.match(agentIconSource, /agentId === 'antigravity'/);
  assert.match(agentIconSource, /agentId === 'codex'/);
  assert.match(agentIconSource, /import \{ siClaude, siCursor, siWindsurf \} from "simple-icons"/);
  assert.match(agentIconSource, /"claude-code": siClaude\.path/);
  assert.match(agentIconSource, /cursor: siCursor\.path/);
  assert.match(agentIconSource, /windsurf: siWindsurf\.path/);
  assert.doesNotMatch(appSource, /shortLabel:/);
  assert.match(toolsSource, /class="agent-function-bar"/);
  assert.match(toolsSource, /v-for="action in selectedFunctionActions"/);
  for (const label of ["Provider", "模型发现", "备份 / 恢复", "历史兼容", "Desktop", "网关", "模型测试", "全局 MCP", "项目 MCP", "恢复点", "诊断", "选择应用", "启动"]) {
    assert.match(toolsSource, new RegExp(`label: "${label.replace("/", "\\/")}"`));
  }
  assert.match(toolsSource, /action\.event === "launch" && !canLaunch\(platform\)/);
  assert.doesNotMatch(toolsSource, />OAuth</);
});

test("the left rail selects one independent Agent and Antigravity owns a horizontal sub-navigation", () => {
  assert.match(appSource, /const embeddedModuleID = embeddedParams\.get\("module"\) \|\| "antigravity"/);
  assert.match(appSource, /const activeModuleID = ref\(supportedEmbeddedModules\.has\(embeddedModuleID\) \? embeddedModuleID : "antigravity"\)/);
  assert.match(appSource, /const embeddedSection = embeddedParams\.get\("section"\) \|\| "dashboard"/);
  assert.match(appSource, /const antigravityTab = ref\(supportedAntigravitySections\.has\(embeddedSection\) \? embeddedSection : "dashboard"\)/);
  for (const agent of ["antigravity", "codex", "claude-code", "cursor", "windsurf"]) {
    assert.match(appSource, new RegExp(`id: "${agent}"`));
  }
  assert.match(appSource, /class="nav-stack" aria-label="Agent 模块"/);
  assert.match(appSource, /v-for="item in agentModules"/);
  assert.match(appSource, /grid-template-columns: 184px minmax\(0, 1fr\)/);
  assert.match(appSource, /grid-template-columns: 176px minmax\(0, 1fr\)/);
  assert.match(appSource, /\.agent-nav-copy \{[\s\S]*white-space: normal;/);
  assert.match(appSource, /class="agent-subnav" aria-label="Antigravity WF 功能导航"/);
  assert.match(appSource, /v-for="item in antigravityTabs"/);
  assert.match(appSource, /:selected-agent-id="activeModuleID"/);
  assert.match(toolsSource, /selectedAgentId:/);
  assert.match(toolsSource, /platform\.agentId === props\.selectedAgentId/);
  assert.match(toolsSource, /v-for="platform in selectedPlatforms"/);
  assert.match(toolsSource, /displayName: agentId === "antigravity" \? "Antigravity WF"/);
  assert.doesNotMatch(toolsSource, /agent-switcher|选择 Agent 模块/);
  assert.doesNotMatch(toolsSource, /selectedAgentID = ref/);
  assert.match(appSource, /\{ label: "总览", value: "dashboard"/);
  assert.match(appSource, /\{ label: "模型", value: "models"/);
  assert.match(appSource, /\{ label: "上游", value: "accounts"/);
  assert.match(appSource, /\{ label: "权限", value: "permissions"/);
  const wakeHandler = appSource.match(/function handleMainWindowShown\(\) \{([\s\S]*?)\n\}/)?.[1] || "";
  assert.doesNotMatch(wakeHandler, /activeModuleID\.value|antigravityTab\.value/);
  assert.match(wakeHandler, /loadPatchStatus/);
});

test("guarded tools remain reachable for read-only recovery and one-shot checks", () => {
  assert.match(toolsSource, /function triggerFunctionAction\(action, platform\)/);
  assert.match(toolsSource, /<span[\s\S]*?v-if="!action\.event"[\s\S]*?aria-current="page"/);
  assert.match(toolsSource, /<button[\s\S]*?v-else[\s\S]*?:data-action="action\.id"/);
  assert.match(toolsSource, /event: "configure"/);
  assert.match(toolsSource, /label: "备份 \/ 恢复"/);
  assert.match(toolsSource, /label: "恢复点"/);
  assert.match(toolsSource, /functionActionDisabled\(action, selectedPlatform\)/);
  assert.doesNotMatch(toolsSource, /!isAvailable\(.*configuration/);
  assert.match(claudeModalSource, /:disabled="!canManage \|\| !readyToSave \|\| busy"/);
});

test("shared configuration dialogs open the exact function selected in the Agent toolbar", () => {
  assert.match(toolsSource, /emit\(action\.event, platform\.agentId, action\.id\)/);
  assert.match(toolsSource, /:data-action="action\.id"/);
  assert.match(toolsSource, /grid-template-columns: repeat\(auto-fit, minmax\(126px, 1fr\)\)/);
  assert.match(appSource, /function handleAgentConfigure\(agentID, actionID = ""\)/);
  assert.match(appSource, /codexConfigurationSection\.value = actionID/);
  assert.match(appSource, /claudeCodeConfigurationSection\.value = actionID/);
  assert.match(appSource, /mcpConfigurationAction\.value = actionID/);
  assert.match(codexModalSource, /data-section="provider"/);
  assert.match(codexModalSource, /data-section="models"/);
  assert.match(codexModalSource, /data-section="history"/);
  assert.match(codexModalSource, /data-section="backups"/);
  assert.match(codexModalSource, /data-section="desktop"/);
  assert.match(codexModalSource, /target\.scrollIntoView\(\{ block: "start", behavior: "auto" \}\)/);
  assert.match(claudeModalSource, /data-section="gateway"/);
  assert.match(claudeModalSource, /data-section="model-test"/);
  assert.match(claudeModalSource, /target\.scrollIntoView\(\{ block: "start", behavior: "auto" \}\)/);
  assert.match(mcpModalSource, /props\.action === "project-mcp"/);
  assert.match(mcpModalSource, /props\.action !== "backups"/);
  assert.match(mcpModalSource, /target\.scrollIntoView\(\{ block: "start", behavior: "auto" \}\)/);
});

test("guarded modals retain their recovery surface before returning a read-only error", () => {
  assert.match(codexModalSource, /data\.value = result;\s*if \(!result\?\.ok\)/);
  assert.match(codexModalSource, /confirmConfigurationRestore\(backup\.id\)/);
  assert.match(claudeModalSource, /applyStatus\(result\);\s*if \(!result\?\.ok\)/);
  assert.match(claudeModalSource, /confirmBackupAction\('restore', backup\.id\)/);
  assert.match(mcpModalSource, /getTargetMCPConfiguration\(props\.target\),\s*refreshBackups\(\{ quiet: true \}\)/);
  assert.match(mcpModalSource, /:disabled="busy" :loading="backupActionID === `restore:\$\{backup\.id\}`"/);
});

test("Claude gateway checks keep credentials local and identify real protocol tests", () => {
  assert.match(claudeModalSource, /discoverClaudeCodeGatewayModels\(request\)/);
  assert.match(claudeModalSource, /testClaudeCodeGateway\(request\)/);
  assert.match(claudeModalSource, /clearGatewayRequest\(request\)/);
	assert.match(claudeModalSource, /resetVisibleCredentials\(\)/);
	assert.match(claudeModalSource, /gatewayModelDiscoveryBlocked/);
	assert.match(claudeModalSource, /不会自动删除这些用户管理的设置/);
  assert.match(claudeModalSource, /Claude Messages 实际请求成功/);
  assert.match(claudeModalSource, /apiKeyHelper 会由 Claude Code 自身执行|XIASS Tools 不执行任意本机命令/);
  assert.doesNotMatch(claudeModalSource, /localStorage/);
});

test("shared modal and segmented controls keep close actions accessible", () => {
  assert.match(modalSource, /type="button" class="x" aria-label="关闭"/);
  assert.doesNotMatch(modalSource, /transition:\s*all/);
  assert.match(segmentedSource, /type="button"/);
  assert.doesNotMatch(segmentedSource, /transition:\s*all/);
});
