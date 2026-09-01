#!/usr/bin/env node

import { randomBytes } from 'node:crypto';
import { spawn } from 'node:child_process';
import { access, mkdtemp, mkdir, rm, stat } from 'node:fs/promises';
import { constants as fsConstants } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import readline from 'node:readline';

const READY_TIMEOUT_MS = 20_000;
const REQUEST_TIMEOUT_MS = 8_000;
const SHUTDOWN_TIMEOUT_MS = 8_000;
const MAX_CAPTURED_OUTPUT = 16 * 1024;

const READ_ONLY_RPC_METHODS = Object.freeze([
  // This is the exact bounded status method used for the first Antigravity
  // dashboard paint. Full GetPatchStatus intentionally performs a deep
  // installation scan and belongs in the dedicated platform fixture suite,
  // not in an installer-startup timeout gate.
  'GetQuickPatchStatus',
  'GetCodexConfiguration',
  'GetClaudeCodeConfiguration',
  'GetCursorMCPConfiguration',
  'GetWindsurfMCPConfiguration',
  'GetAgentStatuses',
]);

function parseArguments(argv) {
  const values = new Map();
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (!argument.startsWith('--')) {
      throw new Error(`Unexpected argument: ${argument}`);
    }
    const value = argv[index + 1];
    if (!value || value.startsWith('--')) {
      throw new Error(`Missing value for ${argument}`);
    }
    values.set(argument, value);
    index += 1;
  }

  const binary = values.get('--binary');
  if (!binary) {
    throw new Error('Usage: node smoke_wf_bridge_sidecar.mjs --binary <path> --platform <macos|windows>');
  }

  const platform = values.get('--platform');
  if (platform !== 'macos' && platform !== 'windows') {
    throw new Error('The --platform value must be macos or windows');
  }
  if (values.size !== 2) {
    throw new Error('Only --binary and --platform are supported');
  }

  return { binary: path.resolve(binary), platform };
}

async function assertExecutable(binary, platform) {
  const details = await stat(binary).catch(() => null);
  if (!details?.isFile()) {
    throw new Error('The supplied WF bridge binary does not exist or is not a file');
  }
  if (platform !== 'windows') {
    await access(binary, fsConstants.X_OK).catch(() => {
      throw new Error('The supplied macOS WF bridge binary is not executable');
    });
  }
}

function appendCapture(current, chunk) {
  const next = `${current}${chunk.toString()}`;
  return next.length <= MAX_CAPTURED_OUTPUT ? next : next.slice(-MAX_CAPTURED_OUTPUT);
}

function createRedactor(values) {
  return (raw) => {
    let text = String(raw ?? '');
    for (const value of values) {
      if (value) {
        text = text.split(value).join('<redacted>');
      }
    }
    return text;
  };
}

function withTimeout(promise, timeoutMs, message) {
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error(message)), timeoutMs);
    }),
  ]).finally(() => clearTimeout(timer));
}

async function createIsolatedEnvironment(platform) {
  const root = await mkdtemp(path.join(os.tmpdir(), 'xiass-wf-bridge-smoke-'));
  const home = path.join(root, 'home');
  const data = path.join(root, 'data');
  const temporary = path.join(root, 'tmp');
  const appData = path.join(root, 'app-data');
  const config = path.join(root, 'config');
  const gemini = path.join(root, 'antigravity');
  const codexHome = path.join(root, 'codex-home');
  const claudeConfig = path.join(root, 'claude-config');
  await Promise.all([
    home,
    data,
    temporary,
    appData,
    config,
    gemini,
    codexHome,
    claudeConfig,
  ].map((directory) => mkdir(directory, { recursive: true, mode: 0o700 })));

  const token = randomBytes(32).toString('hex');
  const homeDrive = path.parse(home).root.replace(/[\\/]+$/, '') || path.parse(home).root;
  const environment = {
    ...process.env,
    HOME: home,
    USERPROFILE: home,
    HOMEDRIVE: homeDrive,
    HOMEPATH: path.relative(homeDrive || home, home) || path.sep,
    APPDATA: appData,
    LOCALAPPDATA: appData,
    XDG_CONFIG_HOME: config,
    CODEX_HOME: codexHome,
    CLAUDE_CONFIG_DIR: claudeConfig,
    ANTIGRAVITY_WF_GEMINI_DIR: gemini,
    XIASS_TOOLS_DATA_DIR: data,
    XIASS_WF_RPC_TOKEN: token,
    XIASS_WF_RPC_PORT: '0',
    TMPDIR: temporary,
    TMP: temporary,
    TEMP: temporary,
  };
  delete environment.XIASS_PARENT_PID;

  if (platform !== 'windows') {
    delete environment.HOMEDRIVE;
    delete environment.HOMEPATH;
  }

  return { root, token, environment };
}

function startBridge(binary, environment, redact) {
  const child = spawn(binary, [], {
    cwd: path.dirname(binary),
    env: environment,
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });

  let stdout = '';
  let stderr = '';
  child.stdout.on('data', (chunk) => {
    stdout = appendCapture(stdout, chunk);
  });
  child.stderr.on('data', (chunk) => {
    stderr = appendCapture(stderr, chunk);
  });

  const describeOutput = () => {
    const fragments = [];
    if (stdout.trim()) fragments.push(`stdout=${redact(stdout.trim())}`);
    if (stderr.trim()) fragments.push(`stderr=${redact(stderr.trim())}`);
    return fragments.length > 0 ? ` (${fragments.join('; ')})` : '';
  };

  const ready = withTimeout(new Promise((resolve, reject) => {
    let settled = false;
    const fail = (error) => {
      if (settled) return;
      settled = true;
      reject(error);
    };
    const succeed = (value) => {
      if (settled) return;
      settled = true;
      resolve(value);
    };
    const lines = readline.createInterface({ input: child.stdout });
    lines.on('line', (line) => {
      const trimmed = line.trim();
      if (!trimmed) return;
      try {
        const message = JSON.parse(trimmed);
        if (message?.event === 'ready') {
          succeed(message);
        }
      } catch {
        // The bridge's only expected stdout message is its ready event. Keep
        // accepting future benign output, but require a valid ready object.
      }
    });
    child.once('error', (error) => fail(new Error(`Could not start the WF bridge: ${redact(error.message)}`)));
    child.once('exit', (code, signal) => {
      fail(new Error(`WF bridge exited before readiness (code=${code ?? 'none'}, signal=${signal ?? 'none'})${describeOutput()}`));
    });
  }), READY_TIMEOUT_MS, 'WF bridge did not emit a ready event within 20 seconds');

  return { child, ready, describeOutput };
}

function assertReadyMessage(message) {
  if (
    !message
    || message.event !== 'ready'
    || message.service !== 'xiass-wf-bridge'
    || message.host !== '127.0.0.1'
    || !Number.isInteger(message.port)
    || message.port < 1
    || message.port > 65535
    || message.schemaVersion !== 1
  ) {
    throw new Error('WF bridge emitted an invalid ready event');
  }
}

async function requestJSON(url, init, label) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  try {
    const response = await fetch(url, { ...init, signal: controller.signal });
    const text = await response.text();
    let body;
    try {
      body = JSON.parse(text);
    } catch {
      throw new Error(`${label} returned non-JSON content (HTTP ${response.status})`);
    }
    if (!response.ok) {
      throw new Error(`${label} failed with HTTP ${response.status}`);
    }
    return body;
  } catch (error) {
    if (error?.name === 'AbortError') {
      throw new Error(`${label} timed out after ${REQUEST_TIMEOUT_MS / 1000} seconds`);
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
}

function assertNoSensitiveRuntimeValues(value, isolated) {
  const serialized = JSON.stringify(value);
  for (const forbidden of [isolated.token, isolated.root]) {
    if (forbidden && serialized.includes(forbidden)) {
      throw new Error('WF bridge response exposed a smoke-test secret or raw local path');
    }
  }
}

async function verifyBridge(ready, isolated) {
  const baseURL = `http://${ready.host}:${ready.port}`;
  const authorization = `Bearer ${isolated.token}`;
  const health = await requestJSON(`${baseURL}/health`, { method: 'GET' }, 'WF bridge health check');
  if (health?.ok !== true || health?.service !== 'xiass-wf-bridge') {
    throw new Error('WF bridge health check returned an unexpected payload');
  }
  assertNoSensitiveRuntimeValues(health, isolated);

  for (const method of READ_ONLY_RPC_METHODS) {
    const body = await requestJSON(
      `${baseURL}/rpc`,
      {
        method: 'POST',
        headers: {
          Authorization: authorization,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ method, args: [] }),
      },
      `WF bridge RPC ${method}`,
    );
    if (body?.ok !== true || !Object.hasOwn(body, 'result')) {
      throw new Error(`WF bridge RPC ${method} returned an unexpected payload`);
    }
    assertNoSensitiveRuntimeValues(body, isolated);
  }

  // Run a second authenticated request after the full workspace set to catch
  // a bridge that only succeeds during initialization and then tears down.
  const repeat = await requestJSON(
    `${baseURL}/rpc`,
    {
      method: 'POST',
      headers: {
        Authorization: authorization,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ method: 'GetAgentStatuses', args: [] }),
    },
    'WF bridge repeated GetAgentStatuses RPC',
  );
  if (repeat?.ok !== true || !Object.hasOwn(repeat, 'result')) {
    throw new Error('WF bridge did not remain available after its initial RPC checks');
  }
  assertNoSensitiveRuntimeValues(repeat, isolated);
}

async function stopBridge(child) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = new Promise((resolve) => child.once('exit', resolve));
  child.kill();
  try {
    await withTimeout(exited, SHUTDOWN_TIMEOUT_MS, 'WF bridge did not exit after shutdown');
  } catch (error) {
    child.kill('SIGKILL');
    await withTimeout(exited, 2_000, 'WF bridge could not be force-stopped');
    throw error;
  }
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  await assertExecutable(options.binary, options.platform);
  const isolated = await createIsolatedEnvironment(options.platform);
  const redact = createRedactor([isolated.root, isolated.token]);
  let bridge;
  try {
    bridge = startBridge(options.binary, isolated.environment, redact);
    const ready = await bridge.ready;
    assertReadyMessage(ready);
    await verifyBridge(ready, isolated);
    await stopBridge(bridge.child);
    console.log(`WF bridge installer smoke passed for ${options.platform}`);
  } catch (error) {
    if (bridge?.child) {
      try {
        await stopBridge(bridge.child);
      } catch {
        // Preserve the original failure. The isolated temp root is removed
        // below regardless of a failed child cleanup.
      }
    }
    const detail = bridge?.describeOutput?.() ?? '';
    throw new Error(`${redact(error?.message || error)}${detail}`);
  } finally {
    await rm(isolated.root, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(`WF bridge installer smoke failed: ${error.message}`);
  process.exitCode = 1;
});
