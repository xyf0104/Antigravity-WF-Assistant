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

test('Windows initial installers carry an offline WebView2 runtime', () => {
  const configPath = path.join(projectRoot, 'src-tauri', 'tauri.conf.json');
  const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));

  assert.deepEqual(config?.bundle?.windows?.webviewInstallMode, {
    type: 'offlineInstaller',
    silent: true,
  });
});

test('Windows clean-install policy keeps offline WebView2 and exercises both installers in CI', () => {
  const configPath = path.join(projectRoot, 'src-tauri', 'tauri.conf.json');
  const schemaPath = path.join(
    projectRoot,
    'node_modules',
    '@tauri-apps',
    'cli',
    'config.schema.json',
  );
  const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
  const schema = JSON.parse(fs.readFileSync(schemaPath, 'utf8'));
  const webviewModes = schema?.definitions?.WebviewInstallMode?.oneOf;
  const offlineMode = Array.isArray(webviewModes)
    ? webviewModes.find((mode) => mode?.properties?.type?.enum?.includes('offlineInstaller'))
    : null;

  assert.ok(offlineMode, 'installed Tauri CLI must recognize the offline WebView2 installer mode');
  assert.match(String(offlineMode.description || ''), /does not require an internet connection/i);
  assert.deepEqual(config?.bundle?.windows?.webviewInstallMode, {
    type: 'offlineInstaller',
    silent: true,
  });

  const workflowsRoot = path.resolve(projectRoot, '..', '.github', 'workflows');
  const ciWorkflows = [
    fs.readFileSync(path.join(workflowsRoot, 'build-windows.yml'), 'utf8'),
    fs.readFileSync(path.join(workflowsRoot, 'release.yml'), 'utf8'),
  ];
  for (const workflow of ciWorkflows) {
    assert.match(workflow, /npx tauri build --ci .*--target x86_64-pc-windows-msvc/);
    assert.match(workflow, /Smoke-test (?:release )?MSI and Setup installation lifecycle/);
    assert.match(workflow, /smoke-windows-installers\.ps1/);
  }

  const installerSmoke = fs.readFileSync(
    path.resolve(projectRoot, '..', '.github', 'scripts', 'smoke-windows-installers.ps1'),
    'utf8',
  );
  assert.match(installerSmoke, /function Invoke-XiassInstallerOperation/);
  assert.match(installerSmoke, /\$InstallerOperationTimeoutSeconds = 300/);
  assert.match(installerSmoke, /Invoke-XiassInstallerOperation -FilePath 'msiexec\.exe' -ArgumentList @\('\/i'/);
  assert.match(installerSmoke, /Invoke-XiassInstallerOperation -FilePath \$nsis\[0\]\.FullName -ArgumentList @\('\/S'\)/);
  assert.match(installerSmoke, /Wait-XiassFrontend \$msiApp/);
  assert.match(installerSmoke, /Wait-XiassFrontend \$nsisApp/);
});

test('Windows CI prepares a verified WiX linker wrapper before both installer builds', () => {
  const workflowsRoot = path.resolve(projectRoot, '..', '.github', 'workflows');
  const wrapperScript = path.join(projectRoot, 'scripts', 'ci', 'prepare-wix-light-wrapper.ps1');
  const wrapperSource = path.join(projectRoot, 'scripts', 'ci', 'wix-light-wrapper.rs');
  const wrapper = fs.readFileSync(wrapperScript, 'utf8');
  const wrapperRust = fs.readFileSync(wrapperSource, 'utf8');

  for (const workflowName of ['build-windows.yml', 'release.yml']) {
    const workflow = fs.readFileSync(path.join(workflowsRoot, workflowName), 'utf8');
    assert.match(workflow, /Prepare verified WiX linker for hosted runner validation/);
    assert.match(workflow, /prepare-wix-light-wrapper\.ps1/);
  }

  assert.match(wrapper, /Get-FileHash -LiteralPath \$archivePath -Algorithm SHA256/);
  assert.match(wrapper, /6ac824e1642d6f7277d0ed7ea09411a508f6116ba6fae0aa5f2c7daa2ff43d31/);
  assert.match(wrapper, /Move-Item -LiteralPath \$originalLight -Destination \$realLight/);
  assert.match(wrapper, /Copy-Item -LiteralPath \$originalLightConfig -Destination \$realLightConfig/);
  assert.match(wrapper, /rustc --edition 2021 \$wrapperSource -O -o \$originalLight/);
  assert.match(wrapperRust, /with_file_name\("light\.real\.exe"\)/);
  assert.match(wrapper, /XIASS_WIX_LIGHT_LOG=\$diagnosticPath/);
  assert.match(wrapper, /& \$originalLight '-\?'/);
  assert.match(wrapperRust, /XIASS_WIX_LIGHT_LOG/);
  assert.match(wrapperRust, /\.arg\("-sval"\)/);
  assert.match(wrapperRust, /append_diagnostic/);
  assert.match(wrapperRust, /\.args\(&args\)/);
  for (const workflowName of ['build-windows.yml', 'release.yml']) {
    const workflow = fs.readFileSync(path.join(workflowsRoot, workflowName), 'utf8');
    assert.match(workflow, /Print WiX linker diagnostic after failed installer build/);
    assert.match(workflow, /XIASS_WIX_LIGHT_LOG/);
  }
});

test('platform CI workflows launch built applications before uploading artifacts', () => {
  const workflowsRoot = path.resolve(projectRoot, '..', '.github', 'workflows');
  const frontendEntry = fs.readFileSync(path.join(projectRoot, 'src', 'main.tsx'), 'utf8');
  const macosWorkflow = fs.readFileSync(path.join(workflowsRoot, 'build-macos.yml'), 'utf8');
  const windowsWorkflow = fs.readFileSync(path.join(workflowsRoot, 'build-windows.yml'), 'utf8');
  const releaseWorkflow = fs.readFileSync(path.join(workflowsRoot, 'release.yml'), 'utf8');

  assert.match(macosWorkflow, /Smoke-test DMG installation and app startup/);
  assert.match(macosWorkflow, /hdiutil attach "\$dmg" -nobrowse -readonly -mountpoint/);
  assert.match(macosWorkflow, /ditto "\$source_app" "\$install_root\/XIASS Tools\.app"/);
  assert.match(macosWorkflow, /hdiutil detach "\$mount_point" -force/);
  assert.match(macosWorkflow, /installed_sidecar="\$\(find "\$app\/Contents"/);
  assert.match(macosWorkflow, /smoke_wf_bridge_sidecar\.mjs/);
  assert.match(macosWorkflow, /open -n -g -F/);
  assert.match(macosWorkflow, /--env "RUST_LOG=info"/);
  assert.match(macosWorkflow, /cocoa_home="\$smoke_root\/home"/);
  assert.match(macosWorkflow, /--env "CFFIXED_USER_HOME=\$cocoa_home"/);
  assert.match(macosWorkflow, /--env "XIASS_TOOLS_DATA_DIR=\$smoke_root\/data"/);
  assert.match(macosWorkflow, /--env "XIASS_TOOLS_PACKAGE_SMOKE=1"/);
  assert.match(macosWorkflow, /find_smoke_pids\(\)/);
  assert.match(macosWorkflow, /--platform macos/);
  assert.match(macosWorkflow, /CFBundleExecutable/);
  assert.match(macosWorkflow, /kill -0 "\$app_pid"/);
  assert.match(macosWorkflow, /main_arches="\$\(lipo -archs/);
  assert.match(macosWorkflow, /Contents\/Resources\/licenses\/\$license/);
  assert.match(macosWorkflow, /\[Diagnostics\] 前端已就绪/);
  assert.match(macosWorkflow, /\[Diagnostics\] 前端启动超时/);
  assert.match(windowsWorkflow, /Smoke-test packaged Windows app startup/);
  assert.match(windowsWorkflow, /Smoke-test MSI and Setup installation lifecycle/);
  assert.match(windowsWorkflow, /smoke-windows-installers\.ps1/);
  assert.match(windowsWorkflow, /BridgeSmokeScript \(Resolve-Path 'scripts\/release\/smoke_wf_bridge_sidecar\.mjs'\)\.Path/);
  assert.match(windowsWorkflow, /HasExited/);
  assert.match(windowsWorkflow, /Installer payload contents are verified by the clean installation lifecycle smoke test/);
  assert.doesNotMatch(windowsWorkflow, /msiexec\.exe -ArgumentList @\('\/a'/);
  assert.doesNotMatch(releaseWorkflow, /msiexec\.exe -ArgumentList @\('\/a'/);
  assert.match(windowsWorkflow, /\[Diagnostics\] 前端已就绪/);
  assert.match(windowsWorkflow, /\[Diagnostics\] 前端启动超时/);
  assert.match(releaseWorkflow, /Smoke-test release DMG installation and app startup/);
  assert.match(releaseWorkflow, /hdiutil attach "\$dmg" -nobrowse -readonly -mountpoint/);
  assert.match(releaseWorkflow, /ditto "\$source_app" "\$install_root\/XIASS Tools\.app"/);
  assert.match(releaseWorkflow, /hdiutil detach "\$mount_point" -force/);
  assert.match(releaseWorkflow, /installed_sidecar="\$\(find "\$app\/Contents"/);
  assert.match(releaseWorkflow, /smoke_wf_bridge_sidecar\.mjs/);
  assert.match(releaseWorkflow, /open -n -g -F/);
  assert.match(releaseWorkflow, /--env "RUST_LOG=info"/);
  assert.match(releaseWorkflow, /cocoa_home="\$smoke_root\/home"/);
  assert.match(releaseWorkflow, /--env "CFFIXED_USER_HOME=\$cocoa_home"/);
  assert.match(releaseWorkflow, /--env "XIASS_TOOLS_DATA_DIR=\$smoke_root\/data"/);
  assert.match(releaseWorkflow, /--env "XIASS_TOOLS_PACKAGE_SMOKE=1"/);
  assert.match(releaseWorkflow, /find_smoke_pids\(\)/);
  assert.match(releaseWorkflow, /--platform macos/);
  assert.match(releaseWorkflow, /CFBundleExecutable/);
  assert.match(releaseWorkflow, /Smoke-test release Windows app startup/);
  assert.match(releaseWorkflow, /Smoke-test release MSI and Setup installation lifecycle/);
  assert.match(releaseWorkflow, /smoke-windows-installers\.ps1/);
  assert.match(releaseWorkflow, /BridgeSmokeScript \(Resolve-Path 'scripts\/release\/smoke_wf_bridge_sidecar\.mjs'\)\.Path/);
  assert.equal((releaseWorkflow.match(/\[Diagnostics\] 前端已就绪/g) || []).length, 2);
  assert.equal((releaseWorkflow.match(/\[Diagnostics\] 前端启动超时/g) || []).length, 2);
  assert.match(frontendEntry, /function FrontendReadyMarker\(\)/);
  assert.match(frontendEntry, /React\.useEffect\(\(\) => \{/);
  assert.match(frontendEntry, /markFrontendReady\("react_committed"\)/);
  assert.doesNotMatch(frontendEntry, /requestAnimationFrame\([\s\S]{0,240}?markFrontendReady/);

  const windowsInstallerSmoke = fs.readFileSync(
    path.resolve(projectRoot, '..', '.github', 'scripts', 'smoke-windows-installers.ps1'),
    'utf8',
  );
  assert.match(windowsInstallerSmoke, /function Invoke-XiassInstallerOperation/);
  assert.match(windowsInstallerSmoke, /\$InstallerOperationTimeoutSeconds = 300/);
  assert.match(windowsInstallerSmoke, /Invoke-XiassInstallerOperation -FilePath 'msiexec\.exe' -ArgumentList @\('\/i'/);
  assert.match(windowsInstallerSmoke, /Invoke-XiassInstallerOperation -FilePath 'msiexec\.exe' -ArgumentList @\('\/x'/);
  assert.match(windowsInstallerSmoke, /Invoke-XiassInstallerOperation -FilePath \$nsis\[0\]\.FullName -ArgumentList @\('\/S'\)/);
  assert.match(windowsInstallerSmoke, /\[Diagnostics\] 前端已就绪/);
  assert.match(windowsInstallerSmoke, /Assert-XiassInstalledPayload \$msiApp 'MSI'/);
  assert.match(windowsInstallerSmoke, /Assert-XiassInstalledPayload \$nsisApp 'NSIS'/);
  assert.match(windowsInstallerSmoke, /function Get-XiassDesktopShortcut/);
  assert.match(windowsInstallerSmoke, /function Get-XiassStartMenuShortcut/);
  assert.match(windowsInstallerSmoke, /Assert-XiassShortcuts 'NSIS'/);
  assert.match(windowsInstallerSmoke, /Assert-XiassShortcutsRemoved 'NSIS'/);
  assert.match(windowsInstallerSmoke, /xiass-wf-bridge\*\.exe/);
  assert.match(windowsInstallerSmoke, /xiass-cliproxy\*\.exe/);
  assert.match(windowsInstallerSmoke, /function Invoke-XiassInstalledWFBridgeSmoke/);
  assert.match(windowsInstallerSmoke, /& node \$BridgeSmokeScript --binary \$bridge\.FullName --platform windows/);
  assert.match(windowsInstallerSmoke, /Invoke-XiassInstalledWFBridgeSmoke \$msiApp 'MSI'/);
  assert.match(windowsInstallerSmoke, /Invoke-XiassInstalledWFBridgeSmoke \$nsisApp 'NSIS'/);
  assert.match(windowsInstallerSmoke, /Wait-XiassUninstalled 'MSI'/);
  assert.match(windowsInstallerSmoke, /Wait-XiassUninstalled 'NSIS'/);
  assert.match(windowsInstallerSmoke, /function Get-XiassMsiBinaryNames/);
  assert.match(windowsInstallerSmoke, /WindowsInstaller\.Installer/);
  assert.match(windowsInstallerSmoke, /SELECT `Name` FROM `Binary`/);
  assert.match(windowsInstallerSmoke, /MicrosoftEdgeWebView2RuntimeInstaller\.exe/);
  assert.match(windowsInstallerSmoke, /function Assert-XiassOfflineWebView2Payload/);
  assert.match(windowsInstallerSmoke, /80MB/);
  assert.match(windowsInstallerSmoke, /Assert-XiassOfflineWebView2Payload \$msi\[0\] \$nsis\[0\]/);
  assert.match(windowsInstallerSmoke, /CLIProxyAPI-MIT\.txt/);
  assert.match(windowsInstallerSmoke, /function Stop-XiassSmokeProcessTree/);
  assert.match(windowsInstallerSmoke, /taskkill\.exe \/PID \$Process\.Id \/T \/F/);
  assert.match(windowsInstallerSmoke, /Stop-XiassSmokeProcessTree \$process/);
  assert.match(windowsWorkflow, /taskkill\.exe \/PID \$process\.Id \/T \/F/);
  assert.match(releaseWorkflow, /taskkill\.exe \/PID \$process\.Id \/T \/F/);
});

test('installed WF bridge smoke is isolated and covers every supported workspace read path', () => {
  const smokeScript = fs.readFileSync(
    path.join(projectRoot, 'scripts', 'release', 'smoke_wf_bridge_sidecar.mjs'),
    'utf8',
  );

  assert.match(smokeScript, /XIASS_WF_RPC_TOKEN/);
  assert.match(smokeScript, /XIASS_WF_RPC_PORT:\s*'0'/);
  assert.match(smokeScript, /XIASS_TOOLS_DATA_DIR/);
  assert.match(smokeScript, /ANTIGRAVITY_WF_GEMINI_DIR/);
  assert.match(smokeScript, /CODEX_HOME/);
  assert.match(smokeScript, /CLAUDE_CONFIG_DIR/);
  assert.match(smokeScript, /USERPROFILE/);
  assert.match(smokeScript, /APPDATA/);
  assert.match(smokeScript, /LOCALAPPDATA/);
  assert.match(smokeScript, /XDG_CONFIG_HOME/);
  assert.match(smokeScript, /127\.0\.0\.1/);
  assert.match(smokeScript, /\/health/);
  assert.match(smokeScript, /\/rpc/);
  assert.match(smokeScript, /Authorization: authorization/);
  assert.match(smokeScript, /GetQuickPatchStatus/);
  assert.match(smokeScript, /GetCodexConfiguration/);
  assert.match(smokeScript, /GetClaudeCodeConfiguration/);
  assert.match(smokeScript, /GetCursorMCPConfiguration/);
  assert.match(smokeScript, /GetWindsurfMCPConfiguration/);
  assert.match(smokeScript, /GetAgentStatuses/);
  assert.match(smokeScript, /await rm\(isolated\.root, \{ recursive: true, force: true \}\)/);
  assert.doesNotMatch(smokeScript, /ApplyPatch|ApplyCodex|ApplyClaude|ApplyCursor|ApplyWindsurf|StartOAuth/);
});

test('package smoke skips the single-instance handoff only for its explicit isolated mode', () => {
  const runtime = fs.readFileSync(path.join(projectRoot, 'src-tauri', 'src', 'lib.rs'), 'utf8');

  assert.match(
    runtime,
    /if !is_package_smoke_run\(\) \{\s*app = app\.plugin\(tauri_plugin_single_instance::init/s,
  );
  assert.match(runtime, /安装包冒烟模式跳过单实例保护/);
  assert.match(runtime, /fn package_smoke_mode_from_env_value\(value: Option<&str>\) -> bool/);
  assert.match(runtime, /matches!\(value\.map\(str::trim\), Some\("1"\)\)/);
});

test('release publication enforces the exact attachment set and verifies live updater downloads', () => {
  const workflowPath = path.resolve(
    projectRoot,
    '..',
    '.github',
    'workflows',
    'release.yml',
  );
  const workflow = fs.readFileSync(workflowPath, 'utf8');

  assert.match(workflow, /test "\$\{#release_files\[@\]\}" -eq 13/);
  assert.match(workflow, /git ls-remote origin refs\/heads\/main/);
  assert.match(workflow, /test "\$release_commit" = "\$main_commit"/);
  assert.match(workflow, /Release attachment is outside the whitelist/);
  assert.match(workflow, /Install updater signature verifier/);
  assert.match(workflow, /minisign -V -m "\$updater_payload"/);
  assert.match(workflow, /test "\$\{#updater_signatures\[@\]\}" -eq 3/);
  assert.match(workflow, /test "\$\{#assets\[@\]\}" -eq 13/);
  assert.match(workflow, /diff -u "\$local_names" "\$remote_names"/);
  assert.match(workflow, /Verify published updater manifests and downloads/);
  assert.match(workflow, /verify_published_updater_manifests\.cjs/);
  assert.match(workflow, /--legacy true/);
});
