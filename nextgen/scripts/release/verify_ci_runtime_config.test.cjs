const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const projectRoot = path.resolve(__dirname, '..', '..');

test('CI Tauri override keeps updater plugin configuration deserializable', () => {
  const configPath = path.join(projectRoot, 'src-tauri', 'tauri.ci.conf.json');
  const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
  const updater = config?.plugins?.updater;

  assert.equal(typeof updater, 'object');
  assert.notEqual(updater, null);
  assert.equal(typeof updater.pubkey, 'string');
  assert.ok(updater.pubkey.length >= 40);
  assert.deepEqual(updater.endpoints, [
    'https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest/download/latest-{{target}}.json',
  ]);
  assert.equal(config?.bundle?.createUpdaterArtifacts, false);
});

test('platform CI workflows launch built applications before uploading artifacts', () => {
  const workflowsRoot = path.resolve(projectRoot, '..', '.github', 'workflows');
  const frontendEntry = fs.readFileSync(path.join(projectRoot, 'src', 'main.tsx'), 'utf8');
  const macosWorkflow = fs.readFileSync(path.join(workflowsRoot, 'build-macos.yml'), 'utf8');
  const windowsWorkflow = fs.readFileSync(path.join(workflowsRoot, 'build-windows.yml'), 'utf8');
  const releaseWorkflow = fs.readFileSync(path.join(workflowsRoot, 'release.yml'), 'utf8');

  assert.match(macosWorkflow, /Smoke-test packaged macOS app startup/);
  assert.match(macosWorkflow, /CFBundleExecutable/);
  assert.match(macosWorkflow, /kill -0 "\$app_pid"/);
  assert.match(macosWorkflow, /\[Diagnostics\] 前端已就绪/);
  assert.match(macosWorkflow, /\[Diagnostics\] 前端启动超时/);
  assert.match(windowsWorkflow, /Smoke-test packaged Windows app startup/);
  assert.match(windowsWorkflow, /HasExited/);
  assert.match(windowsWorkflow, /\[Diagnostics\] 前端已就绪/);
  assert.match(windowsWorkflow, /\[Diagnostics\] 前端启动超时/);
  assert.match(releaseWorkflow, /Smoke-test release macOS app startup/);
  assert.match(releaseWorkflow, /CFBundleExecutable/);
  assert.match(releaseWorkflow, /Smoke-test release Windows app startup/);
  assert.equal((releaseWorkflow.match(/\[Diagnostics\] 前端已就绪/g) || []).length, 2);
  assert.equal((releaseWorkflow.match(/\[Diagnostics\] 前端启动超时/g) || []).length, 2);
  assert.match(frontendEntry, /function FrontendReadyMarker\(\)/);
  assert.match(frontendEntry, /React\.useEffect\(\(\) => \{/);
  assert.match(frontendEntry, /markFrontendReady\("react_committed"\)/);
  assert.doesNotMatch(frontendEntry, /requestAnimationFrame\([\s\S]{0,240}?markFrontendReady/);
});
