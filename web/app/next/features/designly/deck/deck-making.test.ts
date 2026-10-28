import { describe, expect, it } from 'vitest';
import {
  deckHold,
  deckPlayBlock,
  slideOfBoard,
  staleSlidesKey,
} from '@/next/features/designly/deck/deck-making';
import type { IDeckSlide } from '@/next/features/designly/deck/types';

const slide = (id: string, blank: boolean, version = 'v1'): IDeckSlide => ({
  id,
  path: `project/${id}.dc.html`,
  title: id,
  w: 1920,
  h: 1080,
  notes: '',
  voice: false,
  seconds: 0,
  version,
  blank,
});

describe('a deck being made', () => {
  it('holds the stage while there are no slides: making while the agent works, none when it is idle', () => {
    expect(deckHold({ total: 0, slide: null, busy: true })).toBe('making');
    expect(deckHold({ total: 0, slide: null, busy: false })).toBe('none');
  });

  it('holds a blank slide while the agent draws it and shows it as empty once the agent stops', () => {
    expect(deckHold({ total: 8, slide: slide('a', true), busy: true })).toBe('making');
    expect(deckHold({ total: 8, slide: slide('a', true), busy: false })).toBe('blank');
    expect(deckHold({ total: 8, slide: slide('a', false), busy: true })).toBeNull();
    // The deck not read yet: the slide shows as the server serves it.
    expect(deckHold({ total: 8, slide: null, busy: true })).toBeNull();
  });

  it('keeps the show and the recording until the slides are made', () => {
    const blank = [slide('a', false), slide('b', true)];
    expect(deckPlayBlock({ total: 0, slides: [], busy: true })).toBe('next.deck.bar.playMaking');
    expect(deckPlayBlock({ total: 2, slides: blank, busy: true })).toBe('next.deck.bar.playMaking');
    expect(deckPlayBlock({ total: 0, slides: [], busy: false })).toBe('next.deck.bar.playNone');
    expect(deckPlayBlock({ total: 1, slides: [slide('a', true)], busy: false })).toBe(
      'next.deck.bar.playNone',
    );
    expect(deckPlayBlock({ total: 2, slides: blank, busy: false })).toBeNull();
    expect(deckPlayBlock({ total: 2, slides: null, busy: true })).toBeNull();
  });

  it('finds a board’s slide by its path', () => {
    const slides = [slide('a', false), slide('b', true)];
    expect(slideOfBoard(slides, 'project/b.dc.html')?.id).toBe('b');
    expect(slideOfBoard(slides, 'project/x.dc.html')).toBeNull();
    expect(slideOfBoard(null, 'project/a.dc.html')).toBeNull();
  });

  it('tells the slides the project does not list yet, so the library is read again for them', () => {
    const slides = [slide('a', false), slide('b', true)];
    expect(staleSlidesKey(slides, [{ path: 'project/a.dc.html' }])).toBe('project/b.dc.html');
    expect(staleSlidesKey(slides, slides)).toBe('');
    expect(staleSlidesKey(undefined, [])).toBe('');
  });
});
