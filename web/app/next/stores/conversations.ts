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

let ordered: { ver: number; list: readonly Conversation[] } | null = null;

/**
 * Every machine's conversations in the sidebar's order, one list for all its readers per change of the store. An
 * event that changes a chat without moving it (its state, its title, a seen mark) keeps the order as it was: the
 * list is sorted again only when a chat came, went, or its place could have changed.
 */
export function conversationsByActivity(): readonly Conversation[] {
  const ver = conversationsStore.version();
  if (ordered && ordered.ver === ver && ordered.list.length === conversationsStore.size) return ordered.list;
  ordered = { ver, list: reorder(ordered ? ordered.list : null) };
  return ordered.list;
}

function reorder(prev: readonly Conversation[] | null): readonly Conversation[] {
  if (prev && prev.length === conversationsStore.size) {
    let changed = false;
    let moved = false;
    const next = prev.map((was) => {
      const now = conversationsStore.get(was.id);
      if (now === was) return was;
      changed = true;
      if (!now || now.pinned !== was.pinned || now.lastActivityAt !== was.lastActivityAt) moved = true;
      return now ?? was;
    });
    if (!moved) return changed ? next : prev;
  }
  return [...conversationsStore.values()].sort(byActivity);
}

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
    () => conversationsByActivity().filter((c) => machine === null || c.machineId === machine),
    sameItems,
  );
  return loaded ? list : null;
}
