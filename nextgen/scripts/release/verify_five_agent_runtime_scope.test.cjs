const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const root = path.resolve(__dirname, '..', '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

const supportedAgentIds = [
  'antigravity',
  'codex',
  'claude_manager',
  'windsurf',
  'cursor',
];

const retiredAgentNames = [
  'github_copilot',
  'kiro',
  'grok',
  'codebuddy',
  'codebuddy_cn',
  'qoder',
  'zcode',
  'trae',
  'trae_solo',
  'trae_cn',
  'trae_solo_cn',
  'workbuddy',
  'zed',
];

function quotedValues(source) {
  return [...source.matchAll(/['"]([^'"]+)['"]/g)].map((match) => match[1]);
}

test('frontend ownership declares exactly the five XIASS Agent account domains', () => {
  const platformTypes = read('src/types/platform.ts');
  const section = platformTypes.match(
    /export const PRODUCTION_AGENT_ACCOUNT_PLATFORM_IDS = \[([\s\S]*?)\] as const/,
  )?.[1];

  assert.ok(section, 'missing production Agent account allowlist');
  assert.deepEqual(quotedValues(section), supportedAgentIds);
});

test('scheduled frontend refresh never loads a retired Agent account store', () => {
  const autoRefresh = read('src/hooks/useAutoRefresh.ts');
  const forbiddenStores = [
    'useGitHubCopilotAccountStore',
    'useKiroAccountStore',
    'useGrokAccountStore',
    'useCodebuddyAccountStore',
    'useCodebuddyCnAccountStore',
    'useWorkbuddyAccountStore',
    'useQoderAccountStore',
    'useZcodeAccountStore',
    'useTraeAccountStore',
    'useZedAccountStore',
  ];

  for (const name of forbiddenStores) {
    assert.doesNotMatch(autoRefresh, new RegExp(`\\b${name}\\b`));
  }
  for (const name of retiredAgentNames) {
    assert.doesNotMatch(autoRefresh, new RegExp(`key:\\s*['"]${name}['"]`));
  }
});

test('native automatic import and token keepers are restricted to production Agents', () => {
  const autoImport = read('src-tauri/src/modules/auto_local_import.rs');
  const tokenKeeper = read('src-tauri/src/modules/provider_token_keeper.rs');
  const watcherRegion = autoImport.match(
    /fn platform_watchers\(\) -> Vec<PlatformWatcher> \{([\s\S]*?)\n\}/,
  )?.[1] || '';
  const refreshRegion = tokenKeeper.match(
    /async fn run_refresh_cycle\(app_handle: &AppHandle\) \{([\s\S]*?)\n\}/,
  )?.[1] || '';

  assert.deepEqual(
    [...watcherRegion.matchAll(/platform:\s*"([^"]+)"/g)].map((match) => match[1]),
    ['antigravity', 'codex', 'cursor', 'windsurf', 'claude'],
  );
  assert.match(tokenKeeper, /const PRODUCTION_TOKEN_KEEPER_PLATFORMS: \[&str; 2\] = \["codex", "cursor"\]/);
  for (const name of retiredAgentNames) {
    assert.doesNotMatch(refreshRegion, new RegExp(`refresh_platform_if_due\\("${name}"`));
  }
});

test('tray, deep imports and account transfer reject retired Agent execution paths', () => {
  const tray = read('src-tauri/src/modules/tray.rs');
  const externalImport = read('src/utils/externalProviderImport.ts');
  const transfers = read('src/services/accountTransferService.ts');

  const trayOrder = tray.match(/pub\(crate\) fn default_order\(\) -> \[Self; 5\] \{([\s\S]*?)\n\s*\]/)?.[1] || '';
  assert.match(tray, /pub\(crate\) fn is_production_agent\(self\) -> bool/);
  assert.match(tray, /if platform\.is_production_agent\(\) && seen\.insert\(platform\)/);
  for (const expected of ['Self::Claude', 'Self::Codex', 'Self::Antigravity', 'Self::Windsurf', 'Self::Cursor']) {
    assert.match(trayOrder, new RegExp(expected.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  }
  assert.match(externalImport, /!PRODUCTION_EXTERNAL_IMPORT_PLATFORM_IDS\.has\(providerId\)/);
  const transferSection = transfers.match(
    /export const ACCOUNT_TRANSFER_PLATFORM_IDS = \[([\s\S]*?)\] as const/,
  )?.[1];
  assert.ok(transferSection, 'missing account-transfer execution allowlist');
  assert.deepEqual(quotedValues(transferSection), supportedAgentIds);
});

test('the Tauri invoke handler does not expose retired Agent command modules', () => {
  const lib = read('src-tauri/src/lib.rs');
  const handler = lib.match(/tauri::generate_handler!\[([\s\S]*?)\n\s*\]\)/)?.[1] || '';

  assert.ok(handler, 'missing Tauri invoke handler');
  for (const moduleName of retiredAgentNames) {
    assert.doesNotMatch(handler, new RegExp(`commands::${moduleName}::`));
  }
});
