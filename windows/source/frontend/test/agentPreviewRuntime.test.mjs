import assert from "node:assert/strict";
import test from "node:test";

globalThis.window = {};

const { agentPreviewRuntimeMessage, loadAgentStatuses, state } = await import("../src/state/appState.js");

test("a browser preview reports the missing native runtime without synthesizing agent states", async () => {
  state.agents.platforms = [{ agentId: "codex" }];
  state.agents.diagnostics = [{ code: "stale" }];

  const result = await loadAgentStatuses();

  assert.equal(result, null);
  assert.equal(state.agents.preview, true);
  assert.equal(state.agents.message, agentPreviewRuntimeMessage);
  assert.deepEqual(state.agents.platforms, []);
  assert.deepEqual(state.agents.diagnostics, []);
});

test("a real native status failure remains visible instead of becoming a preview message", async () => {
  globalThis.window = {
    go: {
      main: {
        App: {
          GetAgentStatuses: async () => {
            throw new Error("Native agent registry is unavailable.");
          },
        },
      },
    },
  };
  const originalConsoleError = console.error;
  console.error = () => {};

  try {
    const result = await loadAgentStatuses();
    assert.equal(result, null);
  } finally {
    console.error = originalConsoleError;
  }

  assert.equal(state.agents.preview, false);
  assert.equal(state.agents.message, "Native agent registry is unavailable.");
});
