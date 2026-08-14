import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const appSource = await readFile(new URL("../src/App.vue", import.meta.url), "utf8");
const dashboardSource = await readFile(new URL("../src/views/Dashboard.vue", import.meta.url), "utf8");

test("global prompts cover updates, tray wakeups, and optional product reconnects", () => {
	assert.match(appSource, /wf:main-window-shown/);
	assert.match(appSource, /一键下载安装/);
	assert.match(appSource, /Antigravity 需要重新连接/);
	assert.match(appSource, /安装版本或连接规则已更新/);
	assert.match(appSource, /:closable="!state\.patchBusy"/);
	assert.match(appSource, /之后随时在首页手动连接并升级到最新补丁规则/);
	assert.match(appSource, /稍后再说/);
	assert.match(appSource, /dismissRepatchDialog/);
	assert.match(appSource, /handleRequiredReconnect/);
});

test("successful connection stays concise and launch closes the dialog first", () => {
	assert.match(dashboardSource, /已成功连接本地代理，现在可以启动 Antigravity/);
	const handler = dashboardSource.match(/async function handleSuccessLaunch\(target\) \{([\s\S]*?)\n\}/)?.[1] || "";
	assert.ok(handler.indexOf("successDialogOpen.value = false") < handler.indexOf("launchOrRestartAntigravity"));
	assert.doesNotMatch(handler, /successLaunchError/);
	assert.match(dashboardSource, /forcePatch: !state\.dashboardDeepScanComplete/);
});
