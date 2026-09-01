import { createPortal } from 'react-dom';
import { useCallback, useEffect, useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  ArchiveRestore,
  CheckCircle2,
  CircleAlert,
  Clock,
  FileCheck2,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
  ShieldCheck,
  X,
} from 'lucide-react';
import { useEnterConfirm } from '../../hooks/useEnterConfirm';
import { useEscClose } from '../../hooks/useEscClose';
import {
  listCodexConfigBackups,
  restoreCodexConfigBackup,
  verifyCodexConfigBackup,
} from '../../services/codexService';
import type {
  CodexConfigBackupInfo,
  CodexConfigBackupVerification,
  CodexConfigRestoreResult,
} from '../../types/codex';
import './CodexConfigRecoveryModal.css';

interface CodexConfigRecoveryModalProps {
  open: boolean;
  onClose: () => void;
  /** Refreshes surrounding Codex configuration state after a successful restore. */
  onRestored?: () => void | Promise<void>;
}

type Feedback = {
  tone: 'error' | 'success';
  message: string;
};

function formatSnapshotSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(bytes >= 10 * 1024 ? 0 : 1)} KB`;
  }
  if (bytes < 1024 * 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(bytes >= 10 * 1024 * 1024 ? 0 : 1)} MB`;
  }
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatRecoveryTime(timestamp: number): string {
  const value = new Date(timestamp);
  return Number.isNaN(value.getTime()) ? '-' : value.toLocaleString();
}

function getSafeSourceLabel(
  source: string,
  translate: (key: string, fallback: string) => string,
): string {
  if (source === 'nextgen-codex-config-restore') {
    return translate('codex.configRecovery.source.beforeRestore', '恢复前安全备份');
  }
  if (source === 'nextgen-codex-config-format') {
    return translate('codex.configRecovery.source.format', '配置格式整理');
  }
  return translate('codex.configRecovery.source.configurationChange', '配置更新');
}

/**
 * Metadata-only recovery point UI for Codex config.toml.
 *
 * It intentionally never requests or renders config content, local paths, or checksums.
 * A recovery point must pass a newly requested backend verification before its restore action
 * becomes available, then requires a separate explicit confirmation.
 */
export function CodexConfigRecoveryModal({
  open,
  onClose,
  onRestored,
}: CodexConfigRecoveryModalProps) {
  const { t } = useTranslation();
  const titleId = useId();
  const descriptionId = useId();
  const [backups, setBackups] = useState<CodexConfigBackupInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [verifyingBackupId, setVerifyingBackupId] = useState<string | null>(null);
  const [verifiedBackupId, setVerifiedBackupId] = useState<string | null>(null);
  const [verification, setVerification] = useState<CodexConfigBackupVerification | null>(null);
  const [confirmBackupId, setConfirmBackupId] = useState<string | null>(null);
  const [restoringBackupId, setRestoringBackupId] = useState<string | null>(null);
  const [restoreResult, setRestoreResult] = useState<CodexConfigRestoreResult | null>(null);
  const [feedback, setFeedback] = useState<Feedback | null>(null);

  const actionBusy = verifyingBackupId !== null || restoringBackupId !== null;
  const interactionLocked = actionBusy || loading;
  const confirmationBackup = useMemo(
    () => backups.find((backup) => backup.id === confirmBackupId) ?? null,
    [backups, confirmBackupId],
  );

  const loadBackups = useCallback(async () => {
    if (actionBusy) return;
    setLoading(true);
    setFeedback(null);
    setVerification(null);
    setVerifiedBackupId(null);
    setConfirmBackupId(null);
    setRestoreResult(null);
    try {
      const nextBackups = await listCodexConfigBackups();
      setBackups(nextBackups);
    } catch {
      setFeedback({
        tone: 'error',
        message: t('codex.configRecovery.loadFailed', '暂时无法读取恢复点，请稍后重试。'),
      });
    } finally {
      setLoading(false);
    }
  }, [actionBusy, t]);

  useEffect(() => {
    if (!open) return;
    void loadBackups();
  }, [loadBackups, open]);

  const closeConfirmation = useCallback(() => {
    if (interactionLocked) return;
    setConfirmBackupId(null);
  }, [interactionLocked]);

  const requestClose = useCallback(() => {
    if (interactionLocked) return;
    if (confirmBackupId) {
      setConfirmBackupId(null);
      return;
    }
    onClose();
  }, [confirmBackupId, interactionLocked, onClose]);

  useEscClose(open && !interactionLocked, requestClose);

  const handleVerify = useCallback(
    async (backupId: string) => {
      if (interactionLocked) return;
      setVerifyingBackupId(backupId);
      setVerifiedBackupId(null);
      setVerification(null);
      setConfirmBackupId(null);
      setRestoreResult(null);
      setFeedback(null);
      try {
        const nextVerification = await verifyCodexConfigBackup(backupId);
        const verified = nextVerification.valid && nextVerification.id === backupId;
        setVerification(nextVerification);
        setVerifiedBackupId(verified ? backupId : null);
        setFeedback({
          tone: verified ? 'success' : 'error',
          message: verified
            ? t(
                'codex.configRecovery.verificationPassed',
                '恢复点已重新校验，可以继续恢复。',
              )
            : t(
                'codex.configRecovery.verificationFailed',
                '恢复点未通过校验，不能用于恢复。',
              ),
        });
      } catch {
        setFeedback({
          tone: 'error',
          message: t(
            'codex.configRecovery.verificationUnavailable',
            '恢复点校验暂时不可用，请稍后重试。',
          ),
        });
      } finally {
        setVerifyingBackupId(null);
      }
    },
    [interactionLocked, t],
  );

  const handleRestore = useCallback(async () => {
    if (
      !confirmBackupId ||
      confirmBackupId !== verifiedBackupId ||
      actionBusy ||
      !confirmationBackup
    ) {
      return;
    }

    setRestoringBackupId(confirmBackupId);
    setFeedback(null);
    try {
      const result = await restoreCodexConfigBackup(confirmBackupId);
      if (!result.restored || result.restoredBackupId !== confirmBackupId || !result.safetyBackupId) {
        throw new Error('restore_not_confirmed');
      }

      setRestoreResult(result);
      setConfirmBackupId(null);
      setVerifiedBackupId(null);
      setVerification(null);
      setFeedback({
        tone: 'success',
        message: t(
          'codex.configRecovery.restoreSucceeded',
          '恢复完成。重启 Codex 后，恢复的配置才会生效。',
        ),
      });

      try {
        await onRestored?.();
      } catch {
        // The restore already completed atomically; keep the success outcome visible.
      }

      try {
        setBackups(await listCodexConfigBackups());
      } catch {
        // The recovery result remains valid even if listing the refreshed metadata fails.
      }
    } catch {
      setFeedback({
        tone: 'error',
        message: t(
          'codex.configRecovery.restoreFailed',
          '恢复没有完成，当前配置未被此操作替换。',
        ),
      });
    } finally {
      setRestoringBackupId(null);
    }
  }, [actionBusy, confirmBackupId, confirmationBackup, onRestored, t, verifiedBackupId]);

  useEnterConfirm(Boolean(confirmBackupId), handleRestore, {
    enabled: !actionBusy,
  });

  if (!open || typeof document === 'undefined') return null;

  return createPortal(
    <div className="codex-config-recovery-modal__overlay">
      <div
        className="codex-config-recovery-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
      >
        <header className="codex-config-recovery-modal__header">
          <div className="codex-config-recovery-modal__heading">
            <span className="codex-config-recovery-modal__icon" aria-hidden="true">
              <ArchiveRestore size={20} />
            </span>
            <div>
              <h2 id={titleId}>{t('codex.configRecovery.title', '配置恢复点')}</h2>
              <p id={descriptionId}>
                {t(
                  'codex.configRecovery.description',
                  '恢复前会重新校验所选恢复点，并自动创建当前配置的安全恢复点。',
                )}
              </p>
            </div>
          </div>
          <button
            type="button"
            className="codex-config-recovery-modal__close"
            onClick={requestClose}
            disabled={interactionLocked}
            aria-label={t('common.close', '关闭')}
            title={t('common.close', '关闭')}
          >
            <X size={17} />
          </button>
        </header>

        <div className="codex-config-recovery-modal__body">
          <div className="codex-config-recovery-modal__safety-note">
            <ShieldCheck size={17} aria-hidden="true" />
            <span>
              {t(
                'codex.configRecovery.safetyNote',
                '此处只显示恢复点信息；配置内容不会在界面中展示。',
              )}
            </span>
          </div>

          {feedback && (
            <div
              className={`codex-config-recovery-modal__feedback codex-config-recovery-modal__feedback--${feedback.tone}`}
              role={feedback.tone === 'error' ? 'alert' : 'status'}
              aria-live={feedback.tone === 'error' ? 'assertive' : 'polite'}
            >
              {feedback.tone === 'error' ? <CircleAlert size={16} /> : <CheckCircle2 size={16} />}
              <span>{feedback.message}</span>
            </div>
          )}

          {restoreResult && (
            <section className="codex-config-recovery-modal__outcome" aria-live="polite">
              <div className="codex-config-recovery-modal__outcome-title">
                <CheckCircle2 size={17} aria-hidden="true" />
                <span>{t('codex.configRecovery.outcomeTitle', '本次恢复已完成')}</span>
              </div>
              <p>
                {t(
                  'codex.configRecovery.outcomeDescription',
                  '已恢复所选配置，并已保留操作前的安全恢复点。请重启 Codex，使恢复后的配置生效。',
                )}
              </p>
            </section>
          )}

          {confirmationBackup && (
            <section
              className="codex-config-recovery-modal__confirmation"
              role="alert"
              aria-labelledby={`${titleId}-confirm`}
            >
              <div className="codex-config-recovery-modal__confirmation-icon" aria-hidden="true">
                <RotateCcw size={18} />
              </div>
              <div className="codex-config-recovery-modal__confirmation-copy">
                <h3 id={`${titleId}-confirm`}>
                  {t('codex.configRecovery.confirmTitle', '确认恢复此配置？')}
                </h3>
                <p>
                  {t(
                    'codex.configRecovery.confirmDescription',
                    '当前配置会先保存为新的安全恢复点，然后替换为这个已校验恢复点。',
                  )}
                </p>
                <span>
                  {formatRecoveryTime(confirmationBackup.createdAt)} ·{' '}
                  {getSafeSourceLabel(confirmationBackup.source, t)}
                </span>
              </div>
              <div className="codex-config-recovery-modal__confirmation-actions">
                <button
                  type="button"
                  className="btn btn-secondary"
                  onClick={closeConfirmation}
                  disabled={interactionLocked}
                >
                  {t('common.cancel', '取消')}
                </button>
                <button
                  type="button"
                  className="btn btn-primary"
                  onClick={() => void handleRestore()}
                  disabled={actionBusy}
                  autoFocus
                >
                  {restoringBackupId === confirmationBackup.id ? (
                    <LoaderCircle className="codex-config-recovery-modal__spinner" size={15} />
                  ) : (
                    <RotateCcw size={15} />
                  )}
                  {restoringBackupId === confirmationBackup.id
                    ? t('codex.configRecovery.restoring', '恢复中…')
                    : t('codex.configRecovery.confirmRestore', '确认恢复')}
                </button>
              </div>
            </section>
          )}

          {loading ? (
            <div className="codex-config-recovery-modal__loading" role="status">
              <LoaderCircle className="codex-config-recovery-modal__spinner" size={18} />
              <span>{t('codex.configRecovery.loading', '正在读取恢复点…')}</span>
            </div>
          ) : backups.length === 0 ? (
            <div className="codex-config-recovery-modal__empty">
              <FileCheck2 size={22} aria-hidden="true" />
              <strong>{t('codex.configRecovery.emptyTitle', '暂无可用恢复点')}</strong>
              <span>
                {t(
                  'codex.configRecovery.emptyDescription',
                  '后续由 XIASS Tools 保存的 Codex 配置变更会自动创建恢复点。',
                )}
              </span>
            </div>
          ) : (
            <div className="codex-config-recovery-modal__list" aria-label={t('codex.configRecovery.listLabel', '配置恢复点列表')}>
              {backups.map((backup) => {
                const isVerifying = verifyingBackupId === backup.id;
                const isFreshlyVerified = verifiedBackupId === backup.id && verification?.valid === true;
                const canRequestRestore = isFreshlyVerified && !confirmBackupId && !interactionLocked;

                return (
                  <article className="codex-config-recovery-modal__item" key={backup.id}>
                    <div className="codex-config-recovery-modal__item-main">
                      <div className="codex-config-recovery-modal__item-title">
                        <Clock size={16} aria-hidden="true" />
                        <strong>{formatRecoveryTime(backup.createdAt)}</strong>
                      </div>
                      <div className="codex-config-recovery-modal__item-meta">
                        <span>{getSafeSourceLabel(backup.source, t)}</span>
                        <span>{formatSnapshotSize(backup.bytes)}</span>
                        <span>
                          {backup.originalExisted
                            ? t('codex.configRecovery.stateExisted', '当时已有配置')
                            : t('codex.configRecovery.stateAbsent', '当时没有配置')}
                        </span>
                      </div>
                    </div>
                    <div className="codex-config-recovery-modal__item-actions">
                      <span
                        className={`codex-config-recovery-modal__status${isFreshlyVerified ? ' is-verified' : ''}`}
                      >
                        {isFreshlyVerified
                          ? t('codex.configRecovery.freshlyVerified', '已重新校验')
                          : backup.valid
                            ? t('codex.configRecovery.stored', '已存档')
                            : t('codex.configRecovery.needsVerification', '需校验')}
                      </span>
                      <button
                        type="button"
                        className="btn btn-secondary"
                        onClick={() => void handleVerify(backup.id)}
                        disabled={interactionLocked || Boolean(confirmBackupId)}
                      >
                        {isVerifying ? (
                          <LoaderCircle className="codex-config-recovery-modal__spinner" size={14} />
                        ) : (
                          <ShieldCheck size={14} />
                        )}
                        {isVerifying
                          ? t('codex.configRecovery.verifying', '校验中…')
                          : t('codex.configRecovery.verify', '重新校验')}
                      </button>
                      <button
                        type="button"
                        className="btn btn-primary"
                        onClick={() => setConfirmBackupId(backup.id)}
                        disabled={!canRequestRestore}
                      >
                        <RotateCcw size={14} />
                        {t('codex.configRecovery.restore', '恢复')}
                      </button>
                    </div>
                  </article>
                );
              })}
            </div>
          )}
        </div>

        <footer className="codex-config-recovery-modal__footer">
          <span>
            {t(
              'codex.configRecovery.footerHint',
              '恢复不会自动关闭此窗口，便于确认结果。',
            )}
          </span>
          <div>
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => void loadBackups()}
              disabled={interactionLocked || Boolean(confirmBackupId)}
            >
              <RefreshCw size={14} />
              {t('common.refresh', '刷新')}
            </button>
            <button
              type="button"
              className="btn btn-primary"
              onClick={requestClose}
              disabled={interactionLocked}
            >
              {t('common.done', '完成')}
            </button>
          </div>
        </footer>
      </div>
    </div>,
    document.body,
  );
}
