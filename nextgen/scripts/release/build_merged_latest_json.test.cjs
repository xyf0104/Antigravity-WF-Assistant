const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

test('builds a complete macOS and Windows updater manifest from signed assets', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'xiass-merged-manifest-'));
  const assetsDir = path.join(root, 'assets');
  const notesFile = path.join(root, 'notes.md');
  const output = path.join(root, 'latest.json');
  fs.mkdirSync(assetsDir, { recursive: true });
  fs.writeFileSync(notesFile, 'Release notes');

  for (const name of [
    'XIASS.Tools_universal.app.tar.gz',
    'XIASS.Tools_1.2.3_x64_en-US.msi',
    'XIASS.Tools_1.2.3_x64-setup.exe',
  ]) {
    fs.writeFileSync(path.join(assetsDir, name), name);
    fs.writeFileSync(path.join(assetsDir, `${name}.sig`), `${name}-signature`);
  }

  const script = path.join(__dirname, 'build_merged_latest_json.cjs');
  const result = spawnSync(process.execPath, [
    script,
    '--version', '1.2.3',
    '--repo', 'xyf0104/Antigravity-WF-Assistant',
    '--assets-dir', assetsDir,
    '--notes-file', notesFile,
    '--published-at', '2026-08-31T00:00:00Z',
    '--output', output,
  ], { encoding: 'utf8' });

  assert.equal(result.status, 0, result.stderr);
  const manifest = JSON.parse(fs.readFileSync(output, 'utf8'));
  assert.deepEqual(Object.keys(manifest.platforms).sort(), [
    'darwin-aarch64',
    'darwin-aarch64-app',
    'darwin-x86_64',
    'darwin-x86_64-app',
    'windows-x86_64',
    'windows-x86_64-msi',
    'windows-x86_64-nsis',
  ]);
  assert.equal(
    manifest.platforms['darwin-aarch64-app'].url,
    manifest.platforms['darwin-x86_64-app'].url,
  );
});
