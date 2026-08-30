export type Page =
  | 'dashboard'
  | 'manual'
  | 'api-relay'
  | 'overview'
  | 'codex'
  | 'claude'
  | 'claude-cli'
  | 'codex-api-service'
  | 'zed'
  | 'github-copilot'
  | 'windsurf'
  | 'kiro'
  | 'cursor'
  | 'grok'
  | 'codebuddy'
  | 'codebuddy-cn'
  | 'qoder'
  | 'zcode'
  | 'trae'
  | 'trae-solo'
  | 'trae-cn'
  | 'trae-solo-cn'
  | 'workbuddy'
  | 'codex-instances'
  | 'instances'
  | 'accounts'
  | 'wakeup'
  | 'verification'
  | '2fa'
  | 'settings';

/** Pages intentionally exposed by the XIASS product shell. */
export const XIASS_VISIBLE_PAGES: readonly Page[] = [
  'dashboard',
  'manual',
  'overview',
  'codex',
  'codex-api-service',
  'codex-instances',
  'claude',
  'claude-cli',
  'windsurf',
  'cursor',
  'instances',
  'accounts',
  'wakeup',
  'verification',
  '2fa',
  'settings',
] as const;

export function isXiassVisiblePage(page: string): page is Page {
  return (XIASS_VISIBLE_PAGES as readonly string[]).includes(page);
}

/** Pages that tray / floating-card restore may navigate to after main-window recreate. */
export const MAIN_WINDOW_NAVIGABLE_PAGES: readonly Page[] = [
  'dashboard',
  'manual',
  'overview',
  'codex',
  'claude',
  'claude-cli',
  'codex-api-service',
  'windsurf',
  'cursor',
  'settings',
] as const;

export function isMainWindowNavigablePage(page: string): page is Page {
  return (MAIN_WINDOW_NAVIGABLE_PAGES as readonly string[]).includes(page);
}
