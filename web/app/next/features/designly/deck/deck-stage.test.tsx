// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { createRef, type RefObject } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DeckStage, { DECK_FADE_MS, type IDeckStageProps } from '@/next/features/designly/deck/deck-stage';

const A = 'https://slot.example/a.dc.html';
const B = 'https://slot.example/b.dc.html';
const C = 'https://slot.example/c.dc.html';

const frames = () => [...document.querySelectorAll<HTMLIFrameElement>('.hx-deck__frame')];
const frameOf = (src: string) => frames().find((f) => f.getAttribute('src') === src);

/** The board in a frame says it is drawn. */
const drawn = (src: string, steps = 0) => {
  const frame = frameOf(src);
  if (!frame?.contentWindow) throw new Error(`no frame ${src}`);
  act(() => {
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'tk:deck-ready', w: 1920, h: 1080, steps },
        source: frame.contentWindow,
      }),
    );
  });
};

describe('DeckStage', () => {
  const onLoad = vi.fn();
  const onAhead = vi.fn();
  const onReady = vi.fn();
  let frameRef: RefObject<HTMLIFrameElement | null>;
  let painted = 0;

  const props = (src: string, ahead: string[]): IDeckStageProps => ({
    src,
    w: 1920,
    h: 1080,
    n: 1,
    hold: null,
    frameRef,
    ahead,
    onAhead,
    onReady,
    onLoad,
    painted,
    error: null,
    onReload: () => undefined,
  });
  const ui = (src: string, ahead: string[]) => <DeckStage {...props(src, ahead)} />;

  beforeEach(() => {
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        disconnect() {}
      },
    );
    vi.useFakeTimers();
    frameRef = createRef<HTMLIFrameElement>();
    painted = 0;
    onLoad.mockClear();
    onAhead.mockClear();
    onReady.mockClear();
  });
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  /** The first slide on screen: loaded, drawn (the deck's bridge says so through `painted`). */
  const open = (ahead: string[]) => {
    const view = render(ui(A, ahead));
    fireEvent.load(frameOf(A)!);
    painted = 1;
    view.rerender(ui(A, ahead));
    return view;
  };

  it('loads the neighbours out of sight only once the slide on screen is drawn', () => {
    const view = render(ui(A, [B]));
    expect(frames().map((f) => f.getAttribute('src'))).toEqual([A]);
    fireEvent.load(frameOf(A)!);
    view.rerender(ui(A, [B]));
    expect(frames()).toHaveLength(1);
    painted = 1;
    view.rerender(ui(A, [B]));
    expect(frames().map((f) => f.getAttribute('src'))).toEqual([A, B]);
    expect(frameOf(A)!.dataset.on).toBe('1');
    expect(frameOf(B)!.dataset.on).toBe('0');
    expect(frameOf(B)!.dataset.top).toBe('0');
  });

  it('tells a frame loaded ahead to say when it is drawn', () => {
    open([B]);
    fireEvent.load(frameOf(B)!);
    expect(onAhead).toHaveBeenCalledWith(frameOf(B), B);
    expect(onLoad).toHaveBeenCalledTimes(1);
  });

  it('turns to a slide that was drawn ahead without loading it again, and hands over what the board said', () => {
    const view = open([B]);
    const ahead = frameOf(B)!;
    fireEvent.load(ahead);
    drawn(B, 3);
    onLoad.mockClear();
    view.rerender(ui(B, [A, C]));
    // The same document, on top and on screen at once.
    expect(frameOf(B)).toBe(ahead);
    expect(ahead.dataset.top).toBe('1');
    expect(ahead.dataset.on).toBe('1');
    expect(ahead.dataset.quick).toBe('1');
    expect(frameRef.current).toBe(ahead);
    expect(onLoad).toHaveBeenCalledTimes(1);
    expect(onReady).toHaveBeenCalledWith({ w: 1920, h: 1080, steps: 3 });
    // The slide just left stays loaded for the turn back, and the next one comes after it.
    act(() => {
      vi.advanceTimersByTime(DECK_FADE_MS);
    });
    expect(frames().map((f) => f.getAttribute('src'))).toEqual([A, B, C]);
    expect(frameOf(A)!.dataset.on).toBe('0');
  });

  it('keeps the old slide on screen until a slide that was not loaded ahead is drawn', () => {
    const view = open([B]);
    view.rerender(ui(C, [B]));
    expect(frameOf(A)!.dataset.on).toBe('1');
    expect(frameOf(C)!.dataset.on).toBe('0');
    fireEvent.load(frameOf(C)!);
    expect(frameOf(C)!.dataset.on).toBe('0');
    painted = 2;
    view.rerender(ui(C, [B]));
    expect(frameOf(C)!.dataset.on).toBe('1');
    act(() => {
      vi.advanceTimersByTime(DECK_FADE_MS);
    });
    // A is neither on screen nor a neighbour any more.
    expect(frames().map((f) => f.getAttribute('src'))).toEqual([B, C]);
  });

  it('waits for the board’s word that it is drawn when it was loaded ahead but not drawn yet', () => {
    const view = open([B]);
    fireEvent.load(frameOf(B)!);
    view.rerender(ui(B, [A]));
    expect(frameOf(B)!.dataset.on).toBe('0');
    painted = 2;
    view.rerender(ui(B, [A]));
    expect(frameOf(B)!.dataset.on).toBe('1');
  });
});
