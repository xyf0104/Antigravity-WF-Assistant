import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

const toolsSource = await readFile(new URL("../src/views/Tools.vue", import.meta.url), "utf8");
const appSource = await readFile(new URL("../src/App.vue", import.meta.url), "utf8");
const claudeModalSource = await readFile(new URL("../src/components/ClaudeCodeConfigurationModal.vue", import.meta.url), "utf8");
const modalSource = await readFile(new URL("../src/components/ui/Modal.vue", import.meta.url), "utf8");
const segmentedSource = await readFile(new URL("../src/components/ui/SegmentedControl.vue", import.meta.url), "utf8");

test("tools center does not advertise an unimplemented generic launch action", () => {
  assert.doesNotMatch(toolsSource, /platform\.launchable/);
  assert.doesNotMatch(toolsSource, /emit\('launch'/);
  assert.doesNotMatch(appSource, /handleAgentLaunch/);
  assert.doesNotMatch(appSource, /@launch=/);
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
