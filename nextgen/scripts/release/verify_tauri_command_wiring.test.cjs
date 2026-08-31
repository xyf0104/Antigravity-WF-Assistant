const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const NEXTGEN_ROOT = path.resolve(__dirname, '../..');
const FRONTEND_ROOT = path.join(NEXTGEN_ROOT, 'src');
const TAURI_LIB = path.join(NEXTGEN_ROOT, 'src-tauri', 'src', 'lib.rs');

function walkSourceFiles(root, result = []) {
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    if (['dist', 'node_modules', 'target'].includes(entry.name)) continue;
    const fullPath = path.join(root, entry.name);
    if (entry.isDirectory()) walkSourceFiles(fullPath, result);
    else if (entry.isFile() && /\.(?:ts|tsx)$/.test(entry.name)) result.push(fullPath);
  }
  return result;
}

function collectLiteralInvokes() {
  const commands = new Map();
  const invokePattern = /\binvoke(?:<[^;()]*?>)?\s*\(\s*(["'`])([^"'`$]+)\1/g;

  for (const file of walkSourceFiles(FRONTEND_ROOT)) {
    const source = fs.readFileSync(file, 'utf8');
    for (const match of source.matchAll(invokePattern)) {
      const command = match[2];
      const locations = commands.get(command) ?? [];
      locations.push(path.relative(NEXTGEN_ROOT, file).replace(/\\/g, '/'));
      commands.set(command, locations);
    }
  }

  return commands;
}

function collectRegisteredCommands() {
  const source = fs.readFileSync(TAURI_LIB, 'utf8');
  const start = source.indexOf('.invoke_handler(tauri::generate_handler![');
  assert.notEqual(start, -1, 'Tauri generate_handler registration block is missing');
  const end = source.indexOf('])', start);
  assert.notEqual(end, -1, 'Tauri generate_handler registration block is unterminated');

  return new Set(
    [...source.slice(start, end).matchAll(/(?:commands|modules)::[\w:]+::([A-Za-z_][A-Za-z0-9_]*)/g)]
      .map((match) => match[1]),
  );
}

function expectedDynamicCommands() {
  const commands = new Set([
    'refresh_current_quota',
    'refresh_current_codex_quota',
    'refresh_all_claude_quotas',
    'refresh_all_windsurf_tokens',
    'refresh_all_cursor_tokens',
    'antigravity_legacy_start_instance',
    'start_instance',
  ]);
  const prefixes = [
    '',
    'antigravity_legacy',
    'codex',
    'claude',
    'github_copilot',
    'windsurf',
    'kiro',
    'cursor',
    'grok',
    'codebuddy',
    'codebuddy_cn',
    'qoder',
    'trae',
    'workbuddy',
    'zcode',
  ];
  const instanceCommands = [
    'get_instance_defaults',
    'list_instances',
    'create_instance',
    'update_instance',
    'delete_instance',
    'start_instance',
    'stop_instance',
    'close_all_instances',
    'open_instance_window',
  ];

  for (const prefix of prefixes) {
    for (const command of instanceCommands) {
      commands.add(prefix ? `${prefix}_${command}` : command);
    }
  }
  return commands;
}

test('every literal frontend Tauri invoke is registered by the native host', () => {
  const invokes = collectLiteralInvokes();
  const registered = collectRegisteredCommands();
  const missing = [...invokes.entries()]
    .filter(([command]) => !command.startsWith('plugin:') && !registered.has(command))
    .map(([command, locations]) => `${command} (${[...new Set(locations)].join(', ')})`)
    .sort();

  assert.deepEqual(
    missing,
    [],
    `Frontend invokes native commands that are not registered:\n${missing.join('\n')}`,
  );
});

test('every finite dynamic Tauri invoke target is registered by the native host', () => {
  const registered = collectRegisteredCommands();
  const missing = [...expectedDynamicCommands()].filter((command) => !registered.has(command)).sort();

  assert.deepEqual(
    missing,
    [],
    `Dynamic frontend invoke targets are not registered:\n${missing.join('\n')}`,
  );
});
