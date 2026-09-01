import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { CircleAlert, KeyRound, RefreshCw, ShieldCheck } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getWfBridgeSession, type WfBridgeSession } from '../services/wfBridgeService';
import { callWfBridgeRpc } from '../services/wfBridgeRpcService';
import {
  clearLegacyMfaBrowserStorage,
  dedupeMfaRecordsBySecret,
  loadMfaHistoryRecords,
  loadSavedMfaRecords,
} from '../utils/mfaVault';
import './TwoFactorAuthPage.css';

interface TotpStatus {
  ok: boolean;
  message: string;
}

export function TwoFactorAuthPage() {
  const { t } = useTranslation();
  const [session, setSession] = useState<WfBridgeSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [migrationWarning, setMigrationWarning] = useState('');
  const [iframeReady, setIframeReady] = useState(false);
  const [resolvedTheme, setResolvedTheme] = useState<'light' | 'dark'>(() =>
    document.documentElement.dataset.theme === 'light' ? 'light' : 'dark',
  );
  const iframeRef = useRef<HTMLIFrameElement>(null);

  const migrateLegacyBrowserVault = useCallback(async (value: WfBridgeSession) => {
    if (import.meta.env.DEV && value.token.startsWith('xiass-browser-preview-token')) return;
    const legacyRecords = dedupeMfaRecordsBySecret([
      ...loadSavedMfaRecords(),
      ...loadMfaHistoryRecords(),
    ]);
    if (legacyRecords.length === 0) return;

    for (const record of legacyRecords) {
      const status = await callWfBridgeRpc<TotpStatus>(value, 'AddTOTPEntry', [{
        secret: record.secret,
        label: record.accountName || record.remark || '已迁移验证器',
        account: record.accountName || '',
      }]);
      if (!status?.ok) {
        throw new Error(status?.message || '旧验证器迁移失败');
      }
    }
    clearLegacyMfaBrowserStorage();
  }, []);

  const loadVault = useCallback(async () => {
    setLoading(true);
    setError('');
    setMigrationWarning('');
    try {
      const value = await getWfBridgeSession();
      if (value.host !== '127.0.0.1' || !value.url.startsWith('http://127.0.0.1:')) {
        throw new Error('验证器返回了无效的本机地址');
      }
      setSession(value);
      try {
        await migrateLegacyBrowserVault(value);
      } catch (migrationError) {
        setMigrationWarning(`旧验证器尚未迁移：${String(migrationError)}`);
      }
    } catch (reason) {
      setSession(null);
      setError(String(reason));
    } finally {
      setLoading(false);
    }
  }, [migrateLegacyBrowserVault]);

  useEffect(() => {
    void loadVault();
  }, [loadVault]);

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

  const iframeUrl = useMemo(() => {
    if (!session) return '';
    const url = new URL(session.url);
    url.searchParams.set('embedded', '1');
    url.searchParams.set('module', 'antigravity');
    url.searchParams.set('section', 'totp');
    url.searchParams.set('theme', resolvedTheme);
    return url.toString();
  }, [resolvedTheme, session]);

  useEffect(() => {
    setIframeReady(false);
  }, [iframeUrl]);

  useEffect(() => {
    if (!session) return;
    const expectedOrigin = new URL(session.url).origin;
    const handleEmbeddedReady = (event: MessageEvent) => {
      if (
        event.origin !== expectedOrigin
        || event.source !== iframeRef.current?.contentWindow
        || event.data?.type !== 'xiass-wf-ready'
      ) {
        return;
      }
      setIframeReady(true);
    };
    window.addEventListener('message', handleEmbeddedReady);
    return () => window.removeEventListener('message', handleEmbeddedReady);
  }, [session]);

  const deliverToken = useCallback(() => {
    if (!session || !iframeRef.current?.contentWindow) return;
    iframeRef.current.contentWindow.postMessage(
      { type: 'xiass-wf-auth', token: session.token },
      session.url,
    );
  }, [session]);

  return (
    <main className="main-content two-factor-secure-page">
      <header className="two-factor-secure-page__heading">
        <div className="two-factor-secure-page__identity">
          <span className="two-factor-secure-page__icon" aria-hidden="true">
            <KeyRound size={22} />
          </span>
          <div>
            <span className="two-factor-secure-page__kicker">XIASS 本机验证器</span>
            <h1>{t('twoFactorAuth.pageTitle', '2FA')}</h1>
            <p>密钥由 macOS Keychain 或 Windows Credential Manager 保护，不进入浏览器存储、日志、同步或诊断包。</p>
          </div>
        </div>
        <span className="two-factor-secure-page__trust">
          <ShieldCheck size={15} />
          系统凭据库
        </span>
      </header>

      <section className="two-factor-secure-page__canvas" aria-live="polite">
        {migrationWarning ? (
          <div className="two-factor-secure-page__warning" role="alert">
            <CircleAlert size={17} />
            <span>{migrationWarning}</span>
          </div>
        ) : null}
        {loading ? (
          <div className="two-factor-secure-page__state">
            <RefreshCw size={22} className="spin" />
            <strong>正在打开本机验证器</strong>
            <span>首次进入会启动本机安全组件。</span>
          </div>
        ) : error ? (
          <div className="two-factor-secure-page__state is-error" role="alert">
            <CircleAlert size={24} />
            <strong>本机验证器未能启动</strong>
            <span>{error}</span>
            <button type="button" className="btn btn-primary" onClick={() => void loadVault()}>
              <RefreshCw size={15} />
              重新启动
            </button>
          </div>
        ) : session ? (
          <>
            {!iframeReady ? (
              <div className="two-factor-secure-page__state" role="status">
                <RefreshCw size={22} className="spin" />
                <strong>正在打开本机验证器</strong>
                <span>安全组件就绪后会自动显示。</span>
              </div>
            ) : null}
            <iframe
              ref={iframeRef}
              className={`two-factor-secure-page__iframe${iframeReady ? ' is-ready' : ''}`}
              src={iframeUrl}
              name={session.token}
              title="XIASS 本机 2FA 验证器"
              onLoad={deliverToken}
              aria-hidden={!iframeReady}
              allow="clipboard-read; clipboard-write"
            />
          </>
        ) : null}
      </section>
    </main>
  );
}
