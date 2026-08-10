import assert from "node:assert/strict";
import test from "node:test";

const listeners = new Map();
const targets = [
  {
    kind: "ide",
    appPath: "/Applications/Antigravity IDE.app",
    supported: true,
    connectionMode: "user-settings",
    patched: true,
    launchable: true,
  },
  {
    kind: "agent",
    appPath: "/Applications/Antigravity 2.0.app",
    supported: true,
    connectionMode: "user-settings",
    patched: true,
    launchable: true,
  },
];

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
        async ApplyAgentPatch() {
          listeners.get("wf:patch-progress")?.({
            phase: "complete",
            operation: "连接 Antigravity 2.0",
            percent: 100,
            message: "连接成功，可以打开 Antigravity",
          });
          return { ok: true, message: "Antigravity 2.0 已连接" };
        },
        async GetPatchStatus() {
          return { proxyManaged: true, targets };
        },
      },
    },
  },
};

const { applyAgentPatch, applyPatch, bindPatchEvents, state } = await import("../src/state/appState.js");

test("all-connect exposes live progress and refreshes both launch targets", async () => {
  bindPatchEvents();
  const result = await applyPatch();

  assert.equal(result.ok, true);
  assert.equal(state.patchBusy, false);
  assert.equal(state.patchProgress.phase, "complete");
  assert.equal(state.patchProgress.percent, 100);
  assert.equal(state.patchProgress.operation, "全部连接");
  assert.deepEqual(state.patch.targets.map((target) => target.kind), ["ide", "agent"]);
  assert.equal(state.patch.targets.every((target) => target.supported), true);
});

test("agent-only connect uses the dedicated native route and reports its operation", async () => {
  const result = await applyAgentPatch();

  assert.equal(result.ok, true);
  assert.equal(state.patchBusy, false);
  assert.equal(state.patchProgress.phase, "complete");
  assert.equal(state.patchProgress.percent, 100);
  assert.equal(state.patchProgress.operation, "连接 Antigravity 2.0");
});
