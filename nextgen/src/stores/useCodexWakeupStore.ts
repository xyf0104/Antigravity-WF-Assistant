import { create } from 'zustand';
import * as codexWakeupService from '../services/codexWakeupService';
import {
  CodexCliStatus,
  CodexWakeupBatchResult,
  CodexWakeupHistoryItem,
  CodexWakeupModelPreset,
  CodexWakeupReasoningEffort,
  CodexWakeupState,
  CodexWakeupTask,
} from '../types/codexWakeup';

interface CodexWakeupStoreState {
  runtime: CodexCliStatus | null;
  state: CodexWakeupState;
  history: CodexWakeupHistoryItem[];
  loading: boolean;
  saving: boolean;
  runningTaskId: string | null;
  testing: boolean;
  error: string | null;
  loadAll: () => Promise<void>;
  refreshRuntime: (runtimeConfig?: { codexCliPath?: string; nodePath?: string }) => Promise<void>;
  saveState: (
    enabled: boolean,
    tasks: CodexWakeupTask[],
    modelPresets: CodexWakeupModelPreset[],
  ) => Promise<CodexWakeupState>;
  runTask: (taskId: string, runId: string) => Promise<CodexWakeupBatchResult>;
  runTest: (
    accountIds: string[],
    runId: string,
    prompt?: string,
    model?: string,
    modelDisplayName?: string,
    modelReasoningEffort?: CodexWakeupReasoningEffort,
    cancelScopeId?: string,
  ) => Promise<CodexWakeupBatchResult>;
  cancelTestScope: (cancelScopeId: string) => Promise<void>;
  releaseTestScope: (cancelScopeId: string) => Promise<void>;
  clearHistory: () => Promise<void>;
}

const EMPTY_STATE: CodexWakeupState = {
  enabled: false,
  tasks: [],
  model_presets: [],
  model_preset_migrations: [],
};

function formatCodexWakeupError(error: unknown): string {
  let message = '';
  if (error instanceof Error) {
    message = error.message;
  } else if (typeof error === 'string') {
    message = error;
  } else if (error && typeof error === 'object') {
    const record = error as { message?: unknown; error?: unknown };
    if (typeof record.message === 'string') {
      message = record.message;
    } else if (typeof record.error === 'string') {
      message = record.error;
    }
  }

  return message.replace(/^(?:(?:Error|TypeError):\s*)+/i, '').trim() || '操作失败，请稍后重试';
}

let loadAllInFlight: Promise<void> | null = null;

export const useCodexWakeupStore = create<CodexWakeupStoreState>((set) => ({
  runtime: null,
  state: EMPTY_STATE,
  history: [],
  loading: false,
  saving: false,
  runningTaskId: null,
  testing: false,
  error: null,
  loadAll: async () => {
    if (loadAllInFlight) {
      return loadAllInFlight;
    }
    set((current) => ({
      loading: current.runtime === null && current.history.length === 0 && current.state.tasks.length === 0,
      error: null,
    }));
    loadAllInFlight = (async () => {
      try {
        const overview = await codexWakeupService.getCodexWakeupOverview();
        set({
          runtime: overview.runtime,
          state: overview.state,
          history: overview.history,
          loading: false,
        });
      } catch (error) {
        set({ loading: false, error: formatCodexWakeupError(error) });
      } finally {
        loadAllInFlight = null;
      }
    })();
    return loadAllInFlight;
  },
  refreshRuntime: async (runtimeConfig) => {
    try {
      const runtime = runtimeConfig
        ? await codexWakeupService.updateCodexWakeupRuntimeConfig(
            runtimeConfig.codexCliPath,
            runtimeConfig.nodePath,
          )
        : await codexWakeupService.getCodexWakeupCliStatus();
      set({ runtime });
    } catch (error) {
      const message = formatCodexWakeupError(error);
      set({ error: message });
      throw message;
    }
  },
  saveState: async (enabled, tasks, modelPresets) => {
    set({ saving: true, error: null });
    try {
      const currentMigrations = useCodexWakeupStore.getState().state.model_preset_migrations;
      const state = await codexWakeupService.saveCodexWakeupState(
        enabled,
        tasks,
        modelPresets,
        currentMigrations,
      );
      set({ state, saving: false });
      return state;
    } catch (error) {
      const message = formatCodexWakeupError(error);
      set({ saving: false, error: message });
      throw message;
    }
  },
  runTask: async (taskId, runId) => {
    set({ runningTaskId: taskId, error: null });
    try {
      const result = await codexWakeupService.runCodexWakeupTask(taskId, runId);
      const [state, history, runtime] = await Promise.all([
        codexWakeupService.getCodexWakeupState(),
        codexWakeupService.loadCodexWakeupHistory(),
        codexWakeupService.getCodexWakeupCliStatus(),
      ]);
      set({ state, history, runtime, runningTaskId: null });
      return result;
    } catch (error) {
      const message = formatCodexWakeupError(error);
      set({ runningTaskId: null, error: message });
      throw message;
    }
  },
  runTest: async (
    accountIds,
    runId,
    prompt,
    model,
    modelDisplayName,
    modelReasoningEffort,
    cancelScopeId,
  ) => {
    set({ testing: true, error: null });
    try {
      const result = await codexWakeupService.testCodexWakeup(
        accountIds,
        runId,
        prompt,
        model,
        modelDisplayName,
        modelReasoningEffort,
        cancelScopeId,
      );
      const [history, runtime] = await Promise.all([
        codexWakeupService.loadCodexWakeupHistory(),
        codexWakeupService.getCodexWakeupCliStatus(),
      ]);
      set({ history, runtime, testing: false });
      return result;
    } catch (error) {
      const message = formatCodexWakeupError(error);
      set({ testing: false, error: message });
      throw message;
    }
  },
  cancelTestScope: async (cancelScopeId) => {
    await codexWakeupService.cancelCodexWakeupScope(cancelScopeId);
  },
  releaseTestScope: async (cancelScopeId) => {
    await codexWakeupService.releaseCodexWakeupScope(cancelScopeId);
  },
  clearHistory: async () => {
    set({ error: null });
    try {
      await codexWakeupService.clearCodexWakeupHistory();
      set({ history: [] });
    } catch (error) {
      const message = formatCodexWakeupError(error);
      set({ error: message });
      throw message;
    }
  },
}));
