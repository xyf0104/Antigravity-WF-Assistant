import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const appSource = await readFile(new URL("../src/App.vue", import.meta.url), "utf8");
const dashboardSource = await readFile(new URL("../src/views/Dashboard.vue", import.meta.url), "utf8");

test("global prompts cover updates, tray wakeups, and mandatory product reconnects", () => {
	assert.match(appSource, /wf:main-window-shown/);
	assert.match(appSource, /一键下载安装/);
	assert.match(appSource, /Antigravity 版本已变更/);
	assert.match(appSource, /:closable="false"/);
	assert.match(appSource, /handleRequiredReconnect/);
});

test("successful connection stays concise and launch closes the dialog first", () => {
	assert.match(dashboardSource, /已成功连接本地代理，现在可以启动 Antigravity/);
	const handler = dashboardSource.match(/async function handleSuccessLaunch\(target\) \{([\s\S]*?)\n\}/)?.[1] || "";
	assert.ok(handler.indexOf("successDialogOpen.value = false") < handler.indexOf("launchOrRestartAntigravity"));
	assert.doesNotMatch(handler, /successLaunchError/);
	assert.match(dashboardSource, /forcePatch: !state\.dashboardDeepScanComplete/);
});
