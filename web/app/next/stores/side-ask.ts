// Words of a chat's feed sent to its side chat as a question to write («Спросить сбоку» over picked words,
// features/chat/selbar.tsx): the pane opens the side chat (bindings/right-pane.tsx) and its field takes the words as a
// quote (features/pane/side-chat.tsx), then it is taken off here. One at a time: a newer one replaces an untaken one.
import { createStore, useStore } from '@/next/core/store';
import type { ConvId } from '@/next/protocol/v2';

export interface ISideAsk {
  conv: ConvId;
  quote: string;
  /** Tells two asks of the same words apart. */
  nonce: number;
}

const sideAsk = createStore<ISideAsk | null>(null);
let asked = 0;

export const askBeside = (conv: ConvId, quote: string): void => {
  asked += 1;
  sideAsk.set({ conv, quote, nonce: asked });
};

/** The field has the words: the ask is done. */
export const takeSideAsk = (nonce: number): void => {
  if (sideAsk.get()?.nonce === nonce) sideAsk.set(null);
};

/** Hears every new ask, as it is made. */
export const onSideAsk = (listener: (ask: ISideAsk) => void): (() => void) =>
  sideAsk.subscribe(() => {
    const ask = sideAsk.get();
    if (ask) listener(ask);
  });

/** The ask waiting for this chat's side chat, if any. */
export const useSideAsk = (conv: ConvId | null): ISideAsk | null => {
  const ask = useStore(sideAsk);
  return ask !== null && ask.conv === conv ? ask : null;
};
