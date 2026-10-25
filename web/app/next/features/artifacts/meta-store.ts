// The titles and states of the conversation's links. The runtime reads each one on its own (GET
// …/link-meta?url=: the tracker, GitLab, the page of a document), so the page asks for them side by side, six at a
// time, in the order the rows stand; each answer reaches its row the moment it comes. What was read stays: in memory
// for the next opening, in localStorage for the next load of the page, and an old answer is shown while a new one is
// read underneath (the runtime keeps its own for five minutes). An answer that did not come is not remembered as "none":
// it is asked again, once by itself and then by the person's «Повторить».
import { useEffect, useSyncExternalStore } from 'react';
import { runtimePath, platformPath } from '@/next/core/net/machine';
import { pageClock } from '@/next/core/clock';
import {
  LOOK,
  readMeta,
  readableInOrder,
  type IArtifactItem,
  type TLook,
  type TMeta,
} from '@/next/features/artifacts/model';
import type { MachineId } from '@/next/protocol/v2';

/** How many links are read at once. */
export const PARALLEL = 6;
/** An answer younger than this is not read again. */
export const FRESH_MS = 120_000;
/** A link the runtime knows nothing of is not asked again for this long. */
export const NONE_FRESH_MS = 60_000;
export const REQUEST_MS = 12_000;
/** A failed read is tried once more by itself after this. */
export const AGAIN_MS = 1_500;
const KEEP = 300;
const STORE_KEY = 'hx-next:artifacts:meta:v1';
const SAVE_DELAY_MS = 600;

interface IEntry {
  meta: TMeta | null;
  at: number;
}
interface IJob {
  machine: MachineId;
  item: IArtifactItem;
  again: boolean;
}

export type TMetaFetch = (url: string, signal: AbortSignal) => Promise<Response>;
const defaultFetch: TMetaFetch = (url, signal) => fetch(url, { credentials: 'same-origin', signal });
let fetcher: TMetaFetch = defaultFetch;
/** Puts another fetch under the store (tests, the gallery). */
export const setMetaFetch = (next: TMetaFetch | null): void => {
  fetcher = next ?? defaultFetch;
};

const entries = new Map<string, IEntry>();
const failed = new Set<string>();
const running = new Set<string>();
const queued = new Set<string>();
let queue: IJob[] = [];
let version = 0;
const listeners = new Set<() => void>();
let saveTimer: unknown = null;
let restored = false;

const changed = () => {
  version += 1;
  for (const notify of [...listeners]) notify();
};

const storage = (): Storage | null => {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
};

const restore = () => {
  if (restored) return;
  restored = true;
  try {
    const raw = storage()?.getItem(STORE_KEY);
    if (!raw) return;
    const doc: unknown = JSON.parse(raw);
    if (typeof doc !== 'object' || doc === null) return;
    for (const [url, value] of Object.entries(doc)) {
      const v = value as { m?: unknown; at?: unknown };
      const meta = readMeta(v.m);
      if (meta && typeof v.at === 'number' && !entries.has(url)) entries.set(url, { meta, at: v.at });
    }
  } catch {
    // a broken store reads as an empty one
  }
};

const save = () => {
  if (saveTimer !== null) return;
  saveTimer = pageClock().setTimeout(() => {
    saveTimer = null;
    try {
      const found = [...entries].filter(([, e]) => e.meta !== null).sort((a, b) => b[1].at - a[1].at);
      const out: Record<string, { m: TMeta | null; at: number }> = {};
      for (const [url, e] of found.slice(0, KEEP)) out[url] = { m: e.meta, at: e.at };
      storage()?.setItem(STORE_KEY, JSON.stringify(out));
    } catch {
      // storage full or blocked: the page works the same, it only forgets on reload
    }
  }, SAVE_DELAY_MS);
};

const addressOf = (job: IJob): string => {
  const route = job.item.github ? '/github/link-meta' : '/link-meta';
  return runtimePath(
    `${platformPath(route)}?url=${encodeURIComponent(job.item.url)}` as `/${string}`,
    job.machine,
  );
};

const RESULT = { found: 'found', none: 'none', lost: 'lost' } as const;

const read = async (job: IJob): Promise<{ r: typeof RESULT.found; meta: TMeta } | { r: 'none' | 'lost' }> => {
  const ctl = new AbortController();
  const clock = pageClock();
  const timer = clock.setTimeout(() => ctl.abort(), REQUEST_MS);
  try {
    const res = await fetcher(addressOf(job), ctl.signal);
    if (res.status === 404) return { r: RESULT.none };
    if (!res.ok) return { r: RESULT.lost };
    const meta = readMeta(await res.json());
    return meta ? { r: RESULT.found, meta } : { r: RESULT.none };
  } catch {
    return { r: RESULT.lost };
  } finally {
    clock.clearTimeout(timer);
  }
};

let active = 0;

const pump = () => {
  while (active < PARALLEL && queue.length > 0) {
    const job = queue.shift() as IJob;
    queued.delete(job.item.url);
    running.add(job.item.url);
    active += 1;
    void read(job).then((out) => {
      active -= 1;
      running.delete(job.item.url);
      const url = job.item.url;
      if (out.r === RESULT.found) {
        failed.delete(url);
        entries.set(url, { meta: out.meta, at: pageClock().now() });
        save();
      } else if (out.r === RESULT.none) {
        failed.delete(url);
        // The runtime knows nothing of it: that is an answer, kept briefly; an older found one stays for the row.
        const kept = entries.get(url);
        entries.set(url, { meta: kept?.meta ?? null, at: pageClock().now() });
      } else if (job.again) {
        pageClock().setTimeout(() => {
          enqueue({ ...job, again: false });
          pump();
        }, AGAIN_MS);
      } else {
        failed.add(url);
      }
      changed();
      pump();
    });
  }
};

const enqueue = (job: IJob) => {
  const url = job.item.url;
  if (running.has(url) || queued.has(url)) return;
  queued.add(url);
  queue.push(job);
};

const stale = (item: IArtifactItem): boolean => {
  const e = entries.get(item.url);
  if (!e) return true;
  const age = pageClock().now() - e.at;
  return age >= (e.meta ? FRESH_MS : NONE_FRESH_MS);
};

/** Asks for every readable item that has no fresh answer, in the order the rows stand. */
export const requestMetas = (machine: MachineId, items: readonly IArtifactItem[]): void => {
  restore();
  let any = false;
  for (const item of readableInOrder(items)) {
    if (failed.has(item.url) || !stale(item)) continue;
    enqueue({ machine, item, again: true });
    any = true;
  }
  if (any) {
    changed();
    pump();
  }
};

/** The «Повторить»: asks again for what did not come. */
export const retryMetas = (machine: MachineId, items: readonly IArtifactItem[]): void => {
  for (const item of items) {
    failed.delete(item.url);
    // «The runtime knows nothing of it» may have been a tracker that did not answer: asked once more as well.
    if (entries.get(item.url)?.meta === null) entries.delete(item.url);
  }
  requestMetas(machine, items);
  changed();
};

export interface IMetaView {
  /** What is known of a link, and how: still on its way, there, or nothing to be had. */
  of: (item: IArtifactItem) => { meta: TMeta | null; look: TLook };
  /** How many of the links failed to be read. */
  failed: number;
  /** How many are still being read. */
  waiting: number;
}

const view = (items: readonly IArtifactItem[]): IMetaView => {
  const readable = items.filter((item) => item.readable);
  return {
    of: (item) => {
      const e = entries.get(item.url);
      if (e?.meta) return { meta: e.meta, look: LOOK.got };
      if (e) return { meta: null, look: LOOK.none };
      if (failed.has(item.url)) return { meta: null, look: LOOK.none };
      return { meta: null, look: LOOK.wait };
    },
    failed: readable.filter((item) => failed.has(item.url)).length,
    waiting: readable.filter((item) => !entries.has(item.url) && !failed.has(item.url)).length,
  };
};

const subscribe = (notify: () => void) => {
  listeners.add(notify);
  return () => listeners.delete(notify);
};
const snapshot = () => version;

/** The metas of the items, asked for as the list changes. The view is read again whenever an answer comes. */
export const useArtifactMetas = (machine: MachineId, items: readonly IArtifactItem[]): IMetaView => {
  restore();
  useSyncExternalStore(subscribe, snapshot, snapshot);
  useEffect(() => {
    requestMetas(machine, items);
  }, [machine, items]);
  return view(items);
};

/** Forgets everything (tests). */
export const resetMetaStore = (): void => {
  entries.clear();
  failed.clear();
  running.clear();
  queued.clear();
  queue = [];
  active = 0;
  restored = false;
  if (saveTimer !== null) pageClock().clearTimeout(saveTimer);
  saveTimer = null;
  version += 1;
};
