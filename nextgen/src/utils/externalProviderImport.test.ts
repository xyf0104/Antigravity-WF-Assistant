import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

test('external imports are guarded by the exact production Agent allowlist', async () => {
  const source = await readFile(new URL('./externalProviderImport.ts', import.meta.url), 'utf8');
  const section = source.match(
    /const PRODUCTION_EXTERNAL_IMPORT_PLATFORM_IDS[^=]*= new Set\(\[([\s\S]*?)\]\);/,
  )?.[1];

  assert.ok(section);
  assert.deepEqual(
    [...section.matchAll(/'([^']+)'/g)].map((match) => match[1]),
    ['antigravity', 'codex', 'claude_manager', 'windsurf', 'cursor'],
  );
  assert.match(
    source,
    /!providerId \|\| !PRODUCTION_EXTERNAL_IMPORT_PLATFORM_IDS\.has\(providerId\)/,
  );
});
