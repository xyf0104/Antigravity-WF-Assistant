import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modalSource = await readFile(new URL("../src/components/MCPConfigurationModal.vue", import.meta.url), "utf8");
const appStateSource = await readFile(new URL("../src/state/appState.js", import.meta.url), "utf8");
const appSource = await readFile(new URL("../src/App.vue", import.meta.url), "utf8");
const toolsSource = await readFile(new URL("../src/views/Tools.vue", import.meta.url), "utf8");

test("MCP bridge is an explicit, target-scoped native call", () => {
  assert.match(appStateSource, /export async function getMCPConfiguration\(target\) \{\s*return call\("GetMCPConfiguration", target\);\s*\}/);
  assert.match(appStateSource, /export async function applyMCPConfiguration\(input\) \{\s*return call\("ApplyMCPConfiguration", input\);\s*\}/);
  assert.doesNotMatch(appStateSource, /mcpRemoteUrl|mcpEndpoint|mcpConfiguration:\s*\{/);
});

test("MCP modal keeps the endpoint local, clears it before native result rendering, and is persistent", () => {
  assert.match(modalSource, /const remoteURL = ref\(""\)/);
  assert.match(modalSource, /const request = \{ target: props\.target, remoteUrl: remoteURL\.value \}/);
  assert.match(modalSource, /clearEndpoint\(\);\s*try \{\s*const result = await applyMCPConfiguration\(request\)/);
  assert.match(modalSource, /request\.remoteUrl = ""/);
  assert.match(modalSource, /persistent :closable="!busy"/);
  assert.doesNotMatch(modalSource, /localStorage|state\.[A-Za-z]+\s*=\s*remoteURL|snapshot\.remoteUrl|snapshot\.endpoint/);
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
