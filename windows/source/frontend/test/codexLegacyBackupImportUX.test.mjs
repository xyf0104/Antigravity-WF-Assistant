import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modalSource = await readFile(new URL("../src/components/CodexConfigurationModal.vue", import.meta.url), "utf8");
const appStateSource = await readFile(new URL("../src/state/appState.js", import.meta.url), "utf8");

function legacyBackupTemplate(source) {
  const start = source.indexOf('<details v-if="legacyBackupItems.length || legacyBackupWarning"');
  assert.notEqual(start, -1, "legacy backup section is missing");
  const end = source.indexOf("</details>", start);
  assert.notEqual(end, -1, "legacy backup section is incomplete");
  return source.slice(start, end + "</details>".length);
}

test("legacy backup bridge calls use dedicated opaque-ID import methods", () => {
  assert.match(
    appStateSource,
    /export async function importCodexLegacyConfigBackup\(sourceID\) \{\s*return call\("ImportCodexLegacyConfigBackup", sourceID\);\s*\}/,
  );
  assert.match(
    appStateSource,
    /export async function importCodexLegacyHistoryBackup\(sourceID\) \{\s*return call\("ImportCodexLegacyHistoryBackup", sourceID\);\s*\}/,
  );
});

test("Codex configuration modal separates legacy config and history imports", () => {
  assert.match(modalSource, /importCodexLegacyConfigBackup/);
  assert.match(modalSource, /importCodexLegacyHistoryBackup/);
  assert.match(modalSource, /legacyConfigurationBackups/);
  assert.match(modalSource, /legacyHistoryBackups/);
  assert.match(modalSource, /confirmLegacyConfigurationImport/);
  assert.match(modalSource, /confirmLegacyHistoryImport/);
  assert.match(modalSource, /runLegacyBackupImport\("config", sourceID\)/);
  assert.match(modalSource, /runLegacyBackupImport\("history", sourceID\)/);
  assert.match(modalSource, /不会自动恢复/);
});

test("legacy backup UI displays only safe metadata and generic failures", () => {
  const template = legacyBackupTemplate(modalSource);
  assert.match(template, /legacyBackupItems/);
  assert.match(template, /legacyBackupWarning/);
  assert.doesNotMatch(template, /backup\.message/);
  for (const unsafeField of [
    "config_path",
    "source_path",
    "backup_path",
    "original_line",
    "updated_line",
    "snapshot",
    "api_key",
    "experimental_bearer_token",
  ]) {
    assert.doesNotMatch(template, new RegExp(`\\b${unsafeField}\\b`));
  }

  const actionStart = modalSource.indexOf("async function runLegacyBackupImport");
  const actionEnd = modalSource.indexOf("function confirmLegacyConfigurationImport", actionStart);
  const action = modalSource.slice(actionStart, actionEnd);
  assert.match(action, /导入旧版备份失败。该备份未通过安全校验或未能完成导入。/);
  assert.doesNotMatch(action, /result\?\.message|cause(?:\.message)?|\.stack/);
});

test("legacy backup styles are theme-safe", () => {
  assert.match(modalSource, /\.legacy-backup-warning \{[^}]*var\(--orange\)[^}]*var\(--text-secondary\)/);
  assert.match(modalSource, /\.legacy-backup-group \{[^}]*display: grid/);
});
