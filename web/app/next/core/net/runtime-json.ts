// The runtime's own JSON routes (the platform's and preview's, older than v2): a body on success, the runtime's
// refusal in its own words as a thrown RuntimeError otherwise. A route that does not answer within its term fails
// with words too: nothing waits for ever, so every screen that reads one ends in data or in a retry.
import { pageClock } from '@/next/core/clock';
import { runtimePath } from '@/next/core/net/machine';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

/** The runtime's refusal in its own words, or the failure in ours. */
export class RuntimeError extends Error {}

/** How long a route may take before it is given up. */
export const RUNTIME_TIMEOUT_MS = 20_000;

export const runtimeJson = async <T>(m: MachineId, path: `/${string}`, init?: RequestInit): Promise<T> => {
  const ctl = new AbortController();
  init?.signal?.addEventListener('abort', () => ctl.abort());
  const clock = pageClock();
  const timer = clock.setTimeout(() => ctl.abort(), RUNTIME_TIMEOUT_MS);
  let res: Response;
  try {
    res = await fetch(runtimePath(path, m), { credentials: 'same-origin', ...init, signal: ctl.signal });
  } catch (failure) {
    // The caller's own abort is not a failure to tell; a wait that ran out is the machine not answering.
    if (init?.signal?.aborted) throw failure;
    throw new RuntimeError(ctl.signal.aborted ? t('next.failure.timeout') : t('next.failure.network'));
  } finally {
    clock.clearTimeout(timer);
  }
  const body: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const said = typeof body === 'object' && body !== null && 'error' in body ? String(body.error) : '';
    throw new RuntimeError(said || t('next.failure.http'));
  }
  return body as T;
};

export const postJson = (body: unknown): RequestInit => ({
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});
