// The page's start data (GET <platform>/bootstrap) comes from the home machine; on a hub the request goes through the
// hub to that machine. A machine that hangs must not keep the page blank: what the hub knows (the list of chats, the
// machines, the hub's copy of each chat) draws without it. So the page waits on the start data for a moment only,
// then draws with what it has (the owner it saw last) and takes the machine's answer when it comes. Each request is
// cut off after a while: on HTTP/1.1 a request held open takes one of the browser's few connections to the origin.

/** How long the page waits on the start data before it draws without it. */
export const BOOT_WAIT_MS = 1500;
/** How long one request for the start data may stay open. */
export const BOOT_FETCH_MS = 20_000;
/** The pauses before asking again a machine that did not answer, the last one repeated. */
export const BOOT_RETRY_MS = [1000, 2000, 5000, 10_000, 30_000] as const;

const OWNER_KEY = 'hl-boot-owner';

export type BootBody = Record<string, unknown>;

export interface BootHandlers {
  /** The start data to draw with; `meanwhile` while the machine has not answered yet. */
  onData: (body: BootBody, meanwhile: boolean) => void;
  /** The request was answered with 401 or sent to the hub's sign-in. */
  onSignedOut: (res: Response) => void;
}

export interface BootLoadOptions {
  waitMs?: number;
  fetchMs?: number;
  retryMs?: readonly number[];
  storage?: Pick<Storage, 'getItem' | 'setItem'> | null;
}

const defaultStorage = (): Pick<Storage, 'getItem' | 'setItem'> | null => {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
};

const readOwner = (s: BootLoadOptions['storage']): string => {
  try {
    return s?.getItem(OWNER_KEY) ?? '';
  } catch {
    return '';
  }
};

const writeOwner = (s: BootLoadOptions['storage'], owner: unknown): void => {
  if (typeof owner !== 'string' || owner === '') return;
  try {
    s?.setItem(OWNER_KEY, owner);
  } catch {
    // Storage blocked: the next slow start draws without a name until the machine answers.
  }
};

/**
 * Loads the start data with `fetchBoot` and reports it to `h`. Returns a function that stops it (an open request is
 * cancelled). A machine that answers in time gives one onData; one that does not gives onData(meanwhile) after
 * `waitMs` and onData again once it answers; a 5xx or a request that failed is asked again after a growing pause.
 */
export function loadBootstrap(
  fetchBoot: (signal: AbortSignal) => Promise<Response>,
  h: BootHandlers,
  opts: BootLoadOptions = {},
): () => void {
  const waitMs = opts.waitMs ?? BOOT_WAIT_MS;
  const fetchMs = opts.fetchMs ?? BOOT_FETCH_MS;
  const retryMs = opts.retryMs ?? BOOT_RETRY_MS;
  const storage = opts.storage === undefined ? defaultStorage() : opts.storage;
  let alive = true;
  let shown = false;
  let failures = 0;
  let retry: ReturnType<typeof setTimeout> | undefined;
  let open: AbortController | null = null;

  const showMeanwhile = () => {
    if (!alive || shown) return;
    shown = true;
    h.onData({ owner: readOwner(storage) }, true);
  };
  const deadline = setTimeout(showMeanwhile, waitMs);

  const load = async () => {
    const ctrl = new AbortController();
    open = ctrl;
    const cut = setTimeout(() => ctrl.abort(), fetchMs);
    const res = await fetchBoot(ctrl.signal).catch(() => null);
    clearTimeout(cut);
    open = null;
    if (!alive) return;
    if (res && (res.redirected || res.status === 401)) {
      clearTimeout(deadline);
      alive = false;
      h.onSignedOut(res);
      return;
    }
    if (!res || res.status >= 500) {
      // A runtime still starting (503), a hub that gave up on the machine (502/504), no answer at all.
      const pause = retryMs[Math.min(failures, retryMs.length - 1)] ?? 1000;
      failures += 1;
      retry = setTimeout(() => void load(), pause);
      return;
    }
    const body: BootBody = res.ok ? await res.json().catch(() => ({})) : {};
    if (!alive) return;
    clearTimeout(deadline);
    alive = false;
    shown = true;
    writeOwner(storage, body.owner);
    h.onData(body, false);
  };
  void load();

  return () => {
    alive = false;
    clearTimeout(deadline);
    clearTimeout(retry);
    open?.abort();
  };
}
