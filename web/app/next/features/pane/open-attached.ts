// A place attached to a message opens in the chat's right pane: from its chip in the field or in the feed, and a design
// at once when it is attached. The pane of the chat on screen listens (bindings/right-pane.tsx); nothing else does.
import type { MessageRef } from '@/next/protocol/v2';

type TListener = (ref: Pick<MessageRef, 'kind' | 'id'>) => void;

const listeners = new Set<TListener>();

export const openAttached = (ref: Pick<MessageRef, 'kind' | 'id'>): void => {
  listeners.forEach((listen) => listen(ref));
};

/** The pane listens while it is mounted; the returned function stops it. */
export const onOpenAttached = (listen: TListener): (() => void) => {
  listeners.add(listen);
  return () => listeners.delete(listen);
};
