import { ReactNode, useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Clock3, FolderOpen, Layers, Server } from 'lucide-react';
import { ManualHelpIconButton } from '../ManualHelpIconButton';
import { PlatformId } from '../../types/platform';
import {
  findGroupByPlatform,
  resolveGroupChildName,
  usePlatformLayoutStore,
} from '../../stores/usePlatformLayoutStore';
import { getPlatformLabel, renderPlatformIcon } from '../../utils/platformMeta';
import { PlatformGroupSwitcher } from './PlatformGroupSwitcher';
import { useRemoteConfigStore } from '../../stores/useRemoteConfigStore';
import {
  XIASS_WORKSPACE_PANEL_NAVIGATE_EVENT,
  type XiassWorkspacePanelNavigateDetail,
} from '../../utils/xiassWorkspaceNavigation';

export type PlatformOverviewTab =
  | 'overview'
  | 'wakeup'
  | 'instances'
  | 'sessions'
  | 'providers';
export type PlatformOverviewHeaderId =
  | 'codex'
  | 'claude'
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

interface PlatformOverviewTabsHeaderProps {
  platform: PlatformOverviewHeaderId;
  active: PlatformOverviewTab;
  onTabChange?: (tab: PlatformOverviewTab) => void;
  tabs?: PlatformOverviewTab[];
}

interface PlatformOverviewConfig {
  platformLabel: string;
  overviewIcon: ReactNode;
}

interface TabSpec {
  key: PlatformOverviewTab;
  label: string;
  icon: ReactNode;
}

const CONFIGS: Record<PlatformOverviewHeaderId, PlatformOverviewConfig> = {
  codex: {
    platformLabel: 'Codex',
    overviewIcon: renderPlatformIcon('codex', 18),
  },
  claude: {
    platformLabel: 'Claude',
    overviewIcon: renderPlatformIcon('claude_manager', 18),
  },
  zed: {
    platformLabel: 'Zed',
    overviewIcon: renderPlatformIcon('zed', 18),
  },
  'github-copilot': {
    platformLabel: 'GitHub Copilot',
    overviewIcon: renderPlatformIcon('github-copilot', 18),
  },
  windsurf: {
    platformLabel: 'Windsurf',
    overviewIcon: renderPlatformIcon('windsurf', 18),
  },
  kiro: {
    platformLabel: 'Kiro',
    overviewIcon: renderPlatformIcon('kiro', 18),
  },
  cursor: {
    platformLabel: 'Cursor',
    overviewIcon: renderPlatformIcon('cursor', 18),
  },
  grok: {
    platformLabel: 'Grok CLI',
    overviewIcon: renderPlatformIcon('grok', 18),
  },
  codebuddy: {
    platformLabel: 'CodeBuddy',
    overviewIcon: renderPlatformIcon('codebuddy', 18),
  },
  codebuddy_cn: {
    platformLabel: 'CodeBuddy CN',
    overviewIcon: renderPlatformIcon('codebuddy_cn', 18),
  },
  qoder: {
    platformLabel: 'Qoder',
    overviewIcon: renderPlatformIcon('qoder', 18),
  },
  zcode: {
    platformLabel: 'ZCode',
    overviewIcon: renderPlatformIcon('zcode', 18),
  },
  trae: {
    platformLabel: 'Trae',
    overviewIcon: renderPlatformIcon('trae', 18),
  },
  trae_solo: {
    platformLabel: 'TRAE SOLO',
    overviewIcon: renderPlatformIcon('trae_solo', 18),
  },
  trae_cn: {
    platformLabel: 'Trae CN',
    overviewIcon: renderPlatformIcon('trae_cn', 18),
  },
  trae_solo_cn: {
    platformLabel: 'TRAE SOLO CN',
    overviewIcon: renderPlatformIcon('trae_solo_cn', 18),
  },
  workbuddy: {
    platformLabel: 'WorkBuddy',
    overviewIcon: renderPlatformIcon('workbuddy', 18),
  },
};

export function PlatformOverviewTabsHeader({
  platform,
  active,
  onTabChange,
  tabs,
}: PlatformOverviewTabsHeaderProps) {
  const { t } = useTranslation();
  const { platformGroups } = usePlatformLayoutStore();
  const remoteHiddenPlatformIds = useRemoteConfigStore((state) => state.hiddenPlatformIds);
  const config = CONFIGS[platform];
  const currentPlatformId = platform as PlatformId;
  const remoteHiddenPlatformSet = useMemo(
    () => new Set(remoteHiddenPlatformIds),
    [remoteHiddenPlatformIds],
  );
  const currentGroup = useMemo(
    () => findGroupByPlatform(platformGroups, currentPlatformId),
    [platformGroups, currentPlatformId],
  );
  const switchablePlatforms = useMemo(
    () => {
      const source = currentGroup ? currentGroup.platformIds : [currentPlatformId];
      const visible = source.filter((platformId) =>
        platformId === currentPlatformId || !remoteHiddenPlatformSet.has(platformId),
      );
      return visible.length > 0 ? visible : [currentPlatformId];
    },
    [currentGroup, currentPlatformId, remoteHiddenPlatformSet],
  );
  const currentPlatformLabel = getPlatformLabel(currentPlatformId, t);
  const currentDisplayName = useMemo(
    () =>
      currentGroup
        ? resolveGroupChildName(currentGroup, currentPlatformId, currentPlatformLabel || config.platformLabel)
        : currentPlatformLabel || config.platformLabel,
    [currentGroup, currentPlatformId, currentPlatformLabel, config.platformLabel],
  );
  const switchOptions = useMemo(
    () =>
      switchablePlatforms.map((platformId) => {
        const platformName = currentGroup
          ? resolveGroupChildName(currentGroup, platformId, getPlatformLabel(platformId, t))
          : getPlatformLabel(platformId, t);
        return {
          platformId,
          label: platformName,
        };
      }),
    [switchablePlatforms, currentGroup, t],
  );
  const tabOrder: PlatformOverviewTab[] =
    tabs && tabs.length > 0 ? tabs : ['overview', 'instances'];

  useEffect(() => {
    if (!onTabChange) return;
    const handleWorkspaceNavigation = (event: Event) => {
      const detail = (event as CustomEvent<XiassWorkspacePanelNavigateDetail>).detail;
      if (detail?.platform !== platform) return;
      if (!tabOrder.includes(detail.tab as PlatformOverviewTab)) return;
      onTabChange(detail.tab as PlatformOverviewTab);
    };
    window.addEventListener(
      XIASS_WORKSPACE_PANEL_NAVIGATE_EVENT,
      handleWorkspaceNavigation,
    );
    return () => {
      window.removeEventListener(
        XIASS_WORKSPACE_PANEL_NAVIGATE_EVENT,
        handleWorkspaceNavigation,
      );
    };
  }, [onTabChange, platform, tabOrder]);

  const tabLabels: Record<PlatformOverviewTab, TabSpec> = {
    overview: {
      key: 'overview',
      label: t('overview.title', '账号总览'),
      icon: config.overviewIcon,
    },
    wakeup: {
      key: 'wakeup',
      label:
        platform === 'codex'
          ? t('codex.wakeup.tab', '唤醒任务')
          : t('wakeup.title', '唤醒任务'),
      icon: <Clock3 className="tab-icon" />,
    },
    instances: {
      key: 'instances',
      label: t('instances.title', '应用多开'),
      icon: <Layers className="tab-icon" />,
    },
    sessions: {
      key: 'sessions',
      label: t('codex.sessionManager.title', '会话管理'),
      icon: <FolderOpen className="tab-icon" />,
    },
    providers: {
      key: 'providers',
      label: t('codex.modelProviders.tab', '模型供应商'),
      icon: <Server className="tab-icon" />,
    },
  };
  const tabSpecs: TabSpec[] = tabOrder.map((tab) => tabLabels[tab]);

  return (
    <>
      <div className="page-top-strip">
        <div className="page-top-strip-left">
          <span className="page-top-strip-label">
            {t('nav.accounts', 'Accounts')}
          </span>
          <ManualHelpIconButton className="platform-header-help" />
        </div>
        <div className="page-top-strip-right-placeholder" aria-hidden="true" />
      </div>
      <div className="page-tabs-row page-tabs-center page-tabs-row-with-leading">
        <div className="page-tabs-leading">
          <PlatformGroupSwitcher
            currentPlatformId={currentPlatformId}
            currentLabel={currentDisplayName}
            options={switchOptions}
            currentGroupId={currentGroup?.id ?? null}
          />
        </div>
        <div
          className="page-tabs filter-tabs"
          role="tablist"
          aria-label={`${currentDisplayName} ${t('common.navigation', '导航')}`}
        >
          {tabSpecs.map((tab) => (
            <button
              key={tab.key}
              type="button"
              role="tab"
              aria-selected={active === tab.key}
              className={`filter-tab${active === tab.key ? ' active' : ''}`}
              onClick={() => onTabChange?.(tab.key)}
            >
              {tab.icon}
              <span>{tab.label}</span>
            </button>
          ))}
        </div>
      </div>
    </>
  );
}
