const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const NEXTGEN_ROOT = path.resolve(__dirname, '../..');

function read(relativePath) {
  return fs.readFileSync(path.join(NEXTGEN_ROOT, relativePath), 'utf8');
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

test('Codex provider searches expose stable accessible names', () => {
  const source = read('src/components/codex/CodexModelProviderManagerView.tsx');
  const requiredLabels = [
    '搜索模型供应商',
    '搜索待测试供应商',
    '搜索已有 API Key…',
    '搜索 API Key',
    '搜索 Codex 实例',
  ];

  for (const label of requiredLabels) {
    assert.match(
      source,
      new RegExp(`aria-label=\\{t\\([\\s\\S]{0,240}?${escapeRegExp(label)}`),
    );
  }
  assert.doesNotMatch(source, /placeholder=\{t\("common\.search", "搜索\.\.\."\)\}/);
});

test('dashboard stat cards separate navigation from the interactive icon', () => {
  const source = read('src/pages/DashboardPage.tsx');
  const styles = read('src/pages/DashboardPage.css');

  assert.match(source, /className=\{`stat-icon-bg \$\{iconClass\} stat-icon-trigger`\}/);
  assert.match(source, /aria-label=\{t\('dashboard\.interactiveIcon', '互动图标'\)\}/);
  assert.match(source, /className="stat-card-navigation"/);
  assert.doesNotMatch(source, /className=\{`stat-icon-bg[^]*?onClick=\{\(event\) =>/);
  assert.match(styles, /\.stat-card-navigation\s*\{[^}]*flex:\s*1 1 auto;/s);
});

test('settings switches inherit their visible row title when the switch label has no text', () => {
  const source = read('src/pages/SettingsPageView.tsx');

  assert.match(source, /const wrappingLabel = control\.closest\('label'\);/);
  assert.match(source, /wrappingLabel\?\.textContent\?\.trim\(\)/);
  assert.match(source, /control\.setAttribute\('aria-label', label\);/);
  assert.doesNotMatch(source, /\|\| control\.closest\('label'\)/);
});

test('Codex session title search has an accessible name', () => {
  const source = read('src/components/codex/CodexSessionManager.tsx');
  assert.match(
    source,
    /aria-label=\{t\('codex\.sessionManager\.search\.titleAria', '按会话标题搜索'\)\}/,
  );
});

test('Codex session toolbar responds to its workspace width instead of only the viewport', () => {
  const styles = read('src/styles/pages/codex-session-manager.css');
  const managerRule = styles.match(/\.codex-session-manager\s*\{([\s\S]*?)\}/)?.[1] || '';
  const compactRule = styles.match(/@container\s*\(max-width:\s*620px\)\s*\{([\s\S]*?)\n\}/)?.[1] || '';

  assert.match(managerRule, /container-type:\s*inline-size;/);
  assert.match(compactRule, /\.codex-session-manager__kind-filter/);
  assert.match(compactRule, /grid-column:\s*1\s*\/\s*-1;/);
  assert.match(compactRule, /\.single-select-dropdown-trigger/);
  assert.match(compactRule, /width:\s*100%;/);
});

test('Codex API service hero responds to the Agent workspace width', () => {
  const styles = read('src/pages/CodexApiServicePage.css');
  const pageRule = styles.match(/\.codex-api-service-page\s*\{([\s\S]*?)\}/)?.[1] || '';

  assert.match(pageRule, /container-type:\s*inline-size;/);
  assert.match(pageRule, /container-name:\s*codex-api-service;/);
  assert.match(styles, /@container\s+codex-api-service\s*\(max-width:\s*720px\)/);
  assert.match(
    styles,
    /@container\s+codex-api-service[\s\S]*?\.codex-api-service-hero\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0,\s*1fr\);/,
  );
});

test('WebDAV actions respond to the settings canvas width instead of the viewport', () => {
  const source = read('src/components/SettingsWebdavSyncSection.tsx');
  const styles = read('src/pages/settings/Settings.css');

  assert.match(source, /settings-webdav-status-row/);
  assert.match(styles, /container-name:\s*settings-surface;/);
  assert.match(styles, /@container\s+settings-surface\s*\(max-width:\s*720px\)/);
  assert.match(styles, /\.settings-webdav-actions\s*\{[\s\S]*?flex-wrap:\s*wrap;/);
  assert.match(source, /className="settings-webdav-remote-toggle"/);
  assert.match(source, /aria-expanded=\{isRemoteExpanded\}/);
  assert.match(source, /<ChevronRight size=\{16\} aria-hidden="true" \/>/);
  assert.doesNotMatch(source, /settings-webdav-remote-header"\s*\n\s*style=\{\{ cursor:/);
});

test('reachable workspaces avoid structural emoji and broad transitions', () => {
  const announcementSource = read('src/components/AnnouncementCenter.tsx');
  const announcementStyles = read('src/components/AnnouncementCenter.css');
  const instanceStyles = read('src/styles/pages/instances.css');
  const apiServiceSource = read('src/pages/CodexApiServiceView.tsx');
  assert.doesNotMatch(announcementSource, /🚨/);
  assert.doesNotMatch(announcementStyles, /transition:\s*all/);
  assert.doesNotMatch(instanceStyles, /transition:\s*all/);
  assert.doesNotMatch(apiServiceSource, />\s*→\s*</);
  assert.match(apiServiceSource, /<ArrowRight size=\{16\} aria-hidden="true" \/>/);

  const embeddedRoots = [
    path.resolve(NEXTGEN_ROOT, '..', 'macos', 'source', 'frontend'),
    path.resolve(NEXTGEN_ROOT, '..', 'windows', 'source', 'frontend'),
  ];
  for (const root of embeddedRoots) {
    const dashboard = fs.readFileSync(path.join(root, 'src', 'views', 'Dashboard.vue'), 'utf8');
    const accounts = fs.readFileSync(path.join(root, 'src', 'views', 'Accounts.vue'), 'utf8');
    assert.doesNotMatch(dashboard, /transition:\s*all/);
    assert.doesNotMatch(accounts, /◌/);
    assert.match(accounts, /class="empty-icon" aria-hidden="true"/);
  }
});

test('instance editor exposes a named modal, labelled fields and full-size choices', () => {
  const source = read('src/components/InstancesManager.tsx');
  const styles = read('src/styles/pages/instances.css');

  assert.match(source, /role="dialog"\s*\n\s*aria-modal="true"\s*\n\s*aria-labelledby="instance-editor-title"/);
  assert.match(source, /<h2 id="instance-editor-title">/);
  for (const id of [
    'instance-editor-name',
    'instance-editor-path',
    'instance-editor-working-dir',
    'instance-editor-extra-args',
  ]) {
    assert.match(source, new RegExp(`htmlFor="${id}"`));
    assert.match(source, new RegExp(`id="${id}"`));
  }
  const optionRule = styles.match(/\.instances-page \.instance-init-mode-option\s*\{([\s\S]*?)\}/)?.[1] || '';
  assert.match(optionRule, /min-height:\s*44px;/);
  assert.doesNotMatch(optionRule, /transition:\s*all/);
});

test('Claude account entry fields have explicit accessible names', () => {
  const source = read('src/pages/ClaudeAccountsPage.tsx');

  assert.match(source, /aria-label=\{t\('claude\.desktopOAuth\.nameLabel', '账号名称'\)\}/);
  assert.match(source, /aria-label=\{t\('claude\.oauth\.callbackLabel', '回调链接或授权 code'\)\}/);
  assert.match(source, /aria-label=\{t\('claude\.oauth\.emailLabel', '邮箱提示'\)\}/);
});

test('Cursor and Windsurf OAuth progress and errors are live regions', () => {
  for (const file of ['src/pages/CursorAccountsPage.tsx', 'src/pages/WindsurfAccountsPage.tsx']) {
    const source = read(file);
    assert.match(source, /className="add-status error" role="alert" aria-live="assertive"/);
    assert.match(source, /className="add-status loading" role="status" aria-live="polite"/);
    assert.match(source, /aria-label=\{t\('accounts\.oauth\.linkLabel', '授权链接'\)\}/);
    assert.match(source, /aria-label=\{t\('common\.shared\.oauth\.deviceCode', '设备验证码'\)\}/);
    assert.match(source, /aria-label=\{t\('common\.copy', '复制'\)\}/);
  }
  assert.match(read('src/pages/WindsurfAccountsPage.tsx'), /htmlFor="windsurf-oauth-manual-callback"/);
});

test('Codex session copy actions keep a stable desktop hit area and explicit transitions', () => {
  const styles = read('src/styles/pages/codex-session-manager.css');
  const copyRule = styles.match(/\.codex-session-row__copy-button\s*\{([\s\S]*?)\}/)?.[1] || '';

  assert.match(copyRule, /width:\s*44px;/);
  assert.match(copyRule, /height:\s*44px;/);
  assert.doesNotMatch(copyRule, /transition:\s*all/);
  assert.match(copyRule, /background-color\s+0\.18s\s+ease/);
  assert.match(copyRule, /color\s+0\.18s\s+ease/);
});

test('all primary shell and embedded WF actions keep a 44px interaction target', () => {
  const liquidStyles = read('src/styles/xiass-liquid-glass.css');
  assert.match(liquidStyles, /:where\(button, \[role='button'\], \[role='tab'\]\)\s*\{[^}]*min-width:\s*44px\s*!important;[^}]*min-height:\s*44px\s*!important;/s);
  assert.match(liquidStyles, /\.btn:not\(\.btn-sm\)[^}]*min-height:\s*44px\s*!important;/s);

  const embeddedRoots = [
    path.resolve(NEXTGEN_ROOT, '..', 'macos', 'source', 'frontend'),
    path.resolve(NEXTGEN_ROOT, '..', 'windows', 'source', 'frontend'),
  ];

  for (const root of embeddedRoots) {
    const globalStyles = fs.readFileSync(path.join(root, 'src', 'style', 'global.css'), 'utf8');
    const buttonComponent = fs.readFileSync(path.join(root, 'src', 'components', 'ui', 'Button.vue'), 'utf8');
    const dashboard = fs.readFileSync(path.join(root, 'src', 'views', 'Dashboard.vue'), 'utf8');
    assert.match(globalStyles, /:root\[data-embedded="true"\] button\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px\s*!important;/s);
    assert.match(buttonComponent, /\.s-md\s*\{[^}]*min-height:\s*44px;/s);
    assert.match(buttonComponent, /\.s-sm\s*\{[^}]*min-height:\s*44px;/s);
    assert.match(buttonComponent, /:aria-busy="loading \|\| undefined"/);
    assert.match(buttonComponent, /aria-hidden="true"/);
    assert.match(buttonComponent, /<span class="btn-label"><slot \/><\/span>/);
    assert.doesNotMatch(buttonComponent, /<slot v-else\s*\/>/);
    assert.match(dashboard, /aria-label="刷新首页"/);
  }
});

test('embedded WF controls preserve keyboard focus rings on both platforms', () => {
  const roots = [
    path.resolve(NEXTGEN_ROOT, '..', 'macos', 'source', 'frontend', 'src', 'style', 'global.css'),
    path.resolve(NEXTGEN_ROOT, '..', 'windows', 'source', 'frontend', 'src', 'style', 'global.css'),
  ];

  for (const file of roots) {
    const styles = fs.readFileSync(file, 'utf8');
    assert.match(styles, /select:focus-visible[\s\S]{0,180}?outline:\s*2px\s+solid/);
    assert.match(styles, /select:focus:not\(:focus-visible\)\s*\{\s*outline:\s*none;/);
    assert.doesNotMatch(styles, /select:focus\s*\{\s*outline:\s*none;/);
  }
});

test('verification table checkbox hit areas meet the 44px interaction target', () => {
  const styles = read('src/styles/pages/wakeup-verification.css');
  const targetRule = styles.match(/\.verification-check-target\s*\{([\s\S]*?)\}/)?.[1] || '';

  assert.match(targetRule, /width:\s*44px;/);
  assert.match(targetRule, /height:\s*44px;/);
});

test('platform layout editor exposes a named modal dialog', () => {
  const source = read('src/components/PlatformLayoutModal.tsx');

  assert.match(source, /role="dialog"/);
  assert.match(source, /aria-modal="true"/);
  assert.match(source, /aria-labelledby="platform-layout-dialog-title"/);
  assert.match(source, /<h2 id="platform-layout-dialog-title">/);
});

test('liquid glass form surfaces do not erase checkbox and radio state colors', () => {
  const styles = read('src/styles/xiass-liquid-glass.css');
  const formSurfaceRule = styles.match(
    /\/\* Form controls remain stronger than cards[^]*?\*\/\s*([^]*?)\{([^]*?)\}/,
  );

  assert.ok(formSurfaceRule, 'liquid glass form-surface rule should exist');
  const selectors = formSurfaceRule[1];
  assert.match(selectors, /input:not\(\[type='checkbox'\]\)/);
  assert.match(selectors, /:not\(\[type='radio'\]\)/);
  assert.match(selectors, /:not\(\[type='range'\]\)/);
  assert.match(selectors, /:not\(\[type='color'\]\)/);
  assert.match(selectors, /:not\(\[type='file'\]\)/);
  assert.doesNotMatch(selectors, /(^|\n)input\s*,/);
});

test('announcement list, detail and image preview expose named modal dialogs', () => {
  const source = read('src/components/AnnouncementCenter.tsx');

  assert.match(source, /aria-labelledby="announcement-list-dialog-title"/);
  assert.match(source, /id="announcement-list-dialog-title"/);
  assert.match(source, /aria-labelledby="announcement-detail-dialog-title"/);
  assert.match(source, /id="announcement-detail-dialog-title"/);
  assert.match(source, /aria-label=\{t\('announcement\.imagePreview', '图片预览'\)\}/);
});

test('critical icon-only dismiss and account actions expose explicit accessible names', () => {
  const checks = [
    ['src/components/AccountGroupModal.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/components/CloseConfirmDialog.tsx', /className="close-dialog-x"[^>]*aria-label=\{t\('common\.close'/],
    ['src/components/CodexAccountGroupModal.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/components/FileCorruptedModal.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/components/GroupSettingsModal.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/components/SilentUpdateToast.tsx', /className="silent-update-toast-close"[\s\S]{0,180}?aria-label=\{t\('common\.close'/],
    ['src/components/TagEditModal.tsx', /className="btn btn-secondary tag-add-btn"[\s\S]{0,260}?aria-label=\{t\('common\.cancel'/],
    ['src/components/codebuddy/CodebuddySessionManager.tsx', /className="codex-session-manager__search-clear"[\s\S]{0,180}?aria-label=\{t\('common\.clear'/],
    ['src/components/codebuddy-suite/CodebuddySuiteCheckinModal.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/components/codebuddy-suite/TraeAutoCheckinConfigModal.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/components/codebuddy-suite/TraeCheckinModal.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/components/codebuddy-suite/WorkbuddyAutoCheckinConfigModal.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/components/codex/CodexWakeupContent.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/pages/CodebuddyAccountsPage.tsx', /className="action-btn success"[\s\S]{0,320}?aria-label=\{t\('common\.shared\.switchAccount'/],
    ['src/pages/CodexAccountsOverviewPanel.tsx', /setMessage\(null\)[\s\S]{0,160}?aria-label=\{t\("common\.close"/],
    ['src/pages/CodexAccountsView.tsx', /className="modal-close"[\s\S]{0,180}?aria-label=\{t\("common\.close"/],
    ['src/pages/GitHubCopilotAccountsPage.tsx', /setMessage\(null\)[\s\S]{0,160}?aria-label=\{t\('common\.close'/],
    ['src/pages/KiroAccountsPage.tsx', /setMessage\(null\)[\s\S]{0,160}?aria-label=\{t\('common\.close'/],
    ['src/pages/WakeupVerificationPage.tsx', /className="modal-close"[^>]*aria-label=\{t\('common\.close'/],
    ['src/pages/ZedAccountsPage.tsx', /setMessage\(null\)[\s\S]{0,160}?aria-label=\{t\('common\.close'/],
  ];

  for (const [relativePath, pattern] of checks) {
    assert.match(read(relativePath), pattern, `${relativePath} should name its icon-only action`);
  }

  const sharedAccounts = read('src/components/codebuddy-suite/CodebuddySuiteAccountsSharedView.tsx');
  for (const labelKey of [
    'common.shared.switchAccount',
    'accounts.editTags',
    'common.shared.refreshQuota',
    'common.shared.export.title',
    'common.delete',
  ]) {
    assert.match(
      sharedAccounts,
      new RegExp(`className="action-btn(?: success| danger)?"[\\s\\S]{0,360}?aria-label=\\{t\\("${escapeRegExp(labelKey)}"`),
    );
  }
});

test('Workbuddy history disclosure button performs one named stateful action', () => {
  const source = read('src/components/codebuddy-suite/WorkbuddyAutoCheckinConfigModal.tsx');
  const disclosure = source.match(
    /<button[\s\S]{0,900}?className="btn-icon-toggle"[\s\S]{0,900}?<\/button>/,
  )?.[0] || '';

  assert.match(disclosure, /type="button"/);
  assert.match(disclosure, /event\.stopPropagation\(\);/);
  assert.match(disclosure, /toggleExpand\(log\.id\);/);
  assert.match(disclosure, /aria-label=\{/);
  assert.match(disclosure, /common\.collapse/);
  assert.match(disclosure, /common\.expand/);
  assert.match(disclosure, /aria-expanded=\{isExpanded\}/);
});

test('page titles use theme text while manual information blocks use vector icons', () => {
  const componentStyles = read('src/styles/components.css');
  const manualSource = read('src/pages/ManualPage.tsx');
  const titleRule = componentStyles.match(/\.page-title\s*\{([\s\S]*?)\}/)?.[1] || '';

  assert.match(titleRule, /color:\s*var\(--text-primary\);/);
  assert.doesNotMatch(titleRule, /gradient|text-fill-color/);
  assert.match(manualSource, /<Lightbulb size=\{16\} aria-hidden="true" \/>/);
  assert.match(manualSource, /<ListChecks size=\{16\} aria-hidden="true" \/>/);
  assert.match(manualSource, /<TriangleAlert size=\{16\} aria-hidden="true" \/>/);
  assert.doesNotMatch(manualSource, /<h4>💡|<h4>🎯|<h4>⚠️/);
});
