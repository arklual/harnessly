import { prefixAppPath } from '@/lib/app-path';

/**
 * How the sockets fare on the real road (Teleport, the tunnel, a phone's network), counted on the page and
 * sent to the runtime's log once a minute: drops, sockets found dead by the heartbeat, and how long each outage lasted.
 * The server never sees most of it: a dead socket on a phone does not reach it as a close.
 */
interface ISocketCounts {
  drops: number;
  dead: number;
  recovered: number;
  /** Milliseconds from losing the socket to having it back, per recovery. */
  downMs: number[];
}

const FLUSH_MS = 60_000;
const DOWN_SAMPLES_MAX = 20;

let counts: Record<string, ISocketCounts> = {};
const lostAt = new Map<string, number>();
let timer: ReturnType<typeof setTimeout> | null = null;

const entry = (key: string): ISocketCounts =>
  (counts[key] ??= { drops: 0, dead: 0, recovered: 0, downMs: [] });

const flush = () => {
  timer = null;
  if (Object.keys(counts).length === 0) return;
  const body = JSON.stringify({
    sockets: counts,
    hidden: document.visibilityState !== 'visible',
    online: navigator.onLine,
  });
  counts = {};
  const url = prefixAppPath('/api/platform/net-events');
  const sent =
    typeof navigator.sendBeacon === 'function' &&
    navigator.sendBeacon(url, new Blob([body], { type: 'application/json' }));
  if (!sent)
    void fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
      keepalive: true,
    }).catch(() => undefined);
};

const schedule = () => {
  if (timer === null && typeof window !== 'undefined') timer = setTimeout(flush, FLUSH_MS);
};

/** A socket of `key` lost its connection (it is now reconnecting) or got it back. */
export const recordSocketState = (key: string, reconnecting: boolean): void => {
  if (typeof window === 'undefined') return;
  const now = Date.now();
  if (reconnecting) {
    if (lostAt.has(key)) return;
    lostAt.set(key, now);
    entry(key).drops += 1;
  } else {
    const since = lostAt.get(key);
    if (since === undefined) return;
    lostAt.delete(key);
    const counted = entry(key);
    counted.recovered += 1;
    if (counted.downMs.length < DOWN_SAMPLES_MAX) counted.downMs.push(now - since);
  }
  schedule();
};

/** The heartbeat found a socket of `key` dead: open to the page, gone on the road. */
export const recordSocketDead = (key: string): void => {
  if (typeof window === 'undefined') return;
  entry(key).dead += 1;
  schedule();
};

export const __testing = {
  take: () => {
    const taken = counts;
    counts = {};
    lostAt.clear();
    return taken;
  },
};
