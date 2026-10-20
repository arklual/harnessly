import { appFetch } from '@/lib/app-path';
import { startLatencySample } from '@/lib/network-health';
import { createSharedResource } from '@/lib/shared-resource';
import type { IPeerSession } from '@/types/server/hl-peers';
import useSharedResource from '@/hooks/use-shared-resource';
import { t } from '@/lib/i18n';

const POLL = { intervalMs: 15_000, maxBackoffMs: 5 * 60_000 } as const;

export interface IPeerView extends IPeerSession {
  title: string | null;
}

const NO_PEERS: IPeerView[] = [];

const fetchJson = async <T>(path: string, signal: AbortSignal): Promise<T> => {
  const response = await appFetch(path, { signal });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json() as Promise<T>;
};

/** One visibility-aware, single-flight request stream for every peer chip on the page. */
const peersSource = createSharedResource<IPeerView[]>({
  load: async (signal) => {
    const sample = startLatencySample();
    try {
      const [{ peers }, titles] = await Promise.all([
        fetchJson<{ peers: IPeerSession[] }>('/api/platform/peers', signal),
        // Titles only decorate the chips: without them a chip falls back to the peer's name.
        fetchJson<{ conversations: { id: string; title: string }[] }>('/api/platform/conversations', signal)
          .then(({ conversations }) => new Map(conversations.map((c) => [c.id, c.title])))
          .catch(() => new Map<string, string>()),
      ]);
      sample.finish();
      return peers.map((peer) => ({ ...peer, title: peer.tabId ? (titles.get(peer.tabId) ?? null) : null }));
    } finally {
      sample.cancel();
    }
  },
  describeError: (error) =>
    t('hooks.usePeers.refreshFailed', {
      reason: error instanceof Error ? error.message : t('hooks.usePeers.noAnswer'),
    }),
  intervalMs: POLL.intervalMs,
  maxBackoffMs: POLL.maxBackoffMs,
});

/** `from` is the peer socket (`uds:/tmp/cc-socks/<pid>.sock`) when the sender left no name. */
export const findPeer = (list: IPeerView[], name: string, from?: string) => {
  const pid = Number(from?.match(/\/(\d+)\.sock$/)?.[1]);
  return list.find((p) => p.name === name) ?? (pid ? list.find((p) => p.pid === pid) : undefined) ?? null;
};

const usePeers = () => useSharedResource(peersSource).data ?? NO_PEERS;

export default usePeers;
