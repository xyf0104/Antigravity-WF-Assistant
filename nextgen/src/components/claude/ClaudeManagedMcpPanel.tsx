import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AlertTriangle,
  ArrowLeft,
  CheckCircle2,
  CircleAlert,
  LoaderCircle,
  PlugZap,
  RefreshCw,
  ShieldCheck,
  Trash2,
  X,
} from 'lucide-react';
import {
  CLAUDE_MANAGED_MCP_SERVER_NAME,
  configureClaudeManagedHttpMcp,
  getClaudeManagedMcpPublicFailureMessage,
  getClaudeManagedMcpStateLabel,
  getClaudeManagedMcpStatus,
  removeClaudeManagedMcp,
  validateClaudeManagedMcpHttpUrl,
  type ClaudeManagedMcpStatus,
} from '../../services/claudeMcpService';
import './ClaudeManagedMcpPanel.css';

type Feedback = {
  tone: 'success' | 'error';
  message: string;
};

type PendingAction = 'configure' | 'remove' | null;

interface ClaudeManagedMcpPanelProps {
  onBack?: () => void;
}

function statusTone(status: ClaudeManagedMcpStatus | null): 'success' | 'warning' | 'muted' {
  if (status?.state === 'configured') return 'success';
  if (status?.state === 'not_configured') return 'warning';
  return 'muted';
}

function statusIcon(status: ClaudeManagedMcpStatus | null) {
  if (status?.state === 'configured') return <CheckCircle2 size={18} aria-hidden="true" />;
  if (status?.state === 'not_configured') return <CircleAlert size={18} aria-hidden="true" />;
  return <AlertTriangle size={18} aria-hidden="true" />;
}

/**
 * A constrained, status-only management surface for XIASS' fixed Claude Code
 * MCP record. It intentionally cannot enumerate or expose the user's other
 * Claude MCP entries, CLI output, command paths, or stored remote endpoint.
 */
export function ClaudeManagedMcpPanel({ onBack }: ClaudeManagedMcpPanelProps) {
  const [status, setStatus] = useState<ClaudeManagedMcpStatus | null>(null);
  const [statusLoading, setStatusLoading] = useState(true);
  const [endpoint, setEndpoint] = useState('');
  const [pendingAction, setPendingAction] = useState<PendingAction>(null);
  const [confirmingRemoval, setConfirmingRemoval] = useState(false);
  const [feedback, setFeedback] = useState<Feedback | null>(null);
  const mountedRef = useRef(false);
  const statusRequestRef = useRef(0);

  const refreshStatus = useCallback(async () => {
    const requestId = ++statusRequestRef.current;
    if (mountedRef.current) {
      setStatusLoading(true);
    }
    try {
      const nextStatus = await getClaudeManagedMcpStatus();
      if (mountedRef.current && requestId === statusRequestRef.current) {
        setStatus(nextStatus);
      }
      return nextStatus;
    } catch {
      if (mountedRef.current && requestId === statusRequestRef.current) {
        setStatus(null);
        setFeedback({
          tone: 'error',
          message: getClaudeManagedMcpPublicFailureMessage('status'),
        });
      }
      return null;
    } finally {
      if (mountedRef.current && requestId === statusRequestRef.current) {
        setStatusLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    void refreshStatus();
    return () => {
      mountedRef.current = false;
    };
  }, [refreshStatus]);

  const endpointValidation = validateClaudeManagedMcpHttpUrl(endpoint);
  const interactionLocked = pendingAction !== null;
  const cliUnavailable = status?.state === 'cli_unavailable';
  const canConfigure =
    !interactionLocked &&
    !statusLoading &&
    status?.cliAvailable === true &&
    !cliUnavailable &&
    endpointValidation.valid;
  const canRequestRemoval =
    !interactionLocked &&
    !statusLoading &&
    status?.managedServerConfigured === true;

  const handleConfigure = useCallback(async () => {
    if (!endpointValidation.valid || !canConfigure) {
      if (!endpointValidation.valid && endpoint.trim()) {
        setFeedback({ tone: 'error', message: endpointValidation.message });
      }
      return;
    }

    setPendingAction('configure');
    setConfirmingRemoval(false);
    setFeedback(null);
    try {
      const result = await configureClaudeManagedHttpMcp(endpointValidation.normalizedUrl);
      if (!result.ok || result.state !== 'configured') {
        throw new Error('managed_mcp_configuration_unconfirmed');
      }
      // The CLI owns its configuration. Clear the renderer-only copy immediately
      // so an MCP endpoint never becomes a retained UI value or diagnostic input.
      setEndpoint('');
      setFeedback({
        tone: 'success',
        message: '受管 Claude Code MCP 已保存。需要认证时，请在 Claude Code 中执行 /mcp。',
      });
    } catch {
      setFeedback({
        tone: 'error',
        message: getClaudeManagedMcpPublicFailureMessage('configure'),
      });
    } finally {
      setPendingAction(null);
      void refreshStatus();
    }
  }, [canConfigure, endpoint, endpointValidation, refreshStatus]);

  const handleRemove = useCallback(async () => {
    if (!canRequestRemoval) return;
    setPendingAction('remove');
    setFeedback(null);
    try {
      const result = await removeClaudeManagedMcp();
      if (!result.ok || result.state !== 'not_configured') {
        throw new Error('managed_mcp_removal_unconfirmed');
      }
      setConfirmingRemoval(false);
      setFeedback({ tone: 'success', message: 'XIASS 受管 Claude Code MCP 已移除。' });
    } catch {
      setFeedback({
        tone: 'error',
        message: getClaudeManagedMcpPublicFailureMessage('remove'),
      });
    } finally {
      setPendingAction(null);
      void refreshStatus();
    }
  }, [canRequestRemoval, refreshStatus]);

  const configured = status?.managedServerConfigured === true;
  const statusText = getClaudeManagedMcpStateLabel(status);

  return (
    <section className="claude-managed-mcp-panel" aria-labelledby="claude-managed-mcp-title">
      <header className="claude-managed-mcp-panel__header">
        <div className="claude-managed-mcp-panel__heading">
          <span className="claude-managed-mcp-panel__icon" aria-hidden="true">
            <PlugZap size={20} />
          </span>
          <div>
            <div className="claude-managed-mcp-panel__eyebrow">Claude Code</div>
            <h2 id="claude-managed-mcp-title">受管 MCP</h2>
            <p>管理 XIASS Tools 创建的单个用户级 HTTP MCP 条目。</p>
          </div>
        </div>
        <div className="claude-managed-mcp-panel__header-actions">
          {onBack ? (
            <button
              type="button"
              className="btn btn-secondary icon-only"
              onClick={onBack}
              disabled={interactionLocked}
              aria-label="返回 Claude Code 账号"
              title="返回 Claude Code 账号"
            >
              <ArrowLeft size={15} />
            </button>
          ) : null}
          <button
            type="button"
            className="btn btn-secondary icon-only claude-managed-mcp-panel__refresh"
            onClick={() => void refreshStatus()}
            disabled={interactionLocked || statusLoading}
            aria-label="检查 Claude Code MCP 状态"
            title="检查 Claude Code MCP 状态"
          >
            <RefreshCw size={15} className={statusLoading ? 'loading-spinner' : undefined} />
          </button>
        </div>
      </header>

      <div
        className={`claude-managed-mcp-panel__state claude-managed-mcp-panel__state--${statusTone(status)}`}
        role="status"
        aria-live="polite"
      >
        {statusLoading ? <LoaderCircle size={18} className="loading-spinner" aria-hidden="true" /> : statusIcon(status)}
        <div>
          <strong>{statusLoading ? '正在检查本机 Claude Code。' : statusText}</strong>
          <span>
            {status?.cliAvailable
              ? `固定条目：${CLAUDE_MANAGED_MCP_SERVER_NAME}`
              : status?.state === 'cli_unavailable'
                ? '请先安装或更新 Claude Code CLI，再返回此处检查。'
                : '状态检查没有返回本机 CLI 详情，请稍后重试。'}
          </span>
        </div>
      </div>

      <div className="claude-managed-mcp-panel__facts" aria-label="MCP 管理范围">
        <div>
          <span>管理范围</span>
          <strong>仅限 XIASS 受管条目</strong>
        </div>
        <div>
          <span>检查范围</span>
          <strong>CLI 与条目状态</strong>
        </div>
        <div>
          <span>远端认证</span>
          <strong>在 Claude Code /mcp 完成</strong>
        </div>
      </div>

      <div className="claude-managed-mcp-panel__form">
        <label htmlFor="claude-managed-mcp-endpoint">HTTP MCP 地址</label>
        <div className="claude-managed-mcp-panel__input-row">
          <input
            id="claude-managed-mcp-endpoint"
            type="url"
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
            placeholder="https://mcp.example.com/mcp"
            value={endpoint}
            onChange={(event) => {
              setEndpoint(event.target.value);
              if (feedback?.tone === 'error') setFeedback(null);
            }}
            disabled={interactionLocked || cliUnavailable}
            aria-describedby="claude-managed-mcp-endpoint-help"
          />
          {endpoint ? (
            <button
              type="button"
              className="claude-managed-mcp-panel__clear"
              onClick={() => setEndpoint('')}
              disabled={interactionLocked}
              aria-label="清空 MCP 地址"
              title="清空 MCP 地址"
            >
              <X size={15} />
            </button>
          ) : null}
        </div>
        <p id="claude-managed-mcp-endpoint-help" className="claude-managed-mcp-panel__field-help">
          仅支持不含用户名、令牌、查询参数或片段的 HTTP(S) 地址。地址不会显示在状态、日志或诊断导出中。
        </p>
        {endpoint.trim() && !endpointValidation.valid ? (
          <p className="claude-managed-mcp-panel__validation" role="alert">
            {endpointValidation.message}
          </p>
        ) : null}
      </div>

      {feedback ? (
        <div
          className={`claude-managed-mcp-panel__feedback claude-managed-mcp-panel__feedback--${feedback.tone}`}
          role={feedback.tone === 'error' ? 'alert' : 'status'}
          aria-live="polite"
        >
          {feedback.tone === 'success' ? <CheckCircle2 size={16} aria-hidden="true" /> : <CircleAlert size={16} aria-hidden="true" />}
          <span>{feedback.message}</span>
        </div>
      ) : null}

      {confirmingRemoval ? (
        <div className="claude-managed-mcp-panel__remove-confirmation" role="alert">
          <ShieldCheck size={17} aria-hidden="true" />
          <div>
            <strong>仅移除 XIASS 受管 MCP</strong>
            <p>不会读取、列出或修改 Claude Code 中的其他 MCP 条目。</p>
            <div className="claude-managed-mcp-panel__confirmation-actions">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setConfirmingRemoval(false)}
                disabled={interactionLocked}
              >
                取消
              </button>
              <button
                type="button"
                className="btn btn-danger"
                onClick={() => void handleRemove()}
                disabled={!canRequestRemoval}
              >
                {pendingAction === 'remove' ? <LoaderCircle size={15} className="loading-spinner" /> : <Trash2 size={15} />}
                确认移除
              </button>
            </div>
          </div>
        </div>
      ) : null}

      <footer className="claude-managed-mcp-panel__actions">
        <button
          type="button"
          className="btn btn-primary"
          onClick={() => void handleConfigure()}
          disabled={!canConfigure}
        >
          {pendingAction === 'configure' ? <LoaderCircle size={15} className="loading-spinner" /> : <PlugZap size={15} />}
          {configured ? '更新受管 MCP' : '配置受管 MCP'}
        </button>
        {configured && !confirmingRemoval ? (
          <button
            type="button"
            className="btn btn-secondary claude-managed-mcp-panel__remove"
            onClick={() => {
              setFeedback(null);
              setConfirmingRemoval(true);
            }}
            disabled={!canRequestRemoval}
          >
            <Trash2 size={15} />
            移除受管 MCP
          </button>
        ) : null}
      </footer>
    </section>
  );
}
