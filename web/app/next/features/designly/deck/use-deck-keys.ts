// The deck's keys, from the one list of shortcuts (features/keys; the person may change them in Settings): the slides
// turn on the arrows, Page Up/Down and Space (Space not while editing), Home and End go to the ends, C comments (edit,
// link), F goes full screen (link), P shows and R records (R from the show too), Esc steps back; ⌥← and ⌥→ turn the
// slides even from the chat's field. Keys typed into a field are the field's. The same keys come from inside the board
// as `tk:key` while it has the focus (use-deck-bridge.ts).
import { KEY_SCOPE } from '@/next/features/keys/commands';
import { useKeys, type TKeyHandlers } from '@/next/features/keys/use-keys';
import { DECK_SCREEN, type TDeckScreen } from '@/next/features/designly/deck/types';

export { isTypingTarget } from '@/next/features/keys/use-keys';

export const DECK_KEY = {
  next: 'next',
  prev: 'prev',
  first: 'first',
  last: 'last',
  comment: 'comment',
  full: 'full',
  escape: 'escape',
  show: 'show',
  rec: 'rec',
} as const;
export type TDeckKey = (typeof DECK_KEY)[keyof typeof DECK_KEY];

/** The action of the list each deck key is. */
const COMMAND_OF: Record<TDeckKey, string> = {
  next: 'deck.next',
  prev: 'deck.prev',
  first: 'deck.first',
  last: 'deck.last',
  comment: 'deck.comment',
  full: 'deck.full',
  escape: 'deck.back',
  show: 'deck.show',
  rec: 'deck.rec',
};

/** Whether a deck key does something on a screen. */
export const deckKeyOn = (key: TDeckKey, screen: TDeckScreen): boolean => {
  if (key === DECK_KEY.comment) return screen === DECK_SCREEN.edit || screen === DECK_SCREEN.link;
  if (key === DECK_KEY.full) return screen === DECK_SCREEN.link;
  if (key === DECK_KEY.show) return screen === DECK_SCREEN.edit;
  if (key === DECK_KEY.rec) return screen === DECK_SCREEN.edit || screen === DECK_SCREEN.show;
  return true;
};

/** Space turns the slide everywhere but in the editor, where it is the board's. */
export const deckChordOn = (key: TDeckKey, chord: string, screen: TDeckScreen): boolean =>
  !(key === DECK_KEY.next && chord === 'Space' && screen === DECK_SCREEN.edit);

/** The deck's handlers of the list's actions on a screen. */
export const deckHandlers = (screen: TDeckScreen, onKey: (key: TDeckKey) => void): TKeyHandlers => {
  const handlers: TKeyHandlers = {
    'global.stepBack': () => onKey(DECK_KEY.prev),
    'global.stepOn': () => onKey(DECK_KEY.next),
  };
  for (const key of Object.values(DECK_KEY)) {
    if (!deckKeyOn(key, screen)) continue;
    handlers[COMMAND_OF[key]] = (chord) => {
      if (!deckChordOn(key, chord, screen)) return false;
      onKey(key);
    };
  }
  return handlers;
};

/** While `on`, the deck's keys run `onKey`; the keys it acts on do nothing else (no scroll on an arrow or Space). */
export const useDeckKeys = (on: boolean, screen: TDeckScreen, onKey: (key: TDeckKey) => void) => {
  useKeys(KEY_SCOPE.deck, deckHandlers(screen, onKey), on);
};
