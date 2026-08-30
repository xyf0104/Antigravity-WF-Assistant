#!/usr/bin/env node

const fs = require('node:fs');
const path = require('node:path');

function parseArgs(argv) {
  const args = {};
  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (!token.startsWith('--')) continue;
    const key = token.slice(2);
    const value = argv[index + 1];
    if (!value || value.startsWith('--')) {
      throw new Error(`Missing value for --${key}`);
    }
    args[key] = value;
    index += 1;
  }
  return args;
}

function requireNonEmpty(value, label) {
  const normalized = String(value || '').trim();
  if (!normalized) {
    throw new Error(`${label} is required`);
  }
  return normalized;
}

function buildReleaseConfig(options) {
  const repo = requireNonEmpty(options.repo, 'GitHub repository');
  if (!/^[^/\s]+\/[^/\s]+$/.test(repo)) {
    throw new Error(`Invalid GitHub repository: ${repo}`);
  }
  const pubkey = requireNonEmpty(options.pubkey, 'TAURI_UPDATER_PUBLIC_KEY');
  if (pubkey.length < 40) {
    throw new Error('TAURI_UPDATER_PUBLIC_KEY is too short to be a valid updater public key');
  }

  return {
    bundle: {
      createUpdaterArtifacts: true,
      macOS: {
        signingIdentity: '-',
      },
    },
    plugins: {
      updater: {
        pubkey,
        endpoints: [
          `https://github.com/${repo}/releases/latest/download/latest-{{target}}.json`,
        ],
      },
    },
  };
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const output = path.resolve(
    args.output || 'src-tauri/tauri.release.generated.conf.json',
  );
  const config = buildReleaseConfig({
    repo: args.repo || process.env.GITHUB_REPOSITORY,
    pubkey: process.env.TAURI_UPDATER_PUBLIC_KEY,
  });
  fs.mkdirSync(path.dirname(output), { recursive: true });
  fs.writeFileSync(output, `${JSON.stringify(config, null, 2)}\n`, {
    encoding: 'utf8',
    mode: 0o600,
  });
  console.log(`Generated updater-enabled Tauri release config: ${output}`);
}

if (require.main === module) {
  try {
    main();
  } catch (error) {
    console.error(`[generate_tauri_release_config] ${error.message}`);
    process.exit(1);
  }
}

module.exports = { buildReleaseConfig };
