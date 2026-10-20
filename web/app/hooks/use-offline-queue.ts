import { useCallback } from 'react';
import useSWR from 'swr';
import { scopedAppPath } from '@/lib/app-path';
import { t } from '@/lib/i18n';

/**
 * Messages written while the conversation's machine is down: they wait in the cluster and go to the
 * machine once it answers. Shared by the live composer, which stays on screen through an outage, and the stored view
 * of a conversation opened while its machine was already down.
 */

export type TOfflineState = 'queued' | 'sending' | 'delivered' | 'failed';

export interface IOfflinePrompt {
  id: string;
  tabId: string;
  text: string;
  state: TOfflineState;
  error: string | null;
  createdAt: number;
}

const QUEUE_POLL_MS = 4_000;
const PROMPTS_PATH = '/api/platform/offline-prompts';

export const OFFLINE_STATE_LABELS: Record<TOfflineState, string> = {
  queued: t('hooks.useOfflineQueue.queued'),
  sending: t('hooks.useOfflineQueue.sending'),
  delivered: t('hooks.useOfflineQueue.delivered'),
  failed: t('hooks.useOfflineQueue.failed'),
};

const fetchQueue = async (url: string): Promise<IOfflinePrompt[]> => {
  const res = await fetch(url, { cache: 'no-store' });
  if (!res.ok) throw new Error('queue unavailable');
  const body = (await res.json()) as { items?: IOfflinePrompt[] };
  return Array.isArray(body.items) ? body.items : [];
};

/** `watch` keeps the list polled: while the machine is down, and until what was queued has gone out. */
const useOfflineQueue = (
  machineId: string | null | undefined,
  tabId: string | null | undefined,
  watch: boolean,
) => {
  const url = useCallback(
    (query: string) => scopedAppPath(`${PROMPTS_PATH}${query}`, machineId ?? undefined),
    [machineId],
  );
  const key = tabId ? url(`?tab=${encodeURIComponent(tabId)}`) : null;
  const { data, mutate } = useSWR(key, fetchQueue, {
    refreshInterval: watch ? QUEUE_POLL_MS : 0,
    revalidateOnFocus: watch,
  });
  const items = data ?? [];
  const pending = items.some((item) => item.state === 'queued' || item.state === 'sending');
  useSWR(pending && !watch ? key : null, fetchQueue, { refreshInterval: QUEUE_POLL_MS });

  const setItems = useCallback(
    (change: (current: IOfflinePrompt[]) => IOfflinePrompt[]) => {
      void mutate((current) => change(current ?? []), { revalidate: false });
    },
    [mutate],
  );
  const refresh = useCallback(() => void mutate(), [mutate]);

  const queue = useCallback(
    async (message: string) => {
      if (!tabId) throw new Error(t('hooks.useOfflineQueue.noConversation'));
      const res = await fetch(url(''), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tabId, text: message }),
      });
      const body = (await res.json().catch(() => ({}))) as { item?: IOfflinePrompt; error?: string };
      if (!res.ok || !body.item) throw new Error(body.error ?? t('hooks.useOfflineQueue.enqueueFailed'));
      const item = body.item;
      setItems((current) => [...current.filter((existing) => existing.id !== item.id), item]);
    },
    [url, tabId, setItems],
  );

  const drop = useCallback(
    async (item: IOfflinePrompt) => {
      const res = await fetch(url(`?id=${encodeURIComponent(item.id)}`), { method: 'DELETE' }).catch(
        () => null,
      );
      if (res?.ok) setItems((current) => current.filter((existing) => existing.id !== item.id));
      else refresh();
    },
    [url, refresh, setItems],
  );

  const retry = useCallback(
    async (item: IOfflinePrompt) => {
      await queue(item.text);
      await drop(item);
    },
    [queue, drop],
  );

  return { items, queue, drop, retry };
};

export default useOfflineQueue;
