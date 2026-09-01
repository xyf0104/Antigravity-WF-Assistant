const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const root = path.resolve(__dirname, '..', '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

test('Claude Code MCP workspace uses the registered, constrained native command surface', () => {
  const service = read('src/services/claudeMcpService.ts');
  const commands = read('src-tauri/src/commands/claude_mcp.rs');
  const registration = read('src-tauri/src/lib.rs');

  for (const command of [
    'claude_mcp_get_managed_status',
    'claude_mcp_configure_managed_http',
    'claude_mcp_remove_managed',
  ]) {
    assert.match(service, new RegExp(`['"]${command}['"]`));
    assert.match(commands, new RegExp(`fn\\s+${command}\\b`));
    assert.match(registration, new RegExp(`commands::claude_mcp::${command}`));
  }

  assert.match(service, /remoteUrl:\s*validation\.normalizedUrl/);
  assert.match(service, /MAX_REMOTE_URL_LENGTH = 2_048/);
  assert.match(service, /parsed\.username[\s\S]*parsed\.password[\s\S]*parsed\.search[\s\S]*parsed\.hash/);
  assert.match(service, /CLAUDE_MANAGED_MCP_SERVER_NAME = 'xiass-tools'/);
});

test('Claude Code MCP panel exposes only the fixed XIASS entry and retains no endpoint', () => {
  const panel = read('src/components/claude/ClaudeManagedMcpPanel.tsx');
  const page = read('src/pages/ClaudeAccountsPage.tsx');

  assert.match(panel, /<section className="claude-managed-mcp-panel" aria-labelledby="claude-managed-mcp-title">/);
  assert.match(panel, /id="claude-managed-mcp-endpoint"/);
  assert.match(panel, /aria-label="检查 Claude Code MCP 状态"/);
  assert.match(panel, /更新受管 MCP/);
  assert.match(panel, /确认移除/);
  assert.match(panel, /不会读取、列出或修改 Claude Code 中的其他 MCP 条目/);
  assert.match(panel, /setEndpoint\(''\);/);
  assert.match(panel, /地址不会显示在状态、日志或诊断导出中/);
  assert.match(panel, /需要认证时，请在 Claude Code 中执行 \/mcp/);
  assert.doesNotMatch(panel, /localStorage|sessionStorage|navigator\.clipboard|console\./);

  assert.match(page, /aria-label="打开 Claude Code MCP"/);
  assert.match(page, /<ClaudeManagedMcpPanel onBack=\{\(\) => setActiveSection\('cli'\)\} \/>/);
  assert.match(page, /const isMcpSection = activeSection === 'mcp'/);
});

test('native Claude MCP projection keeps CLI output and endpoints off the renderer boundary', () => {
  const nativeModule = read('src-tauri/src/modules/claude_mcp.rs');

  assert.match(nativeModule, /const MANAGED_SERVER_NAME: &str = "xiass-tools"/);
  assert.match(nativeModule, /struct ClaudeManagedMcpStatus \{[\s\S]*cli_available:[\s\S]*managed_server_configured:[\s\S]*state:[\s\S]*message:/);
  assert.doesNotMatch(
    nativeModule.match(/pub struct ClaudeManagedMcpStatus \{[\s\S]*?\n\}/)?.[0] || '',
    /remote_url|stdout|stderr|cli_path|endpoint/i,
  );
  assert.match(nativeModule, /assert!\(!serialized\.contains\(secret\)\);/);
  assert.match(nativeModule, /assert!\(!serialized\.contains\("mcp\.example\.test"\)\);/);
});
