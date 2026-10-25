// The conversations of every machine, from the convs channel: one record per conversation, replaced by rev. Unread
// is lastTurn.endedAt > seenAt; it is never an agent state.

import { createKeyedStore, sameItems, useAll, useKey } from '@/next/core/store';
import type { ConvId, Conversation, MachineId } from '@/next/protocol/v2';

export const conversationsStore = createKeyedStore<ConvId, Conversation>();
/** Whether a machine's convs snapshot has arrived: absent means loading, not empty. */
export const convsLoadedStore = createKeyedStore<MachineId, true>();

/**
 * Machines whose chat list on screen is what the browser kept last time, not yet the machine's own snapshot:
 * the sidebar draws it with the cached feed, so the page does not come up half drawn. The snapshot replaces it.
 */
export const convsCachedStore = createKeyedStore<MachineId, true>();

export const useConversation = (id: ConvId | null) => useKey(conversationsStore, id);

export const isUnread = (c: Conversation): boolean =>
  c.lastTurn !== null && c.lastTurn.endedAt !== null && c.lastTurn.endedAt > c.seenAt;

/** Pinned first, then by last activity. */
export const byActivity = (a: Conversation, b: Conversation): number =>
  Number(b.pinned) - Number(a.pinned) || b.lastActivityAt - a.lastActivityAt;

/** Whether the machine itself has answered with its list (the cached one does not count); for «the chat is not there». */
export function useConvsAnswered(machine: MachineId | null): boolean {
  return useAll(convsLoadedStore, (s) => (machine === null ? s.size > 0 : s.has(machine)));
}

/** A machine's conversations (or every machine's with null), sorted; null until its snapshot arrived or the cache drew it. */
export function useConversationList(machine: MachineId | null): Conversation[] | null {
  const answered = useConvsAnswered(machine);
  const cached = useAll(convsCachedStore, (s) => (machine === null ? s.size > 0 : s.has(machine)));
  const loaded = answered || cached;
  const list = useAll(
    conversationsStore,
    (s) => [...s.values()].filter((c) => machine === null || c.machineId === machine).sort(byActivity),
    sameItems,
  );
  return loaded ? list : null;
}
