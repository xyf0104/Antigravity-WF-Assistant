import { invoke } from '@tauri-apps/api/core';

export const CLAUDE_MANAGED_MCP_SERVER_NAME = 'xiass-tools';

const MAX_REMOTE_URL_LENGTH = 2_048;

export type ClaudeManagedMcpState =
  | 'configured'
  | 'not_configured'
  | 'cli_unavailable'
  | 'unable_to_verify';

export interface ClaudeManagedMcpStatus {
  cliAvailable: boolean;
  managedServerConfigured: boolean;
  state: ClaudeManagedMcpState;
}

export interface ClaudeManagedMcpMutationResult {
  ok: boolean;
  state: ClaudeManagedMcpState;
}

export type ClaudeManagedMcpUrlValidation =
  | { valid: true; normalizedUrl: string }
  | { valid: false; message: string };

const KNOWN_STATES = new Set<ClaudeManagedMcpState>([
  'configured',
  'not_configured',
  'cli_unavailable',
  'unable_to_verify',
]);

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function normalizeState(value: unknown): ClaudeManagedMcpState {
  return typeof value === 'string' && KNOWN_STATES.has(value as ClaudeManagedMcpState)
    ? (value as ClaudeManagedMcpState)
    : 'unable_to_verify';
}

/**
 * The native command deliberately returns only a redacted status projection.
 * Do not pass its free-form message field through the renderer: an unexpected
 * CLI implementation must never turn command output into visible UI content.
 */
export function normalizeClaudeManagedMcpStatus(value: unknown): ClaudeManagedMcpStatus {
  const record = asRecord(value);
  const state = normalizeState(record?.state);
  const nativeCliAvailable = record?.cliAvailable === true || record?.cli_available === true;

  return {
    cliAvailable:
      state === 'configured' || state === 'not_configured'
        ? true
        : state === 'cli_unavailable'
          ? false
          : nativeCliAvailable,
    managedServerConfigured: state === 'configured',
    state,
  };
}

function normalizeClaudeManagedMcpMutation(value: unknown): ClaudeManagedMcpMutationResult {
  const record = asRecord(value);
  const state = normalizeState(record?.state);
  const ok = record?.ok === true && (state === 'configured' || state === 'not_configured');
  return { ok, state };
}

/**
 * Mirrors the native URL boundary before an invoke is attempted. The native
 * module remains authoritative, but early validation prevents an accidental
 * token-bearing URL from crossing the renderer/native boundary at all.
 */
export function validateClaudeManagedMcpHttpUrl(rawValue: string): ClaudeManagedMcpUrlValidation {
  const value = rawValue.trim();
  if (!value || value.length > MAX_REMOTE_URL_LENGTH) {
    return {
      valid: false,
      message: '请输入有效的 HTTP(S) MCP 地址。',
    };
  }

  try {
    const parsed = new URL(value);
    if (
      !['http:', 'https:'].includes(parsed.protocol) ||
      !parsed.hostname ||
      parsed.username ||
      parsed.password ||
      parsed.search ||
      parsed.hash
    ) {
      return {
        valid: false,
        message: 'MCP 地址仅支持不含凭据、查询参数或片段的 HTTP(S) 地址。',
      };
    }
    return { valid: true, normalizedUrl: parsed.toString() };
  } catch {
    return {
      valid: false,
      message: '请输入有效的 HTTP(S) MCP 地址。',
    };
  }
}

export function getClaudeManagedMcpStateLabel(status: ClaudeManagedMcpStatus | null): string {
  if (!status) return '正在检查 Claude Code MCP 状态。';
  switch (status.state) {
    case 'configured':
      return 'XIASS 受管 MCP 已配置。';
    case 'not_configured':
      return '尚未配置 XIASS 受管 MCP。';
    case 'cli_unavailable':
      return '未检测到 Claude Code CLI。';
    default:
      return '暂时无法确认受管 MCP 状态。';
  }
}

export function getClaudeManagedMcpPublicFailureMessage(
  operation: 'status' | 'configure' | 'remove',
): string {
  switch (operation) {
    case 'configure':
      return '无法配置受管 Claude Code MCP。';
    case 'remove':
      return '无法移除受管 Claude Code MCP。';
    default:
      return '无法检查受管 Claude Code MCP。';
  }
}

export async function getClaudeManagedMcpStatus(): Promise<ClaudeManagedMcpStatus> {
  const response = await invoke<unknown>('claude_mcp_get_managed_status');
  return normalizeClaudeManagedMcpStatus(response);
}

export async function configureClaudeManagedHttpMcp(
  rawRemoteUrl: string,
): Promise<ClaudeManagedMcpMutationResult> {
  const validation = validateClaudeManagedMcpHttpUrl(rawRemoteUrl);
  if (!validation.valid) {
    throw new Error(validation.message);
  }
  const response = await invoke<unknown>('claude_mcp_configure_managed_http', {
    remoteUrl: validation.normalizedUrl,
  });
  return normalizeClaudeManagedMcpMutation(response);
}

export async function removeClaudeManagedMcp(): Promise<ClaudeManagedMcpMutationResult> {
  const response = await invoke<unknown>('claude_mcp_remove_managed');
  return normalizeClaudeManagedMcpMutation(response);
}
