#!/usr/bin/env node

const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const dist = path.join(root, 'dist');

function fail(message) {
  console.error(message);
  process.exit(1);
}

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

function walk(directory) {
  if (!fs.existsSync(directory)) return [];
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const absolute = path.join(directory, entry.name);
    return entry.isDirectory() ? walk(absolute) : [absolute];
  });
}

const windsurfIcon = read('src/components/icons/WindsurfIcon.tsx');
if (!/assets\/icons\/windsurf\.svg/.test(windsurfIcon) || /devin\.png/.test(windsurfIcon)) {
  fail('Windsurf must use the Windsurf asset and must not reuse the Devin icon.');
}

const sideNav = read('src/components/layout/SideNav.tsx');
if (!/src-tauri\/icons\/app-icon-source\.png/.test(sideNav)) {
  fail('The product shell must use the user-provided transparent XIASS source logo.');
}

const forbiddenPublicPaths = [
  'public/donate/alipay.png',
  'public/donate/wechat.png',
  'public/vite.svg',
];
for (const relativePath of forbiddenPublicPaths) {
  if (fs.existsSync(path.join(root, relativePath))) {
    fail(`Unrelated public asset must be removed: ${relativePath}`);
  }
}

if (!fs.existsSync(path.join(dist, 'index.html'))) {
  fail('Production dist is missing; run the web build before asset-scope verification.');
}

const forbiddenDistPatterns = [
  /(^|\/)donate\//i,
  /(^|\/)vite\.svg$/i,
  /(^|\/)(?:apikey-fun|devin|codebuddy|kiro|qoder|trae(?:-solo)?|workbuddy|zcode|zed)-/i,
];
const relativeDistFiles = walk(dist).map((file) => path.relative(dist, file).replaceAll('\\', '/'));
const forbiddenDistFile = relativeDistFiles.find((file) =>
  forbiddenDistPatterns.some((pattern) => pattern.test(file)),
);
if (forbiddenDistFile) {
  fail(`Production dist contains an unrelated product asset: ${forbiddenDistFile}`);
}

const forbiddenDistContentPatterns = [
  { pattern: /apikey\.fun/i, label: 'legacy APIKEY.FUN brand' },
  { pattern: /\bcc[\s_-]*switch\b/i, label: 'legacy CC Switch brand' },
  // Provider templates use the original sponsor-module transport for
  // configuration data. Do not treat that account setup feature as a banner
  // or advertisement; actual ad commands and promotion UI remain forbidden.
  {
    pattern: /announcement_(?:get|force_refresh)_top_right_ad/i,
    label: 'remote promotion command',
  },
  { pattern: /(?:app-)?global-(?:promo|ad-slot)/i, label: 'promotion UI styling' },
  {
    pattern: /[?&](?:aff|ref|invitecode|utm_[a-z_]+|ytag|ch|ic)=/i,
    label: 'affiliate or referral query parameter',
  },
];

for (const relativePath of relativeDistFiles.filter((file) => /\.(?:css|html|js)$/i.test(file))) {
  const source = fs.readFileSync(path.join(dist, relativePath), 'utf8');
  const forbidden = forbiddenDistContentPatterns.find(({ pattern }) => pattern.test(source));
  if (forbidden) {
    fail(`Production dist contains ${forbidden.label}: ${relativePath}`);
  }
}

if (!relativeDistFiles.some((file) => /(^|\/)windsurf-[^/]+\.svg$/i.test(file))) {
  fail('Production dist does not contain the Windsurf icon.');
}

console.log('XIASS production asset scope verified.');
