import { describe, expect, it } from 'vitest';
import {
  DECK_SLIDES_MAX,
  DECK_SLIDES_MIN,
  DEFAULT_CREATE_SETTINGS,
  autoSlides,
  deckTitleOf,
  isPptx,
  secondsPerSlide,
  slidesOf,
  stepSlides,
  toWireDeck,
  wordsOf,
} from '@/next/features/designly/deck/create-settings';

const base = DEFAULT_CREATE_SETTINGS;

describe('create settings', () => {
  it('picks max(3, round(m / 2)) slides on its own', () => {
    expect(autoSlides(5)).toBe(3);
    expect(autoSlides(10)).toBe(5);
    expect(autoSlides(15)).toBe(8);
    expect(autoSlides(30)).toBe(15);
    expect(autoSlides(45)).toBe(23);
  });

  it('leaves the length and the count to the agent by default', () => {
    expect(base.minutes).toBeNull();
    expect(base.slides).toBeNull();
    expect(slidesOf(base)).toBeNull();
    expect(secondsPerSlide(base)).toBeNull();
    expect(toWireDeck(base, null)).toMatchObject({ minutes: 0, slides: null });
  });

  it('counts the words and the seconds a slide gets once a length is set', () => {
    const ten = { ...base, minutes: 10 };
    expect(wordsOf(10)).toBe(1300);
    expect(slidesOf(ten)).toBe(5);
    expect(secondsPerSlide(ten)).toBe(120);
    expect(secondsPerSlide({ ...ten, slides: 8 })).toBe(75);
    expect(secondsPerSlide({ ...base, slides: 8 })).toBeNull();
  });

  it('steps from the auto count and stays within 2..40', () => {
    const ten = { ...base, minutes: 10 };
    expect(stepSlides(ten, 1).slides).toBe(6);
    expect(stepSlides(ten, -1).slides).toBe(4);
    expect(stepSlides(base, 1).slides).toBe(6);
    expect(stepSlides({ ...base, slides: DECK_SLIDES_MIN }, -1).slides).toBe(DECK_SLIDES_MIN);
    expect(stepSlides({ ...base, slides: DECK_SLIDES_MAX }, 1).slides).toBe(DECK_SLIDES_MAX);
    expect(slidesOf({ ...base, minutes: 20 })).toBe(10);
  });

  it('sends the template only when it is attached and in use', () => {
    const style = {
      name: 'acme.pptx',
      colors: ['#111', '#222', '#333'],
      fonts: { head: 'A', body: 'B' },
      layouts: 9,
      vars: { '--deck-bg': '#fff' },
    };
    expect(toWireDeck(base, null).style).toBeNull();
    expect(toWireDeck(base, style).style).toEqual({ source: 'acme.pptx', vars: style.vars, layouts: 9 });
    expect(toWireDeck({ ...base, useStyle: false }, style).style).toBeNull();
    expect(toWireDeck({ ...base, slides: 7, tone: 'biz', script: false }, null)).toEqual({
      minutes: 0,
      slides: 7,
      tone: 'biz',
      script: false,
      style: null,
    });
  });

  it('knows a template by its name and cuts a long title', () => {
    expect(isPptx('Template.PPTX')).toBe(true);
    expect(isPptx('notes.md')).toBe(false);
    expect(deckTitleOf('a'.repeat(50))).toBe(`${'a'.repeat(42)}…`);
  });
});
