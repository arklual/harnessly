import { useEffect, useSyncExternalStore } from 'react';
import { chatHeaders } from '@/hooks/use-hl-conversations';
import { chatConversationKey, type Conversation } from '@/lib/hl-conversations';
import type { TResolvedChat } from '@/lib/hl-sidebar-model';
import { t } from '@/lib/i18n';

/**
 * Pinned and project chats that are not on the loaded pages of Open WebUI, fetched one by one.
 * Only rows on screen ask, at most three requests at a time, answers kept for the session.
 * 404 means the chat was deleted; any other failure is an error, never "deleted".
 */
const MAX_PARALLEL = 3;
const RETRY_ERROR_MS = 60_000;

let resolved: Record<string, TResolvedChat> = {};
const failedAt = new Map<string, number>();
const queue: string[] = [];
let running = 0;
const listeners = new Set<() => void>();

const commit = (id: string, value: TResolvedChat) => {
  resolved = { ...resolved, [id]: value };
  for (const listener of listeners) listener();
};

const fetchChat = async (id: string): Promise<TResolvedChat> => {
  const response = await fetch(`/api/v1/chats/${encodeURIComponent(id)}`, { headers: chatHeaders() }).catch(
    () => null,
  );
  if (!response) return 'error';
  if (response.status === 404) return 'missing';
  if (!response.ok) return 'error';
  const body = (await response.json().catch(() => null)) as {
    id?: string;
    title?: string;
    updated_at?: number;
  } | null;
  if (!body?.id) return 'error';
  return {
    key: chatConversationKey(body.id),
    id: body.id,
    title: body.title || t('hooks.useChatRefs.untitled'),
    updatedAt: (body.updated_at ?? 0) * 1000,
    provider: 'chat',
    kind: 'chat',
  } satisfies Conversation;
};

const pump = () => {
  while (running < MAX_PARALLEL && queue.length) {
    const id = queue.shift()!;
    running += 1;
    void fetchChat(id)
      .then((value) => {
        if (value === 'error') failedAt.set(id, Date.now());
        commit(id, value);
      })
      .finally(() => {
        running -= 1;
        pump();
      });
  }
};

const request = (ids: readonly string[]) => {
  const now = Date.now();
  for (const id of ids) {
    const current = resolved[id];
    const retry = current === 'error' && now - (failedAt.get(id) ?? 0) > RETRY_ERROR_MS;
    if ((current && !retry) || queue.includes(id)) continue;
    resolved = { ...resolved, [id]: 'loading' };
    queue.push(id);
  }
  for (const listener of listeners) listener();
  pump();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

const EMPTY: Record<string, TResolvedChat> = {};

/** `ids` are chats on screen that the loaded pages lack; `enabled` waits for the chat sign-in. */
const useChatRefs = (ids: readonly string[], enabled: boolean): Readonly<Record<string, TResolvedChat>> => {
  const snapshot = useSyncExternalStore(
    subscribe,
    () => resolved,
    () => EMPTY,
  );
  const idsKey = ids.join(',');
  useEffect(() => {
    if (enabled && idsKey) request(idsKey.split(','));
  }, [enabled, idsKey]);
  return snapshot;
};

export default useChatRefs;
