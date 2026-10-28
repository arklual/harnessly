// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import {
  deckViewOf,
  pdfAllowed,
  readDeckLinkView,
  startOpens,
  statsOn,
} from '@/next/features/designly/deck/deck-link-view';
import { createSeenTracker } from '@/next/features/designly/deck/guest-seen';
import { chaptersOf, clock, currentChapter } from '@/next/features/designly/deck/video-chapters';
import { mediaBaseOf, readGuestConfig, seenUrlOf } from '@/next/features/designly/guest/guest-config';

const config = (extra: Record<string, unknown>) => {
  const doc = document.implementation.createHTMLDocument('x');
  const el = doc.createElement('script');
  el.id = 'guest-config';
  el.textContent = JSON.stringify({
    v: 1,
    token: 'tok',
    title: 'Deck',
    rights: 'view',
    boards: [{ id: 'A', title: 'One', path: 'one.dc.html', w: 1920, h: 1080 }],
    filesBase: '/s/tok/f/',
    commentsUrl: '/s/tok/comments',
    ...extra,
  });
  doc.body.append(el);
  return readGuestConfig(doc);
};

describe('guest config of a deck', () => {
  it('reads kind and deck', () => {
    const c = config({
      kind: 'slides',
      deck: {
        video: {
          chapters: [
            { page: 'A', title: 'One', t: 0 },
            { page: '', t: 1 },
          ],
        },
        pdf: true,
      },
    });
    expect(c?.kind).toBe('slides');
    expect(c?.deck).toEqual({ video: { chapters: [{ page: 'A', title: 'One', t: 0 }] }, pdf: true });
    expect(c?.mediaBase).toBe('/s/tok/m/');
    expect(c?.seenUrl).toBe('/s/tok/seen');
  });
  it('takes missing or null fields as no deck', () => {
    expect(config({})?.kind).toBeNull();
    expect(config({})?.deck).toBeNull();
    expect(config({ kind: 'slides', deck: null })?.deck).toBeNull();
    expect(config({ kind: 'bogus' })?.kind).toBeNull();
  });
  it('derives addresses', () => {
    expect(mediaBaseOf('./f/')).toBe('./m/');
    expect(seenUrlOf('')).toBe('./seen');
  });
});

describe('deck link view', () => {
  it('round-trips and starts as asked', () => {
    const view = deckViewOf({ opens: 'video', pdf: true, stats: false });
    expect(readDeckLinkView(view)).toEqual({ opens: 'video', pdf: true, stats: false });
    expect(startOpens(view, true)).toBe('video');
    expect(startOpens(view, false)).toBe('slides');
    expect(startOpens(null, true)).toBe('slides');
    expect(startOpens({ deck: 'full' }, false)).toBe('full');
  });
  it('offers the pdf only when allowed and published', () => {
    expect(pdfAllowed({ pdf: true }, true)).toBe(true);
    expect(pdfAllowed({ pdf: true }, false)).toBe(false);
    expect(pdfAllowed({ pdf: false }, true)).toBe(false);
    expect(pdfAllowed(null, true)).toBe(false);
    expect(statsOn({ stats: true })).toBe(true);
    expect(statsOn({})).toBe(false);
  });
});

describe('chapters', () => {
  it('ties chapters to slides and finds the current', () => {
    const list = chaptersOf(
      [
        { page: 'B', title: '', t: 4000 },
        { page: 'A', title: 'Intro', t: 0 },
        { page: 'X', title: 'gone', t: 9 },
      ],
      [
        { id: 'A', title: 'One' },
        { id: 'B', title: 'Two' },
      ],
    );
    expect(list).toEqual([
      { slide: 0, title: 'Intro', t: 0 },
      { slide: 1, title: 'Two', t: 4000 },
    ]);
    expect(currentChapter([0, 4000], 3999)).toBe(0);
    expect(currentChapter([0, 4000], 4000)).toBe(1);
    expect(currentChapter([], 5)).toBeNull();
    expect(clock(372_000)).toBe('6:12');
  });
});

describe('seen tracker', () => {
  it('reports a board when it changes, in pieces of at most two minutes', () => {
    let now = 0;
    const sent: { board: string; ms: number }[] = [];
    const tr = createSeenTracker(
      () => now,
      (r) => sent.push(r),
    );
    tr.show('a');
    now = 500;
    tr.show('b');
    expect(sent).toEqual([]);
    now = 500 + 250_000;
    tr.show(null);
    expect(sent).toEqual([
      { board: 'b', ms: 120_000 },
      { board: 'b', ms: 120_000 },
      { board: 'b', ms: 10_000 },
    ]);
  });
});
