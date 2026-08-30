import { invoke } from '@tauri-apps/api/core';

export interface WfBridgeSession {
  url: string;
  token: string;
  host: string;
  port: number;
  schemaVersion: number;
}

export interface WfBridgeStatus {
  running: boolean;
  url?: string | null;
  lastError?: string | null;
}

export async function getWfBridgeSession(): Promise<WfBridgeSession> {
  return invoke<WfBridgeSession>('wf_bridge_get_session');
}

export async function getWfBridgeStatus(): Promise<WfBridgeStatus> {
  return invoke<WfBridgeStatus>('wf_bridge_get_status');
}

export async function stopWfBridge(): Promise<void> {
  await invoke('wf_bridge_stop');
}
