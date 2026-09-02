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
 * Only the five XIASS-supported products are exposed as user-facing products.
 * Auxiliary runtime IDs remain in the type union because their existing pages,
 * commands, persisted records, and migration paths still depend on them.
 */
export const XIASS_AGENT_PLATFORM_IDS: readonly PlatformId[] = [
  'antigravity',
  'codex',
  'claude_manager',
  'cursor',
  'windsurf',
];

/** Account domains allowed to execute in production transfer/import workflows. */
export const PRODUCTION_AGENT_ACCOUNT_PLATFORM_IDS = [
  'antigravity',
  'codex',
  'claude_manager',
  'windsurf',
  'cursor',
] as const satisfies readonly PlatformId[];

export type ProductionAgentAccountPlatformId =
  (typeof PRODUCTION_AGENT_ACCOUNT_PLATFORM_IDS)[number];

export function isProductionAgentAccountPlatform(
  platformId: PlatformId,
): platformId is ProductionAgentAccountPlatformId {
  return (PRODUCTION_AGENT_ACCOUNT_PLATFORM_IDS as readonly PlatformId[]).includes(platformId);
}

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
