// The chat cache (spec §6): the 12 last conversations stay in memory with their turns, entries and cursor, so going
// back to one draws at once and its sub fetches only the delta. A conversation that is open is never let go.

import { batch } from '@/next/core/store';
import { forgetResume } from '@/next/core/realtime/cursors';
import { dropConversationData, feedStore } from '@/next/stores/conversation';
import { dropConversationMessages } from '@/next/stores/messages';
import type { ConvId } from '@/next/protocol/v2';

export const MEMORY_CHATS = 12;
export const STORED_CHATS = 6;
export const STORED_TURNS = 3;

/** Most recent first. */
let recent: ConvId[] = [];
const held = new Map<ConvId, number>();
const listeners = new Set<() => void>();

export function touchConversation(conv: ConvId): void {
  if (recent[0] === conv) return;
  recent = [conv, ...recent.filter((c) => c !== conv)];
  evict();
  for (const l of [...listeners]) l();
}

/** Keeps a conversation in the cache while the returned release is not called. */
export function holdConversation(conv: ConvId): () => void {
  held.set(conv, (held.get(conv) ?? 0) + 1);
  touchConversation(conv);
  let done = false;
  return () => {
    if (done) return;
    done = true;
    const n = (held.get(conv) ?? 1) - 1;
    if (n > 0) held.set(conv, n);
    else held.delete(conv);
    evict();
  };
}

export const recentConversations = (n = MEMORY_CHATS): ConvId[] => recent.slice(0, n);

/** Hears every change of the recent list (the IndexedDB writer). */
export function onRecentChange(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function evict(): void {
  if (recent.length <= MEMORY_CHATS) return;
  const keep: ConvId[] = [];
  const gone: ConvId[] = [];
  recent.forEach((c, i) => (i < MEMORY_CHATS || held.has(c) ? keep : gone).push(c));
  if (gone.length === 0) return;
  recent = keep;
  batch(() => {
    for (const c of gone) {
      const machine = feedStore.get(c)?.machineId;
      dropConversationData(c);
      dropConversationMessages(c);
      if (machine) forgetResume(machine, `conv:${c}`);
    }
  });
}

export function resetCache(): void {
  recent = [];
  held.clear();
}
