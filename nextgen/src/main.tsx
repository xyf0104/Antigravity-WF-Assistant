import React from "react";
import ReactDOM from "react-dom/client";
import packageJson from "../package.json";
import { initI18n } from "./i18n";
import { AppRuntimeGuard } from "./components/AppRuntimeGuard";
import {
  captureError,
  initErrorReporter,
  markFrontendReady,
  recordFrontendStage,
} from "./utils/errorReporter";
import { setBootSplashStage } from "./utils/bootSplash";
import { hydrateUiPreferences } from "./utils/uiPreferences";

const BROWSER_PREVIEW_APP_VERSION = packageJson.version;

function FrontendReadyMarker() {
  React.useEffect(() => {
    recordFrontendStage("react_commit_complete");
    setBootSplashStage("react_mounted");
    markFrontendReady("react_committed");
  }, []);

  return null;
}

initErrorReporter();
recordFrontendStage("script_loaded");
setBootSplashStage("script_loaded");
void initI18n();

async function installBrowserPreviewRuntime(): Promise<void> {
  if (!import.meta.env.DEV || "__TAURI_INTERNALS__" in window) {
    return;
  }
  const { mockIPC, mockWindows } = await import("@tauri-apps/api/mocks");
  const emptyUsage = {
    requestCount: 0,
    successCount: 0,
    failureCount: 0,
    clientCanceledCount: 0,
    upstreamResponseFailedCount: 0,
    streamIncompleteCount: 0,
    totalLatencyMs: 0,
    textRequestCount: 0,
    imageRequestCount: 0,
    imageGenerationRequestCount: 0,
    imageEditRequestCount: 0,
    imageGenerationCapabilityFailureCount: 0,
    inputTokens: 0,
    outputTokens: 0,
    totalTokens: 0,
    cachedTokens: 0,
    reasoningTokens: 0,
    estimatedCostUsd: 0,
  };
  const emptyStatsWindow = {
    since: 0,
    updatedAt: 0,
    totals: emptyUsage,
    accounts: [],
    models: [],
    apiKeys: [],
  };
  const emptyLocalAccessState = {
    collection: null,
    running: false,
    preparing: false,
    preparationTotal: 0,
    preparationCompleted: 0,
    refreshingAccounts: false,
    accountRefreshTotal: 0,
    accountRefreshCompleted: 0,
    defaultProfile: null,
    apiPortUrl: null,
    baseUrl: null,
    lanBaseUrl: null,
    modelIds: [],
    modelPricingPresets: [],
    lastError: null,
    memberCount: 0,
    stats: {
      ...emptyStatsWindow,
      daily: emptyStatsWindow,
      weekly: emptyStatsWindow,
      monthly: emptyStatsWindow,
      events: [],
    },
    accountHealth: [],
    accountPoolHealth: [],
    quotaReserveStatus: null,
  };
  let previewGeneralConfig = {
    language: "zh-cn",
    default_terminal: "system",
    theme: "system",
    theme_color: "default",
    auto_refresh_minutes: 10,
    reduced_motion_enabled: false,
    ui_scale: 1,
  };
  mockWindows("main");
  mockIPC((command, payload) => {
    if (command === "plugin:app|version") {
      return BROWSER_PREVIEW_APP_VERSION;
    }
    if (command === "load_legal_notices") {
      return {
        notices: [
          {
            id: "origin_and_license",
            title: "来源与许可说明",
            content: "浏览器预览不会读取安装包资源。已安装的 XIASS Tools 会在此离线显示完整来源与许可说明。",
          },
          {
            id: "third_party_notices",
            title: "第三方声明",
            content: "已安装的 XIASS Tools 会在此离线显示完整第三方组件声明。",
          },
          {
            id: "cc_by_nc_sa_4_0",
            title: "CC BY-NC-SA 4.0 法律文本",
            content: "已安装的 XIASS Tools 会在此离线显示完整法律文本。",
          },
          {
            id: "xiass_nextgen_license",
            title: "XIASS Tools Nextgen 许可",
            content: "已安装的 XIASS Tools 会在此离线显示完整许可文本。",
          },
        ],
      };
    }
    if (command === "get_available_terminals") {
      return ["system", "Terminal", "iTerm2", "Warp"];
    }
    if (command === "get_general_config") {
      return previewGeneralConfig;
    }
    if (command === "patch_general_config") {
      const updates = payload && !Array.isArray(payload) && typeof payload === "object"
        ? (payload as Record<string, unknown>).updates
        : null;
      if (updates && typeof updates === "object") {
        previewGeneralConfig = {
          ...previewGeneralConfig,
          ...(updates as Partial<typeof previewGeneralConfig>),
        };
      }
      return previewGeneralConfig;
    }
    if (
      command === "load_codex_account_groups"
      || command === "load_codex_model_providers"
    ) {
      // These production commands return serialized JSON. Returning null here
      // makes the browser preview look like a corrupted user configuration and
      // masks real UI console errors during page-by-page review.
      return "[]";
    }
    if (command === "load_ui_preferences") {
      return { values: {} };
    }
    if (command === "load_user_memory") {
      return { dismissed: {}, lists: {} };
    }
    if (command === "load_antigravity_switch_history") {
      return [];
    }
    if (command === "wf_bridge_get_session") {
      return {
        url: "http://127.0.0.1:5177",
        token: "xiass-browser-preview-token-00000000000000000000000000000000",
        host: "127.0.0.1",
        port: 5177,
        schemaVersion: 1,
      };
    }
    if (command === "wf_bridge_get_status") {
      return { running: true, url: "http://127.0.0.1:5177", lastError: null };
    }
    if (command === "codex_local_access_get_state") {
      return emptyLocalAccessState;
    }
    if (command === "logs_get_snapshot") {
      return {
        log_dir_path: "",
        log_file_path: "",
        log_file_name: "",
        content: "",
        line_limit: 200,
        file_size: 0,
        modified_at_ms: null,
        available_files: [],
      };
    }
    if (command === "logs_open_log_directory") {
      return null;
    }
    if (command === "get_auto_backup_settings") {
      return {
        enabled: false,
        include_accounts: true,
        include_config: true,
        retention_days: 15,
        last_backup_at: null,
        directory_path: "",
      };
    }
    if (command === "get_backup_usage") {
      return {
        total_file_count: 0,
        total_size_bytes: 0,
        entries: [],
      };
    }
    if (
      command === "get_codex_app_speed_config"
      || command === "get_codex_api_service_app_speed_config"
    ) {
      return { speed: "standard" };
    }
    if (command === "list_ssh_servers") {
      return { selected_server_id: null, servers: [] };
    }
    if (command === "announcement_get_state" || command === "announcement_force_refresh") {
      return { announcements: [], unreadIds: [], popupAnnouncement: null };
    }
    if (command === "remote_config_get_state" || command === "remote_config_force_refresh") {
      return {
        version: "",
        codexOAuthAppVersion: "26.820.60940",
        updatedAt: 0,
        currentOs: "browser-preview",
        hiddenPlatformIds: [],
        appliedRules: [],
        refreshIntervalMs: 3600000,
        updatePromptMode: "normal",
      };
    }
    if (command.includes("get_all") || command.includes("list_")) {
      return [];
    }
    return null;
  }, { shouldMockEvents: true });
}

void installBrowserPreviewRuntime().then(() => hydrateUiPreferences()).then(async () => {
  const { default: App } = await import("./App");

  const rootElement = document.getElementById("root");
  if (!rootElement) {
    const error = new Error("Root element not found");
    captureError(error, { source: "frontend_boot", phase: "root_lookup" });
    throw error;
  }

  recordFrontendStage("react_mount_start");
  ReactDOM.createRoot(rootElement).render(
    <React.StrictMode>
      <AppRuntimeGuard>
        <FrontendReadyMarker />
        <App />
      </AppRuntimeGuard>
    </React.StrictMode>,
  );
});
