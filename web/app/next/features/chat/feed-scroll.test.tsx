// @vitest-environment jsdom
// The feed and the reader's position (the owner, 14.11): away from the bottom nothing moves the feed; at the bottom it
// follows the turn; a finished answer taller than the screen is read from its start, and the feed stops following.
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Feed, { type IFeedProps } from '@/next/features/chat/feed';
import { forgetPlaces } from '@/next/features/chat/feed-place';
import type { FeedItem, TurnItem } from '@/next/features/chat/types';
import { noteExit } from '@/next/ui/flight';

const VIEW = 600;
const DOCK = 200;

/** The feed's layout, laid out by hand: jsdom has none. */
const geo = { height: 3000, answerTop: 0, answerHeight: 0, top: 0 };

let observers: ResizeObserverCallback[] = [];
class FakeObserver {
  constructor(callback: ResizeObserverCallback) {
    observers.push(callback);
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}
/** Something in the feed changed height: what the observer would say after layout. */
const resized = () => act(() => observers.forEach((callback) => callback([], {} as ResizeObserver)));

const rect = (top: number, height: number) =>
  ({
    top,
    bottom: top + height,
    height,
    left: 0,
    right: 800,
    width: 800,
    x: 0,
    y: top,
    toJSON: () => ({}),
  }) as DOMRect;

const turnItem = (key: string, status: 'live' | 'done', answer: string | null): TurnItem =>
  ({
    kind: 'turn',
    key,
    turn: {
      id: key,
      author: { provider: 'claude', role: null },
      startedAt: 1,
      endedAt: status === 'done' ? 2 : null,
      durationMs: 1000,
      toolCount: 0,
      stepCount: 1,
      status,
      failure: null,
      answerEntryId: null,
      files: [],
      outputs: [],
    },
    prompt: null,
    woke: null,
    peer: null,
    note: null,
    steps: [],
    answer: answer === null ? null : { md: answer },
    now: null,
    wait: null,
    endNote: null,
  }) as unknown as TurnItem;

const outgoing = (clientId: string) =>
  ({
    kind: 'outgoing',
    key: `out:${clientId}`,
    message: { clientId, text: 'ещё вопрос', status: 'sending', attachments: [] },
  }) as unknown as FeedItem;

const done = () => Promise.resolve();
const props = (items: FeedItem[]): IFeedProps => ({
  items,
  hasOlder: false,
  loadingOlder: false,
  onLoadOlder: () => {},
  density: 'compact',
  offline: false,
  enterAfter: Number.MAX_SAFE_INTEGER,
  machineName: 'm',
  onContinueTurn: done,
  onRetryTurn: done,
  onRetryMessage: done,
  onDeleteMessage: done,
});

const history = [turnItem('t1', 'done', 'старый ответ'), turnItem('t2', 'done', 'ещё один')];

/** Renders the feed over the hand-made layout; returns its scroller. */
const open = (items: FeedItem[]) => {
  const view = render(<Feed {...props(items)} />);
  const scroller = view.container.querySelector<HTMLElement>('.hx-feed')!;
  Object.defineProperty(scroller, 'scrollTop', {
    configurable: true,
    get: () => geo.top,
    set: (value: number) => {
      geo.top = Math.max(0, Math.min(value, geo.height - VIEW));
    },
  });
  view.rerender(<Feed {...props([...items])} />);
  return { ...view, scroller, show: (next: FeedItem[]) => view.rerender(<Feed {...props(next)} />) };
};

/** The reader moves the feed: a wheel up, then the scroll event. */
const readerScrollsTo = (scroller: HTMLElement, top: number) => {
  fireEvent.wheel(scroller, { deltaY: top - geo.top });
  geo.top = top;
  fireEvent.scroll(scroller);
};

beforeEach(() => {
  forgetPlaces();
  observers = [];
  Object.assign(geo, { height: 3000, answerTop: 0, answerHeight: 0, top: 0 });
  vi.stubGlobal('ResizeObserver', FakeObserver);
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(() => geo.height);
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(() => VIEW);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    if (this.classList.contains('hx-answer')) return rect(geo.answerTop - geo.top, geo.answerHeight);
    return rect(0, VIEW);
  });
  vi.spyOn(window, 'getComputedStyle').mockImplementation(
    () => ({ paddingBottom: `${DOCK}px`, display: 'block' }) as unknown as CSSStyleDeclaration,
  );
  HTMLElement.prototype.scrollTo = function scrollTo(this: HTMLElement, options?: ScrollToOptions | number) {
    if (typeof options === 'object') this.scrollTop = options.top ?? this.scrollTop;
  };
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('the feed away from the bottom', () => {
  it('holds still when the agent answers, the turn streams, folds or a card comes; «To new» says a message came', () => {
    const { scroller, show, container } = open(history);
    expect(geo.top).toBe(geo.height - VIEW);
    readerScrollsTo(scroller, 900);

    // A new turn comes and streams: the feed is taller each time, the reader's place is the same.
    geo.height = 3400;
    show([...history, turnItem('t3', 'live', null)]);
    resized();
    expect(geo.top).toBe(900);
    geo.height = 4200;
    resized();
    expect(geo.top).toBe(900);

    // The turn ends and folds its work: shorter, still the same place.
    geo.height = 3600;
    show([...history, turnItem('t3', 'done', 'итог')]);
    resized();
    expect(geo.top).toBe(900);
    expect(container.querySelector('.hx-tonew')?.getAttribute('data-shown')).toBe('true');
    expect(container.querySelector('.hx-tonew')?.textContent).toBe('New message');
  });

  it('is not pulled down by a queued message the runtime hands over, only by one the person sends', () => {
    const { scroller, show } = open(history);
    readerScrollsTo(scroller, 700);
    // The queue's row left by itself when the turn ended: it flies, the reader stays.
    noteExit('q1', { left: 0, top: 0, width: 10, height: 10 }, 'ещё вопрос');
    geo.height = 3200;
    show([...history, outgoing('q1')]);
    resized();
    expect(geo.top).toBe(700);
  });

  it('a shrink that pulls the position up does not count as the reader coming back to the bottom', () => {
    const { scroller, show } = open(history);
    readerScrollsTo(scroller, 2300);
    // The turn below folded: the browser clamps the position, the reader did not move.
    geo.height = 2850;
    geo.top = 2250;
    fireEvent.scroll(scroller);
    geo.height = 3500;
    show([...history, turnItem('t3', 'live', null)]);
    resized();
    expect(geo.top).toBe(2250);
  });
});

describe('the feed at the bottom', () => {
  it('follows the turn while the agent works', () => {
    const { show } = open(history);
    geo.height = 3300;
    show([...history, turnItem('t3', 'live', 'пишу')]);
    expect(geo.top).toBe(3300 - VIEW);
    geo.height = 3900;
    resized();
    expect(geo.top).toBe(3900 - VIEW);
  });

  it('a long finished answer stands with its start at the top, and the feed does not pull it down after', () => {
    const { show, container } = open(history);
    geo.height = 5000;
    geo.answerTop = 3200;
    geo.answerHeight = 1500;
    show([...history, turnItem('t3', 'done', 'длинный итог')]);
    expect(geo.top).toBe(3200 - 16);
    expect(container.querySelector('.hx-tonew')?.getAttribute('data-shown')).toBe('true');
    expect(container.querySelector('.hx-tonew')?.textContent).toBe('Down');
    // A picture or a code block in it loads: the reader keeps reading from where they are.
    geo.height = 5400;
    resized();
    expect(geo.top).toBe(3200 - 16);
  });

  it('a short finished answer just stays at the bottom', () => {
    const { show } = open(history);
    geo.height = 3300;
    geo.answerTop = 3000;
    geo.answerHeight = 200;
    show([...history, turnItem('t3', 'done', 'коротко')]);
    expect(geo.top).toBe(3300 - VIEW);
  });

  it('the answers already there when the chat opens do not move it off the bottom', () => {
    geo.answerTop = 1200;
    geo.answerHeight = 1500;
    open(history);
    resized();
    expect(geo.top).toBe(geo.height - VIEW);
  });
});
