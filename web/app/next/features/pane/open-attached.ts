// A place attached to a message opens in the chat's right pane: from its chip in the field or in the feed, and a design
// at once when it is attached. The pane of the chat on screen listens (bindings/right-pane.tsx); nothing else does. A
// chat is no pane's: its chip opens the chat itself.
import { goTo, ROUTE } from '@/next/routes/routes';
import type { MessageRef } from '@/next/protocol/v2';

type TListener = (ref: Pick<MessageRef, 'kind' | 'id'>) => void;

const listeners = new Set<TListener>();

export const openAttached = (ref: Pick<MessageRef, 'kind' | 'id'>): void => {
  if (ref.kind === 'chat') {
    goTo({ kind: ROUTE.chat, id: ref.id });
    return;
  }
  listeners.forEach((listen) => listen(ref));
};

/** The pane listens while it is mounted; the returned function stops it. */
export const onOpenAttached = (listen: TListener): (() => void) => {
  listeners.add(listen);
  return () => listeners.delete(listen);
};
