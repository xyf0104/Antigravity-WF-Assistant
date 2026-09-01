const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const nextgenRoot = path.resolve(__dirname, '..', '..');
const repositoryRoot = path.resolve(nextgenRoot, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(repositoryRoot, relativePath), 'utf8');
}

test('macOS bundle, embedded bridge source, and release documentation share the macOS 12 floor', () => {
  const tauriConfig = JSON.parse(read('nextgen/src-tauri/tauri.conf.json'));
  const infoPlist = read('nextgen/src-tauri/Info.plist');
  const swiftPackage = read('nextgen/src-tauri/native/macos-native-menu/Package.swift');
  const releaseChecklist = read('nextgen/RELEASE_CHECKLIST.md');
  const rootReadme = read('README.md');
  const macosReadme = read('macos/source/README.md');
  const macosOverview = read('macos/source/README_MAC.md');
  const macosUsage = read('docs/macOS使用说明.md');

  assert.equal(tauriConfig.bundle?.macOS?.minimumSystemVersion, '12.0');
  assert.match(infoPlist, /<key>LSMinimumSystemVersion<\/key>\s*<string>12\.0<\/string>/);
  assert.match(swiftPackage, /\.macOS\(\.v12\)/);
  assert.match(releaseChecklist, /macOS 12\+/);
  assert.match(releaseChecklist, /declares macOS 12\.0 as its minimum system version/);
  for (const document of [rootReadme, macosReadme, macosOverview, macosUsage]) {
    assert.match(document, /macOS 12/);
  }
});
