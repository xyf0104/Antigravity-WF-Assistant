import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from 'react';
import { useTranslation } from 'react-i18next';
import {
  Blocks,
  CircleAlert,
  Clock3,
  FolderClock,
  Gauge,
  Image,
  KeyRound,
  Layers3,
  MonitorCog,
  Network,
  RefreshCw,
  ServerCog,
  ShieldCheck,
  SlidersHorizontal,
  TerminalSquare,
  UserRoundCheck,
  Users,
} from 'lucide-react';
import { PlatformGroupSwitcher } from '../components/platform/PlatformGroupSwitcher';
import {
  findGroupByPlatform,
  resolveGroupChildName,
  usePlatformLayoutStore,
} from '../stores/usePlatformLayoutStore';
import { getPlatformLabel } from '../utils/platformMeta';
import type { PlatformId } from '../types/platform';
import {
  getWfBridgeSession,
  handleWfBridgeHostAction,
  type WfBridgeHostActionRequest,
  type WfBridgeSession,
} from '../services/wfBridgeService';
import {
  navigateXiassWorkspacePanel,
  type XiassWorkspacePanelNavigateDetail,
} from '../utils/xiassWorkspaceNavigation';
import './XiassAgentWorkspace.css';

export type XiassCoreModule = 'antigravity' | 'codex' | 'claude-code' | 'cursor' | 'windsurf';

type NativeSectionId = 'dashboard' | 'models' | 'upstreams' | 'permissions' | 'configuration';

interface NativeNavigationTarget {
  kind: 'native';
  section: NativeSectionId;
}

interface PanelNavigationTarget {
  kind: 'panel';
  panelId: string;
  panelNavigation?: XiassWorkspacePanelNavigateDetail;
}

type WorkspaceNavigationTarget = NativeNavigationTarget | PanelNavigationTarget;

interface WorkspaceNavigationItem {
  id: string;
  label: string;
  caption: string;
  icon: ReactNode;
  target: WorkspaceNavigationTarget;
}

export interface XiassWorkspacePanel {
  id: string;
  content: ReactNode;
  managedNavigation?: boolean;
}

interface XiassAgentWorkspaceProps {
  module: XiassCoreModule;
  platformId: PlatformId;
  title: string;
  description: string;
  panels?: XiassWorkspacePanel[];
  initialView?: string;
  requestedView?: string;
}

function nativeItem(
  section: NativeSectionId,
  label: string,
  caption: string,
  icon: ReactNode,
): WorkspaceNavigationItem {
  return {
    id: `native:${section}`,
    label,
    caption,
    icon,
    target: { kind: 'native', section },
  };
}

function panelItem(
  id: string,
  panelId: string,
  label: string,
  caption: string,
  icon: ReactNode,
  panelNavigation?: XiassWorkspacePanelNavigateDetail,
): WorkspaceNavigationItem {
  return {
    id,
    label,
    caption,
    icon,
    target: { kind: 'panel', panelId, panelNavigation },
  };
}

const WF_HOST_ACTION_KINDS = new Set<WfBridgeHostActionRequest['kind']>([
  'open_url',
  'open_file',
  'open_directory',
  'save_file',
  'claude_code_account_candidates',
  'claude_code_apply_account',
]);

const EMBEDDED_WORKSPACE_MIN_HEIGHT = 520;
const EMBEDDED_WORKSPACE_MAX_HEIGHT = 32_000;
const EMBEDDED_WORKSPACE_MAX_SCROLL_DELTA = 1_200;

function normalizeEmbeddedWorkspaceHeight(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) return null;
  const height = Math.ceil(value);
  if (height < EMBEDDED_WORKSPACE_MIN_HEIGHT || height > EMBEDDED_WORKSPACE_MAX_HEIGHT) {
    return null;
  }
  return height;
}

function normalizeEmbeddedWorkspaceScrollDelta(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) return null;
  return Math.max(
    -EMBEDDED_WORKSPACE_MAX_SCROLL_DELTA,
    Math.min(EMBEDDED_WORKSPACE_MAX_SCROLL_DELTA, value),
  );
}

function isWfBridgeHostActionRequest(value: unknown): value is WfBridgeHostActionRequest {
  if (!value || typeof value !== 'object') return false;
  const request = value as Partial<WfBridgeHostActionRequest>;
  return (
    typeof request.requestId === 'string'
    && /^[0-9a-f]{64}$/.test(request.requestId)
    && typeof request.kind === 'string'
    && WF_HOST_ACTION_KINDS.has(request.kind as WfBridgeHostActionRequest['kind'])
  );
}

function navigationItemsForModule(module: XiassCoreModule): WorkspaceNavigationItem[] {
  switch (module) {
    case 'antigravity':
      return [
        nativeItem('dashboard', '运行总览', '代理、补丁与快捷启动', <Gauge size={18} />),
        nativeItem('models', '模型与生图', '模型、推理与图片路由', <Image size={18} />),
        nativeItem('upstreams', '上游代理', 'API、账号池与链路测试', <Network size={18} />),
        panelItem('panel:official-accounts', 'official-accounts', '官方账号', 'OAuth、额度与账号切换', <UserRoundCheck size={18} />),
        panelItem('panel:instances', 'instances', '应用多开', '独立配置与并行运行', <Layers3 size={18} />),
        panelItem('panel:wakeup', 'wakeup', '唤醒任务', '定时任务与额度调度', <Clock3 size={18} />),
        panelItem('panel:verification', 'verification', '账号验证', '批量验证与历史记录', <ShieldCheck size={18} />),
        nativeItem('permissions', '终端权限', '命令批准与安全策略', <TerminalSquare size={18} />),
      ];
    case 'codex':
      return [
        nativeItem('configuration', '配置中心', 'Provider、模型与历史兼容', <SlidersHorizontal size={18} />),
        panelItem('panel:accounts', 'account-center', '账号中心', 'OAuth、Token、额度与账号池', <Users size={18} />, { platform: 'codex', tab: 'overview' }),
        panelItem('panel:providers', 'account-center', '模型供应商', '上游模型、映射与用量', <Blocks size={18} />, { platform: 'codex', tab: 'providers' }),
        panelItem('panel:api-service', 'api-service', 'API 服务', 'Key、路由、模型与日志', <ServerCog size={18} />),
        panelItem('panel:wakeup', 'account-center', '唤醒任务', '定时任务与额度恢复', <Clock3 size={18} />, { platform: 'codex', tab: 'wakeup' }),
        panelItem('panel:instances', 'account-center', '应用多开', '实例、账号与启动配置', <Layers3 size={18} />, { platform: 'codex', tab: 'instances' }),
        panelItem('panel:sessions', 'account-center', '会话管理', '恢复、可见性与历史会话', <FolderClock size={18} />, { platform: 'codex', tab: 'sessions' }),
      ];
    case 'claude-code':
      return [
        nativeItem('configuration', '配置中心', '网关、模型与恢复点', <SlidersHorizontal size={18} />),
        panelItem('panel:desktop-accounts', 'account-center', 'Claude 账号', '桌面登录、额度与切换', <Users size={18} />, { platform: 'claude-code', tab: 'desktop' }),
        panelItem('panel:cli-accounts', 'account-center', 'Claude Code 账号', 'OAuth、API Key 与本地导入', <KeyRound size={18} />, { platform: 'claude-code', tab: 'cli' }),
        panelItem('panel:instances', 'account-center', '应用多开', '独立实例与账号配置', <Layers3 size={18} />, { platform: 'claude-code', tab: 'instances' }),
      ];
    case 'cursor':
      return [
        nativeItem('configuration', '本机与 MCP', '全局、项目配置与恢复点', <MonitorCog size={18} />),
        panelItem('panel:accounts', 'account-center', '账号中心', 'OAuth、Token、额度与切换', <Users size={18} />, { platform: 'cursor', tab: 'overview' }),
        panelItem('panel:instances', 'account-center', '应用多开', '独立实例与账号配置', <Layers3 size={18} />, { platform: 'cursor', tab: 'instances' }),
      ];
    case 'windsurf':
      return [
        nativeItem('configuration', '本机与 MCP', '配置、诊断与恢复点', <MonitorCog size={18} />),
        panelItem('panel:accounts', 'account-center', '账号中心', 'OAuth、密码、额度与切换', <Users size={18} />, { platform: 'windsurf', tab: 'overview' }),
        panelItem('panel:instances', 'account-center', '应用多开', '独立实例与账号配置', <Layers3 size={18} />, { platform: 'windsurf', tab: 'instances' }),
      ];
  }
}

function defaultView(items: WorkspaceNavigationItem[], initialView?: string): string {
  if (initialView && items.some((item) => item.id === initialView)) return initialView;
  return items[0]?.id ?? 'native:configuration';
}

function readRememberedView(
  module: XiassCoreModule,
  items: WorkspaceNavigationItem[],
  initialView?: string,
): string {
  const fallback = defaultView(items, initialView);
  try {
    const current = localStorage.getItem(`xiass-tools.workspace.${module}.section.v2`);
    if (current && items.some((item) => item.id === current)) return current;

    const legacy = localStorage.getItem(`xiass-tools.workspace.${module}.mode`);
    const migrated = legacy?.startsWith('core:')
      ? legacy.replace(/^core:/, 'native:')
      : legacy === 'accounts'
        ? 'panel:accounts'
        : legacy === 'core'
          ? fallback
          : null;
    return migrated && items.some((item) => item.id === migrated) ? migrated : fallback;
  } catch {
    return fallback;
  }
}

export function XiassAgentWorkspace({
  module,
  platformId,
  title,
  description,
  panels = [],
  initialView,
  requestedView,
}: XiassAgentWorkspaceProps) {
  const { t } = useTranslation();
  const { platformGroups } = usePlatformLayoutStore();
  const navigationItems = useMemo(() => navigationItemsForModule(module), [module]);
  const [view, setView] = useState(() => readRememberedView(module, navigationItems, initialView));
  const [session, setSession] = useState<WfBridgeSession | null>(null);
  const [loading, setLoading] = useState(false);
  const [iframeReady, setIframeReady] = useState(false);
  const [iframeContentHeight, setIframeContentHeight] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [resolvedTheme, setResolvedTheme] = useState<'light' | 'dark'>(() =>
    document.documentElement.dataset.theme === 'light' ? 'light' : 'dark',
  );
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const handledHostActionIdsRef = useRef(new Set<string>());
  const activeItem = navigationItems.find((item) => item.id === view) ?? navigationItems[0];
  const activeTarget = activeItem?.target;
  const activePanelId = activeTarget?.kind === 'panel' ? activeTarget.panelId : null;
  const panelIdSet = useMemo(() => new Set(panels.map((panel) => panel.id)), [panels]);
  const currentGroup = useMemo(
    () => findGroupByPlatform(platformGroups, platformId),
    [platformGroups, platformId],
  );
  const currentPlatformLabel = useMemo(
    () => getPlatformLabel(platformId, t),
    [platformId, t],
  );
  const groupSwitcherOptions = useMemo(() => {
    const platformIds = currentGroup ? currentGroup.platformIds : [platformId];
    return platformIds.map((candidatePlatformId) => ({
      platformId: candidatePlatformId,
      label: currentGroup
        ? resolveGroupChildName(
            currentGroup,
            candidatePlatformId,
            getPlatformLabel(candidatePlatformId, t),
          )
        : getPlatformLabel(candidatePlatformId, t),
    }));
  }, [currentGroup, platformId, t]);
  const groupSwitcherLabel = useMemo(
    () =>
      currentGroup
        ? resolveGroupChildName(currentGroup, platformId, currentPlatformLabel)
        : currentPlatformLabel,
    [currentGroup, currentPlatformLabel, platformId],
  );

  const loadNativeWorkspace = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const value = await getWfBridgeSession();
      if (value.host !== '127.0.0.1' || !value.url.startsWith('http://127.0.0.1:')) {
        throw new Error('工作台返回了无效的本机地址');
      }
      setSession(value);
    } catch (reason) {
      setSession(null);
      setError(String(reason));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activeTarget?.kind !== 'native' || session || loading) return;
    void loadNativeWorkspace();
  }, [activeTarget, loadNativeWorkspace, loading, session]);

  useEffect(() => {
    const root = document.documentElement;
    const syncTheme = () => {
      setResolvedTheme(root.dataset.theme === 'light' ? 'light' : 'dark');
    };
    syncTheme();
    const observer = new MutationObserver(syncTheme);
    observer.observe(root, { attributes: true, attributeFilter: ['data-theme'] });
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    setView(readRememberedView(module, navigationItems, initialView));
  }, [initialView, module, navigationItems]);

  useEffect(() => {
    if (!navigationItems.some((item) => item.id === view)) return;
    try {
      localStorage.setItem(`xiass-tools.workspace.${module}.section.v2`, view);
    } catch {
      // Read-only browser profiles still need working navigation.
    }
  }, [module, navigationItems, view]);

  useEffect(() => {
    if (!requestedView || requestedView === view) return;
    if (!navigationItems.some((item) => item.id === requestedView)) return;
    setView(requestedView);
  }, [navigationItems, requestedView, view]);

  useEffect(() => {
    if (activeTarget?.kind !== 'panel' || !activeTarget.panelNavigation) return;
    navigateXiassWorkspacePanel(activeTarget.panelNavigation);
  }, [activeTarget]);

  const iframeUrl = useMemo(() => {
    if (!session || activeTarget?.kind !== 'native') return '';
    const url = new URL(session.url);
    url.searchParams.set('embedded', '1');
    url.searchParams.set('module', module);
    url.searchParams.set('theme', resolvedTheme);
    if (module === 'antigravity') {
      url.searchParams.set(
        'section',
        activeTarget.section === 'upstreams' ? 'accounts' : activeTarget.section,
      );
    }
    return url.toString();
  }, [activeTarget, module, resolvedTheme, session]);

  useEffect(() => {
    setIframeReady(false);
    setIframeContentHeight(null);
  }, [iframeUrl]);

  useEffect(() => {
    handledHostActionIdsRef.current.clear();
  }, [session?.port, session?.token]);

  useEffect(() => {
    if (!session) return;
    const expectedOrigin = new URL(session.url).origin;
    const handleEmbeddedMessage = (event: MessageEvent) => {
      if (
        event.origin !== expectedOrigin
        || event.source !== iframeRef.current?.contentWindow
      ) {
        return;
      }

      if (event.data?.type === 'xiass-wf-ready') {
        setIframeReady(true);
        return;
      }
      if (event.data?.type === 'xiass-wf-content-height') {
        const contentHeight = normalizeEmbeddedWorkspaceHeight(event.data?.height);
        if (contentHeight !== null) {
          setIframeContentHeight((current) =>
            current !== null && Math.abs(current - contentHeight) < 2
              ? current
              : contentHeight,
          );
        }
        return;
      }
      if (event.data?.type === 'xiass-wf-scroll') {
        const deltaX = normalizeEmbeddedWorkspaceScrollDelta(event.data?.deltaX) ?? 0;
        const deltaY = normalizeEmbeddedWorkspaceScrollDelta(event.data?.deltaY) ?? 0;
        if (deltaX === 0 && deltaY === 0) return;
        document.querySelector<HTMLElement>('.main-wrapper')?.scrollBy({
          left: deltaX,
          top: deltaY,
          behavior: 'auto',
        });
        return;
      }
      if (event.data?.type !== 'xiass-wf-host-action') return;

      const request = event.data.request;
      if (
        !isWfBridgeHostActionRequest(request)
        || handledHostActionIdsRef.current.has(request.requestId)
      ) {
        return;
      }
      handledHostActionIdsRef.current.add(request.requestId);
      void handleWfBridgeHostAction(session.port, request).catch(() => undefined);
    };
    window.addEventListener('message', handleEmbeddedMessage);
    return () => window.removeEventListener('message', handleEmbeddedMessage);
  }, [session]);

  const deliverToken = useCallback(() => {
    if (!session || !iframeRef.current?.contentWindow) return;
    iframeRef.current.contentWindow.postMessage(
      { type: 'xiass-wf-auth', token: session.token },
      session.url,
    );
  }, [session]);

  const selectItem = (item: WorkspaceNavigationItem) => {
    setView(item.id);
    if (item.id === view && item.target.kind === 'panel' && item.target.panelNavigation) {
      navigateXiassWorkspacePanel(item.target.panelNavigation);
    }
  };

  const embeddedFrameStyle = useMemo<CSSProperties | undefined>(
    () =>
      activeTarget?.kind !== 'native' || iframeContentHeight === null
        ? undefined
        : { '--xiass-embedded-frame-height': `${iframeContentHeight}px` } as CSSProperties,
    [activeTarget?.kind, iframeContentHeight],
  );

  return (
    <section
      className="xiass-agent-workspace"
      data-module={module}
      aria-label={`${title}：${description}`}
    >
      <header className="page-top-strip xiass-agent-workspace__top-strip">
        <div className="page-top-strip-left xiass-agent-workspace__top-strip-left">
          <span className="page-top-strip-label xiass-agent-workspace__page-label">
            账号
          </span>
        </div>
        <div className="page-top-strip-right-placeholder" aria-hidden="true" />
      </header>

      <div className="page-tabs-row page-tabs-center page-tabs-row-with-leading xiass-agent-workspace__tabs-row">
        <div className="page-tabs-leading">
          <PlatformGroupSwitcher
            currentPlatformId={platformId}
            currentLabel={groupSwitcherLabel}
            options={groupSwitcherOptions}
            currentGroupId={currentGroup?.id ?? null}
          />
        </div>
        <nav
          className="page-tabs filter-tabs xiass-agent-workspace__navigation"
          aria-label={`${title} 功能导航`}
          role="tablist"
        >
          {navigationItems.map((item) => (
            <button
              key={item.id}
              type="button"
              role="tab"
              aria-selected={view === item.id}
              className={`filter-tab${view === item.id ? ' active' : ''}`}
              title={item.caption}
              onClick={() => selectItem(item)}
            >
              <span className="xiass-agent-workspace__navigation-icon" aria-hidden="true">
                {item.icon}
              </span>
              <span>{item.label}</span>
            </button>
          ))}
        </nav>
      </div>

      <main
        className="xiass-agent-workspace__canvas"
        style={embeddedFrameStyle}
        aria-busy={activeTarget?.kind === 'native' && loading}
      >
        <header className="xiass-agent-workspace__section-head">
          <div>
            <strong>{activeItem?.label}</strong>
            <span>{activeItem?.caption}</span>
          </div>
          <span className="xiass-agent-workspace__section-status" aria-live="polite">
            {activeTarget?.kind === 'native' ? '本机组件' : '统一账号服务'}
          </span>
        </header>

        <div className="xiass-agent-workspace__surface">
          <div
            className="xiass-agent-workspace__native"
            hidden={activeTarget?.kind !== 'native'}
          >
            {(loading || (session && !iframeReady)) && (
              <div className="xiass-agent-workspace__state" role="status">
                <RefreshCw size={22} className="spin" />
                <h2>正在加载工作台</h2>
                <p>首次进入会启动本机组件，完成后会自动显示。</p>
              </div>
            )}
            {!loading && error && (
              <div className="xiass-agent-workspace__state error" role="alert">
                <CircleAlert size={24} />
                <h2>工作台未能启动</h2>
                <p>{error}</p>
                <button type="button" onClick={() => void loadNativeWorkspace()}>
                  <RefreshCw size={15} />
                  重新启动
                </button>
              </div>
            )}
            {!loading && session && activeTarget?.kind === 'native' && (
              <iframe
                ref={iframeRef}
                className={`xiass-agent-workspace__iframe${iframeReady ? ' is-ready' : ''}`}
                src={iframeUrl}
                name={session.token}
                title={`${title} ${activeItem?.label}`}
                onLoad={deliverToken}
                aria-hidden={!iframeReady}
                allow="clipboard-read; clipboard-write"
              />
            )}
          </div>

          {panels.map((panel) => (
            <section
              key={panel.id}
              className="xiass-agent-workspace__panel"
              data-panel={panel.id}
              data-managed-navigation={panel.managedNavigation ? 'true' : 'false'}
              hidden={activePanelId !== panel.id}
              aria-hidden={activePanelId !== panel.id}
            >
              {panel.content}
            </section>
          ))}

          {activeTarget?.kind === 'panel' && !panelIdSet.has(activeTarget.panelId) && (
            <div className="xiass-agent-workspace__state error" role="alert">
              <CircleAlert size={24} />
              <h2>该功能尚未加载</h2>
              <p>请重新打开 XIASS Tools；如果问题持续存在，请导出诊断日志。</p>
            </div>
          )}
        </div>
      </main>
    </section>
  );
}
