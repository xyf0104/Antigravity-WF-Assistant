const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

const SCRIPT = path.join(__dirname, 'gen_checksums.cjs');

test('generates checksums for release payloads without hashing signatures, manifests, or itself', (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'xiass-checksums-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));

  const payloads = [
    'XIASS.Tools_1.7.0_universal.dmg',
    'XIASS.Tools_universal.app.tar.gz',
    'XIASS.Tools_1.7.0_x64_en-US.msi',
    'XIASS.Tools_1.7.0_x64-setup.exe',
  ];

  for (const [index, name] of payloads.entries()) {
    fs.writeFileSync(path.join(root, name), `payload-${index}`);
  }

  fs.writeFileSync(path.join(root, 'XIASS.Tools_universal.app.tar.gz.sig'), 'signature');
  fs.writeFileSync(path.join(root, 'latest.json'), '{}');
  fs.writeFileSync(path.join(root, 'latest-windows-x86_64-nsis.json'), '{}');
  fs.writeFileSync(path.join(root, 'SHA256SUMS.txt'), 'stale checksum file\n');

  const result = spawnSync(
    process.execPath,
    [SCRIPT, '--input', '.', '--output', 'SHA256SUMS.txt'],
    { cwd: root, encoding: 'utf8' },
  );

  assert.equal(result.status, 0, result.stderr || result.stdout);
  const lines = fs.readFileSync(path.join(root, 'SHA256SUMS.txt'), 'utf8').trim().split('\n');
  assert.equal(lines.length, 4);

  const names = lines.map((line) => line.replace(/^[a-f0-9]{64}  /, ''));
  assert.deepEqual(names.sort(), payloads.sort());
  assert.equal(lines.some((line) => line.includes('.sig')), false);
  assert.equal(lines.some((line) => line.includes('latest')), false);
  assert.equal(lines.some((line) => line.includes('SHA256SUMS.txt')), false);
});
