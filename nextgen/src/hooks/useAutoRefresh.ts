import { useCallback, useEffect, useRef, type MutableRefObject } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { listen, type UnlistenFn } from '@tauri-apps/api/event';
import { useAccountStore } from '../stores/useAccountStore';
import { useCodexAccountStore } from '../stores/useCodexAccountStore';
import { useClaudeAccountStore } from '../stores/useClaudeAccountStore';
import { useWindsurfAccountStore } from '../stores/useWindsurfAccountStore';
import { useCursorAccountStore } from '../stores/useCursorAccountStore';
import { getWindsurfAccountDisplayEmail } from '../types/windsurf';
import { getCursorAccountDisplayEmail } from '../types/cursor';
import { getClaudeAccountDisplayEmail } from '../types/claude';
import {
  loadCurrentAccountRefreshMinutesMap,
  getAccountRefreshMinutes,
  type CurrentAccountRefreshPlatform,
} from '../utils/currentAccountRefresh';
import {
  createAutoRefreshScheduler,
  type AutoRefreshSchedulerHandle,
  type AutoRefreshSchedulerTask,
} from '../utils/autoRefreshScheduler';
import { CURRENT_ACCOUNT_CHANGED_EVENT } from '../utils/accountSyncEvents';
import { refreshCodexApiKeyUsageForAccounts } from '../services/codexApiKeyUsageRefreshService';
import * as codexService from '../services/codexService';
import {
  getCodexAccountGroups,
  getCodexCustomQuotaRefreshAccountIdsByMinutes,
  getCodexInheritPlatformQuotaRefreshAccountIds,
  resolveCodexGroupQuotaAutoRefreshMinutes,
} from '../services/codexAccountGroupService';
import { isCodexApiKeyAccount, isCodexNewApiAccount } from '../types/codex';

interface GeneralConfig {
  language: string;
  theme: string;
  auto_refresh_minutes: number;
  codex_auto_refresh_minutes: number;
  claude_auto_refresh_minutes: number;
  codex_sync_wsl: boolean;
  codex_wsl_config_dir: string;
  ghcp_auto_refresh_minutes: number;
  windsurf_auto_refresh_minutes: number;
  kiro_auto_refresh_minutes: number;
  cursor_auto_refresh_minutes: number;
  grok_auto_refresh_minutes: number;
  codebuddy_auto_refresh_minutes: number;
  codebuddy_cn_auto_refresh_minutes: number;
  workbuddy_auto_refresh_minutes: number;
  qoder_auto_refresh_minutes: number;
  zcode_auto_refresh_minutes: number;
  trae_auto_refresh_minutes: number;
  trae_solo_auto_refresh_minutes: number;
  trae_cn_auto_refresh_minutes: number;
  trae_solo_cn_auto_refresh_minutes: number;
  zed_auto_refresh_minutes: number;
  auto_switch_enabled: boolean;
  codex_auto_switch_enabled?: boolean;
  codex_quota_alert_enabled?: boolean;
  close_behavior: string;
  opencode_app_path?: string;
  antigravity_app_path?: string;
  codex_app_path?: string;
  vscode_app_path?: string;
  windsurf_app_path?: string;
  kiro_app_path?: string;
  cursor_app_path?: string;
  codebuddy_app_path?: string;
  codebuddy_cn_app_path?: string;
  qoder_app_path?: string;
  zcode_app_path?: string;
  trae_app_path?: string;
  zed_app_path?: string;
  opencode_sync_on_switch?: boolean;
  opencode_auth_overwrite_on_switch?: boolean;
  codex_launch_on_switch?: boolean;
  cursor_quota_alert_enabled?: boolean;
  cursor_quota_alert_threshold?: number;
  grok_quota_alert_enabled?: boolean;
  grok_quota_alert_threshold?: number;
}

interface PlatformRefreshDescriptor {
  key: CurrentAccountRefreshPlatform;
  label: string;
  intervalMinutes: number;
  currentMinutes: number;
  fullRefreshingRef: MutableRefObject<boolean>;
  currentRefreshingRef: MutableRefObject<boolean>;
  runFullRefresh: () => Promise<void>;
  runCurrentRefresh: () => Promise<void>;
}

const STARTUP_AUTO_REFRESH_SETUP_DELAY_MS = 2500;
const AUTO_REFRESH_TICK_MS = 5_000;
const AUTO_REFRESH_MAX_CONCURRENT = 1;

function minutesToMs(minutes: number): number {
  return minutes * 60 * 1000;
}

function buildEnabledPlatformsSummary(
  descriptors: PlatformRefreshDescriptor[],
): string {
  const fullSummary = descriptors
    .filter((descriptor) => descriptor.intervalMinutes > 0)
    .map((descriptor) => `${descriptor.key}=${descriptor.intervalMinutes}`);
  const currentSummary = descriptors
    .filter((descriptor) => descriptor.intervalMinutes > 0)
    .map((descriptor) => `${descriptor.key}:${descriptor.currentMinutes}`);

  const parts = [...fullSummary];
  if (currentSummary.length > 0) {
    parts.push(`current=${currentSummary.join('|')}`);
  }
  return parts.join(', ');
}

function resolveCurrentMinutes(
  platform: CurrentAccountRefreshPlatform,
  email: string | null,
  defaultMap: Record<CurrentAccountRefreshPlatform, number>,
): number {
  return email
    ? getAccountRefreshMinutes(platform, email, defaultMap[platform])
    : defaultMap[platform];
}

function getCurrentAccountEmails() {
  const getProviderEmail = <T extends { id: string; email?: string | null }>(
    store: { getState: () => { currentAccountId: string | null; accounts: T[] } },
    getDisplayEmail: (account: T) => string,
  ): string | null => {
    const state = store.getState();
    const account = state.accounts.find((a) => a.id === state.currentAccountId);
    if (!account) return null;
    return getDisplayEmail(account);
  };
  return {
    antigravity: useAccountStore.getState().currentAccount?.email ?? null,
    codex: useCodexAccountStore.getState().currentAccount?.email ?? null,
    claude: getProviderEmail(useClaudeAccountStore, getClaudeAccountDisplayEmail),
    windsurf: getProviderEmail(useWindsurfAccountStore, getWindsurfAccountDisplayEmail),
    cursor: getProviderEmail(useCursorAccountStore, getCursorAccountDisplayEmail),
  };
}

export function useAutoRefresh() {
  const refreshAllQuotas = useAccountStore((state) => state.refreshAllQuotas);
  const fetchAccounts = useAccountStore((state) => state.fetchAccounts);
  const fetchCurrentAccount = useAccountStore((state) => state.fetchCurrentAccount);

  const fetchCodexAccounts = useCodexAccountStore((state) => state.fetchAccounts);
  const fetchCurrentCodexAccount = useCodexAccountStore((state) => state.fetchCurrentAccount);
  const refreshAllClaudeQuotas = useClaudeAccountStore((state) => state.refreshAllTokens);
  const fetchCurrentClaudeAccountId = useClaudeAccountStore((state) => state.fetchCurrentAccountId);
  const refreshClaudeQuota = useClaudeAccountStore((state) => state.refreshToken);
  const refreshAllWindsurfTokens = useWindsurfAccountStore((state) => state.refreshAllTokens);
  const fetchCurrentWindsurfAccountId = useWindsurfAccountStore((state) => state.fetchCurrentAccountId);
  const refreshWindsurfToken = useWindsurfAccountStore((state) => state.refreshToken);
  const refreshAllCursorTokens = useCursorAccountStore((state) => state.refreshAllTokens);
  const fetchCurrentCursorAccountId = useCursorAccountStore((state) => state.fetchCurrentAccountId);
  const refreshCursorToken = useCursorAccountStore((state) => state.refreshToken);

  const antigravityRefreshingRef = useRef(false);
  const antigravityCurrentRefreshingRef = useRef(false);
  const codexRefreshingRef = useRef(false);
  const codexCurrentRefreshingRef = useRef(false);
  const claudeRefreshingRef = useRef(false);
  const claudeCurrentRefreshingRef = useRef(false);
  const windsurfRefreshingRef = useRef(false);
  const windsurfCurrentRefreshingRef = useRef(false);
  const cursorRefreshingRef = useRef(false);
  const cursorCurrentRefreshingRef = useRef(false);

  const schedulerRef = useRef<AutoRefreshSchedulerHandle | null>(null);
  const setupRunningRef = useRef(false);
  const setupPendingRef = useRef(false);
  const destroyedRef = useRef(false);

  const stopScheduler = useCallback(() => {
    schedulerRef.current?.stop();
    schedulerRef.current = null;
  }, []);

  const executeWithGuard = useCallback(
    async (
      refreshingRef: MutableRefObject<boolean>,
      task: () => Promise<void>,
      startMessage: string | null,
      errorMessage: string,
    ) => {
      if (refreshingRef.current) {
        return;
      }

      refreshingRef.current = true;
      try {
        if (startMessage) {
          console.log(startMessage);
        }
        await task();
      } catch (error) {
        console.error(errorMessage, error);
      } finally {
        refreshingRef.current = false;
      }
    },
    [],
  );

  const setupAutoRefresh = useCallback(async () => {
    const setupStartedAt = performance.now();
    console.log('[StartupPerf][AutoRefresh] setupAutoRefresh start');

    if (destroyedRef.current) {
      console.log('[StartupPerf][AutoRefresh] setupAutoRefresh aborted: destroyed flag set');
      return;
    }

    if (setupRunningRef.current) {
      setupPendingRef.current = true;
      console.log('[StartupPerf][AutoRefresh] setupAutoRefresh skipped: previous run still active');
      return;
    }

    setupRunningRef.current = true;

    try {
      do {
        setupPendingRef.current = false;

        try {
          const configInvokeStartedAt = performance.now();
          const config = await invoke<GeneralConfig>('get_general_config');
          console.log(
            `[StartupPerf][AutoRefresh] get_general_config completed in ${(performance.now() - configInvokeStartedAt).toFixed(2)}ms`,
          );

          if (destroyedRef.current) {
            console.log('[StartupPerf][AutoRefresh] setupAutoRefresh aborted after config load: destroyed flag set');
            return;
          }

          const wakeupEnabled = localStorage.getItem('agtools.wakeup.enabled') === 'true';
          if (wakeupEnabled) {
            const tasksJson = localStorage.getItem('agtools.wakeup.tasks');
            if (tasksJson) {
              try {
                const tasks = JSON.parse(tasksJson);
                const hasActiveResetTask = Array.isArray(tasks)
                  && tasks.some((task: unknown) => {
                    if (!task || typeof task !== 'object') {
                      return false;
                    }
                    const taskObject = task as {
                      enabled?: boolean;
                      schedule?: { wakeOnReset?: boolean };
                    };
                    return Boolean(taskObject.enabled && taskObject.schedule?.wakeOnReset);
                  });

                if (
                  hasActiveResetTask
                  && (config.auto_refresh_minutes === -1 || config.auto_refresh_minutes > 2)
                ) {
                  console.log(
                    `[AutoRefresh] 检测到活跃的配额重置任务，自动修正刷新间隔: ${config.auto_refresh_minutes} -> 2`,
                  );
                  const saveConfigStartedAt = performance.now();
                  await invoke('save_refresh_interval_config', {
                    autoRefreshMinutes: 2,
                  });
                  console.log(
                    `[StartupPerf][AutoRefresh] save_refresh_interval_config completed in ${(performance.now() - saveConfigStartedAt).toFixed(2)}ms`,
                  );
                  config.auto_refresh_minutes = 2;
                }
              } catch (error) {
                console.error('[AutoRefresh] 解析任务列表失败:', error);
              }
            }
          }

          if (destroyedRef.current) {
            console.log('[StartupPerf][AutoRefresh] setupAutoRefresh aborted before scheduler setup: destroyed flag set');
            return;
          }

          stopScheduler();

          const currentRefreshMinutesMap = loadCurrentAccountRefreshMinutesMap();
          const currentAccountEmails = getCurrentAccountEmails();
          const runProviderCurrentRefresh = async (
            fetchCurrentProviderAccountId: () => Promise<string | null>,
            refreshProviderToken: (accountId: string) => Promise<void>,
          ) => {
            const accountId = await fetchCurrentProviderAccountId();
            if (!accountId) {
              return;
            }
            await refreshProviderToken(accountId);
          };

          // Codex 分组额度策略：继承平台 / 自定义间隔 / 不刷新（最高优先级）
          const codexGroups = await getCodexAccountGroups().catch(() => []);
          const codexCurrentAccount = useCodexAccountStore.getState().currentAccount;
          let codexCurrentMinutes = resolveCurrentMinutes(
            'codex',
            currentAccountEmails.codex,
            currentRefreshMinutesMap,
          );
          if (codexCurrentAccount?.id) {
            const group = codexGroups.find((item) =>
              item.accountIds.includes(codexCurrentAccount.id),
            );
            const policy = resolveCodexGroupQuotaAutoRefreshMinutes(group);
            if (policy === -1) {
              codexCurrentMinutes = -1;
            } else if (typeof policy === 'number' && policy > 0) {
              codexCurrentMinutes = policy;
            }
          }
          const codexCustomRefreshByMinutes =
            await getCodexCustomQuotaRefreshAccountIdsByMinutes().catch(
              () => new Map<number, string[]>(),
            );

          const descriptors: PlatformRefreshDescriptor[] = [
            {
              key: 'antigravity',
              label: 'Antigravity IDE',
              intervalMinutes: config.auto_refresh_minutes,
              currentMinutes: resolveCurrentMinutes('antigravity', currentAccountEmails.antigravity, currentRefreshMinutesMap),
              fullRefreshingRef: antigravityRefreshingRef,
              currentRefreshingRef: antigravityCurrentRefreshingRef,
              runFullRefresh: async () => {
                await refreshAllQuotas('auto');
              },
              runCurrentRefresh: async () => {
                if (!useAccountStore.getState().currentAccount?.id) {
                  await fetchCurrentAccount();
                }
                if (!useAccountStore.getState().currentAccount?.id) {
                  return;
                }
                await invoke('refresh_current_quota');
                await fetchAccounts();
                await fetchCurrentAccount();
              },
            },
            {
              key: 'codex',
              label: 'Codex',
              intervalMinutes: config.codex_auto_refresh_minutes,
              currentMinutes: codexCurrentMinutes,
              fullRefreshingRef: codexRefreshingRef,
              currentRefreshingRef: codexCurrentRefreshingRef,
              runFullRefresh: async () => {
                try {
                  // 平台间隔只刷「继承平台 + 未分组」；自定义间隔分组由独立任务负责
                  const accounts = useCodexAccountStore.getState().accounts;
                  const refreshableIds = accounts
                    .filter(
                      (account) =>
                        !isCodexApiKeyAccount(account) || isCodexNewApiAccount(account),
                    )
                    .map((account) => account.id);
                  const inheritIds =
                    await getCodexInheritPlatformQuotaRefreshAccountIds(refreshableIds);
                  if (inheritIds.length > 0) {
                    await codexService.refreshCodexQuotasBatch(inheritIds, {
                      respectGroupQuotaRefresh: true,
                    });
                  }
                } finally {
                  await refreshCodexApiKeyUsageForAccounts(
                    useCodexAccountStore.getState().accounts,
                  ).catch((error) => {
                    console.error('[AutoRefresh] Codex API Key usage refresh failed:', error);
                  });
                  await fetchCodexAccounts();
                  await fetchCurrentCodexAccount();
                }
              },
              runCurrentRefresh: async () => {
                if (!useCodexAccountStore.getState().currentAccount?.id) {
                  await fetchCurrentCodexAccount();
                }
                if (!useCodexAccountStore.getState().currentAccount?.id) {
                  return;
                }
                try {
                  await invoke('refresh_current_codex_quota');
                  await fetchCodexAccounts();
                  await fetchCurrentCodexAccount();
                } finally {
                  const currentAccount = useCodexAccountStore.getState().currentAccount;
                  if (currentAccount) {
                    await refreshCodexApiKeyUsageForAccounts([currentAccount]).catch((error) => {
                      console.error('[AutoRefresh] Codex API Key usage refresh failed:', error);
                    });
                  }
                }
              },
            },
            {
              key: 'claude',
              label: 'Claude',
              intervalMinutes: config.claude_auto_refresh_minutes,
              currentMinutes: resolveCurrentMinutes('claude', currentAccountEmails.claude, currentRefreshMinutesMap),
              fullRefreshingRef: claudeRefreshingRef,
              currentRefreshingRef: claudeCurrentRefreshingRef,
              runFullRefresh: async () => {
                await refreshAllClaudeQuotas();
              },
              runCurrentRefresh: async () => {
                await runProviderCurrentRefresh(fetchCurrentClaudeAccountId, refreshClaudeQuota);
              },
            },
            {
              key: 'windsurf',
              label: 'Windsurf',
              intervalMinutes: config.windsurf_auto_refresh_minutes,
              currentMinutes: resolveCurrentMinutes('windsurf', currentAccountEmails.windsurf, currentRefreshMinutesMap),
              fullRefreshingRef: windsurfRefreshingRef,
              currentRefreshingRef: windsurfCurrentRefreshingRef,
              runFullRefresh: async () => {
                await refreshAllWindsurfTokens();
              },
              runCurrentRefresh: async () => {
                await runProviderCurrentRefresh(
                  fetchCurrentWindsurfAccountId,
                  refreshWindsurfToken,
                );
              },
            },
            {
              key: 'cursor',
              label: 'Cursor',
              intervalMinutes: config.cursor_auto_refresh_minutes,
              currentMinutes: resolveCurrentMinutes('cursor', currentAccountEmails.cursor, currentRefreshMinutesMap),
              fullRefreshingRef: cursorRefreshingRef,
              currentRefreshingRef: cursorCurrentRefreshingRef,
              runFullRefresh: async () => {
                await refreshAllCursorTokens();
              },
              runCurrentRefresh: async () => {
                await runProviderCurrentRefresh(fetchCurrentCursorAccountId, refreshCursorToken);
              },
            },
          ];

          const tasks: AutoRefreshSchedulerTask[] = [];
          for (const descriptor of descriptors) {
            if (descriptor.intervalMinutes > 0) {
              console.log(`[AutoRefresh] ${descriptor.label} 已启用: 每 ${descriptor.intervalMinutes} 分钟`);
              tasks.push({
                key: `full:${descriptor.key}`,
                label: `${descriptor.label} 全量刷新`,
                intervalMs: minutesToMs(descriptor.intervalMinutes),
                run: () =>
                  executeWithGuard(
                    descriptor.fullRefreshingRef,
                    descriptor.runFullRefresh,
                    `[AutoRefresh] 触发 ${descriptor.label} 刷新...`,
                    `[AutoRefresh] ${descriptor.label} 刷新失败:`,
                  ),
              });
            } else {
              console.log(`[AutoRefresh] ${descriptor.label} 已禁用`);
            }

            if (descriptor.intervalMinutes > 0 && descriptor.currentMinutes > 0) {
              console.log(`[AutoRefresh] ${descriptor.label} 当前账号刷新: 每 ${descriptor.currentMinutes} 分钟`);
              tasks.push({
                key: `current:${descriptor.key}`,
                label: `${descriptor.label} 当前账号刷新`,
                intervalMs: minutesToMs(descriptor.currentMinutes),
                shouldSkip: () => descriptor.fullRefreshingRef.current,
                run: () =>
                  executeWithGuard(
                    descriptor.currentRefreshingRef,
                    descriptor.runCurrentRefresh,
                    null,
                    `[AutoRefresh] ${descriptor.label} 当前账号刷新失败:`,
                  ),
              });
            } else {
              console.log(`[AutoRefresh] ${descriptor.label} 当前账号刷新已禁用${descriptor.currentMinutes === -1 ? '（账号级覆盖禁用）' : '（配额自动刷新未开启）'}`);
            }
          }

          // Codex 分组自定义间隔：独立调度（最高优先级，不依赖平台间隔是否开启）
          for (const [minutes, accountIds] of codexCustomRefreshByMinutes.entries()) {
            if (minutes <= 0 || accountIds.length === 0) {
              continue;
            }
            const uniqueIds = [...new Set(accountIds.filter(Boolean))];
            if (uniqueIds.length === 0) {
              continue;
            }
            console.log(
              `[AutoRefresh] Codex 分组自定义刷新: 每 ${minutes} 分钟, accounts=${uniqueIds.length}`,
            );
            tasks.push({
              key: `full:codex-group:${minutes}`,
              label: `Codex 分组自定义刷新 (${minutes}m)`,
              intervalMs: minutesToMs(minutes),
              run: () =>
                executeWithGuard(
                  codexRefreshingRef,
                  async () => {
                    try {
                      // 自定义分组任务目标明确，不因「不刷新」外的策略再过滤
                      await codexService.refreshCodexQuotasBatch(uniqueIds, {
                        respectGroupQuotaRefresh: false,
                      });
                    } finally {
                      await fetchCodexAccounts();
                      await fetchCurrentCodexAccount();
                    }
                  },
                  `[AutoRefresh] 触发 Codex 分组自定义刷新 (${minutes}m)...`,
                  `[AutoRefresh] Codex 分组自定义刷新失败 (${minutes}m):`,
                ),
            });
          }

          if (tasks.length > 0) {
            const scheduler = createAutoRefreshScheduler(tasks, {
              tickMs: AUTO_REFRESH_TICK_MS,
              maxConcurrent: AUTO_REFRESH_MAX_CONCURRENT,
            });
            scheduler.start();
            schedulerRef.current = scheduler;
          }

          const enabledPlatforms = buildEnabledPlatformsSummary(descriptors);
          console.log(
            `[StartupPerf][AutoRefresh] setupAutoRefresh completed in ${(performance.now() - setupStartedAt).toFixed(2)}ms; enabled=${enabledPlatforms || 'none'}`,
          );
        } catch (err) {
          console.error('[AutoRefresh] 加载配置失败:', err);
          console.error(
            `[StartupPerf][AutoRefresh] setupAutoRefresh failed after ${(performance.now() - setupStartedAt).toFixed(2)}ms:`,
            err,
          );
        }
      } while (setupPendingRef.current && !destroyedRef.current);
    } finally {
      setupRunningRef.current = false;
      console.log(
        `[StartupPerf][AutoRefresh] setupAutoRefresh exit after ${(performance.now() - setupStartedAt).toFixed(2)}ms`,
      );
    }
  }, [
    executeWithGuard,
    fetchCodexAccounts,
    fetchCurrentAccount,
    fetchCurrentClaudeAccountId,
    fetchCurrentCodexAccount,
    fetchCurrentCursorAccountId,
    fetchCurrentWindsurfAccountId,
    fetchAccounts,
    refreshAllClaudeQuotas,
    refreshAllCursorTokens,
    refreshAllQuotas,
    refreshAllWindsurfTokens,
    refreshClaudeQuota,
    refreshCursorToken,
    refreshWindsurfToken,
    stopScheduler,
  ]);

  useEffect(() => {
    destroyedRef.current = false;
    let disposed = false;
    let unlistenCurrentAccount: UnlistenFn | undefined;
    let startupTimer = window.setTimeout(() => {
      startupTimer = 0;
      console.log(
        `[StartupPerf][AutoRefresh] deferred startup setup triggered after ${STARTUP_AUTO_REFRESH_SETUP_DELAY_MS}ms`,
      );
      void setupAutoRefresh();
    }, STARTUP_AUTO_REFRESH_SETUP_DELAY_MS);

    const handleConfigUpdate = () => {
      if (startupTimer) {
        window.clearTimeout(startupTimer);
        startupTimer = 0;
      }
      console.log('[AutoRefresh] 检测到配置变更，重新设置调度器');
      void setupAutoRefresh();
    };

    window.addEventListener('config-updated', handleConfigUpdate);
    void listen(CURRENT_ACCOUNT_CHANGED_EVENT, handleConfigUpdate).then((unlisten) => {
      if (disposed) {
        unlisten();
      } else {
        unlistenCurrentAccount = unlisten;
      }
    });

    return () => {
      disposed = true;
      destroyedRef.current = true;
      setupPendingRef.current = false;
      if (startupTimer) {
        window.clearTimeout(startupTimer);
      }
      stopScheduler();
      unlistenCurrentAccount?.();
      window.removeEventListener('config-updated', handleConfigUpdate);
    };
  }, [setupAutoRefresh, stopScheduler]);
}
