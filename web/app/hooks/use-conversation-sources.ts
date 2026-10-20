import { useEffect, useMemo, useState } from 'react';
import { appFetch } from '@/lib/app-path';
import { collectArtifacts, collectWorkPaths, type IArtifactLink } from '@/lib/hl-artifacts';
import type { IConversationSources } from '@/types/server/hl-conversation-sources';
import type { ITimelineEntry } from '@/types/timeline';

/** A burst of timeline entries (a long answer streaming in) asks the runtime once, not per line. */
const REFETCH_DELAY_MS = 1_500;
const MAX_CACHED = 32;

// Reopening a conversation in this tab shows its sources at once, the runtime refreshes them underneath.
const cache = new Map<string, IConversationSources>();

const remember = (key: string, value: IConversationSources) => {
  cache.delete(key);
  cache.set(key, value);
  while (cache.size > MAX_CACHED) cache.delete(cache.keys().next().value!);
};

// The panel, its summary and auto-open watch the same conversation: one request serves them all.
const inflight = new Map<string, Promise<IConversationSources | null>>();

const requestSources = (
  key: string,
  jsonlPath: string,
  cwd: string | null,
): Promise<IConversationSources | null> => {
  const running = inflight.get(key);
  if (running) return running;
  const params = new URLSearchParams({ jsonlPath, ...(cwd ? { cwd } : {}) });
  const request = appFetch(`/api/platform/sources?${params}`)
    .then((response) => (response.ok ? (response.json() as Promise<IConversationSources>) : null))
    .catch(() => null)
    .finally(() => {
      inflight.delete(key);
    });
  inflight.set(key, request);
  return request;
};

/** Server links first; what the loaded timeline shows beyond them (a reply the runtime has not indexed yet) joins them. */
const mergeLinks = (server: readonly IArtifactLink[], local: readonly IArtifactLink[]): IArtifactLink[] => {
  const byUrl = new Map(server.map((link) => [link.url, link]));
  for (const link of local) {
    const known = byUrl.get(link.url);
    byUrl.set(
      link.url,
      known
        ? { ...known, timestamp: Math.max(known.timestamp, link.timestamp), label: known.label ?? link.label }
        : link,
    );
  }
  return [...byUrl.values()].sort((a, b) => b.timestamp - a.timestamp);
};

const mergePaths = (server: readonly string[], local: readonly string[]) => [
  ...new Set([...local, ...server]),
];

export interface IConversationSourcesView {
  links: IArtifactLink[];
  workPaths: string[];
  /** The whole transcript is covered, not only the loaded part of the timeline. */
  complete: boolean;
}

/**
 * Links, artifacts, boards, merge requests and work paths of the whole conversation from its runtime, merged
 * with the loaded timeline so a fresh reply shows before the next refresh. Without a transcript (a chat) the timeline
 * is all there is.
 */
const useConversationSources = (
  jsonlPath: string | null,
  cwd: string | null,
  entries: readonly ITimelineEntry[],
): IConversationSourcesView => {
  const key = jsonlPath ? `${jsonlPath}\n${cwd ?? ''}` : null;
  const [loaded, setLoaded] = useState<{ key: string; value: IConversationSources } | null>(null);
  const activity = entries.length;

  useEffect(() => {
    if (!key || !jsonlPath) return;
    let cancelled = false;
    const timer = setTimeout(
      () => {
        void requestSources(key, jsonlPath, cwd).then((value) => {
          if (cancelled || !value) return;
          remember(key, value);
          setLoaded({ key, value });
        });
      },
      cache.has(key) || activity === 0 ? 0 : REFETCH_DELAY_MS,
    );
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [key, jsonlPath, cwd, activity]);

  const local = useMemo(
    () => ({ links: collectArtifacts(entries), workPaths: collectWorkPaths(entries, cwd) }),
    [entries, cwd],
  );
  const server = key ? (loaded?.key === key ? loaded.value : (cache.get(key) ?? null)) : null;
  return useMemo(() => {
    if (!server) return { ...local, complete: false };
    return {
      links: mergeLinks(server.links, local.links),
      workPaths: mergePaths(server.workPaths, local.workPaths),
      complete: true,
    };
  }, [server, local]);
};

export default useConversationSources;
