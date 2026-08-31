const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const NEXTGEN_ROOT = path.resolve(__dirname, '../..');

function read(relativePath) {
  return fs.readFileSync(path.join(NEXTGEN_ROOT, relativePath), 'utf8');
}

test('Codex provider and group loading never converts storage failures into empty configuration', () => {
  const providers = read('src/services/codexModelProviderService.ts');
  const groups = read('src/services/codexAccountGroupService.ts');

  assert.doesNotMatch(providers, /loadProvidersFromDisk\(\)\.catch/);
  assert.doesNotMatch(providers, /saveProvidersToDisk\(loaded\)\.catch/);
  assert.doesNotMatch(groups, /loadGroupsFromDisk[\s\S]*?catch\s*\{\s*return \[\]/);
  assert.match(groups, /已保留磁盘原件，没有按空分组覆盖/);
});

test('Codex provider cache is published only after persistent storage succeeds', () => {
  const source = read('src/services/codexModelProviderService.ts');
  const writeFunction = source.match(
    /async function writeProviders\([\s\S]*?\n\}/,
  )?.[0];

  assert.ok(writeFunction, 'writeProviders implementation should exist');
  assert.ok(
    writeFunction.indexOf('await saveProvidersToDisk(next)')
      < writeFunction.indexOf('cachedProviders = next'),
    'provider cache must not change before the disk write succeeds',
  );
});

test('native Codex provider and group storage validates arrays and uses atomic replacement', () => {
  const source = read('src-tauri/src/commands/codex_model_provider_commands.rs');

  assert.match(source, /validate_codex_json_array\(&data, "Codex 账号分组"\)\?/);
  assert.match(source, /validate_codex_json_array\(&data, "Codex 模型供应商"\)\?/);
  assert.equal(
    (source.match(/atomic_write::write_string_atomic\(&path, &data\)/g) ?? []).length,
    2,
  );
  assert.doesNotMatch(source, /std::fs::write\(&path, data\)/);
});
