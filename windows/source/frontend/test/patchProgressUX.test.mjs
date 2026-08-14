import assert from "node:assert/strict";
import test from "node:test";

const listeners = new Map();
const targets = [
  { kind: "ide", appPath: "C:/IDE", supported: true, patched: true, launchable: true },
  { kind: "agent", appPath: "C:/Agent", supported: true, patched: true, launchable: true },
];
let resolveStatusRefresh;
let statusRefreshStarted = false;

globalThis.window = {
  runtime: {
    EventsOn(name, callback) {
      listeners.set(name, callback);
    },
  },
  go: {
    main: {
      App: {
        async ApplyPatch() {
          listeners.get("wf:patch-progress")?.({
            phase: "patching",
            operation: "全部连接",
            percent: 64,
            message: "正在验证结构并安全写入补丁",
          });
          listeners.get("wf:patch-progress")?.({
            phase: "complete",
            operation: "全部连接",
            percent: 100,
            message: "连接成功，可以打开 Antigravity",
          });
          return { ok: true, message: "连接成功" };
        },
        async GetPatchStatus() {
			statusRefreshStarted = true;
			return new Promise((resolve) => {
				resolveStatusRefresh = () => resolve({ proxyManaged: true, targets });
			});
        },
      },
    },
  },
};

const { applyPatch, bindPatchEvents, state } = await import("../src/state/appState.js");

test("all-connect returns immediately while target verification refreshes in background", async () => {
  bindPatchEvents();
  const result = await applyPatch();

  assert.equal(result.ok, true);
  assert.equal(state.patchBusy, false);
  assert.equal(state.patchProgress.phase, "complete");
  assert.equal(state.patchProgress.percent, 100);
  assert.equal(state.patchProgress.operation, "全部连接");
	assert.equal(statusRefreshStarted, true);
	assert.deepEqual(state.patch.targets, []);
	resolveStatusRefresh();
	await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(state.patch.targets.map((target) => target.kind), ["ide", "agent"]);
});
