const assert = require('node:assert/strict');
const test = require('node:test');

const { buildReleaseConfig } = require('./generate_tauri_release_config.cjs');

test('builds an updater-enabled config bound to the current repository', () => {
  const config = buildReleaseConfig({
    repo: 'xyf0104/Antigravity-WF-Assistant',
    pubkey: 'dW50cnVzdGVkIGNvbW1lbnQ6IHRlc3QgcHVibGljIGtleQ==',
  });
  assert.equal(config.bundle.createUpdaterArtifacts, true);
  assert.equal(config.bundle.macOS.signingIdentity, '-');
  assert.equal(config.plugins.updater.pubkey.length > 40, true);
  assert.deepEqual(config.plugins.updater.endpoints, [
    'https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest/download/latest-{{target}}.json',
  ]);
});

test('refuses to generate release config without a real public key', () => {
  assert.throws(
    () => buildReleaseConfig({ repo: 'xyf0104/repo', pubkey: '' }),
    /TAURI_UPDATER_PUBLIC_KEY is required/,
  );
  assert.throws(
    () => buildReleaseConfig({ repo: 'xyf0104/repo', pubkey: 'placeholder' }),
    /too short/,
  );
});
