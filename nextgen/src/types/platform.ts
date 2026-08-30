import { Page } from './navigation';

export type PlatformId =
  | 'antigravity'
  | 'antigravity_ide'
  | 'codex'
  | 'codex_api_service'
  | 'claude_manager'
  | 'zed'
  | 'github-copilot'
  | 'windsurf'
  | 'kiro'
  | 'cursor'
  | 'grok'
  | 'codebuddy'
  | 'codebuddy_cn'
  | 'qoder'
  | 'zcode'
  | 'trae'
  | 'trae_solo'
  | 'trae_cn'
  | 'trae_solo_cn'
  | 'workbuddy';

export const ALL_PLATFORM_IDS: PlatformId[] = [
  'claude_manager',
  'codex',
  'codex_api_service',
  'antigravity',
  'antigravity_ide',
  'zed',
  'github-copilot',
  'windsurf',
  'kiro',
  'cursor',
  'grok',
  'codebuddy',
  'codebuddy_cn',
  'qoder',
  'zcode',
  'trae',
  'trae_solo',
  'trae_cn',
  'trae_solo_cn',
  'workbuddy',
];

/** Platforms that do not own account lists (service / feature pages). */
export const ACCOUNTLESS_PLATFORM_IDS: readonly PlatformId[] = ['codex_api_service'];

export function isAccountPlatform(platformId: PlatformId): boolean {
  return !ACCOUNTLESS_PLATFORM_IDS.includes(platformId);
}

/**
 * The XIASS shell intentionally exposes five independent Agent workspaces.
 * Auxiliary pages such as the Codex API service and the two Antigravity
 * runtimes stay grouped under their owning Agent instead of becoming extra
 * sidebar products. Imported Cockpit modules remain in the data layer solely
 * for backwards-compatible reads and migrations.
 */
export const XIASS_AGENT_PLATFORM_IDS: readonly PlatformId[] = [
  'antigravity',
  'antigravity_ide',
  'codex',
  'codex_api_service',
  'claude_manager',
  'windsurf',
  'cursor',
];

export const MENU_HIDDEN_PLATFORM_IDS: PlatformId[] = ALL_PLATFORM_IDS.filter(
  (platformId) => !XIASS_AGENT_PLATFORM_IDS.includes(platformId),
);

export const MENU_VISIBLE_PLATFORM_IDS: PlatformId[] = [...XIASS_AGENT_PLATFORM_IDS];

export function isMenuVisiblePlatform(platformId: PlatformId): boolean {
  return !MENU_HIDDEN_PLATFORM_IDS.includes(platformId);
}

export const PLATFORM_PAGE_MAP: Record<PlatformId, Page> = {
  antigravity: 'overview',
  antigravity_ide: 'overview',
  codex: 'codex',
  codex_api_service: 'codex-api-service',
  claude_manager: 'claude',
  zed: 'zed',
  'github-copilot': 'github-copilot',
  windsurf: 'windsurf',
  kiro: 'kiro',
  cursor: 'cursor',
  grok: 'grok',
  codebuddy: 'codebuddy',
  codebuddy_cn: 'codebuddy-cn',
  qoder: 'qoder',
  zcode: 'zcode',
  trae: 'trae',
  trae_solo: 'trae-solo',
  trae_cn: 'trae-cn',
  trae_solo_cn: 'trae-solo-cn',
  workbuddy: 'workbuddy',
};
