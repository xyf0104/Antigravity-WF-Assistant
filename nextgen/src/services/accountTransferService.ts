import { ALL_PLATFORM_IDS, PlatformId } from '../types/platform';
import * as accountService from './accountService';
import * as claudeService from './claudeService';
import * as codexService from './codexService';
import * as githubCopilotService from './githubCopilotService';
import * as windsurfService from './windsurfService';
import * as kiroService from './kiroService';
import * as cursorService from './cursorService';
import * as codebuddyService from './codebuddyService';
import * as codebuddyCnService from './codebuddyCnService';
import * as qoderService from './qoderService';
import * as zcodeService from './zcodeService';
import * as traeService from './traeService';
import * as workbuddyService from './workbuddyService';
import * as zedService from './zedService';
import type { ClaudeAccount } from '../types/claude';
import {
  exportWfHelperTransfer,
  restoreWfHelperTransfer,
} from './wfBridgeService';
import {
  XIASS_ACCOUNT_TRANSFER_SCHEMA,
  isSupportedAccountTransferSchema,
} from '../utils/transferSchemas';

type AccountWithId = { id: string };

async function listClaudeManagerTransferAccounts(): Promise<AccountWithId[]> {
  const accounts = await claudeService.listClaudeAccounts();
  const seen = new Set<string>();
  return accounts.filter((account: ClaudeAccount) => {
    if (!account.id || seen.has(account.id)) {
      return false;
    }
    seen.add(account.id);
    return true;
  });
}

interface TransferAdapter {
  listAccounts: () => Promise<AccountWithId[]>;
  exportAccounts: (accountIds: string[]) => Promise<string>;
  importFromJson: (jsonContent: string) => Promise<unknown[]>;
}

// Grok exports are intentionally metadata-only and cannot restore a login, so
// it is excluded from the generic credential backup/import pipeline.
const PLATFORM_ADAPTERS: Partial<Record<PlatformId, TransferAdapter>> = {
  antigravity: {
    listAccounts: accountService.listAccounts,
    exportAccounts: accountService.exportAccounts,
    importFromJson: accountService.importFromJson,
  },
  antigravity_ide: {
    listAccounts: accountService.listAccounts,
    exportAccounts: accountService.exportAccounts,
    importFromJson: accountService.importFromJson,
  },
  codex: {
    listAccounts: codexService.listCodexAccounts,
    exportAccounts: codexService.exportCodexAccounts,
    importFromJson: codexService.importCodexFromJson,
  },
  claude_manager: {
    listAccounts: listClaudeManagerTransferAccounts,
    exportAccounts: claudeService.exportClaudeAccounts,
    importFromJson: claudeService.importClaudeFromJson,
  },
  zed: {
    listAccounts: zedService.listZedAccounts,
    exportAccounts: zedService.exportZedAccounts,
    importFromJson: zedService.importZedFromJson,
  },
  'github-copilot': {
    listAccounts: githubCopilotService.listGitHubCopilotAccounts,
    exportAccounts: githubCopilotService.exportGitHubCopilotAccounts,
    importFromJson: githubCopilotService.importGitHubCopilotFromJson,
  },
  windsurf: {
    listAccounts: windsurfService.listWindsurfAccounts,
    exportAccounts: windsurfService.exportWindsurfAccounts,
    importFromJson: windsurfService.importWindsurfFromJson,
  },
  kiro: {
    listAccounts: kiroService.listKiroAccounts,
    exportAccounts: kiroService.exportKiroAccounts,
    importFromJson: kiroService.importKiroFromJson,
  },
  cursor: {
    listAccounts: cursorService.listCursorAccounts,
    exportAccounts: cursorService.exportCursorAccounts,
    importFromJson: cursorService.importCursorFromJson,
  },
  codebuddy: {
    listAccounts: codebuddyService.listCodebuddyAccounts,
    exportAccounts: codebuddyService.exportCodebuddyAccounts,
    importFromJson: codebuddyService.importCodebuddyFromJson,
  },
  codebuddy_cn: {
    listAccounts: codebuddyCnService.listCodebuddyCnAccounts,
    exportAccounts: codebuddyCnService.exportCodebuddyCnAccounts,
    importFromJson: codebuddyCnService.importCodebuddyCnFromJson,
  },
  qoder: {
    listAccounts: qoderService.listQoderAccounts,
    exportAccounts: qoderService.exportQoderAccounts,
    importFromJson: qoderService.importQoderFromJson,
  },
  zcode: {
    listAccounts: zcodeService.listZcodeAccounts,
    exportAccounts: zcodeService.exportZcodeAccounts,
    importFromJson: zcodeService.importZcodeFromJson,
  },
  trae: {
    listAccounts: traeService.listTraeAccounts,
    exportAccounts: traeService.exportTraeAccounts,
    importFromJson: traeService.importTraeFromJson,
  },
  trae_solo: {
    listAccounts: traeService.listTraeAccounts,
    exportAccounts: traeService.exportTraeAccounts,
    importFromJson: traeService.importTraeFromJson,
  },
  trae_cn: {
    listAccounts: traeService.listTraeAccounts,
    exportAccounts: traeService.exportTraeAccounts,
    importFromJson: traeService.importTraeFromJson,
  },
  trae_solo_cn: {
    listAccounts: traeService.listTraeAccounts,
    exportAccounts: traeService.exportTraeAccounts,
    importFromJson: traeService.importTraeFromJson,
  },
  workbuddy: {
    listAccounts: workbuddyService.listWorkbuddyAccounts,
    exportAccounts: workbuddyService.exportWorkbuddyAccounts,
    importFromJson: workbuddyService.importWorkbuddyFromJson,
  },
};

export const ACCOUNT_TRANSFER_SCHEMA = XIASS_ACCOUNT_TRANSFER_SCHEMA;
export const ACCOUNT_TRANSFER_VERSION = 2;
const LEGACY_ACCOUNT_TRANSFER_VERSION = 1;

// Only the account stores owned by the five XIASS Agent workspaces belong in
// a newly-created backup. The embedded WF helper owns a separate credential
// store, and inherited Cockpit adapters remain available only for local
// backwards-compatible reads; neither boundary is silently swept into a
// scheduled or WebDAV backup.
export const ACCOUNT_TRANSFER_PLATFORM_IDS = [
  'antigravity',
  'codex',
  'claude_manager',
  'windsurf',
  'cursor',
] as const satisfies readonly PlatformId[];

export interface AccountTransferCoverage {
  included_platforms: PlatformId[];
  excluded_platforms: PlatformId[];
  embedded_wf_accounts: 'included_as_explicit_section';
  credential_export: 'plaintext_user_authorized';
  external_codex_auth: 'never_read_or_included';
  restore_scope: 'included_platforms_and_wf_helper';
}

export interface AccountTransferPlatformPayload {
  account_count: number;
  exported_data: unknown;
}

export interface AccountTransferBundle {
  schema: typeof ACCOUNT_TRANSFER_SCHEMA;
  version: typeof ACCOUNT_TRANSFER_VERSION;
  exported_at: string;
  summary: {
    platform_count: number;
    account_count: number;
  };
  coverage: AccountTransferCoverage;
  platforms: Partial<Record<PlatformId, AccountTransferPlatformPayload>>;
  wf_helper: unknown;
}

export interface AccountTransferPlatformImportDetail {
  platform: PlatformId;
  imported_count: number;
  skipped: boolean;
  reason?: 'excluded_from_xiass_product_scope';
  error?: string;
}

export interface AccountTransferImportResult {
  imported_count: number;
  platform_success_count: number;
  platform_failed_count: number;
  platform_skipped_count: number;
  wf_helper_restored: boolean;
  wf_helper_account_count: number;
  wf_helper_model_count: number;
  details: AccountTransferPlatformImportDetail[];
}

export type AccountTransferImportPlatformStatus =
  | 'pending'
  | 'running'
  | 'success'
  | 'failed'
  | 'skipped';

export interface AccountTransferImportProgressDetail {
  platform: PlatformId;
  status: AccountTransferImportPlatformStatus;
  expected_count: number;
  imported_count: number;
  reason?: 'excluded_from_xiass_product_scope';
  error?: string;
}

export interface AccountTransferImportProgress {
  total_platforms: number;
  completed_platforms: number;
  total_accounts: number;
  processed_accounts: number;
  imported_accounts: number;
  current_platform: PlatformId | null;
  details: AccountTransferImportProgressDetail[];
}

export interface AccountTransferImportOptions {
  onProgress?: (progress: AccountTransferImportProgress) => void;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function parseJsonOrThrow(json: string, errorCode: string): unknown {
  try {
    return JSON.parse(json) as unknown;
  } catch {
    throw new Error(errorCode);
  }
}

function normalizeAccountIds(accounts: AccountWithId[]): string[] {
  return accounts
    .map((account) => account.id)
    .filter((id): id is string => typeof id === 'string' && id.trim().length > 0);
}

function resolvePlatformPayload(rawSection: unknown): AccountTransferPlatformPayload | null {
  if (rawSection === undefined) return null;
  if (rawSection === null) {
    return {
      account_count: 0,
      exported_data: [],
    };
  }

  if (isRecord(rawSection)) {
    const isWrappedPayload = 'account_count' in rawSection || 'exported_data' in rawSection;
    if (isWrappedPayload) {
      const wrapped = rawSection as {
        account_count?: unknown;
        exported_data?: unknown;
        data?: unknown;
        accounts?: unknown;
      };
      const exportedData =
        wrapped.exported_data ?? wrapped.data ?? wrapped.accounts ?? [];
      const accountCount =
        typeof wrapped.account_count === 'number' && Number.isFinite(wrapped.account_count)
          ? Math.max(0, Math.floor(wrapped.account_count))
          : Array.isArray(exportedData)
            ? exportedData.length
            : 0;
      return {
        account_count: accountCount,
        exported_data: exportedData,
      };
    }
  }

  return {
    account_count: Array.isArray(rawSection) ? rawSection.length : 0,
    exported_data: rawSection,
  };
}

function estimatePayloadCount(payload: AccountTransferPlatformPayload): number {
  if (payload.account_count > 0) {
    return payload.account_count;
  }
  if (Array.isArray(payload.exported_data)) {
    return payload.exported_data.length;
  }
  if (payload.exported_data == null) {
    return 0;
  }
  return 1;
}

async function exportPlatformPayload(platform: PlatformId): Promise<AccountTransferPlatformPayload> {
  const adapter = PLATFORM_ADAPTERS[platform];
  if (!adapter) {
    return {
      account_count: 0,
      exported_data: [],
    };
  }
  const accounts = await adapter.listAccounts();
  const accountIds = normalizeAccountIds(accounts);

  if (accountIds.length === 0) {
    return {
      account_count: 0,
      exported_data: [],
    };
  }

  const exportedJson = await adapter.exportAccounts(accountIds);
  const exportedData = parseJsonOrThrow(exportedJson, `invalid_export_json:${platform}`);
  const accountCount = Array.isArray(exportedData) ? exportedData.length : accountIds.length;

  return {
    account_count: accountCount,
    exported_data: exportedData,
  };
}

export async function buildAccountTransferBundle(): Promise<AccountTransferBundle> {
  const entries: Array<readonly [PlatformId, AccountTransferPlatformPayload]> = [];
  for (const platform of ACCOUNT_TRANSFER_PLATFORM_IDS) {
    const payload = await exportPlatformPayload(platform);
    entries.push([platform, payload] as const);
  }

  const platforms = entries.reduce<Partial<Record<PlatformId, AccountTransferPlatformPayload>>>(
    (acc, [platform, payload]) => {
      acc[platform] = payload;
      return acc;
    },
    {},
  );

  const accountCount = entries.reduce((sum, [, payload]) => sum + payload.account_count, 0);
  // This call is deliberately part of account export rather than best-effort:
  // a unified backup must fail visibly if the helper-owned account/model store
  // cannot be included.
  const wfHelper = await exportWfHelperTransfer();

  return {
    schema: ACCOUNT_TRANSFER_SCHEMA,
    version: ACCOUNT_TRANSFER_VERSION,
    exported_at: new Date().toISOString(),
    summary: {
      platform_count: ACCOUNT_TRANSFER_PLATFORM_IDS.length,
      account_count: accountCount,
    },
    coverage: {
      included_platforms: [...ACCOUNT_TRANSFER_PLATFORM_IDS],
      excluded_platforms: ALL_PLATFORM_IDS.filter(
        (platform) => !ACCOUNT_TRANSFER_PLATFORM_IDS.includes(
          platform as (typeof ACCOUNT_TRANSFER_PLATFORM_IDS)[number],
        ),
      ),
      embedded_wf_accounts: 'included_as_explicit_section',
      credential_export: 'plaintext_user_authorized',
      external_codex_auth: 'never_read_or_included',
      restore_scope: 'included_platforms_and_wf_helper',
    },
    platforms,
    wf_helper: wfHelper,
  };
}

export async function exportAllAccountsTransferJson(): Promise<string> {
  const bundle = await buildAccountTransferBundle();
  return JSON.stringify(bundle, null, 2);
}

interface ParsedAccountTransferBundle {
  platforms: Record<PlatformId, AccountTransferPlatformPayload>;
  present_platforms: Set<PlatformId>;
  wf_helper: unknown | null;
}

function parseAccountTransferBundle(jsonContent: string): ParsedAccountTransferBundle {
  const parsed = parseJsonOrThrow(jsonContent, 'invalid_json');
  if (!isRecord(parsed)) {
    throw new Error('invalid_bundle_root');
  }

  if (!isSupportedAccountTransferSchema(parsed.schema)) {
    throw new Error('invalid_bundle_schema');
  }

  if (
    parsed.version !== ACCOUNT_TRANSFER_VERSION
    && parsed.version !== LEGACY_ACCOUNT_TRANSFER_VERSION
  ) {
    throw new Error('invalid_bundle_version');
  }

  const rawPlatforms = parsed.platforms;
  if (!isRecord(rawPlatforms)) {
    throw new Error('invalid_bundle_platforms');
  }

  const wfHelper = parsed.wf_helper;
  if (parsed.version === ACCOUNT_TRANSFER_VERSION && !isRecord(wfHelper)) {
    throw new Error('invalid_bundle_wf_helper');
  }

  const platforms: Record<PlatformId, AccountTransferPlatformPayload> = {} as Record<
    PlatformId,
    AccountTransferPlatformPayload
  >;
  const presentPlatforms = new Set<PlatformId>();

  for (const platform of ALL_PLATFORM_IDS) {
    const resolved = resolvePlatformPayload(rawPlatforms[platform]);
    if (resolved) {
      presentPlatforms.add(platform);
    }
    platforms[platform] =
      resolved ??
      ({
        account_count: 0,
        exported_data: [],
      } as AccountTransferPlatformPayload);
  }

  return {
    platforms,
    present_platforms: presentPlatforms,
    wf_helper: isRecord(wfHelper) ? wfHelper : null,
  };
}

export async function importAllAccountsFromTransferJson(
  jsonContent: string,
  options: AccountTransferImportOptions = {},
): Promise<AccountTransferImportResult> {
  const { onProgress } = options;
  const parsedBundle = parseAccountTransferBundle(jsonContent);
  const wfRestore = parsedBundle.wf_helper
    ? await restoreWfHelperTransfer(parsedBundle.wf_helper)
    : null;
  const platformsToProcess = ALL_PLATFORM_IDS.filter((platform) => (
    ACCOUNT_TRANSFER_PLATFORM_IDS.includes(
      platform as (typeof ACCOUNT_TRANSFER_PLATFORM_IDS)[number],
    ) || parsedBundle.present_platforms.has(platform)
  ));
  const progressDetails: AccountTransferImportProgressDetail[] = platformsToProcess.map((platform) => {
    const payload = parsedBundle.platforms[platform];
    return {
      platform,
      status: 'pending',
      expected_count: estimatePayloadCount(payload),
      imported_count: 0,
    };
  });

  const emitProgress = (currentPlatform: PlatformId | null) => {
    const completed = progressDetails.filter((item) =>
      item.status === 'success' || item.status === 'failed' || item.status === 'skipped',
    );
    const totalAccounts = progressDetails.reduce((sum, item) => sum + item.expected_count, 0);
    const processedAccounts = completed.reduce((sum, item) => sum + item.expected_count, 0);
    const importedAccounts = progressDetails.reduce((sum, item) => sum + item.imported_count, 0);

    onProgress?.({
      total_platforms: progressDetails.length,
      completed_platforms: completed.length,
      total_accounts: totalAccounts,
      processed_accounts: processedAccounts,
      imported_accounts: importedAccounts,
      current_platform: currentPlatform,
      details: progressDetails.map((item) => ({ ...item })),
    });
  };

  emitProgress(null);

  for (const platform of platformsToProcess) {
    const adapter = PLATFORM_ADAPTERS[platform];
    const payload = parsedBundle.platforms[platform];
    const data = payload.exported_data;
    const detailIndex = progressDetails.findIndex((item) => item.platform === platform);
    const detail = progressDetails[detailIndex];

    if (!ACCOUNT_TRANSFER_PLATFORM_IDS.includes(
      platform as (typeof ACCOUNT_TRANSFER_PLATFORM_IDS)[number],
    )) {
      progressDetails[detailIndex] = {
        ...detail,
        status: 'skipped',
        imported_count: 0,
        reason: 'excluded_from_xiass_product_scope',
      };
      emitProgress(null);
      continue;
    }

    if (!adapter) {
      progressDetails[detailIndex] = {
        ...detail,
        status: 'skipped',
        imported_count: 0,
      };
      emitProgress(null);
      continue;
    }

    const isEmptyPayload =
      data == null || (Array.isArray(data) && data.length === 0);

    if (isEmptyPayload) {
      progressDetails[detailIndex] = {
        ...detail,
        status: 'skipped',
        imported_count: 0,
      };
      emitProgress(null);
      continue;
    }

    progressDetails[detailIndex] = {
      ...detail,
      status: 'running',
      error: undefined,
    };
    emitProgress(platform);

    try {
      const imported = await adapter.importFromJson(JSON.stringify(data));
      progressDetails[detailIndex] = {
        ...progressDetails[detailIndex],
        status: 'success',
        imported_count: Array.isArray(imported) ? imported.length : 0,
        error: undefined,
      };
      emitProgress(null);
    } catch (error) {
      progressDetails[detailIndex] = {
        ...progressDetails[detailIndex],
        status: 'failed',
        imported_count: 0,
        error: String(error).replace(/^Error:\s*/, ''),
      };
      emitProgress(null);
    }
  }

  const details: AccountTransferPlatformImportDetail[] = progressDetails.map((item) => ({
    platform: item.platform,
    imported_count: item.imported_count,
    skipped: item.status === 'skipped',
    reason: item.reason,
    error: item.status === 'failed' ? item.error : undefined,
  }));

  const importedCount = details.reduce((sum, item) => sum + item.imported_count, 0)
    + (wfRestore?.accountCount ?? 0);
  const platformFailedCount = details.filter((item) => item.error).length;
  const platformSkippedCount = details.filter((item) => item.skipped).length;
  const platformSuccessCount = details.length - platformFailedCount - platformSkippedCount;

  return {
    imported_count: importedCount,
    platform_success_count: platformSuccessCount,
    platform_failed_count: platformFailedCount,
    platform_skipped_count: platformSkippedCount,
    wf_helper_restored: wfRestore?.ok === true,
    wf_helper_account_count: wfRestore?.accountCount ?? 0,
    wf_helper_model_count: wfRestore?.modelCount ?? 0,
    details,
  };
}
