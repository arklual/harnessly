// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import DeckGuest from '@/next/features/designly/deck/deck-guest';
import { DECK_OPENS } from '@/next/features/designly/deck/types';

// a guest's turn no longer drops the slide's frame and shows a white slot while the next one loads.

const slides = ['one', 'two', 'three', 'four'].map((n) => ({
  id: n,
  title: n,
  path: `${n}.dc.html`,
  src: `https://share.example/s/t/f/${n}.dc.html`,
  w: 1920,
  h: 1080,
}));

const show = () =>
  render(
    <DeckGuest
      title="Deck"
      slides={slides}
      video={null}
      pdf={null}
      start={DECK_OPENS.slides}
      comments={null}
    />,
  );

const frames = () => [...document.querySelectorAll<HTMLIFrameElement>('.hx-deck-guest__frame')];
const frameOf = (n: string) => frames().find((f) => f.src.endsWith(`/${n}.dc.html`));
const onScreen = () =>
  frames()
    .filter((f) => f.dataset.ready === '1')
    .map((f) => f.src.split('/').pop());

const loadAndDraw = (n: string, steps = 0) => {
  const frame = frameOf(n);
  if (!frame?.contentWindow) throw new Error(`no frame ${n}`);
  act(() => {
    fireEvent.load(frame);
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'tk:deck-ready', w: 1920, h: 1080, steps },
        source: frame.contentWindow,
      }),
    );
  });
};

const press = (code: string) => act(() => window.dispatchEvent(new KeyboardEvent('keydown', { code })));

beforeEach(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

describe('a guest turns the slides of a deck', () => {
  it('loads the neighbours once the first slide is drawn', () => {
    show();
    expect(frames()).toHaveLength(1);
    loadAndDraw('one');
    expect(onScreen()).toEqual(['one.dc.html']);
    expect(frames().map((f) => f.src.split('/').pop())).toEqual([
      'one.dc.html',
      'two.dc.html',
      'three.dc.html',
    ]);
  });

  it('keeps the slide on screen until the next one is drawn, then shows it at once on the turn back', () => {
    show();
    loadAndDraw('one');
    press('ArrowRight');
    // «two» is loading ahead and has not said it is drawn: «one» stays, nothing white in between.
    expect(onScreen()).toEqual(['one.dc.html']);
    loadAndDraw('two');
    expect(onScreen()).toEqual(['two.dc.html']);
    expect(frameOf('one')).toBeDefined();
    press('ArrowLeft');
    // «one» was drawn before: it is on screen with no load.
    expect(onScreen()).toEqual(['one.dc.html']);
  });

  it('turns to a slide drawn ahead without waiting', () => {
    show();
    loadAndDraw('one');
    loadAndDraw('two');
    press('ArrowRight');
    expect(onScreen()).toEqual(['two.dc.html']);
  });
});
