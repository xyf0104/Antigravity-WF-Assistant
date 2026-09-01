import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

let resolveNativeCheck;
let checkCalls = 0;
let cancelCalls = 0;
let installCalls = 0;
let skipCalls = 0;
globalThis.window = {
	go: {
		main: {
			App: {
				CheckForUpdates: () => {
					checkCalls += 1;
					return new Promise((resolve) => { resolveNativeCheck = resolve; });
				},
				CancelUpdateCheck: async () => {
					cancelCalls += 1;
					return { ok: true, message: "已取消检查更新" };
				},
				InstallLatestUpdate: async () => {
					installCalls += 1;
					return { ok: true, message: "安装程序已启动" };
				},
				SkipUpdateVersion: async () => {
					skipCalls += 1;
					return { ok: true, message: "该版本已跳过" };
				},
				GetAppSettings: async () => ({ updates: { autoCheck: true, skippedVersion: "" } }),
			},
		},
	},
};

const {
	cancelUpdateCheck,
	checkForUpdates,
	installLatestUpdate,
	setEmbeddedRuntimeMode,
	skipUpdateVersion,
	state,
} = await import("../src/state/appState.js");
const appSource = await readFile(new URL("../src/App.vue", import.meta.url), "utf8");
const appStateSource = await readFile(new URL("../src/state/appState.js", import.meta.url), "utf8");
const standaloneVersion = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8")).version;

test("cancelling an update check clears the spinner and ignores a late native result", async () => {
	setEmbeddedRuntimeMode(false);
	const pending = checkForUpdates();
	assert.equal(state.update.checking, true);
	assert.equal(checkCalls, 1);

  const cancelled = await cancelUpdateCheck();
  assert.equal(cancelled.ok, true);
  assert.equal(cancelCalls, 1);
  assert.equal(state.update.checking, false);
  assert.equal(state.update.message, "已取消检查更新");

  resolveNativeCheck({
    ok: true,
    message: "旧请求不应覆盖取消状态",
    info: { latestVersion: "99.0.0", available: true },
  });
  await pending;

  assert.equal(state.update.checking, false);
	assert.equal(state.update.message, "已取消检查更新");
	assert.notEqual(state.update.info.latestVersion, "99.0.0");
});

test("embedded runtime never calls the helper updater while standalone install remains available", async () => {
	setEmbeddedRuntimeMode(true);
	const callsBefore = { checkCalls, cancelCalls, installCalls, skipCalls };

	for (const result of await Promise.all([
		checkForUpdates(),
		cancelUpdateCheck(),
		installLatestUpdate(),
		skipUpdateVersion("99.0.0"),
	])) {
		assert.equal(result.ok, false);
		assert.equal(result.disabled, true);
		assert.match(result.message, /主应用统一管理/);
	}
	assert.deepEqual({ checkCalls, cancelCalls, installCalls, skipCalls }, callsBefore);
	assert.equal(state.update.checking, false);
	assert.equal(state.update.installing, false);

	setEmbeddedRuntimeMode(false);
	const standaloneInstall = await installLatestUpdate();
	assert.equal(standaloneInstall.ok, true);
	assert.equal(installCalls, callsBefore.installCalls + 1);
});

test("embedded shell gates automatic checks, update events, modal visibility, and install handling", () => {
	assert.match(appSource, /setEmbeddedRuntimeMode\(embeddedMode\)/);
	assert.match(appSource, /const embeddedVersionPillLabel = "由宿主更新";/);
	assert.ok(
		appSource.includes(`const versionPillLabel = computed(() => embeddedMode ? embeddedVersionPillLabel : "v${standaloneVersion}");`),
		"standalone version pill must mirror the helper package version",
	);
	assert.match(appSource, /版本与更新由宿主 XIASS Tools 管理/);
	assert.doesNotMatch(appSource, /v1\.6\.8/);
	assert.match(appSource, /if \(!embeddedMode && state\.settings\?\.updates\?\.autoCheck !== false\)/);
	assert.match(appSource, /if \(embeddedMode\) return;[\s\S]*installLatestUpdate\(\)/);
	assert.match(appSource, /:open="!embeddedMode && updateDialogOpen"/);
	assert.match(appStateSource, /if \(!embeddedRuntimeMode\) \{[\s\S]*bindUpdateEvents\(\);[\s\S]*checkForUpdates\(\)/);
});
