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
