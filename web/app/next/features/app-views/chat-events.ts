// The chat's events a view can hear (events:<kind>), from what the page already holds of the chat: the
// agent's state for the end of a turn, the feed's entries for a tool call and a message. Only what happened is told,
// never what was said or passed: the view reads the chat with chat.read when it holds that right.
import type { ConvId, Entry } from '@/next/protocol/v2';
import { entryStore } from '@/next/stores/conversation';
import { conversationsStore } from '@/next/stores/conversations';
import type { TEventKind } from './bridge';

export type TEmit = (kind: TEventKind, params: Record<string, unknown>) => void;

/** What an entry of the feed is to the view: a tool call, a message, or nothing to tell. */
export const eventOfEntry = (e: Entry): { kind: TEventKind; params: Record<string, unknown> } | null => {
  switch (e.t) {
    case 'tool':
      return { kind: 'tool', params: { tool: e.tool, at: e.at } };
    case 'user':
      return { kind: 'message', params: { role: 'person', at: e.at } };
    case 'say':
      return { kind: 'message', params: { role: 'agent', at: e.at } };
    default:
      return null;
  }
};

/** Tells emit the events of a chat from now on; what the feed held before is not told again. */
export const watchChatEvents = (conv: ConvId, emit: TEmit, now = Date.now()): (() => void) => {
  const prefix = `${conv} `;
  const told = new Set<string>();
  let working = conversationsStore.get(conv)?.state.k === 'working';
  const offState = conversationsStore.subscribeKey(conv, () => {
    const state = conversationsStore.get(conv)?.state;
    if (!state) return;
    const was = working;
    working = state.k === 'working';
    if (was && !working) emit('turn', { state: state.k, at: state.since });
  });
  const offEntries = entryStore.subscribeAll((changed) => {
    for (const k of changed) {
      if (!k.startsWith(prefix) || told.has(k)) continue;
      const e = entryStore.get(k);
      if (!e || e.at < now) continue;
      const ev = eventOfEntry(e);
      if (!ev) continue;
      told.add(k);
      emit(ev.kind, ev.params);
    }
  });
  return () => {
    offState();
    offEntries();
  };
};
