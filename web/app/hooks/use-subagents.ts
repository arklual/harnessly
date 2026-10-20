import { useCallback, useSyncExternalStore } from 'react';
import { appFetch } from '@/lib/app-path';
import { startLatencySample } from '@/lib/network-health';
import { createSharedResource, type IResourceState, type ISharedResource } from '@/lib/shared-resource';
import type { ISubagentSummary } from '@/types/server/hl-subagents';
import { t } from '@/lib/i18n';

/**
 * While a subagent works its card shows live progress; otherwise the main transcript's activity key (see `poke`)
 * says when to look again. A transcript the server cannot read subagents from is asked rarely.
 */
const POLL = {
  runningMs: 3_000,
  idleMs: 60_000,
  unsupportedMs: 5 * 60_000,
  maxBackoffMs: 5 * 60_000,
} as const;
/** Sessions nobody shows any more are forgotten once more than this many are kept. */
const KEEP_SESSIONS = 16;

interface ISubagentsResponse {
  subagents: ISubagentSummary[];
  supported: boolean;
}

interface IEntry {
  resource: ISharedResource<ISubagentsResponse>;
  subscribers: number;
}

const entries = new Map<string, IEntry>();
const EMPTY: IResourceState<ISubagentsResponse> = { data: null, error: null };

const nextInterval = (data: ISubagentsResponse | null): number => {
  if (data?.supported === false) return POLL.unsupportedMs;
  if (data?.subagents.some((a) => a.status === 'running')) return POLL.runningMs;
  return POLL.idleMs;
};

const forgetIdle = (keep: string) => {
  for (const [path, entry] of entries) {
    if (entries.size <= KEEP_SESSIONS) return;
    if (path !== keep && entry.subscribers === 0) entries.delete(path);
  }
};

const entryFor = (jsonlPath: string): IEntry => {
  const existing = entries.get(jsonlPath);
  if (existing) return existing;
  const resource: ISharedResource<ISubagentsResponse> = createSharedResource<ISubagentsResponse>({
    load: async (signal) => {
      const sample = startLatencySample();
      try {
        const response = await appFetch(
          `/api/platform/subagents?jsonlPath=${encodeURIComponent(jsonlPath)}`,
          { signal },
        );
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const data = (await response.json()) as ISubagentsResponse;
        sample.finish();
        return data;
      } finally {
        sample.cancel();
      }
    },
    describeError: () => t('hooks.useSubagents.loadFailed'),
    intervalMs: () => nextInterval(resource.getSnapshot().data),
    maxBackoffMs: POLL.maxBackoffMs,
  });
  const entry: IEntry = { resource, subscribers: 0 };
  entries.set(jsonlPath, entry);
  forgetIdle(jsonlPath);
  return entry;
};

const serverSnapshot = () => EMPTY;

/**
 * Subagents of one session, polled once (visibility-aware, single-flight) for every island and timeline card
 * that shows them. `enabled: false` never asks the server (Codex has no subagents).
 */
const useSubagents = (jsonlPath: string | null, enabled = true) => {
  const key = enabled ? jsonlPath : null;

  const subscribe = useCallback(
    (notify: () => void) => {
      if (!key) return () => undefined;
      const entry = entryFor(key);
      entry.subscribers += 1;
      const stop = entry.resource.subscribe(notify);
      return () => {
        entry.subscribers -= 1;
        stop();
      };
    },
    [key],
  );
  const getSnapshot = useCallback(
    () => (key ? (entries.get(key)?.resource.getSnapshot() ?? EMPTY) : EMPTY),
    [key],
  );
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, serverSnapshot);

  /** A change signal from the main transcript: fetch once after `delayMs`, merged with other pokes. */
  const poke = useCallback(
    (delayMs?: number) => {
      if (key) entries.get(key)?.resource.poke(delayMs);
    },
    [key],
  );

  return {
    /** `null` until the first answer. */
    agents: snapshot.data?.subagents ?? null,
    supported: snapshot.data?.supported ?? true,
    /** The last load failed; earlier agents stay on screen meanwhile. */
    error: snapshot.error,
    poke,
  };
};

export default useSubagents;
