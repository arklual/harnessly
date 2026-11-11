// How the stack of the smart notice behaves, one rule set for the chat and the gallery (the flow spec):
// the front plate changes only by what the person does (paging, a row of the list, answering or putting it away), or,
// while nobody touches the plate, when something more urgent than it comes in; and the pointer on the plate keeps what
// it shows, it never brings back news that has already gone.
import { useMemo, useState } from 'react';
import type { ISmartItem, TSmartKind } from './smart-notice';

/** The order of the stack: the lower, the more urgent. */
export const KIND_RANK: Record<TSmartKind, number> = {
  code: 0,
  permission: 1,
  machine: 2,
  question: 2,
  plan: 2,
  ask: 2,
  failed: 3,
  limit: 3,
  done: 4,
  info: 4,
};

/** What comes forward of the items that just came in: the most urgent of them, if it is more urgent than the front. */
export const arrivalToFront = <T extends ISmartItem>(fresh: readonly T[], front: T | null): T | null => {
  let best: T | null = null;
  for (const item of fresh) if (best === null || KIND_RANK[item.kind] < KIND_RANK[best.kind]) best = item;
  if (best === null) return null;
  if (front !== null && KIND_RANK[best.kind] >= KIND_RANK[front.kind]) return null;
  return best;
};

/**
 * The front plate, kept: once shown it stays the front until the person moves it or it is gone (then the most urgent
 * left). New items go behind it; while nobody touches the plate, one more urgent than the front comes forward.
 * Called while rendering the owner of `mainKey` (it adjusts that state there, as React advises).
 */
export const useFront = <T extends ISmartItem>(
  items: readonly T[],
  mainKey: string | null,
  setMainKey: (key: string | null) => void,
  touched: boolean,
): T | null => {
  const [known, setKnown] = useState<ReadonlySet<string>>(() => new Set(items.map((item) => item.key)));
  const held = items.find((item) => item.key === mainKey) ?? null;
  const fresh = items.filter((item) => !known.has(item.key));
  if (fresh.length > 0 || known.size !== items.length) {
    setKnown(new Set(items.map((item) => item.key)));
  }
  const forward = touched ? null : arrivalToFront(fresh, held);
  const front = forward ?? held ?? items[0] ?? null;
  const key = front?.key ?? null;
  if (key !== mainKey) setMainKey(key);
  return front;
};

/** Once the plate is let go, news still on it has at least this long left: it does not vanish from under the eye. */
export const AFTER_HOLD_MS = 2000;

/**
 * Each piece of news on its own clock, paused while the plate is held (the pointer, the focus, the card open), as
 * notifications behave in macOS: holding hides nothing, news that comes while held waits to start, and let go each one
 * goes on from what it had left. `pausedAt` is when the hold began; null while nobody holds the plate.
 */
export interface INewsClock {
  deadlines: ReadonlyMap<string, number>;
  pausedAt: number | null;
}

export const NEWS_CLOCK: INewsClock = { deadlines: new Map(), pausedAt: null };

const sameDeadlines = (a: ReadonlyMap<string, number>, b: ReadonlyMap<string, number>): boolean =>
  a.size === b.size && [...a].every(([key, at]) => b.get(key) === at);

/** The clock after `now`: the hold begun or ended, the news that came on it, the news that is not there any more off it.
 *  The same object when nothing changed. */
export const tickNews = (
  clock: INewsClock,
  news: readonly { key: string; at: number }[],
  held: boolean,
  now: number,
  lifeMs: number,
): INewsClock => {
  let pausedAt = clock.pausedAt;
  let deadlines = clock.deadlines;
  if (held && pausedAt === null) pausedAt = now;
  if (!held && pausedAt !== null) {
    const since = pausedAt;
    deadlines = new Map(
      [...deadlines].map(([key, at]) => [
        key,
        // What had gone before the hold stays gone; the rest goes on from what it had left.
        at > since ? Math.max(at + (now - since), now + AFTER_HOLD_MS) : at,
      ]),
    );
    pausedAt = null;
  }
  const next = new Map<string, number>();
  for (const item of news) {
    const had = deadlines.get(item.key);
    // Come while held: its time starts when the hold ends (the shift above), as if it came then.
    next.set(item.key, had ?? (pausedAt !== null ? Math.max(item.at, pausedAt) : item.at) + lifeMs);
  }
  if (pausedAt === clock.pausedAt && sameDeadlines(next, clock.deadlines)) return clock;
  return { deadlines: next, pausedAt };
};

/** The news still on the plate: its time not up (at the start of the hold, while held). */
export const liveNews = (clock: INewsClock, now: number): ReadonlySet<string> => {
  const at = clock.pausedAt ?? now;
  return new Set([...clock.deadlines].filter(([, until]) => until > at).map(([key]) => key));
};

/** The news on the plate now, each on its own clock (see INewsClock). Called while rendering, as React advises for
 *  state that follows props. */
export const useNewsClock = (
  news: readonly { key: string; at: number }[],
  held: boolean,
  now: number,
  lifeMs: number,
): ReadonlySet<string> => {
  const [clock, setClock] = useState<INewsClock>(NEWS_CLOCK);
  const next = tickNews(clock, news, held, now, lifeMs);
  if (next !== clock) setClock(next);
  return useMemo(() => liveNews(next, now), [next, now]);
};
