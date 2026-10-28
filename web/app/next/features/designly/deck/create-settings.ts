// What a new deck is asked to be, as the create screen holds it: the settings sheet's choices and what they come to
// (the auto slide count, the words, the seconds per slide), and the request's `deck` they are sent as. No React here.
import { DESIGN_DECK_TONES, type DesignDeckTone, type DesignlyDeck } from '@/next/protocol/v2';
import type { IDeckPptxStyle } from '@/next/features/designly/deck/types';

/** The minutes the settings offer, as the board's buttons. */
export const DECK_DURATIONS = [5, 10, 15, 20, 30, 45] as const;
/** The words a minute of speech takes, as the board counts. */
export const DECK_WORDS_PER_MINUTE = 130;
/** The slides a stepper can set (the runtime's bounds). */
export const DECK_SLIDES_MIN = 2;
export const DECK_SLIDES_MAX = 40;
export const DECK_TONES = DESIGN_DECK_TONES;

export interface IDeckCreateSettings {
  /** null: the length does not matter, the agent decides. */
  minutes: number | null;
  /** null: the agent picks the count. */
  slides: number | null;
  tone: DesignDeckTone;
  script: boolean;
  /** The company template's style is used; false keeps a template attached but unused. */
  useStyle: boolean;
}

export const DEFAULT_CREATE_SETTINGS: IDeckCreateSettings = {
  minutes: null,
  slides: null,
  tone: 'talk',
  script: true,
  useStyle: true,
};

/** The count the agent would pick for a talk of `minutes`: one slide per two minutes, three at least. */
export const autoSlides = (minutes: number): number => Math.max(3, Math.round(minutes / 2));

/** The slides on screen: the set count, else the auto one; null when no length is set (the agent decides). */
export const slidesOf = (s: IDeckCreateSettings): number | null =>
  s.slides ?? (s.minutes === null ? null : autoSlides(s.minutes));

export const wordsOf = (minutes: number): number => minutes * DECK_WORDS_PER_MINUTE;

/** Seconds a slide gets; null while the length or the count is the agent's to decide. */
export const secondsPerSlide = (s: IDeckCreateSettings): number | null => {
  const slides = slidesOf(s);
  return s.minutes === null || slides === null ? null : Math.round((s.minutes * 60) / slides);
};

const clampSlides = (n: number): number => Math.min(DECK_SLIDES_MAX, Math.max(DECK_SLIDES_MIN, n));

/** The count the stepper's first press starts from when the agent decides everything. */
const STEP_START = 5;

/** The stepper's step from what shows now; the first press sets the count by hand. */
export const stepSlides = (s: IDeckCreateSettings, by: 1 | -1): IDeckCreateSettings => ({
  ...s,
  slides: clampSlides((slidesOf(s) ?? STEP_START) + by),
});

/** The request's `deck`; the template's vars only when it is attached and in use. */
export const toWireDeck = (s: IDeckCreateSettings, style: IDeckPptxStyle | null): DesignlyDeck => ({
  minutes: s.minutes ?? 0,
  slides: s.slides,
  tone: s.tone,
  script: s.script,
  style: style && s.useStyle ? { source: style.name, vars: style.vars, layouts: style.layouts } : null,
});

export const isPptx = (name: string): boolean => name.toLowerCase().endsWith('.pptx');

/** The title of a new project from its brief, as Designly's start makes it. */
export const DECK_TITLE_MAX = 42;
export const deckTitleOf = (brief: string): string =>
  brief.length > DECK_TITLE_MAX ? `${brief.slice(0, DECK_TITLE_MAX)}…` : brief;
