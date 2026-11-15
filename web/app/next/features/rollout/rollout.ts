// Whether the hub or the runtimes are being rolled out now (internal/hub/rollout): the page says «Идёт обновление —
// возможны перебои с подключением к машинам» while it lasts and «Обновление завершено» for a moment once the hub says it
// is over and the machines' links are back. The hub is the one source; the page asks it
//   - when a machine's link drops (a replica that stops moves its machines first, so the drop is the hub's signal),
//   - once a minute while the page is open, and every few seconds while a rollout is under way.
// A hub that does not answer at all (502, 503, 504 from what stands in front of it, no JSON) is a hub restarting: the
// same words. Standalone there is no hub: the runtime answers 404 and nothing is ever shown.

import { pageClock } from '@/next/core/clock';
import { createStore, useStore } from '@/next/core/store';
import { setHold } from '@/next/core/net/http';
import { clusterApi } from '@/next/core/net/machine';
import { connectionStore } from '@/next/stores/connection';

export const ROLLOUT = { idle: 'idle', active: 'active', done: 'done' } as const;
export type TRollout = (typeof ROLLOUT)[keyof typeof ROLLOUT];

export const rolloutStore = createStore<TRollout>(ROLLOUT.idle);
export const useRollout = (): TRollout => useStore(rolloutStore);

/** Whether a rollout is under way now: requests that fail meanwhile are tried again instead of failing. */
export const rolloutActive = (): boolean => rolloutStore.get() === ROLLOUT.active;

/** How often the page asks while nothing is under way, and while something is. */
export const IDLE_POLL_MS = 60_000;
export const ACTIVE_POLL_MS = 4_000;
/** «Обновление завершено» stays this long. */
export const DONE_MS = 2_500;
/** Once the hub says it is over, the page waits for the machines' links at most this long before it says so. */
export const LINKS_BACK_MAX_MS = 20_000;
/** A drop asks the hub at most this often. */
const SUSPECT_GAP_MS = 2_000;
const CHECK_TIMEOUT_MS = 5_000;

type Fetch = (url: string, init: RequestInit) => Promise<Response>;
let fetcher: Fetch = (url, init) => fetch(url, init);

/** Tests put a fake hub here. */
export function setRolloutFetch(next: Fetch): void {
  fetcher = next;
}

/** What the hub said: under way, not, or nothing to go by (the network, a hub without the route). */
export async function askHub(): Promise<boolean | null> {
  const clock = pageClock();
  const ctl = new AbortController();
  const timer = clock.setTimeout(() => ctl.abort(), CHECK_TIMEOUT_MS);
  try {
    const res = await fetcher(clusterApi('/rollout'), {
      credentials: 'same-origin',
      cache: 'no-store',
      signal: ctl.signal,
    });
    const body: unknown = await res.json().catch(() => undefined);
    if (res.ok && typeof body === 'object' && body !== null && 'active' in body) return body.active === true;
    // Nobody behind the balancer: the hub itself is being replaced.
    if (body === undefined && (res.status === 502 || res.status === 503 || res.status === 504)) return true;
    return null;
  } catch {
    return null;
  } finally {
    clock.clearTimeout(timer);
  }
}

const linksDown = (): boolean => {
  for (const c of connectionStore.values()) if (c.k !== 'live') return true;
  return false;
};

let running = false;
let poll: unknown;
let doneTimer: unknown;
/** When the hub first said «over» while links were still down; null when it has not. */
let overSince: number | null = null;
let lastAsk = 0;
let asking = false;
let stops: (() => void)[] = [];

function schedule(ms: number): void {
  const clock = pageClock();
  clock.clearTimeout(poll);
  poll = clock.setTimeout(() => void check(), ms);
}

function settle(active: boolean | null): void {
  const clock = pageClock();
  const phase = rolloutStore.get();
  if (active === true) {
    overSince = null;
    clock.clearTimeout(doneTimer);
    rolloutStore.set(ROLLOUT.active);
    return;
  }
  if (active === null || phase !== ROLLOUT.active) return;
  // Over by the hub's word: say so once the links are back (or have had their time).
  const now = clock.now();
  overSince ??= now;
  if (linksDown() && now - overSince < LINKS_BACK_MAX_MS) return;
  overSince = null;
  rolloutStore.set(ROLLOUT.done);
  doneTimer = clock.setTimeout(() => {
    if (rolloutStore.get() === ROLLOUT.done) rolloutStore.set(ROLLOUT.idle);
  }, DONE_MS);
}

/** Asks the hub now and plans the next ask. */
export async function check(): Promise<void> {
  if (!running || asking) return;
  asking = true;
  lastAsk = pageClock().now();
  try {
    settle(await askHub());
  } finally {
    asking = false;
  }
  if (running) schedule(rolloutStore.get() === ROLLOUT.active ? ACTIVE_POLL_MS : IDLE_POLL_MS);
}

/** Something looks like a rollout (a link dropped, a request failed at the gateway): ask the hub soon. */
export function suspectRollout(): void {
  if (!running || rolloutStore.get() === ROLLOUT.active) return;
  const wait = Math.max(0, lastAsk + SUSPECT_GAP_MS - pageClock().now());
  schedule(wait);
}

/** How long a held request waits before it is sent again. */
export const HOLD_STEP_MS = 1_000;

const sleep = (ms: number) => new Promise<void>((resolve) => pageClock().setTimeout(resolve, ms));

/**
 * The page's hold (core/net/http): a request that met a gap is sent again while the hub says a rollout is under way.
 * The first gap asks the hub at once (unless it was just asked), so the very request that met the rollout waits too.
 */
export async function holdRequest(elapsedMs: number, budgetMs: number): Promise<boolean> {
  if (!running) return false;
  if (rolloutStore.get() !== ROLLOUT.active && pageClock().now() - lastAsk >= SUSPECT_GAP_MS) await check();
  if (rolloutStore.get() !== ROLLOUT.active) return false;
  if (elapsedMs + HOLD_STEP_MS > budgetMs) return false;
  await sleep(HOLD_STEP_MS);
  return true;
}

/** Starts watching; the returned function stops. One watch per page. */
export function watchRollout(): () => void {
  if (running) return () => {};
  running = true;
  setHold(holdRequest);
  stops.push(() => setHold(null));
  const seen = new Map<string, string>();
  stops.push(
    connectionStore.subscribeAll((changed) => {
      for (const m of changed) {
        const k = connectionStore.get(m)?.k ?? 'gone';
        const was = seen.get(m);
        seen.set(m, k);
        if (was === 'live' && k !== 'live') suspectRollout();
        // A link that came back may be what an ending rollout waits for.
        if (k === 'live' && rolloutStore.get() === ROLLOUT.active && overSince !== null) schedule(0);
      }
    }),
  );
  if (typeof document !== 'undefined') {
    const onVisible = () => {
      if (document.visibilityState === 'visible') suspectRollout();
    };
    document.addEventListener('visibilitychange', onVisible);
    stops.push(() => document.removeEventListener('visibilitychange', onVisible));
  }
  void check();
  return stopRollout;
}

export function stopRollout(): void {
  const clock = pageClock();
  running = false;
  clock.clearTimeout(poll);
  clock.clearTimeout(doneTimer);
  for (const s of stops) s();
  stops = [];
  overSince = null;
  lastAsk = 0;
  asking = false;
  rolloutStore.set(ROLLOUT.idle);
}
