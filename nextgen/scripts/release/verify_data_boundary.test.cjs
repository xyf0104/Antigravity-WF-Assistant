const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const nextgenRoot = path.resolve(__dirname, '..', '..');
const read = (relativePath) => fs.readFileSync(path.join(nextgenRoot, relativePath), 'utf8');
const readRepository = (relativePath) =>
  fs.readFileSync(path.join(nextgenRoot, '..', relativePath), 'utf8');

test('account backups use an explicit XIASS allowlist and include the helper transfer section', () => {
  const source = read('src/services/accountTransferService.ts');
  const allowlistMatch = source.match(
    /export const ACCOUNT_TRANSFER_PLATFORM_IDS = \[([\s\S]*?)\] as const/,
  );
  assert.ok(allowlistMatch, 'missing explicit account-transfer allowlist');

  const allowlist = [...allowlistMatch[1].matchAll(/'([^']+)'/g)].map((match) => match[1]);
  assert.deepEqual(allowlist, [
    'antigravity',
    'codex',
    'claude_manager',
    'windsurf',
    'cursor',
  ]);
  assert.match(source, /for \(const platform of ACCOUNT_TRANSFER_PLATFORM_IDS\)/);
  assert.match(source, /embedded_wf_accounts: 'included_as_explicit_section'/);
  assert.match(source, /credential_export: 'plaintext_user_authorized'/);
  assert.match(source, /const wfHelper = await exportWfHelperTransfer\(\)/);
  assert.match(
    source,
    /parsed\.version === ACCOUNT_TRANSFER_VERSION && !isRecord\(wfHelper\)/,
    'account-transfer v2 must reject a missing helper section',
  );
  assert.match(source, /await restoreWfHelperTransfer\(parsedBundle\.wf_helper\)/);
  assert.match(source, /external_codex_auth: 'never_read_or_included'/);
  assert.match(source, /excluded_from_xiass_product_scope/);
});

test('new transfer files use XIASS schemas while retaining legacy restore compatibility', () => {
  const schemas = read('src/utils/transferSchemas.ts');
  const accounts = read('src/services/accountTransferService.ts');
  const data = read('src/services/dataTransferService.ts');

  assert.match(schemas, /XIASS_ACCOUNT_TRANSFER_SCHEMA = 'xiass-tools\.account-transfer'/);
  assert.match(schemas, /XIASS_DATA_TRANSFER_SCHEMA = 'xiass-tools\.data-transfer'/);
  assert.match(schemas, /'cockpit-tools\.account-transfer'/);
  assert.match(schemas, /'cockpit-tools\.data-transfer'/);
  assert.match(accounts, /ACCOUNT_TRANSFER_SCHEMA = XIASS_ACCOUNT_TRANSFER_SCHEMA/);
  assert.match(accounts, /isSupportedAccountTransferSchema\(parsed\.schema\)/);
  assert.match(data, /DATA_TRANSFER_SCHEMA = XIASS_DATA_TRANSFER_SCHEMA/);
  assert.match(data, /isSupportedDataTransferSchema\(value\.schema\)/);
  assert.match(data, /isSupportedAccountTransferSchema\(value\.schema\)/);
});

test('parent diagnostic export is registered and excludes account and Codex auth stores', () => {
  const commands = read('src-tauri/src/commands/logs.rs');
  const registration = read('src-tauri/src/lib.rs');
  assert.match(registration, /commands::logs::logs_export_diagnostics/);
  assert.match(commands, /accounts\/\*\/account\.json/);
  assert.match(commands, /external_codex_auth/);
  assert.match(commands, /symlink_metadata/);
  assert.match(commands, /MAX_DIAGNOSTIC_TOTAL_BYTES/);
});

test('public privacy documentation distinguishes explicit local account actions from automatic collection', () => {
  const readme = readRepository('README.md');
  const architecture = readRepository('docs/XIASS-Tools-architecture.md');

  assert.match(readme, /用户主动执行 OAuth、API Key \/ Token \/ JSON 导入、从本机 Codex 或 ChatGPT Desktop 导入/);
  assert.match(readme, /只有点击“本机导入”“切换\/注入”或启动受保护事务时/);
  assert.match(readme, /不会被统一备份或诊断导出自动读取/);
  assert.match(readme, /默认情况下，XIASS Tools 不会在后台扫描、批量导入或上传其他客户端的账号/);
  assert.match(readme, /用户在“设置”中主动开启“本机账号自动导入”/);
  assert.doesNotMatch(readme, /外部 Codex `auth\.json` 始终不读取/);
  assert.match(architecture, /用户点击“从本机导入”时才读取所选 Agent 的必要本机授权资料/);
  assert.match(architecture, /用户在设置中明确启用“本机账号自动导入”/);
  assert.match(architecture, /默认不会为了“自动管理”而后台扫描未选择客户端/);
});
