import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modalSource = await readFile(new URL("../src/components/CodexConfigurationModal.vue", import.meta.url), "utf8");

function sourceSlice(source, startText, endText) {
  const start = source.indexOf(startText);
  assert.notEqual(start, -1, `missing ${startText}`);
  const end = source.indexOf(endText, start);
  assert.notEqual(end, -1, `missing ${endText}`);
  return source.slice(start, end);
}

test("Codex status requires native managed-provider verification instead of only an active ID", () => {
  const state = sourceSlice(modalSource, "const activeXIASSProvider", "const xiassSelectionReady");
  assert.match(state, /snapshot\.value\?\.managed_provider_verified === true/);
  assert.match(state, /managedProviderNeedsRepair/);
  assert.match(state, /XIASS Tools 配置需要修复/);
  assert.match(state, /未通过完整性验证/);
  // The renderer receives a fixed issue enum but does not render it directly;
  // that prevents a future native value from turning into a diagnostic channel.
  assert.doesNotMatch(state, /managed_provider_issue/);
});

test("Codex configuration restore makes a fresh Desktop safety observation before writing", () => {
  const action = sourceSlice(modalSource, "async function runConfigurationBackupAction", "async function repairHistory");
  assert.match(action, /action === "restore"/);
  assert.match(action, /await refreshCodexDesktopStatus\(\)/);
  assert.match(action, /desktop\.bridgeAvailable && desktop\.running/);
  assert.match(action, /再恢复配置备份/);

  const confirmation = sourceSlice(modalSource, "function confirmConfigurationRestore", "function confirmHistoryRestore");
  assert.match(confirmation, /请先确认 Codex 已退出/);
});
