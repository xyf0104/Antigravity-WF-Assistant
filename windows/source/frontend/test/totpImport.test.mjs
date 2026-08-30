import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const componentSource = await readFile(new URL("../src/components/TOTPSettingsCard.vue", import.meta.url), "utf8");
const appStateSource = await readFile(new URL("../src/state/appState.js", import.meta.url), "utf8");

test("encrypted TOTP import stays native, persistent, and out of shared state", () => {
  assert.match(appStateSource, /export function importTOTPEncrypted\(password\) \{\s*return call\("ImportTOTPEncrypted", password\);\s*\}/);
  assert.match(componentSource, /const importBackupOpen = ref\(false\)/);
  assert.match(componentSource, /const importBackupDraft = ref\(\{ password: "" \}\)/);
  assert.match(componentSource, /function openImportBackup\(\)/);
  assert.match(componentSource, /function closeImportBackup\(force = false\)/);
  assert.match(componentSource, /async function importEncryptedBackup\(\)/);
  assert.match(componentSource, /await importTOTPEncrypted\(importBackupDraft\.value\.password\)/);
  assert.match(componentSource, /<Modal :open="importBackupOpen" title="导入加密验证器备份" persistent/);
  assert.match(componentSource, /:closable="actionID !== 'import-backup'"/);
  assert.match(componentSource, /文件路径、密码和密钥不会进入日志、诊断或共享前端状态/);
  assert.match(componentSource, /importBackupDraft\.value = \{ password: "" \}/);
  assert.match(componentSource, /选择备份文件并导入/);
  const importFlow = componentSource.slice(componentSource.indexOf("function openImportBackup"), componentSource.indexOf("function formatEntry"));
  assert.doesNotMatch(importFlow, /localStorage|state\.[A-Za-z]+\s*=/);
});
