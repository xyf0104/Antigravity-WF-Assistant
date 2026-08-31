const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const NEXTGEN_ROOT = path.resolve(__dirname, '../..');

function read(relativePath) {
  return fs.readFileSync(path.join(NEXTGEN_ROOT, relativePath), 'utf8');
}

test('every explicit native quit path marks the application exit as requested', () => {
  const sources = [
    'src-tauri/src/lib.rs',
    'src-tauri/src/commands/system_app_commands.rs',
    'src-tauri/src/modules/tray.rs',
    'src-tauri/src/modules/macos_native_menu_quota.rs',
  ];

  for (const relativePath of sources) {
    const source = read(relativePath);
    const exitCalls = [...source.matchAll(/\b(?:app|window\.app_handle\(\))\.exit\(0\);/g)];
    assert.ok(exitCalls.length > 0, `${relativePath} should contain an explicit exit path`);
    for (const match of exitCalls) {
      const precedingBlock = source.slice(Math.max(0, match.index - 220), match.index);
      assert.match(
        precedingBlock,
        /floating_card_window::request_app_exit\(\);/,
        `${relativePath}:${match.index} exits without marking the application exit as requested`,
      );
    }
  }
});

test('tray destruction only consumes one synthetic empty-window exit request', () => {
  const source = read('src-tauri/src/modules/floating_card_window.rs');

  assert.match(source, /static NEXT_EMPTY_WINDOW_EXIT_SHOULD_KEEP_ALIVE: AtomicBool/);
  assert.match(
    source,
    /NEXT_EMPTY_WINDOW_EXIT_SHOULD_KEEP_ALIVE\.swap\(false, Ordering::SeqCst\)/,
  );
  assert.match(
    source,
    /let will_leave_no_webview_windows = [\s\S]{0,260}?\.webview_windows\(\)[\s\S]{0,180}?MAIN_WINDOW_DESTROYED_TO_TRAY\.store\(true, Ordering::SeqCst\);\s*NEXT_EMPTY_WINDOW_EXIT_SHOULD_KEEP_ALIVE\s*\.store\(will_leave_no_webview_windows, Ordering::SeqCst\);/,
  );
  assert.match(
    source,
    /pub fn request_app_exit\(\) \{\s*APP_EXIT_REQUESTED\.store\(true, Ordering::SeqCst\);\s*NEXT_EMPTY_WINDOW_EXIT_SHOULD_KEEP_ALIVE\.store\(false, Ordering::SeqCst\);/,
  );
});

test('Codex formatted batch export only enables opener-scoped Downloads directories', () => {
  const source = read('src/pages/useCodexAccountsBaseController.tsx');

  assert.match(source, /function isDirectoryWithinDownloads\(/);
  assert.match(
    source,
    /return isDirectoryWithinDownloads\(\s*directory,\s*formattedExportDownloadsDirectory,/,
  );
  assert.match(
    source,
    /if \(!formattedExportSavedPath \|\| !canOpenFormattedExportSavedDirectory\) return;/,
  );
  assert.doesNotMatch(
    source,
    /const canOpenFormattedExportSavedDirectory = useMemo\(\s*\(\) => Boolean\(formattedExportSavedPath\)/,
  );
});
