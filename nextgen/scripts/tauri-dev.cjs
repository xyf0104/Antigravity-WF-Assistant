const { spawnSync } = require('node:child_process');

function resolveMacosSdkRoot() {
  if (process.platform !== 'darwin') {
    return process.env.SDKROOT;
  }
  // Some shells/Xcode setups leave SDKROOT pointing at iPhoneOS, which breaks
  // swift-rs / macos-native-menu (targets arm64-apple-macosx).
  const current = process.env.SDKROOT || '';
  if (current && /MacOSX\.platform|MacOSX[^/]*\.sdk/i.test(current)) {
    return current;
  }
  const probed = spawnSync('xcrun', ['--sdk', 'macosx', '--show-sdk-path'], {
    encoding: 'utf8',
  });
  if (probed.status === 0) {
    const path = String(probed.stdout || '').trim();
    if (path) return path;
  }
  return current || undefined;
}

const env = {
  ...process.env,
  XIASS_TOOLS_PROFILE: process.env.XIASS_TOOLS_PROFILE || 'dev',
  XIASS_TOOLS_API_PORT: process.env.XIASS_TOOLS_API_PORT || '1456',
  VITE_XIASS_TOOLS_PROFILE: process.env.VITE_XIASS_TOOLS_PROFILE || 'dev',
  // The existing React bundle still consumes the historical Vite key. Keep this
  // build-time projection until the UI migration lands; native runtime identity
  // and persisted data exclusively use XIASS_TOOLS_*.
  VITE_COCKPIT_TOOLS_PROFILE:
    process.env.VITE_XIASS_TOOLS_PROFILE ||
    process.env.VITE_COCKPIT_TOOLS_PROFILE ||
    'dev',
};
const macosSdkRoot = resolveMacosSdkRoot();
if (macosSdkRoot) {
  env.SDKROOT = macosSdkRoot;
}
const extraArgs = process.argv.slice(2);

const syncResult = spawnSync('npm', ['run', 'sync-version'], {
  stdio: 'inherit',
  env,
});

if (syncResult.status !== 0) {
  process.exit(syncResult.status ?? 1);
}

const tauriResult = spawnSync(
  'tauri',
  ['dev', '--config', 'src-tauri/tauri.dev.conf.json', ...extraArgs],
  {
    stdio: 'inherit',
    env,
  },
);

process.exit(tauriResult.status ?? 1);
