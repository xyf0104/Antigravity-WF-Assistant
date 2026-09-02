import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

globalThis.window = {};

const { agentPreviewRuntimeMessage, loadAgentStatuses, state } = await import("../src/state/appState.js");
const toolsSource = await readFile(new URL("../src/views/Tools.vue", import.meta.url), "utf8");

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

test("browser preview renders fallback Agent surfaces but disables every native action", () => {
  assert.doesNotMatch(toolsSource, /if \(props\.preview\) return \[\]/);
  assert.match(toolsSource, /const fallbackPlatforms = \[/);
  assert.match(toolsSource, /<div v-if="preview" class="tools-preview"/);
  assert.match(toolsSource, /class="agent-function-bar"/);
  assert.match(toolsSource, /class="agent-workbench"/);
  assert.match(toolsSource, /if \(props\.preview\) return true/);
  assert.match(toolsSource, /:disabled="loading \|\| preview"/);
  assert.match(toolsSource, /预览模式不会模拟本机能力/);
  assert.match(toolsSource, /state: platform\.state \|\| "unknown"/);
  assert.doesNotMatch(toolsSource, /state:\s*"ready"|state:\s*"detected"/);
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
