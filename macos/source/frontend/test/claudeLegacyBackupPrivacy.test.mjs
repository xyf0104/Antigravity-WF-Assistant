import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modalSource = await readFile(new URL("../src/components/ClaudeCodeConfigurationModal.vue", import.meta.url), "utf8");

function legacyTemplate(source) {
  const start = source.indexOf('<details v-if="legacyBackups.length || data?.legacyBackupWarning"');
  assert.notEqual(start, -1, "Claude legacy backup section is missing");
  const end = source.indexOf("</details>", start);
  assert.notEqual(end, -1, "Claude legacy backup section is incomplete");
  return source.slice(start, end + "</details>".length);
}

test("Claude legacy migration keeps source identifiers internal to the action", () => {
  const template = legacyTemplate(modalSource);
  assert.doesNotMatch(template, /\{\{\s*backup\.source\s*\}\}/);
  assert.match(modalSource, /migrateClaudeCodeLegacyBackup\(backup\.source, backup\.id\)/);
  assert.match(modalSource, /仅可复制已校验的旧版备份为新的恢复点/);
});
