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
const thirdPartyNotices = read('nextgen/THIRD_PARTY_NOTICES.md');
const tauriConfig = JSON.parse(read('nextgen/src-tauri/tauri.conf.json'));
const aboutPage = read('nextgen/src/pages/SettingsPageView.tsx');
const legalNoticesModule = read('nextgen/src-tauri/src/modules/legal_notices.rs');
const webdavPage = read('nextgen/src/components/SettingsWebdavSyncSection.tsx');
const windsurfIcon = read('nextgen/src/components/icons/WindsurfIcon.tsx');
const sideNav = read('nextgen/src/components/layout/SideNav.tsx');

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
  thirdPartyNotices,
  /CLIProxyAPI v7\.2\.140[\s\S]*Tauri[\s\S]*React[\s\S]*jsQR[\s\S]*Lucide[\s\S]*protobuf\.js/,
  'Third-party notices must identify the bundled sidecar and desktop runtimes.',
);

const requiredBundleLicenseResources = {
  '../../LICENSE': 'licenses/XIASS-Tools-MIT.txt',
  '../CC-BY-NC-SA-4.0-LEGALCODE.txt': 'licenses/CC-BY-NC-SA-4.0-LEGALCODE.txt',
  '../LICENSE': 'licenses/XIASS-Tools-Nextgen-CC-BY-NC-SA-4.0.txt',
  '../ORIGIN_AND_LICENSE.md': 'licenses/ORIGIN_AND_LICENSE.md',
  '../THIRD_PARTY_NOTICES.md': 'licenses/THIRD_PARTY_NOTICES.md',
  '../node_modules/@tauri-apps/api/LICENSE_APACHE-2.0': 'licenses/Tauri-APACHE-2.0.txt',
  '../node_modules/@tauri-apps/api/LICENSE_MIT': 'licenses/Tauri-MIT.txt',
  '../node_modules/jsqr/LICENSE': 'licenses/jsQR-APACHE-2.0.txt',
  '../node_modules/lucide-react/LICENSE': 'licenses/Lucide-ISC-and-MIT.txt',
  '../node_modules/protobufjs/LICENSE': 'licenses/protobufjs-BSD-3-Clause.txt',
  '../node_modules/react/LICENSE': 'licenses/React-MIT.txt',
  '../sidecars/cockpit-cliproxy/third_party/CLIProxyAPI/LICENSE': 'licenses/CLIProxyAPI-MIT.txt',
};
const bundleResources = tauriConfig?.bundle?.resources || {};
for (const [source, destination] of Object.entries(requiredBundleLicenseResources)) {
  if (bundleResources[source] !== destination) {
    throw new Error(`Tauri bundle must ship ${source} as ${destination}.`);
  }
  const sourcePath = path.resolve(repositoryRoot, 'nextgen', 'src-tauri', source);
  if (!fs.existsSync(sourcePath) || !fs.statSync(sourcePath).isFile() || fs.statSync(sourcePath).size === 0) {
    throw new Error(`Bundled license resource is missing or empty: ${source}`);
  }
}
requireMatch(
  aboutPage,
  /licensesAndNotices[\s\S]*licenseNoticeOpen/,
  'The in-app About view must expose the license-notice entry point.',
);
requireMatch(
  aboutPage,
  /loadLegalNotices\(\)/,
  'The in-app license notice must load the fixed offline document catalog.',
);
requireMatch(
  legalNoticesModule,
  /LEGAL_NOTICE_SPECS[\s\S]*origin_and_license[\s\S]*third_party_notices[\s\S]*cc_by_nc_sa_4_0[\s\S]*xiass_nextgen_license/,
  'The native legal-notice catalog must retain every fixed bundled declaration.',
);
requireMatch(
  legalNoticesModule,
  /pub fn load_bundled_legal_notices\(app: &AppHandle\)/,
  'The legal notice catalog must remain available from installed app resources.',
);
forbidMatch(
  aboutPage,
  /github\.com\/jlcodes99\/cockpit-tools|CC BY-NC-SA 4\.0/,
  'The default XIASS About source must not render upstream branding outside the offline notice viewer.',
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
requireMatch(
  windsurfIcon,
  /assets\/icons\/windsurf\.svg/,
  'Windsurf must use its own product icon.',
);
forbidMatch(
  windsurfIcon,
  /devin\.png/,
  'Windsurf must not reuse the Devin icon.',
);
requireMatch(
  sideNav,
  /src-tauri\/icons\/app-icon-source\.png/,
  'The product shell must use the user-provided transparent XIASS logo.',
);

for (const relativePath of [
  'nextgen/public/donate/alipay.png',
  'nextgen/public/donate/wechat.png',
  'nextgen/public/vite.svg',
]) {
  if (fs.existsSync(path.join(repositoryRoot, relativePath))) {
    throw new Error(`Unrelated public asset must be removed: ${relativePath}`);
  }
}

console.log('XIASS brand and license guard passed.');
