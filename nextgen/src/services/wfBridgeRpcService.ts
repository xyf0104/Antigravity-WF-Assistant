import type { WfBridgeSession } from './wfBridgeService';

interface WfBridgeRpcEnvelope<T> {
  ok: boolean;
  result?: T;
  error?: string;
}

export async function callWfBridgeRpc<T>(
  session: WfBridgeSession,
  method: string,
  args: unknown[] = [],
): Promise<T> {
  const rpcUrl = new URL('/rpc', session.url);
  const response = await fetch(rpcUrl, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${session.token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ method, args }),
    cache: 'no-store',
  });
  const body = (await response.json()) as WfBridgeRpcEnvelope<T>;
  if (!response.ok || !body.ok) {
    throw new Error(body.error || `本机组件请求失败（${response.status}）`);
  }
  return body.result as T;
}
