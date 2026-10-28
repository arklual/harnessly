// The deck's line to the board on screen: only messages from that frame's window count (the stage keeps a second frame
// for the crossfade, which is not listened to until it is the one on screen), read through ./bridge. Keys pressed while
// the board has the focus come over as `tk:key` and are the deck's keys (use-deck-keys.ts).
import { useEffect, useEffectEvent, useRef, type RefObject } from 'react';
import {
  postDeck,
  readDeckMessage,
  type TDeckBoardMessage,
  type TDeckHostMessage,
} from '@/next/features/designly/deck/bridge';

/** The keys the board hands to the deck, as `KeyboardEvent.key`. */
const BOARD_KEYS: Record<string, string> = {
  Escape: 'Escape',
  f: 'f',
  c: 'c',
  p: 'p',
  r: 'r',
  ArrowLeft: 'ArrowLeft',
  ArrowRight: 'ArrowRight',
  Space: ' ',
  ' ': ' ',
};

/** A `tk:key` of the board as a `KeyboardEvent.key`; null for anything else. */
export const readBoardKey = (data: unknown): string | null => {
  if (!data || typeof data !== 'object') return null;
  const m = data as Record<string, unknown>;
  if (m.type !== 'tk:key' || typeof m.key !== 'string') return null;
  return Object.hasOwn(BOARD_KEYS, m.key) ? BOARD_KEYS[m.key] : null;
};

export interface IDeckBridge {
  /** Sends to the board on screen; nothing while there is none. */
  post: (message: TDeckHostMessage) => void;
}

/**
 * `frame` is the board on screen and `src` its address (the origin a message goes to). `onMessage` gets the board's
 * deck messages, `onKey` its keys.
 */
export const useDeckBridge = (
  frame: RefObject<HTMLIFrameElement | null>,
  src: string | null,
  onMessage: (message: TDeckBoardMessage) => void,
  onKey: (key: string) => void,
): IDeckBridge => {
  const take = useEffectEvent(onMessage);
  const key = useEffectEvent(onKey);
  const srcRef = useRef(src);
  useEffect(() => {
    srcRef.current = src;
  }, [src]);
  useEffect(() => {
    const listen = (event: MessageEvent) => {
      const win = frame.current?.contentWindow;
      if (!win || event.source !== win) return;
      const read = readDeckMessage(event.data);
      if (read) {
        take(read);
        return;
      }
      const pressed = readBoardKey(event.data);
      if (pressed !== null) key(pressed);
    };
    window.addEventListener('message', listen);
    return () => window.removeEventListener('message', listen);
  }, [frame]);
  return {
    post: (message) => postDeck(frame.current, srcRef.current, message),
  };
};
