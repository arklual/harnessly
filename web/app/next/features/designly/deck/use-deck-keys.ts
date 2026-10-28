// The deck's keys, as the board has them: arrows and Page Up/Down turn the slides (Space too, except while editing),
// C comments (edit, link), F goes full screen (link), P shows and R records (R from the show too), Esc steps back.
// Russian layout letters do the same (с, а, з, к). Keys typed into a field or into editable text are the field's.
// The same keys come from inside the board as `tk:key` while it has the focus (use-deck-bridge.ts).
import { useEffect, useEffectEvent } from 'react';
import { DECK_SCREEN, type TDeckScreen } from '@/next/features/designly/deck/types';

export const DECK_KEY = {
  next: 'next',
  prev: 'prev',
  comment: 'comment',
  full: 'full',
  escape: 'escape',
  show: 'show',
  rec: 'rec',
} as const;
export type TDeckKey = (typeof DECK_KEY)[keyof typeof DECK_KEY];

const NEXT_KEYS = new Set(['ArrowRight', 'ArrowDown', 'PageDown']);
const PREV_KEYS = new Set(['ArrowLeft', 'ArrowUp', 'PageUp']);

/** What a key does on a screen; null when it does nothing there. */
export const deckKeyAction = (key: string, screen: TDeckScreen): TDeckKey | null => {
  if (NEXT_KEYS.has(key) || (key === ' ' && screen !== DECK_SCREEN.edit)) return DECK_KEY.next;
  if (PREV_KEYS.has(key)) return DECK_KEY.prev;
  if (key === 'Escape') return DECK_KEY.escape;
  const letter = key.toLowerCase();
  if ((letter === 'c' || letter === 'с') && (screen === DECK_SCREEN.edit || screen === DECK_SCREEN.link))
    return DECK_KEY.comment;
  if ((letter === 'f' || letter === 'а') && screen === DECK_SCREEN.link) return DECK_KEY.full;
  if ((letter === 'p' || letter === 'з') && screen === DECK_SCREEN.edit) return DECK_KEY.show;
  if ((letter === 'r' || letter === 'к') && (screen === DECK_SCREEN.edit || screen === DECK_SCREEN.show))
    return DECK_KEY.rec;
  return null;
};

/** A key typed where text is written: an input, a text area, editable text. */
export const isTypingTarget = (target: EventTarget | null): boolean => {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName);
};

/**
 * Listens to the window's keys while `on`, and calls `onKey` with the action; the keys it acts on do nothing else
 * (no scroll on an arrow or Space). A key with a modifier is the browser's.
 */
export const useDeckKeys = (on: boolean, screen: TDeckScreen, onKey: (key: TDeckKey) => void) => {
  const act = useEffectEvent(onKey);
  useEffect(() => {
    if (!on) return;
    const down = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
      if (isTypingTarget(event.target)) return;
      const action = deckKeyAction(event.key, screen);
      if (action === null) return;
      event.preventDefault();
      act(action);
    };
    window.addEventListener('keydown', down);
    return () => window.removeEventListener('keydown', down);
  }, [on, screen]);
};
