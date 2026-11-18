import { machineApi } from '@/next/core/net/machine';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import type { NotebookOutput } from './notebook-model';

export interface NotebookRuntime {
  machine: MachineId;
  conv: ConvId;
}
export interface KernelState {
  state: 'stopped' | 'idle' | 'busy';
  kernel?: string;
}
export interface ExecutionResult {
  outputs: NotebookOutput[];
  execution_count: number | null;
  status: 'ok' | 'error';
}
export type NotebookAction = 'execute' | 'interrupt' | 'restart' | 'shutdown';

export const notebookRequest = async <T>(
  runtime: NotebookRuntime,
  path: string,
  action?: NotebookAction,
  code?: string,
): Promise<T> => {
  // The machine proxy's ordinary header budget is shorter than a cell run. Use its existing long-request hint
  // for the kernel's 30-second startup plus 120-second execution budget; runtime limits remain authoritative.
  const wait = action === 'execute' ? '&timeout=150' : '';
  const url = machineApi(runtime.machine).http(
    `/conversations/${encodeURIComponent(runtime.conv)}/notebook?path=${encodeURIComponent(path)}${wait}`,
  );
  const res = await fetch(url, {
    credentials: 'same-origin',
    ...(action
      ? {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action, ...(code !== undefined ? { code } : {}) }),
        }
      : {}),
  });
  const doc: unknown = await res.json().catch(() => null);
  if (!res.ok || doc === null) {
    const body = typeof doc === 'object' && doc !== null ? (doc as Record<string, unknown>) : {};
    const message =
      typeof body.error === 'string'
        ? body.error
        : typeof body.message === 'string'
          ? body.message
          : `HTTP ${res.status}`;
    throw new Error(message);
  }
  return doc as T;
};
