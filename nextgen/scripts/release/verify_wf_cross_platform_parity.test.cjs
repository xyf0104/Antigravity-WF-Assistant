const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const nextgenRoot = path.resolve(__dirname, '..', '..');
const repositoryRoot = path.resolve(nextgenRoot, '..');
const platforms = ['macos', 'windows'];

function read(relativePath, encoding = 'utf8') {
  return fs.readFileSync(path.join(repositoryRoot, relativePath), encoding);
}

function readJson(relativePath) {
  return JSON.parse(read(relativePath));
}

function collectFiles(absoluteRoot) {
  const files = new Map();
  const visit = (directory) => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const absolutePath = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        visit(absolutePath);
        continue;
      }
      if (entry.isFile()) {
        files.set(path.relative(absoluteRoot, absolutePath).replaceAll(path.sep, '/'), fs.readFileSync(absolutePath));
      }
    }
  };
  visit(absoluteRoot);
  return files;
}

function assertIdenticalTree(relativeDirectory) {
  const macosRoot = path.join(repositoryRoot, 'macos', 'source', 'frontend', relativeDirectory);
  const windowsRoot = path.join(repositoryRoot, 'windows', 'source', 'frontend', relativeDirectory);
  const macosFiles = collectFiles(macosRoot);
  const windowsFiles = collectFiles(windowsRoot);

  assert.deepEqual(
    [...macosFiles.keys()].sort(),
    [...windowsFiles.keys()].sort(),
    `${relativeDirectory} file set must be identical across macOS and Windows`,
  );
  for (const relativePath of macosFiles.keys()) {
    assert.deepEqual(
      macosFiles.get(relativePath),
      windowsFiles.get(relativePath),
      `${relativeDirectory}/${relativePath} must be byte-identical across macOS and Windows`,
    );
  }
}

function publicAppMethods(platform) {
  const sourceRoot = path.join(repositoryRoot, platform, 'source');
  const source = [...collectFiles(sourceRoot).entries()]
    .filter(([relativePath]) => relativePath.endsWith('.go') && !relativePath.endsWith('_test.go'))
    .map(([, contents]) => contents.toString('utf8'))
    .join('\n');
  return [...source.matchAll(/^func \(a \*App\) ([A-Z][A-Za-z0-9_]*)\(/gm)]
    .map((match) => match[1])
    .sort();
}

function assertHelperVersion(platform, expectedVersion) {
  const prefix = `${platform}/source`;
  const helperVersion = read(`${prefix}/VERSION`).trim();
  const frontendPackage = readJson(`${prefix}/frontend/package.json`);
  const frontendLockfile = readJson(`${prefix}/frontend/package-lock.json`);
  const wailsConfig = readJson(`${prefix}/wails.json`);
  const updaterSource = read(`${prefix}/internal/updater/updater.go`);
  const appSource = read(`${prefix}/frontend/src/App.vue`);

  assert.equal(helperVersion, expectedVersion, `${platform} helper VERSION must follow the host release`);
  assert.equal(frontendPackage.version, expectedVersion, `${platform} frontend package version`);
  assert.equal(frontendLockfile.version, expectedVersion, `${platform} frontend lockfile version`);
  assert.equal(frontendLockfile.packages?.['']?.version, expectedVersion, `${platform} frontend lockfile root version`);
  assert.equal(wailsConfig.info?.productVersion, expectedVersion, `${platform} Wails product version`);
  assert.match(updaterSource, new RegExp(`CurrentVersion\\s*=\\s*"${expectedVersion}"`));
  assert.match(appSource, new RegExp(`: "v${expectedVersion}"`));
  assert.match(appSource, new RegExp(`XIASS Tools v${expectedVersion}`));
}

test('embedded WF renderer source and XIASS visual assets are identical on macOS and Windows', () => {
  assertIdenticalTree('src');
  assertIdenticalTree('public');
});

test('every embedded WF surface keeps the original XIASS Tools logo asset', () => {
  const originalLogo = fs.readFileSync(path.join(nextgenRoot, 'src-tauri', 'icons', 'icon.png'));
  const originalLogoHash = '578a1efa0454ab664a8616c4f0d448f433ae85c615396fd4c4638a95ead16d12';
  const actualHash = require('node:crypto').createHash('sha256').update(originalLogo).digest('hex');

  assert.equal(actualHash, originalLogoHash, 'the host must keep the v1.7.0 XIASS Tools logo source');
  for (const platform of platforms) {
    const sourceRoot = path.join(repositoryRoot, platform, 'source');
    assert.deepEqual(
      fs.readFileSync(path.join(sourceRoot, 'frontend', 'public', 'xiass-tools-logo.png')),
      originalLogo,
      `${platform} embedded WF logo must use the original XIASS Tools mark`,
    );
    assert.deepEqual(
      fs.readFileSync(path.join(sourceRoot, 'frontend', 'public', 'xiass-tools-appicon.png')),
      originalLogo,
      `${platform} embedded WF app icon must use the original XIASS Tools mark`,
    );
    assert.deepEqual(
      fs.readFileSync(path.join(sourceRoot, 'build', 'appicon.png')),
      originalLogo,
      `${platform} embedded WF build icon must use the original XIASS Tools mark`,
    );
  }
});

test('embedded WF native bridge exposes the same public App contract on macOS and Windows', () => {
  const [macosMethods, windowsMethods] = platforms.map(publicAppMethods);
  assert.ok(macosMethods.length > 0, 'macOS App must expose Wails methods');
  assert.deepEqual(macosMethods, windowsMethods);
});

test('all embedded helper versions follow the host XIASS Tools release', () => {
  const hostPackage = readJson('nextgen/package.json');
  const hostTauriConfig = readJson('nextgen/src-tauri/tauri.conf.json');
  const hostCargo = read('nextgen/src-tauri/Cargo.toml');
  const hostLockfile = readJson('nextgen/package-lock.json');
  const expectedVersion = hostPackage.version;

  assert.equal(hostTauriConfig.version, expectedVersion, 'Tauri config version');
  assert.equal(hostLockfile.version, expectedVersion, 'host lockfile version');
  assert.equal(hostLockfile.packages?.['']?.version, expectedVersion, 'host lockfile root version');
  assert.match(hostCargo, new RegExp(`^version\\s*=\\s*"${expectedVersion}"$`, 'm'));
  for (const platform of platforms) assertHelperVersion(platform, expectedVersion);
});
