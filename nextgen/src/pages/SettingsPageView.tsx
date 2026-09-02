import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { UnlockFireworksOverlay } from '../components/UnlockFireworksOverlay';
import { SettingsAccountTransferSection } from '../components/SettingsAccountTransferSection';
import { SettingsWebdavSyncSection } from '../components/SettingsWebdavSyncSection';
import { useEscClose } from '../hooks/useEscClose';
import { loadLegalNotices, type LegalNoticeDocument } from '../services/legalNoticesService';
import './settings/Settings.css';
import { Github, User, Save, AlertCircle, RefreshCw, FileText, Download, X } from 'lucide-react';
import xiassToolsLogo from '../../src-tauri/icons/app-icon-source.png';
import xiassToolsLightLogo from '../assets/xiass-tools-logo-light.png';
import type { PlatformId } from '../types/platform';
import type { useSettingsPageController } from "./SettingsPage";
import { SettingsGeneralPanel } from "./SettingsGeneralPanel";


export type SettingsPageViewProps = ReturnType<typeof useSettingsPageController>;

/** 渲染 SettingsPage 的界面；业务状态与动作统一由 Controller 提供。 */
export function SettingsPageView(props: SettingsPageViewProps) {
  const settingsContainerRef = useRef<HTMLDivElement>(null);
  const licenseNoticeDialogRef = useRef<HTMLDivElement>(null);
  const licenseNoticeTriggerRef = useRef<HTMLButtonElement>(null);
  const licenseNoticeCloseButtonRef = useRef<HTMLButtonElement>(null);
  const [licenseNoticeOpen, setLicenseNoticeOpen] = useState(false);
  const [legalNotices, setLegalNotices] = useState<LegalNoticeDocument[]>([]);
  const [selectedLegalNoticeId, setSelectedLegalNoticeId] = useState<string | null>(null);
  const [legalNoticesLoading, setLegalNoticesLoading] = useState(false);
  const [legalNoticesFailed, setLegalNoticesFailed] = useState(false);
  const [legalNoticesReloadVersion, setLegalNoticesReloadVersion] = useState(0);
  const {
    activeTab,
    actualPort,
    appVersion,
    defaultPort,
    generateReportToken,
    globalProxyEnabled,
    globalProxyNoProxy,
    globalProxyUrl,
    handleAboutAvatarTap,
    handleCheckUpdate,
    handleCloseMenuBarQuotaModal,
    handleCloseReleaseHistory,
    handleConfirmMenuBarQuotaModal,
    handleDownloadReleaseVersion,
    handleOpenReleaseHistory,
    handleSaveNetworkConfig,
    menuBarQuotaDraftPlatform,
    menuBarQuotaDraftShowPrefix,
    menuBarQuotaModalMode,
    menuBarQuotaModalOpen,
    menuBarQuotaPlatformOptions,
    needsRestart,
    networkSaving,
    openLink,
    reducedMotionEnabled,
    releaseHistoryError,
    releaseHistoryItems,
    releaseHistoryLoading,
    releaseHistoryOpen,
    releaseHistorySections,
    renderReleaseHistoryLine,
    reportActualPort,
    reportDefaultPort,
    reportEnabled,
    reportPort,
    reportRawPreviewUrl,
    reportRenderedPreviewUrl,
    reportToken,
    setActiveTab,
    setGlobalProxyEnabled,
    setGlobalProxyNoProxy,
    setGlobalProxyUrl,
    setMenuBarQuotaDraftPlatform,
    setMenuBarQuotaDraftShowPrefix,
    setReportEnabled,
    setReportPort,
    setReportToken,
    setWsEnabled,
    setWsPort,
    showUnlockFireworks,
    t,
    updateChecking,
    updateCheckMessage,
    wsEnabled,
    wsPort,
  } = props;

  useEffect(() => {
    const root = settingsContainerRef.current;
    if (!root) return;
    const controls = root.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(
      'input, select, textarea',
    );
    controls.forEach((control) => {
      const wrappingLabel = control.closest('label');
      if (
        control.getAttribute('aria-label')
        || control.getAttribute('aria-labelledby')
        || wrappingLabel?.textContent?.trim()
      ) return;
      const rowTitle = control.closest('.settings-row')?.querySelector<HTMLElement>('.row-title');
      const label = rowTitle?.textContent?.trim();
      if (label) control.setAttribute('aria-label', label);
    });
  });

  useEscClose(licenseNoticeOpen, () => setLicenseNoticeOpen(false));

  useEffect(() => {
    if (!licenseNoticeOpen) {
      return;
    }

    const previouslyFocused = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
    const frameId = window.requestAnimationFrame(() => {
      licenseNoticeCloseButtonRef.current?.focus();
    });

    return () => {
      window.cancelAnimationFrame(frameId);
      (licenseNoticeTriggerRef.current ?? previouslyFocused)?.focus();
    };
  }, [licenseNoticeOpen]);

  useEffect(() => {
    if (!licenseNoticeOpen) {
      return;
    }

    let cancelled = false;
    setLegalNoticesLoading(true);
    setLegalNoticesFailed(false);

    void loadLegalNotices()
      .then((collection) => {
        if (cancelled) {
          return;
        }
        setLegalNotices(collection.notices);
        setSelectedLegalNoticeId((current) =>
          current && collection.notices.some((notice) => notice.id === current)
            ? current
            : collection.notices[0].id,
        );
      })
      .catch(() => {
        if (!cancelled) {
          setLegalNotices([]);
          setSelectedLegalNoticeId(null);
          setLegalNoticesFailed(true);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLegalNoticesLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [licenseNoticeOpen, legalNoticesReloadVersion]);

  const selectedLegalNotice = legalNotices.find((notice) => notice.id === selectedLegalNoticeId)
    ?? legalNotices[0]
    ?? null;

  const retryLegalNoticeLoad = () => {
    setLegalNoticesReloadVersion((version) => version + 1);
  };

  const handleLegalNoticeTabKeyDown = (event: ReactKeyboardEvent<HTMLButtonElement>, currentIndex: number) => {
    const isPrevious = event.key === 'ArrowUp' || event.key === 'ArrowLeft';
    const isNext = event.key === 'ArrowDown' || event.key === 'ArrowRight';
    const isBoundary = event.key === 'Home' || event.key === 'End';
    if (!isPrevious && !isNext && !isBoundary) {
      return;
    }

    event.preventDefault();
    const nextIndex = event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? legalNotices.length - 1
        : (currentIndex + (isNext ? 1 : -1) + legalNotices.length) % legalNotices.length;
    const nextNotice = legalNotices[nextIndex];
    if (!nextNotice) {
      return;
    }

    setSelectedLegalNoticeId(nextNotice.id);
    window.requestAnimationFrame(() => {
      document.getElementById(`settings-license-tab-${nextNotice.id}`)?.focus();
    });
  };

  const handleLicenseDialogKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Tab') {
      return;
    }

    const focusableElements = Array.from(
      licenseNoticeDialogRef.current?.querySelectorAll<HTMLElement>('[data-license-dialog-focusable]') ?? [],
    );
    if (focusableElements.length === 0) {
      return;
    }

    const firstElement = focusableElements[0];
    const lastElement = focusableElements[focusableElements.length - 1];
    const currentIndex = focusableElements.indexOf(document.activeElement as HTMLElement);
    if (event.shiftKey && (currentIndex <= 0 || document.activeElement === firstElement)) {
      event.preventDefault();
      lastElement.focus();
    } else if (!event.shiftKey && (currentIndex === -1 || document.activeElement === lastElement)) {
      event.preventDefault();
      firstElement.focus();
    }
  };

  return (
    <main className="main-content">
      <div className="page-tabs-row settings-page-tabs-row">
        <div className="page-tabs-label">{t('settings.title')}</div>
        <div className="page-tabs filter-tabs" role="tablist" aria-label={t('settings.title')}>
          <button
            type="button"
            className={`filter-tab ${activeTab === 'general' ? 'active' : ''}`}
            role="tab"
            aria-selected={activeTab === 'general'}
            onClick={() => setActiveTab('general')}
          >
            {t('settings.tabs.general')}
          </button>
          <button
            type="button"
            className={`filter-tab ${activeTab === 'network' ? 'active' : ''}`}
            role="tab"
            aria-selected={activeTab === 'network'}
            onClick={() => setActiveTab('network')}
          >
            {t('settings.tabs.network')}
          </button>
          <button
            type="button"
            className={`filter-tab ${activeTab === 'data' ? 'active' : ''}`}
            role="tab"
            aria-selected={activeTab === 'data'}
            onClick={() => setActiveTab('data')}
          >
            {t('settings.tabs.data', '数据管理')}
          </button>
          <button
            type="button"
            className={`filter-tab ${activeTab === 'about' ? 'active' : ''}`}
            role="tab"
            aria-selected={activeTab === 'about'}
            onClick={() => setActiveTab('about')}
          >
            {t('settings.tabs.about')}
          </button>
        </div>
      </div>

      {/* 2. Content Area */}
      <div className="settings-container" ref={settingsContainerRef}>
        <div className="settings-content">
        {/* === General Tab === */}
        {activeTab === 'general' && <SettingsGeneralPanel {...props} />}

        {activeTab === 'data' && (
          <>
            <SettingsAccountTransferSection />
            <SettingsWebdavSyncSection />
          </>
        )}

        {/* === Network Tab === */}
        {activeTab === 'network' && (
          <>
            <div className="group-title">本地服务</div>
            <div className="settings-group">
              <div className="settings-row">
                <div className="row-label">
                  <div className="row-title">本地代理</div>
                  <div className="row-desc">为各 Agent 提供统一的本地连接服务</div>
                </div>
                <div className="row-control">
                  <label className="switch">
                    <input
                      type="checkbox"
                      checked={wsEnabled}
                      onChange={(e) => setWsEnabled(e.target.checked)}
                    />
                    <span className="slider"></span>
                  </label>
                </div>
              </div>

              {wsEnabled && (
                <>
                  <div className="settings-row settings-service-status" role="status">
                    <div className="row-label">
                      <div className="row-title">本地服务状态</div>
                      <div className="row-desc">
                        {actualPort ? '本地服务已启动' : '本地服务正在启动'}
                      </div>
                    </div>
                    <div className={`settings-service-pill ${actualPort ? 'is-running' : 'is-starting'}`}>
                      {actualPort ? '已启动' : '启动中'}
                    </div>
                  </div>
                  <div className="settings-row settings-internal-detail" style={{ animation: 'fadeUp 0.3s ease both' }}>
                    <div className="row-label">
                      <div className="row-title">{t('settings.network.preferredPort')}</div>
                      <div className="row-desc">
                        {t('settings.network.preferredPortDesc').replace('{port}', String(defaultPort))}
                      </div>
                    </div>
                    <div className="row-control">
                      <input
                        type="number"
                        className="settings-input"
                        value={wsPort}
                        onChange={(e) => setWsPort(e.target.value)}
                        placeholder={String(defaultPort)}
                        min="1024"
                        max="65535"
                      />
                    </div>
                  </div>

                  {actualPort && (
                    <div className="settings-row settings-internal-detail" style={{ animation: 'fadeUp 0.3s ease both' }}>
                      <div className="row-label">
                        <div className="row-title">{t('settings.network.currentPort')}</div>
                        <div className="row-desc">
                          {actualPort === parseInt(wsPort, 10)
                            ? t('settings.network.portNormal')
                            : t('settings.network.portFallback')
                                .replace('{configured}', wsPort)
                                .replace('{actual}', String(actualPort))}
                        </div>
                      </div>
                      <div className="row-control">
                        <span style={{
                          fontFamily: 'var(--font-mono)',
                          fontSize: '14px',
                          color: actualPort === parseInt(wsPort, 10) ? 'var(--accent)' : 'var(--warning, #f59e0b)'
                        }}>
                          ws://127.0.0.1:{actualPort}
                        </span>
                      </div>
                    </div>
                  )}
                </>
              )}
            </div>

            <div className="group-title">诊断服务</div>
            <div className="settings-group">
              <div className="settings-row">
                <div className="row-label">
                  <div className="row-title">远程诊断</div>
                  <div className="row-desc">按需开启只读诊断查询，关闭后不会提供外部访问</div>
                </div>
                <div className="row-control">
                  <label className="switch">
                    <input
                      type="checkbox"
                      checked={reportEnabled}
                      onChange={(e) => setReportEnabled(e.target.checked)}
                    />
                    <span className="slider"></span>
                  </label>
                </div>
              </div>

              {reportEnabled && (
                <>
                  <div className="settings-row settings-service-status" role="status">
                    <div className="row-label">
                      <div className="row-title">诊断服务状态</div>
                      <div className="row-desc">
                        {reportActualPort ? '诊断服务已启动' : '诊断服务正在启动'}
                      </div>
                    </div>
                    <div className={`settings-service-pill ${reportActualPort ? 'is-running' : 'is-starting'}`}>
                      {reportActualPort ? '已启动' : '启动中'}
                    </div>
                  </div>
                  <div className="settings-row settings-internal-detail" style={{ animation: 'fadeUp 0.3s ease both' }}>
                    <div className="row-label">
                      <div className="row-title">{t('settings.network.reportPort')}</div>
                      <div className="row-desc">
                        {t('settings.network.reportPortDesc').replace('{port}', String(reportDefaultPort))}
                      </div>
                    </div>
                    <div className="row-control">
                      <input
                        type="number"
                        className="settings-input"
                        value={reportPort}
                        onChange={(e) => setReportPort(e.target.value)}
                        placeholder={String(reportDefaultPort)}
                        min="1024"
                        max="65535"
                      />
                    </div>
                  </div>

                  <div className="settings-row" style={{ animation: 'fadeUp 0.3s ease both' }}>
                    <div className="row-label">
                      <div className="row-title">{t('settings.network.reportToken')}</div>
                      <div className="row-desc">{t('settings.network.reportTokenDesc')}</div>
                    </div>
                    <div className="row-control" style={{ minWidth: '260px', display: 'flex', gap: '8px', alignItems: 'center' }}>
                      <input
                        type="text"
                        className="settings-input"
                        value={reportToken}
                        onChange={(e) => setReportToken(e.target.value)}
                        placeholder="change-this-token"
                      />
                      <button
                        className="btn btn-secondary"
                        onClick={() => setReportToken(generateReportToken())}
                        type="button"
                      >
                        {t('settings.network.generateToken')}
                      </button>
                    </div>
                  </div>

                  {reportActualPort && (
                    <div className="settings-row settings-internal-detail" style={{ animation: 'fadeUp 0.3s ease both' }}>
                      <div className="row-label">
                        <div className="row-title">{t('settings.network.currentPort')}</div>
                        <div className="row-desc">
                          {reportActualPort === parseInt(reportPort, 10)
                            ? t('settings.network.portNormal')
                            : t('settings.network.portFallback')
                                .replace('{configured}', reportPort)
                                .replace('{actual}', String(reportActualPort))}
                        </div>
                      </div>
                      <div className="row-control">
                        <span style={{
                          fontFamily: 'var(--font-mono)',
                          fontSize: '14px',
                          color: reportActualPort === parseInt(reportPort, 10) ? 'var(--accent)' : 'var(--warning, #f59e0b)',
                        }}>
                          http://0.0.0.0:{reportActualPort}
                        </span>
                      </div>
                    </div>
                  )}

                  <div className="settings-row settings-internal-detail" style={{ animation: 'fadeUp 0.3s ease both' }}>
                    <div className="row-label">
                      <div className="row-title">{t('settings.network.reportUrlPreview')}</div>
                      <div className="row-desc">
                        {t('settings.network.reportUrlPreviewDesc')}
                      </div>
                    </div>
                    <div className="row-control">
                      <div style={{
                        display: 'flex',
                        flexDirection: 'column',
                        gap: '6px',
                        alignItems: 'flex-start',
                        fontFamily: 'var(--font-mono)',
                        fontSize: '12px',
                        color: 'var(--text-secondary)',
                        wordBreak: 'break-all',
                      }}>
                        <span>{`${t('settings.network.reportUrlRaw')}: ${reportRawPreviewUrl}`}</span>
                        <span>{`${t('settings.network.reportUrlRendered')}: ${reportRenderedPreviewUrl}`}</span>
                      </div>
                    </div>
                  </div>

                  <div className="settings-row settings-internal-detail" style={{ animation: 'fadeUp 0.3s ease both' }}>
                    <div className="row-label">
                      <div className="row-title">{t('settings.network.firewallHintTitle')}</div>
                      <div className="row-desc">{t('settings.network.firewallHint')}</div>
                    </div>
                  </div>
                </>
              )}
            </div>

            <div className="group-title">{t('settings.network.proxyTitle')}</div>
            <div className="settings-group">
              <div className="settings-row">
                <div className="row-label">
                  <div className="row-title">{t('settings.network.proxyEnabled')}</div>
                  <div className="row-desc">{t('settings.network.proxyEnabledDesc')}</div>
                </div>
                <div className="row-control">
                  <label className="switch">
                    <input
                      type="checkbox"
                      checked={globalProxyEnabled}
                      onChange={(e) => setGlobalProxyEnabled(e.target.checked)}
                    />
                    <span className="slider"></span>
                  </label>
                </div>
              </div>

              {globalProxyEnabled && (
                <>
                  <div className="settings-row" style={{ animation: 'fadeUp 0.3s ease both' }}>
                    <div className="row-label">
                      <div className="row-title">{t('settings.network.proxyUrl')}</div>
                      <div className="row-desc">{t('settings.network.proxyUrlDesc')}</div>
                    </div>
                    <div className="row-control">
                      <input
                        type="text"
                        className="settings-input"
                        value={globalProxyUrl}
                        onChange={(e) => setGlobalProxyUrl(e.target.value)}
                        placeholder={t('settings.network.proxyUrlPlaceholder')}
                      />
                    </div>
                  </div>

                  <div className="settings-row" style={{ animation: 'fadeUp 0.3s ease both' }}>
                    <div className="row-label">
                      <div className="row-title">{t('settings.network.proxyNoProxy')}</div>
                      <div className="row-desc">{t('settings.network.proxyNoProxyDesc')}</div>
                    </div>
                    <div className="row-control">
                      <input
                        type="text"
                        className="settings-input"
                        value={globalProxyNoProxy}
                        onChange={(e) => setGlobalProxyNoProxy(e.target.value)}
                        placeholder={t('settings.network.proxyNoProxyPlaceholder')}
                      />
                    </div>
                  </div>
                </>
              )}
            </div>

            {needsRestart && (
              <div style={{
                display: 'flex',
                alignItems: 'center',
                gap: '8px',
                padding: '12px 16px',
                marginTop: '12px',
                background: 'rgba(245, 158, 11, 0.1)',
                borderRadius: '8px',
                color: 'var(--warning, #f59e0b)',
                fontSize: '14px'
              }}>
                <AlertCircle size={18} />
                {t('settings.network.restartRequired')}
              </div>
            )}

            <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '12px' }}>
                <button
                  className="btn btn-primary"
                  onClick={handleSaveNetworkConfig}
                  disabled={networkSaving}
                >
                    <Save size={16} /> {networkSaving ? t('common.saving') : t('settings.saveSettings')}
                </button>
            </div>
          </>
        )}

        {/* === About Tab === */}
        {activeTab === 'about' && (
          <div className="about-container">
            <div className="about-logo-section">
              <button
                type="button"
                className={`app-icon-squircle${showUnlockFireworks ? ' unlock-fireworks-active' : ''}`}
                onClick={handleAboutAvatarTap}
                onMouseDown={(event) => event.preventDefault()}
                aria-label={t('settings.about.appName')}
              >
                <img
                  className="app-icon-squircle__asset app-icon-squircle__asset--dark"
                  src={xiassToolsLogo}
                  alt=""
                />
                <img
                  className="app-icon-squircle__asset app-icon-squircle__asset--light"
                  src={xiassToolsLightLogo}
                  alt=""
                />
              </button>
              <div className="app-info">
                <h2>{t('settings.about.appName')}</h2>
                <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                  <div className="version-tag">{appVersion}</div>
                  <button
                    type="button"
                    className="btn btn-sm btn-ghost"
                    onClick={handleCheckUpdate}
                    disabled={updateChecking}
                    style={{
                      fontSize: '12px',
                      padding: '4px 10px',
                      display: 'flex',
                      alignItems: 'center',
                      gap: '4px'
                    }}
                  >
                    <>
                      <RefreshCw size={14} className={updateChecking ? 'spin' : undefined} />
                      {updateChecking ? t('settings.about.checking') : t('settings.about.checkUpdate')}
                    </>
                  </button>
                  <button
                    type="button"
                    className="btn btn-sm btn-ghost"
                    onClick={handleOpenReleaseHistory}
                    disabled={releaseHistoryLoading}
                    style={{
                      fontSize: '12px',
                      padding: '4px 10px',
                      display: 'flex',
                      alignItems: 'center',
                      gap: '4px',
                    }}
                  >
                    <FileText size={14} />
                    {t('settings.about.viewReleaseHistory', '更新记录')}
                  </button>
                </div>
                {updateCheckMessage && (
                  <div
                    className={`action-message${updateCheckMessage.tone ? ` ${updateCheckMessage.tone}` : ''}`}
                    style={{ marginTop: '10px', marginBottom: 0 }}
                    role={updateCheckMessage.tone === 'error' ? 'alert' : 'status'}
                    aria-live="polite"
                  >
                    <span className="action-message-text">{updateCheckMessage.text}</span>
                  </div>
                )}
              </div>
              <p style={{ color: 'var(--text-secondary)', fontSize: '14px' }}>
                {t('settings.about.slogan')}
              </p>
            </div>

            <div className="credits-list">
              <button type="button" className="credit-item" onClick={() => openLink('https://github.com/xyf0104')}>
                <div className="credit-icon"><User size={24} /></div>
                <h3>{t('settings.about.author', '主作者')}</h3>
                <p>xyf0104</p>
              </button>

              <button type="button" className="credit-item" onClick={() => openLink('https://github.com/xyf0104/Antigravity-WF-Assistant')}>
                <div className="credit-icon credit-icon--repository"><Github size={24} /></div>
                <h3>{t('settings.about.github', '开源仓库')}</h3>
                <p>XIASS Tools</p>
              </button>

              <button
                type="button"
                className="credit-item credit-item--license-notice"
                onClick={() => setLicenseNoticeOpen(true)}
                aria-haspopup="dialog"
                aria-expanded={licenseNoticeOpen}
                ref={licenseNoticeTriggerRef}
              >
                <div className="credit-icon credit-icon--license"><FileText size={24} /></div>
                <h3>{t('settings.about.licensesAndNotices', '开源许可与第三方声明')}</h3>
                <p>{t('settings.about.licensesAndNoticesDesc', '查看归属与许可证')}</p>
              </button>
            </div>
          </div>
        )}
        </div>
      </div>
      {menuBarQuotaModalOpen && (
        <div className="modal-overlay">
          <div
            className="modal settings-menu-bar-quota-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="settings-menu-bar-quota-title"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="modal-header">
              <h2 id="settings-menu-bar-quota-title">
                {t('settings.general.menuBarQuotaModalTitle', '菜单栏额度')}
              </h2>
              <button
                type="button"
                className="modal-close"
                onClick={handleCloseMenuBarQuotaModal}
                aria-label={t('common.close', '关闭')}
              >
                <X size={16} />
              </button>
            </div>
            <div className="modal-body">
              <p className="settings-menu-bar-quota-modal-desc">
                {t(
                  'settings.general.menuBarQuotaModalDesc',
                  '以下为菜单栏额度的专属选项：跟随所选平台当前账号。Codex 当前为 API 服务时显示「API + 池剩余%」；API Key 账号显示「API + 剩余额度」；普通账号显示邮箱前缀与剩余%（多条取最低；低红、中橙、高绿）。'
                )}
              </p>
              <div className="settings-menu-bar-quota-modal-field">
                <label className="settings-menu-bar-quota-modal-label" htmlFor="menu-bar-quota-platform">
                  {t('settings.general.menuBarQuotaPlatform', '额度账号平台')}
                </label>
                <p className="settings-menu-bar-quota-modal-field-desc">
                  {t(
                    'settings.general.menuBarQuotaPlatformDesc',
                    '跟随该平台当前正在使用的账号，刷新或切换后自动更新'
                  )}
                </p>
                <select
                  id="menu-bar-quota-platform"
                  className="settings-select settings-menu-bar-quota-modal-select"
                  value={menuBarQuotaDraftPlatform}
                  onChange={(e) => setMenuBarQuotaDraftPlatform(e.target.value as PlatformId)}
                >
                  {menuBarQuotaPlatformOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="settings-menu-bar-quota-modal-field">
                <label className="settings-menu-bar-quota-modal-label" htmlFor="menu-bar-quota-prefix">
                  {t('settings.general.menuBarAccountPrefix', '显示账号邮箱前 4 位')}
                </label>
                <p className="settings-menu-bar-quota-modal-field-desc">
                  {t(
                    'settings.general.menuBarAccountPrefixDesc',
                    '仅普通账号：关闭后不显示邮箱前缀。Codex API 服务 / API Key 仍会显示 API 标签'
                  )}
                </p>
                <select
                  id="menu-bar-quota-prefix"
                  className="settings-select settings-menu-bar-quota-modal-select"
                  value={menuBarQuotaDraftShowPrefix ? 'true' : 'false'}
                  onChange={(e) => setMenuBarQuotaDraftShowPrefix(e.target.value === 'true')}
                >
                  <option value="true">{t('common.enable', '启用')}</option>
                  <option value="false">{t('common.disable', '停用')}</option>
                </select>
              </div>
            </div>
            <div className="modal-footer">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={handleCloseMenuBarQuotaModal}
              >
                {t('common.cancel', '取消')}
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={handleConfirmMenuBarQuotaModal}
              >
                {menuBarQuotaModalMode === 'enable'
                  ? t('settings.general.menuBarQuotaConfirmEnable', '启用')
                  : t('common.save', '保存')}
              </button>
            </div>
          </div>
        </div>
      )}
      {releaseHistoryOpen && (
        <div className="modal-overlay">
          <div
            className="modal settings-release-history-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="settings-release-history-title"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="modal-header">
              <h2 id="settings-release-history-title">{t('settings.about.releaseHistoryTitle', '更新记录')}</h2>
              <button
                className="modal-close"
                onClick={handleCloseReleaseHistory}
                aria-label={t('common.close', '关闭')}
              >
                <X size={16} />
              </button>
            </div>
            <div className="modal-body settings-release-history-body">
              {releaseHistoryLoading && (
                <div className="settings-release-history-state" role="status">
                  <RefreshCw size={14} className="spin" />
                  <span>{t('settings.about.releaseHistoryLoading', '加载中…')}</span>
                </div>
              )}
              {!releaseHistoryLoading && releaseHistoryError && (
                <div className="settings-release-history-state settings-release-history-state-error" role="alert">
                  {t('settings.about.releaseHistoryLoadFailed', '加载失败：{{error}}', {
                    error: releaseHistoryError,
                  })}
                </div>
              )}
              {!releaseHistoryLoading && !releaseHistoryError && releaseHistoryItems.length === 0 && (
                <div className="settings-release-history-state">
                  {t('settings.about.releaseHistoryEmpty', '暂无更新记录')}
                </div>
              )}
              {!releaseHistoryLoading &&
                !releaseHistoryError &&
                releaseHistoryItems.map((item) => (
                  <article
                    key={`${item.version}-${item.date || 'unknown'}`}
                    className="settings-release-history-item"
                  >
                    <div className="settings-release-history-item-head">
                      <span className="settings-release-history-version">v{item.version}</span>
                      <div className="settings-release-history-item-meta">
                        {item.date ? (
                          <span className="settings-release-history-date">{item.date}</span>
                        ) : null}
                        <button
                          className="settings-release-history-download-btn"
                          onClick={() => {
                            void handleDownloadReleaseVersion(item.version);
                          }}
                          type="button"
                        >
                          <Download size={12} />
                          {t('settings.about.downloadThisVersion', '下载此版本')}
                        </button>
                      </div>
                    </div>
                    <div className="settings-release-history-sections">
                      {releaseHistorySections.map((section) => {
                        const lines = item[section.key];
                        if (!Array.isArray(lines) || lines.length === 0) {
                          return null;
                        }
                        return (
                          <section key={`${item.version}-${section.key}`} className="settings-release-history-section">
                            <h3>{section.label}</h3>
                            <ul>
                              {lines.map((line, index) => (
                                <li key={`${item.version}-${section.key}-${index}`}>
                                  {renderReleaseHistoryLine(line)}
                                </li>
                              ))}
                            </ul>
                          </section>
                        );
                      })}
                    </div>
                  </article>
                ))}
            </div>
            <div className="modal-footer">
              <button className="btn btn-secondary" onClick={handleCloseReleaseHistory}>
                {t('common.close', '关闭')}
              </button>
            </div>
          </div>
        </div>
      )}
      {licenseNoticeOpen && (
        <div className="modal-overlay">
          <div
            className="modal settings-license-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="settings-license-title"
            aria-describedby="settings-license-description"
            aria-busy={legalNoticesLoading}
            onClick={(event) => event.stopPropagation()}
            onKeyDown={handleLicenseDialogKeyDown}
            ref={licenseNoticeDialogRef}
          >
            <div className="modal-header">
              <h2 id="settings-license-title">
                {t('settings.about.licensesAndNotices', '开源许可与第三方声明')}
              </h2>
              <button
                type="button"
                className="modal-close"
                onClick={() => setLicenseNoticeOpen(false)}
                aria-label={t('common.close', '关闭')}
                data-license-dialog-focusable
                ref={licenseNoticeCloseButtonRef}
              >
                <X size={16} />
              </button>
            </div>
            <div className="modal-body settings-license-body">
              <p id="settings-license-description">
                {t(
                  'settings.about.licenseIntro',
                  'XIASS Tools 包含受开源许可证约束的组件。以下内容从当前安装包离线读取，不依赖网络，也不会访问你的本机文件。',
                )}
              </p>
              {legalNoticesLoading && (
                <div className="settings-license-status" role="status" aria-live="polite">
                  <RefreshCw size={16} className="spin" aria-hidden="true" />
                  {t('settings.about.licenseLoading', '正在读取内置许可资料…')}
                </div>
              )}
              {legalNoticesFailed && (
                <div className="settings-license-status settings-license-status-error" role="alert">
                  <span>
                    {t(
                      'settings.about.licenseLoadFailed',
                      '无法读取内置许可资料。请重新安装 XIASS Tools 后重试。',
                    )}
                  </span>
                  <button
                    type="button"
                    className="btn btn-secondary"
                    onClick={retryLegalNoticeLoad}
                    data-license-dialog-focusable
                  >
                    {t('common.retry', '重试')}
                  </button>
                </div>
              )}
              {!legalNoticesLoading && !legalNoticesFailed && selectedLegalNotice && (
                <div className="settings-license-layout">
                  <div
                    className="settings-license-tabs"
                    role="tablist"
                    aria-label={t('settings.about.licenseDocuments', '内置许可资料')}
                  >
                    {legalNotices.map((notice, index) => {
                      const isActive = notice.id === selectedLegalNotice.id;
                      return (
                        <button
                          key={notice.id}
                          type="button"
                          className={`settings-license-tab${isActive ? ' active' : ''}`}
                          role="tab"
                          id={`settings-license-tab-${notice.id}`}
                          aria-selected={isActive}
                          aria-controls={`settings-license-panel-${notice.id}`}
                          tabIndex={isActive ? 0 : -1}
                          onClick={() => setSelectedLegalNoticeId(notice.id)}
                          onKeyDown={(event) => handleLegalNoticeTabKeyDown(event, index)}
                          data-license-dialog-focusable
                        >
                          {notice.title}
                        </button>
                      );
                    })}
                  </div>
                  <section
                    className="settings-license-document"
                    role="tabpanel"
                    id={`settings-license-panel-${selectedLegalNotice.id}`}
                    aria-labelledby={`settings-license-tab-${selectedLegalNotice.id}`}
                    tabIndex={0}
                    data-license-dialog-focusable
                  >
                    <h3>{selectedLegalNotice.title}</h3>
                    <pre>{selectedLegalNotice.content}</pre>
                  </section>
                </div>
              )}
            </div>
            <div className="modal-footer">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setLicenseNoticeOpen(false)}
                data-license-dialog-focusable
              >
                {t('common.close', '关闭')}
              </button>
            </div>
          </div>
        </div>
      )}
      {showUnlockFireworks && !reducedMotionEnabled && (
        <UnlockFireworksOverlay />
      )}
    </main>
  );
}
