import assert from "node:assert/strict";
import test from "node:test";

let resolveNativeCheck;
let cancelCalls = 0;
globalThis.window = {
  go: {
    main: {
      App: {
        CheckForUpdates: () => new Promise((resolve) => { resolveNativeCheck = resolve; }),
        CancelUpdateCheck: async () => {
          cancelCalls += 1;
          return { ok: true, message: "已取消检查更新" };
        },
      },
    },
  },
};

const { cancelUpdateCheck, checkForUpdates, state } = await import("../src/state/appState.js");

test("cancelling an update check clears the spinner and ignores a late native result", async () => {
  const pending = checkForUpdates();
  assert.equal(state.update.checking, true);

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
