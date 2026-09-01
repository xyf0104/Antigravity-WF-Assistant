const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const nextgenRoot = path.resolve(__dirname, '..', '..');
const repositoryRoot = path.resolve(nextgenRoot, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(repositoryRoot, relativePath), 'utf8');
}

function appImplementationSource(platform) {
  const sourceDirectory = path.join(repositoryRoot, platform, 'source');
  return fs
    .readdirSync(sourceDirectory, { withFileTypes: true })
    .filter((entry) => entry.isFile() && entry.name.endsWith('.go') && !entry.name.endsWith('_test.go'))
    .map((entry) => fs.readFileSync(path.join(sourceDirectory, entry.name), 'utf8'))
    .join('\n');
}

test('embedded WF bridge delegates native actions to the Tauri host on both platforms', () => {
  for (const platform of ['macos', 'windows']) {
    const bridge = read(`${platform}/source/wfbridge_main.go`);
    const actions = read(`${platform}/source/native_actions.go`);

    assert.match(bridge, /application\.embeddedMode = true/);
    assert.match(bridge, /application\.nativeActions = actions/);
    assert.match(bridge, /applicationContext, cancelApplication := context\.WithCancel\(context\.Background\(\)\)/);
    assert.match(bridge, /cancelApplication\(\)/);
    assert.match(bridge, /actions\.Close\(\)/);
    assert.match(bridge, /"\/host-action-result"/);
    assert.match(bridge, /newWFBridgeRequestID/);
    assert.match(bridge, /wfBridgeHostActionTimeout/);
    assert.match(bridge, /event\.name === "wf:host-action"/);
    assert.match(actions, /if !a\.embeddedMode \{/);
    assert.match(actions, /a\.nativeActions\.Execute\(a\.ctx, request\)/);
    assert.match(actions, /errEmbeddedNativeHostUnavailable/);
  }
});

test('embedded host actions are validated, scoped to the active bridge and fail closed', () => {
  const bridge = read('nextgen/src-tauri/src/modules/wf_bridge.rs');

  assert.match(bridge, /fn valid_host_action_id\(value: &str\) -> bool/);
  assert.match(bridge, /matches!\(parsed\.scheme\(\), "http" \| "https"\)/);
  assert.match(bridge, /"open_file" \| "open_directory" \| "save_file"/);
  assert.match(bridge, /filter\(\|session\| session\.port == port\)/);
  assert.match(bridge, /host_action_ids\.insert\(request\.request_id\.clone\(\)\)/);
  assert.match(bridge, /get_or_start_session[\s\S]*?probe_health\(&session\.url\)/);
  assert.match(bridge, /stop_child\(&mut child\)/);
  assert.match(bridge, /WF_BRIDGE_STOP_TIMEOUT/);
});

test('only the matching local iframe can request a host-native action', () => {
  const workspace = read('nextgen/src/pages/XiassAgentWorkspace.tsx');
  const commands = read('nextgen/src-tauri/src/commands/wf_bridge.rs');
  const registration = read('nextgen/src-tauri/src/lib.rs');

  assert.match(workspace, /event\.origin !== expectedOrigin/);
  assert.match(workspace, /event\.source !== iframeRef\.current\?\.contentWindow/);
  assert.match(workspace, /\^\[0-9a-f\]\{64\}\$/);
  assert.match(workspace, /handledHostActionIdsRef/);
  assert.match(workspace, /handleWfBridgeHostAction\(session\.port, request\)/);
  assert.match(commands, /wf_bridge_handle_host_action/);
  assert.match(registration, /commands::wf_bridge::wf_bridge_handle_host_action/);
});

test('embedded workbenches use the XIASS document scroll instead of a second viewport', () => {
  const workspace = read('nextgen/src/pages/XiassAgentWorkspace.tsx');
  const workspaceStyles = read('nextgen/src/pages/XiassAgentWorkspace.css');

  assert.match(workspace, /const EMBEDDED_WORKSPACE_MAX_SCROLL_DELTA = 1_200;/);
  assert.match(workspace, /function normalizeEmbeddedWorkspaceScrollDelta\(value: unknown\): number \| null/);
  assert.match(workspace, /event\.data\?\.type === 'xiass-wf-scroll'/);
  assert.match(workspace, /document\.querySelector<HTMLElement>\('\.main-wrapper'\)\?\.scrollBy/);
  assert.match(workspace, /className="page-top-strip xiass-agent-workspace__top-strip"/);
  assert.match(
    workspace,
    /className="page-tabs-row page-tabs-center page-tabs-row-with-leading xiass-agent-workspace__tabs-row"/,
  );
  assert.match(workspace, /<PlatformGroupSwitcher/);
  assert.match(workspace, /className="page-tabs filter-tabs xiass-agent-workspace__navigation"/);
  assert.match(
    workspaceStyles,
    /\.xiass-agent-workspace__canvas \{[\s\S]*?min-height: 0;[\s\S]*?overflow: visible;/,
  );
  assert.match(
    workspaceStyles,
    /\.xiass-agent-workspace__native \{[\s\S]*?height: var\(--xiass-embedded-frame-height, var\(--workspace-frame-fallback\)\);/,
  );
  assert.match(
    workspaceStyles,
    /\.xiass-agent-workspace__panel \{[\s\S]*?height: auto;[\s\S]*?overflow: visible;/,
  );

  for (const platform of ['macos', 'windows']) {
    const app = read(`${platform}/source/frontend/src/App.vue`);
    const modal = read(`${platform}/source/frontend/src/components/ui/Modal.vue`);

    assert.match(app, /function embeddedScrollableAncestorCanConsumeWheel\(target, deltaY\)/);
    assert.match(app, /function relayEmbeddedWheel\(event\)/);
    assert.match(app, /type: "xiass-wf-scroll"/);
    assert.match(app, /window\.addEventListener\("wheel", relayEmbeddedWheel, \{ capture: true, passive: false \}\)/);
    assert.match(app, /window\.removeEventListener\("wheel", relayEmbeddedWheel, true\)/);
    assert.match(modal, /\.mask\.inline \{[\s\S]*?position: static;[\s\S]*?height: auto;/);
    assert.match(modal, /\.sheet\.inline \{[\s\S]*?height: auto;[\s\S]*?overflow: visible;/);
    assert.match(modal, /\.sheet\.inline > \.body \{[\s\S]*?overflow: visible;/);
  }
});

test('embedded Codex configuration helper keeps its complete RPC surface on both platforms', () => {
  const methods = [
    'GetCodexConfiguration',
    'ApplyCodexConfiguration',
    'ApplyCodexConfigurationWithLifecycle',
    'RemoveCodexXIASSProvider',
    'MigrateCodexLegacyProvider',
    'MigrateCodexLegacyProviderWithLifecycle',
    'DiscoverCodexModels',
    'GetCodexAccountCandidates',
    'DiscoverCodexAccountModels',
    'ApplyCodexConfigurationFromAccount',
    'ApplyCodexConfigurationFromAccountWithLifecycle',
    'RestoreCodexConfiguration',
    'DeleteCodexConfigurationBackup',
    'ImportCodexLegacyConfigBackup',
    'ImportCodexLegacyHistoryBackup',
    'RepairCodexHistory',
    'RestoreCodexHistoryBackup',
    'DeleteCodexHistoryBackup',
    'StartCodexXIASSKeySelection',
    'GetCodexXIASSKeySelectionStatus',
    'CompleteCodexXIASSKeySelectionManual',
    'CancelCodexXIASSKeySelection',
    'DiscoverCodexXIASSSelectionModels',
    'ApplyCodexXIASSSelection',
    'ApplyCodexXIASSSelectionWithLifecycle',
    'GetCodexDesktopControlStatus',
    'SelectCodexDesktopInstallation',
    'SelectCodexDesktopInstallationPath',
    'LaunchCodexDesktop',
    'StopCodexDesktop',
    'RestartCodexDesktop',
  ];

  for (const platform of ['macos', 'windows']) {
    const bridge = read(`${platform}/source/wfbridge_main.go`);
    const state = read(`${platform}/source/frontend/src/state/appState.js`);
    const modal = read(`${platform}/source/frontend/src/components/CodexConfigurationModal.vue`);
    const nativeApp = appImplementationSource(platform);

    assert.match(bridge, /func prepareWFBridgeMethod\(application \*App, request wfBridgeRPCRequest\)/);
    assert.match(bridge, /reflect\.ValueOf\(application\)\.MethodByName\(methodName\)/);
    assert.match(bridge, /methodType\.NumIn\(\) != len\(request\.Args\)/);
    assert.match(bridge, /json\.Unmarshal\(request\.Args\[index\], argument\.Interface\(\)\)/);
    assert.match(bridge, /const app = new Proxy\(\{\}, \{ get: \(_, method\) => \(\.\.\.args\) => call\(String\(method\), args\) \}\)/);
    assert.match(bridge, /window\.go = \{ main: \{ App: app \} \}/);
    assert.match(modal, /startCodexXIASSKeySelection/);
    assert.match(modal, /completeCodexXIASSKeySelectionManual/);
    assert.match(modal, /applyCodexXIASSSelectionWithLifecycle/);
    assert.match(modal, /restoreCodexConfiguration/);
    assert.match(modal, /repairCodexHistory/);

    for (const method of methods) {
      // Direct wrappers call the bridge by name, while compatibility wrappers
      // deliberately keep a first-party method name in an alias list. Either
      // form is a renderer-side route through the runtime Proxy above.
      assert.match(state, new RegExp(`(?:call\\(\\s*\\"${method}\\"|:\\s*\\[\\s*\\"${method}\\"(?:\\s*,|\\s*\\]))`));

      // The bridge uses reflection by design, so individual method names do
      // not belong in wfbridge_main.go. Prove instead that each UI-visible
      // route is an exported App method in the native executable, which the
      // reflection dispatcher resolves and JSON-binds before invocation.
      assert.match(nativeApp, new RegExp(`func \\(a \\*App\\) ${method}\\(`));
    }
  }
});

test('embedded Claude Code and MCP helpers keep their complete RPC surface on both platforms', () => {
  const methods = [
    'GetClaudeCodeConfiguration',
    'ApplyClaudeCodeConfiguration',
    'GetClaudeCodeAccountCandidates',
    'ApplyClaudeCodeConfigurationFromAccount',
    'DiscoverClaudeCodeGatewayModels',
    'TestClaudeCodeGateway',
    'RestoreClaudeCodeConfiguration',
    'DeleteClaudeCodeConfigurationBackup',
    'MigrateClaudeCodeLegacyBackup',
    'GetMCPConfiguration',
    'ApplyMCPConfiguration',
    'GetCursorMCPConfiguration',
    'ApplyCursorMCPConfiguration',
    'RemoveCursorMCPConfiguration',
    'ListCursorMCPBackups',
    'RestoreCursorMCPBackup',
    'DeleteCursorMCPBackup',
    'GetWindsurfMCPConfiguration',
    'ApplyWindsurfMCPConfiguration',
    'RemoveWindsurfMCPConfiguration',
    'ListWindsurfMCPBackups',
    'RestoreWindsurfMCPBackup',
    'DeleteWindsurfMCPBackup',
    'ChooseCursorProjectMCPConfiguration',
    'GetCursorProjectMCPConfiguration',
    'ApplyCursorProjectMCPConfiguration',
    'RemoveCursorProjectMCPConfiguration',
    'ListCursorProjectMCPBackups',
    'RestoreCursorProjectMCPBackup',
    'DeleteCursorProjectMCPBackup',
  ];

  assert.equal(methods.length, 30);
  for (const platform of ['macos', 'windows']) {
    const bridge = read(`${platform}/source/wfbridge_main.go`);
    const bridgeTests = read(`${platform}/source/wfbridge_main_test.go`);
    const state = read(`${platform}/source/frontend/src/state/appState.js`);
    const claudeModal = read(`${platform}/source/frontend/src/components/ClaudeCodeConfigurationModal.vue`);
    const mcpModal = read(`${platform}/source/frontend/src/components/MCPConfigurationModal.vue`);
    const nativeApp = appImplementationSource(platform);

    assert.match(bridge, /func prepareWFBridgeMethod\(application \*App, request wfBridgeRPCRequest\)/);
    assert.match(bridge, /reflect\.ValueOf\(application\)\.MethodByName\(methodName\)/);
    assert.match(bridge, /json\.Unmarshal\(request\.Args\[index\], argument\.Interface\(\)\)/);
    assert.match(bridgeTests, /func TestWFBridgeResolvesClaudeCodeAndMCPSurfaceWithoutExecuting\(t \*testing\.T\)/);
    assert.match(bridgeTests, /Claude\/MCP embedded bridge surface has %d methods, want 30/);
    assert.match(bridgeTests, /ChooseCursorProjectMCPConfiguration/);
    assert.match(bridgeTests, /ApplyCursorMCPConfiguration/);
    assert.match(bridgeTests, /GetWindsurfProjectMCPConfiguration/);

    for (const method of methods) {
      assert.match(nativeApp, new RegExp(`func \\(a \\*App\\) ${method}\\(`));
      assert.match(bridgeTests, new RegExp(`"${method}"`));
      // Direct state wrappers call the bridge by name; target-scoped MCP
      // wrappers keep their method name in a compatibility alias list. Both
      // remain renderer routes through the embedded runtime Proxy.
      assert.match(state, new RegExp(`(?:call\\(\\s*"${method}"|:\\s*\\[\\s*"${method}"(?:\\s*,|\\s*\\]))`));
    }

    assert.match(claudeModal, /discoverClaudeCodeGatewayModels/);
    assert.match(claudeModal, /testClaudeCodeGateway/);
    assert.match(claudeModal, /restoreClaudeCodeConfiguration/);
    assert.match(claudeModal, /deleteClaudeCodeConfigurationBackup/);
    assert.match(claudeModal, /migrateClaudeCodeLegacyBackup/);
    assert.match(mcpModal, /chooseCursorProjectMCPConfiguration/);
    assert.match(mcpModal, /applyCursorProjectMCPConfiguration/);
    assert.match(mcpModal, /restoreCursorProjectMCPBackup/);
    assert.match(mcpModal, /deleteCursorProjectMCPBackup/);

    // Cursor has an explicit project-level MCP capability. Windsurf does not:
    // it must remain limited to its verified global MCP configuration rather
    // than accepting a renderer-controlled project path.
    assert.doesNotMatch(nativeApp, /WindsurfProjectMCP/);
    assert.doesNotMatch(state, /WindsurfProjectMCP/);
    assert.doesNotMatch(mcpModal, /WindsurfProjectMCP/);
  }
});
