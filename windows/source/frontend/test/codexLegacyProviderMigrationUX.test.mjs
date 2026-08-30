import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modalSource = await readFile(new URL("../src/components/CodexConfigurationModal.vue", import.meta.url), "utf8");
const appStateSource = await readFile(new URL("../src/state/appState.js", import.meta.url), "utf8");

function sourceSlice(source, startText, endText) {
  const start = source.indexOf(startText);
  assert.notEqual(start, -1, `missing ${startText}`);
  const end = source.indexOf(endText, start);
  assert.notEqual(end, -1, `missing ${endText}`);
  return source.slice(start, end);
}

test("legacy Provider migration uses fixed parameterless native bridges", () => {
  const direct = sourceSlice(appStateSource, "export async function migrateCodexLegacyProvider", "export async function discoverCodexModels");
  assert.match(direct, /migrateCodexLegacyProvider\(\)/);
  assert.match(direct, /call\("MigrateCodexLegacyProvider"\)/);
  assert.doesNotMatch(direct, /api[_A-Za-z]*key|base[_A-Za-z]*url|provider[_A-Za-z]*id|localStorage|console\.(?:log|error|warn)/i);

  const lifecycle = sourceSlice(appStateSource, "export async function migrateCodexLegacyProviderWithLifecycle", "// ─── Claude Code user settings");
  assert.match(lifecycle, /if \(!confirmed\)/);
  assert.match(lifecycle, /codexDesktopConfirmationPhrase/);
  assert.match(appStateSource, /legacyMigration:\s*\["MigrateCodexLegacyProviderWithLifecycle"\]/);
  assert.doesNotMatch(lifecycle, /api[_A-Za-z]*key|base[_A-Za-z]*url|provider[_A-Za-z]*id|localStorage|console\.(?:log|error|warn)/i);
});

test("migration action is rendered only from native safe eligibility", () => {
  const eligibility = sourceSlice(modalSource, "const legacyProviderMigration = computed", "const contextWindow = computed");
  assert.match(eligibility, /snapshot\.value\?\.legacy_provider_migration/);
  assert.match(eligibility, /raw\?\.available === true/);
  assert.match(eligibility, /providerID === "xiass" \|\| providerID === "codex_local_access"/);
  assert.match(eligibility, /canMigrateLegacyProvider/);
  assert.doesNotMatch(eligibility, /snapshot\.value\?\.providers|api_key|base_url/);

  const start = modalSource.indexOf('<div v-if="canMigrateLegacyProvider"');
  assert.notEqual(start, -1, "safe migration section is missing");
  const template = modalSource.slice(start, start + 1600);
  assert.match(template, /唯一可迁移的第一方旧 Provider/);
  assert.match(template, /迁移此旧 Provider/);
  assert.match(template, /不会读取凭据、不会触及其他 Provider 或会话/);
  assert.doesNotMatch(template, /api_key|experimental_bearer_token|auth\.json|snapshot\.location/);
});

test("running Desktop path can only use the confirmed native lifecycle migration", () => {
  const action = sourceSlice(modalSource, "async function migrateLegacyProvider", "function confirmLegacyProviderMigration");
  assert.match(action, /await refreshCodexDesktopStatus\(\)/);
  assert.match(action, /desktop\.bridgeAvailable && desktop\.running/);
  assert.match(action, /await migrateCodexLegacyProviderWithLifecycle\(true\)/);
  assert.match(action, /await migrateCodexLegacyProvider\(\)/);
  assert.match(action, /legacyProviderMigrationCompleted !== true/);
  assert.doesNotMatch(action, /result\?\.message|result\.message|console\.(?:log|error|warn)/);

  const confirmation = sourceSlice(modalSource, "function confirmLegacyProviderMigration", "function confirmRemoveXIASSProvider");
  assert.match(confirmation, /window\.confirm/);
  assert.match(confirmation, /不会强制结束进程/);
  assert.match(confirmation, /仅迁移一个已验证的旧版 Provider/);
  assert.doesNotMatch(confirmation, /api_key|base_url|experimental_bearer_token/);
});

test("migration UI remains theme-safe and blocks competing modal controls", () => {
  assert.match(modalSource, /migratingLegacyProvider/);
  assert.match(modalSource, /:closable="!saving && !removingXIASSProvider && !migratingLegacyProvider/);
  assert.match(modalSource, /\.legacy-migration \{[^}]*display: flex[^}]*var\(--separator\)/);
  assert.match(modalSource, /\.legacy-migration strong \{[^}]*var\(--text-primary\)/);
  assert.match(modalSource, /@media \(max-width: 680px\)/);
});
