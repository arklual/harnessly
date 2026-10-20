import { githubRefOf } from '@/lib/site';
import { useCallback, useMemo, useRef, useSyncExternalStore } from 'react';
import { appFetch } from '@/lib/app-path';
import { linkTargetKey, parseLinkTarget, type TLinkMeta } from '@/lib/link-meta';

const MAX_CONCURRENT = 4;

const results = new Map<string, TLinkMeta | null>();
const requested = new Set<string>();
const listeners = new Map<string, Set<() => void>>();
const queue: { key: string; url: string }[] = [];
let active = 0;

const publish = (key: string, meta: TLinkMeta | null) => {
  results.set(key, meta);
  listeners.get(key)?.forEach((notify) => notify());
};

const pump = () => {
  while (active < MAX_CONCURRENT && queue.length) {
    const { key, url } = queue.shift()!;
    active += 1;
    appFetch(
      `${githubRefOf(url) ? '/api/platform/github/link-meta' : '/api/platform/link-meta'}?url=${encodeURIComponent(url)}`,
    )
      .then((response) => (response.ok ? (response.json() as Promise<TLinkMeta>) : null))
      .catch(() => null)
      .then((meta) => publish(key, meta))
      .finally(() => {
        active -= 1;
        pump();
      });
  }
};

const request = (key: string, url: string) => {
  if (requested.has(key)) return;
  requested.add(key);
  queue.push({ key, url });
  pump();
};

/** Metadata for tracker issues, merge requests and Hl artifacts; one request per link per page load. */
const useLinkMeta = (url: string | null): TLinkMeta | null => {
  const target = url ? parseLinkTarget(url) : null;
  const key = target ? linkTargetKey(target) : null;

  const subscribe = useCallback(
    (notify: () => void) => {
      if (!key || !url) return () => {};
      let set = listeners.get(key);
      if (!set) listeners.set(key, (set = new Set()));
      set.add(notify);
      request(key, url);
      return () => {
        set.delete(notify);
      };
    },
    [key, url],
  );

  return useSyncExternalStore(
    subscribe,
    () => (key ? (results.get(key) ?? null) : null),
    () => null,
  );
};

/** Metadata for a whole list at once, e.g. to group tasks under their epics. */
export const useLinkMetas = (urls: string[]): Map<string, TLinkMeta | null> => {
  const targets = useMemo(
    () =>
      urls.flatMap((url) => {
        const target = parseLinkTarget(url);
        return target ? [{ url, key: linkTargetKey(target) }] : [];
      }),
    [urls],
  );
  const subscribe = useCallback(
    (notify: () => void) => {
      const sets = targets.map(({ key, url }) => {
        let set = listeners.get(key);
        if (!set) listeners.set(key, (set = new Set()));
        set.add(notify);
        request(key, url);
        return set;
      });
      return () => sets.forEach((set) => set.delete(notify));
    },
    [targets],
  );
  // useSyncExternalStore needs the same Map back until some metadata actually arrives.
  const cache = useRef<{ stamp: string; map: Map<string, TLinkMeta | null> }>({ stamp: '', map: new Map() });
  const snapshot = () => {
    const stamp = targets.map((t) => `${t.key}:${Number(results.has(t.key))}`).join('|');
    if (stamp !== cache.current.stamp) {
      cache.current = { stamp, map: new Map(targets.map((t) => [t.url, results.get(t.key) ?? null])) };
    }
    return cache.current.map;
  };
  return useSyncExternalStore(subscribe, snapshot, snapshot);
};

export default useLinkMeta;
