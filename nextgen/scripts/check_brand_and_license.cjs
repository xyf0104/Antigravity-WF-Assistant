#!/usr/bin/env node

const fs = require('node:fs');
const path = require('node:path');

const nextgenRoot = path.resolve(__dirname, '..');
const repositoryRoot = path.resolve(nextgenRoot, '..');

function read(relativeToRepository) {
  return fs.readFileSync(path.join(repositoryRoot, relativeToRepository), 'utf8');
}

function requireMatch(source, pattern, message) {
  if (!pattern.test(source)) {
    throw new Error(message);
  }
}

function forbidMatch(source, pattern, message) {
  if (pattern.test(source)) {
    throw new Error(message);
  }
}

const rootReadme = read('README.md');
const originNotice = read('nextgen/ORIGIN_AND_LICENSE.md');
const aboutPage = read('nextgen/src/pages/SettingsPageView.tsx');
const webdavPage = read('nextgen/src/components/SettingsWebdavSyncSection.tsx');

requireMatch(
  rootReadme,
  /MIT\s*\+\s*CC%20BY--NC--SA%204\.0|MIT License[\s\S]*CC BY-NC-SA 4\.0/,
  'README must describe both the MIT and CC BY-NC-SA scopes.',
);
forbidMatch(
  rootReadme,
  /代码按\s*\[MIT License\]\(LICENSE\)\s*发布/,
  'README must not claim that all imported source is MIT licensed.',
);
requireMatch(
  originNotice,
  /jlcodes99\/cockpit-tools[\s\S]*CC BY-NC-SA 4\.0/,
  'The imported Cockpit revision and CC BY-NC-SA notice must be preserved.',
);
requireMatch(
  aboutPage,
  /github\.com\/jlcodes99\/cockpit-tools[\s\S]*settings\.about\.upstreamAttribution[\s\S]*jlcodes99 \/ cockpit-tools · CC BY-NC-SA 4\.0/,
  'The in-app About view must retain the required upstream attribution.',
);
requireMatch(
  webdavPage,
  /useState\('xiass-tools'\)/,
  'New WebDAV settings must default to the XIASS directory.',
);
forbidMatch(
  webdavPage,
  /[“"'](?:我的坚果云\/)?cockpit-tools[”"']/,
  'User-facing WebDAV instructions must not suggest the legacy Cockpit directory.',
);

console.log('XIASS brand and license guard passed.');
