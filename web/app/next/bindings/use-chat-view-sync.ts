// The chat's view goes to the machine and comes back from it (stores/chat-view.ts): what the person arranged on this
// device — the screen, the chat's place, the pane, the feed's place — is sent after a pause (every EXPORT_MS at most,
// so the feed's scrolling is not a write per line), and a newer one that another device sent is put into this device's
// stores as the chats arrive. A machine without the feature, and a chat nobody arranged, send and get nothing.
import { useEffect, useRef } from 'react';
import { setConversationView } from '@/next/commands';
import type { ConvId } from '@/next/protocol/v2';
import { ROUTE, type TRoute } from '@/next/routes/routes';
import { screenOf } from '@/next/stores/chat-screen';
import {
  applyView,
  isPlain,
  noteSeenAt,
  parseView,
  seenAt,
  viewOf,
  viewSignature,
} from '@/next/stores/chat-view';
import { useConversation, useConversationList } from '@/next/stores/conversations';
import { hasFeature } from '@/next/stores/machines';
import { chatPanes } from '@/next/stores/pane-state';

/** The longest the page waits before it sends a change of the view. */
export const EXPORT_MS = 2000;

export const useChatViewSync = (route: TRoute) => {
  const conv = (route.kind === ROUTE.chat ? route.id : (screenOf(route)?.conv ?? null)) as ConvId | null;
  const record = useConversation(conv);
  const list = useConversationList(null);
  // What the machine has, as this device last saw it: a view equal to it is not sent again.
  const sent = useRef(new Map<string, string>());

  useEffect(() => {
    for (const c of list ?? []) {
      if (!c.view || !hasFeature(c.machineId, 'chat-view')) continue;
      const view = parseView(c.view);
      const seen = seenAt(c.id);
      if (!view || view.at <= seen) continue;
      // The chat on screen takes the machine's view only once. Even the first snapshot must not undo a pane edit
      // made before the export timer had a chance to send it (and set seenAt).
      if (c.id === conv && seen !== 0) continue;
      const keepPane = c.id === conv && chatPanes.changedHere(c.id);
      applyView(c.id, c.machineId, view, { keepPane });
      const applied = viewOf(c.id);
      // The preserved pane is still local: compare the next export with what the machine actually has.
      sent.current.set(c.id, viewSignature(keepPane ? { ...applied, pane: view.pane } : applied));
    }
  }, [list, conv]);

  const machine = record?.machineId ?? null;
  const kept = record?.view;
  useEffect(() => {
    if (!conv || !machine || !hasFeature(machine, 'chat-view')) return;
    const send = () => {
      const view = viewOf(conv);
      const signature = viewSignature(view);
      const known = sent.current.get(conv);
      if (signature === known) return;
      if (known === undefined && !kept && isPlain(view)) return;
      sent.current.set(conv, signature);
      const at = Date.now();
      noteSeenAt(conv, at);
      void setConversationView(machine, conv, { ...view, at });
    };
    const timer = window.setInterval(send, EXPORT_MS);
    window.addEventListener('pagehide', send);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('pagehide', send);
    };
  }, [conv, machine, kept]);
};
