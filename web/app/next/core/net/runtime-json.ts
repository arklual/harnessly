// The runtime's own JSON routes (the platform's and preview's, older than v2): a body on success, the runtime's
// refusal in its own words as a thrown RuntimeError otherwise. A route that does not answer within its term fails
// with words too: nothing waits for ever, so every screen that reads one ends in data or in a retry.
import { pageClock } from '@/next/core/clock';
import { gatewayPath, runtimePath } from '@/next/core/net/machine';
import { held } from '@/next/core/net/http';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

/** The runtime's refusal in its own words, or the failure in ours. */
export class RuntimeError extends Error {
  constructor(
    message: string,
    /** What the runtime answered with, when it answered an error. */
    readonly body?: unknown,
  ) {
    super(message);
  }
}

/** How long a route may take before it is given up. */
export const RUNTIME_TIMEOUT_MS = 20_000;

export const runtimeJson = <T>(
  m: MachineId,
  path: `/${string}`,
  init?: RequestInit,
  timeoutMs = RUNTIME_TIMEOUT_MS,
): Promise<T> => fetchJson<T>(runtimePath(path, m), init, timeoutMs);

/** A route the hub answers itself (design store, access: under the platform's prefix, no machine named; HRN-1464). */
export const hubJson = <T>(
  path: `/${string}`,
  init?: RequestInit,
  timeoutMs = RUNTIME_TIMEOUT_MS,
): Promise<T> => fetchJson<T>(gatewayPath(path), init, timeoutMs);

const fetchJson = async <T>(url: string, init?: RequestInit, timeoutMs = RUNTIME_TIMEOUT_MS): Promise<T> => {
  const ctl = new AbortController();
  init?.signal?.addEventListener('abort', () => ctl.abort());
  const clock = pageClock();
  const timer = clock.setTimeout(() => ctl.abort(), timeoutMs);
  let thrown: unknown = null;
  // Across a rollout's gap the request is sent again (core/net/http «held»); the caller's abort and the term end it.
  const got = await held(
    async () => {
      try {
        thrown = null;
        return await fetch(url, { credentials: 'same-origin', ...init, signal: ctl.signal });
      } catch (failure) {
        thrown = failure;
        return ctl.signal.aborted ? 'timeout' : 'network';
      }
    },
    init?.method ?? 'GET',
    timeoutMs,
  ).finally(() => clock.clearTimeout(timer));
  if (typeof got === 'string') {
    // The caller's own abort is not a failure to tell; a wait that ran out is the machine not answering.
    if (init?.signal?.aborted) throw thrown;
    throw new RuntimeError(got === 'timeout' ? t('next.failure.timeout') : t('next.failure.network'));
  }
  const res = got;
  const body: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const said = refusalWords(body);
    throw new RuntimeError(said || t('next.failure.http'), body);
  }
  return body as T;
};

/** The words of a refusal: the runtime's {error: words}, or for the hub's own about the machine ({error:
 *  "machine_offline", message: "Машина не на связи"}) its message, not its code. */
const refusalWords = (body: unknown): string => {
  if (typeof body !== 'object' || body === null || !('error' in body)) return '';
  const code = String(body.error);
  const hubs = code.startsWith('machine_') && 'message' in body && typeof body.message === 'string';
  return hubs && body.message !== '' ? String(body.message) : code;
};

export const postJson = (body: unknown): RequestInit => ({
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});
