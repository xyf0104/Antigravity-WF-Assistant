#!/usr/bin/env node

const { createHash } = require('node:crypto');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const nextgenRoot = path.resolve(__dirname, '..');
const repositoryRoot = path.resolve(nextgenRoot, '..');
const platform = process.env.XIASS_WF_BRIDGE_PLATFORM || process.platform;

const platformDirectory =
  platform === 'win32' || platform === 'windows'
    ? 'windows'
    : platform === 'darwin' || platform === 'macos'
      ? 'macos'
      : null;

if (!platformDirectory) {
  console.error(`Unsupported WF bridge frontend platform: ${platform}`);
  process.exit(1);
}

const frontendDirectory = path.join(
  repositoryRoot,
  platformDirectory,
  'source',
  'frontend',
);
const packageLockPath = path.join(frontendDirectory, 'package-lock.json');
const nodeModulesDirectory = path.join(frontendDirectory, 'node_modules');
const dependencyMarkerPath = path.join(
  nodeModulesDirectory,
  '.xiass-package-lock.sha256',
);
const distDirectory = path.join(frontendDirectory, 'dist');
const npmCommand = 'npm';

function run(command, args) {
  console.log(`$ ${command} ${args.join(' ')}`);
  const result = spawnSync(command, args, {
    cwd: frontendDirectory,
    stdio: 'inherit',
    shell: process.platform === 'win32',
    env: process.env,
  });

  if (result.error) {
    console.error(result.error.message);
    process.exit(1);
  }
  if (result.status !== 0) {
    process.exit(typeof result.status === 'number' ? result.status : 1);
  }
}

function sha256(filePath) {
  return createHash('sha256').update(fs.readFileSync(filePath)).digest('hex');
}

function dependenciesAreCurrent(expectedHash) {
  try {
    return (
      fs.statSync(nodeModulesDirectory).isDirectory() &&
      fs.readFileSync(dependencyMarkerPath, 'utf8').trim() === expectedHash
    );
  } catch {
    return false;
  }
}

function verifyDist() {
  const indexPath = path.join(distDirectory, 'index.html');
  const assetsDirectory = path.join(distDirectory, 'assets');
  if (!fs.existsSync(indexPath) || fs.statSync(indexPath).size === 0) {
    throw new Error(`WF bridge frontend is missing ${indexPath}`);
  }
  if (!fs.existsSync(assetsDirectory) || !fs.statSync(assetsDirectory).isDirectory()) {
    throw new Error(`WF bridge frontend is missing ${assetsDirectory}`);
  }
  const assets = fs
    .readdirSync(assetsDirectory)
    .filter((name) => /\.(?:css|js)$/u.test(name))
    .filter((name) => fs.statSync(path.join(assetsDirectory, name)).size > 0);
  if (assets.length === 0) {
    throw new Error('WF bridge frontend contains no non-empty JavaScript or CSS assets');
  }
}

if (!fs.existsSync(packageLockPath)) {
  console.error(`WF bridge frontend package lock is missing: ${packageLockPath}`);
  process.exit(1);
}

const dependencyHash = sha256(packageLockPath);
if (!dependenciesAreCurrent(dependencyHash)) {
  run(npmCommand, ['ci', '--ignore-scripts', '--no-audit', '--no-fund']);
  fs.writeFileSync(dependencyMarkerPath, `${dependencyHash}\n`);
}

run(npmCommand, ['run', 'build']);

try {
  verifyDist();
  console.log(`WF bridge frontend ready: ${platformDirectory}/source/frontend/dist`);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}
