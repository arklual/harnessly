// The v2 pages whose refusals carry their own words (`{r: 'refused', code, message}`, worded by the runtime for the
// person): Designly's, the conversation's files and voice. A refusal or a lost answer is thrown as a WordedError.
import { pageClock } from '@/next/core/clock';
import { deviceId } from '@/next/core/device';
import { machineApi } from '@/next/core/net/machine';
import { DEVICE_HEADER, type MachineId, type RefuseCode } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

export class WordedError extends Error {
  constructor(
    message: string,
    readonly code: RefuseCode | 'network' | 'http' = 'http',
    /** Designly: a project made before the failure (its conversation did not start). */
    readonly projectId: string | null = null,
  ) {
    super(message);
  }
}

const TIMEOUT_MS = 30_000;

export const wordedPage = async <T>(
  m: MachineId,
  path: `/${string}`,
  read: (v: unknown) => T,
  readRefusal: (v: unknown) => { code: RefuseCode; message: string; projectId?: string | null },
  /** A Blob body goes as it is, with its own type; anything else as JSON. */
  init: { method?: 'GET' | 'POST' | 'PUT'; body?: unknown; signal?: AbortSignal; timeoutMs?: number } = {},
): Promise<T> => {
  const ctl = new AbortController();
  init.signal?.addEventListener('abort', () => ctl.abort());
  const timer = pageClock().setTimeout(() => ctl.abort(), init.timeoutMs ?? TIMEOUT_MS);
  const raw = init.body instanceof Blob;
  let res: Response;
  try {
    res = await fetch(machineApi(m).http(path), {
      method: init.method ?? 'GET',
      credentials: 'same-origin',
      signal: ctl.signal,
      headers: {
        [DEVICE_HEADER]: deviceId(),
        ...(init.body === undefined || raw ? {} : { 'Content-Type': 'application/json' }),
      },
      body: init.body === undefined || raw ? (init.body as Blob | undefined) : JSON.stringify(init.body),
    });
  } catch {
    // The wait that ran out is not a lost network: the machine was reached, it did not answer in time.
    if (ctl.signal.aborted && !init.signal?.aborted) throw new WordedError(t('next.failure.timeout'));
    throw new WordedError(t('next.failure.network'), 'network');
  } finally {
    pageClock().clearTimeout(timer);
  }
  const doc: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    let refusal: ReturnType<typeof readRefusal>;
    try {
      refusal = readRefusal(doc);
    } catch {
      throw new WordedError(t('next.failure.http'));
    }
    throw new WordedError(
      refusal.message || t(`next.refuse.${refusal.code}`),
      refusal.code,
      refusal.projectId ?? null,
    );
  }
  try {
    return read(doc);
  } catch {
    throw new WordedError(t('next.failure.http'));
  }
};
