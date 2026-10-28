// A deck while its agent makes it: the agent first makes the pages (the boards are there, blank) and then draws them one
// by one. What stands in for the slide on screen, and why the show and the recording cannot start yet. No React here.
import type { IDeckSlide } from '@/next/features/designly/deck/types';

/**
 * What stands in for the slide: the agent is making the slides (`making`: none yet, or this one is still blank), the
 * deck has no slides and the agent is idle (`none`), the slide on screen is blank and the agent is idle (`blank`).
 */
export const DECK_HOLD = { making: 'making', none: 'none', blank: 'blank' } as const;
export type TDeckHold = (typeof DECK_HOLD)[keyof typeof DECK_HOLD];

/** The deck's slide of a board; null while the deck is not read or the board is not one of its slides. */
export const slideOfBoard = (slides: readonly IDeckSlide[] | null, path: string): IDeckSlide | null =>
  slides?.find((s) => s.path === path) ?? null;

export interface IDeckHoldInput {
  /** The deck's boards. */
  total: number;
  /** The slide on screen; null while the deck is not read. */
  slide: IDeckSlide | null;
  /** The agent of the deck's chat is starting or working. */
  busy: boolean;
}

/** What stands in for the slide on screen; null shows the slide. */
export const deckHold = ({ total, slide, busy }: IDeckHoldInput): TDeckHold | null => {
  if (total === 0) return busy ? DECK_HOLD.making : DECK_HOLD.none;
  if (slide?.blank !== true) return null;
  return busy ? DECK_HOLD.making : DECK_HOLD.blank;
};

export interface IDeckPlayInput {
  total: number;
  /** The deck's slides; null while the deck is not read. */
  slides: readonly IDeckSlide[] | null;
  busy: boolean;
}

/** The hint of «Показать» and «Записать» when they cannot start (an i18n key), null when they can. */
export const deckPlayBlock = ({ total, slides, busy }: IDeckPlayInput): string | null => {
  const blank = slides?.some((s) => s.blank) === true;
  if (busy && (total === 0 || blank)) return 'next.deck.bar.playMaking';
  if (total === 0 || (slides !== null && slides.length > 0 && slides.every((s) => s.blank)))
    return 'next.deck.bar.playNone';
  return null;
};

/** The deck's slides that the project's boards do not list yet, as one key; '' when there are none. */
export const staleSlidesKey = (
  slides: readonly { path: string }[] | undefined,
  boards: readonly { path: string }[],
): string => {
  const known = new Set(boards.map((b) => b.path));
  return (slides ?? [])
    .filter((s) => !known.has(s.path))
    .map((s) => s.path)
    .join('\n');
};
