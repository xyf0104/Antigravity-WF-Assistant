import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

test('managed backups write XIASS names instead of legacy Cockpit names', async () => {
  const source = await readFile(
    new URL('./scheduledBackupService.ts', import.meta.url),
    'utf8',
  );

  assert.match(source, /`xiass_\$\{trigger\}_backup_\$\{mode\}_/);
  assert.doesNotMatch(source, /`cockpit_\$\{trigger\}_backup_/);
});
