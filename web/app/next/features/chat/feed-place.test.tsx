// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Feed, { type IFeedProps } from '@/next/features/chat/feed';
import {
  AT_BOTTOM,
  MAX_PLACES,
  PLACE,
  forgetPlaces,
  placeAmong,
  placeOf,
  rememberPlace,
  scrollToPlace,
} from '@/next/features/chat/feed-place';
import type { FeedItem, ModelSwitchItem } from '@/next/features/chat/types';

const row = (n: number): ModelSwitchItem => ({
  kind: 'switch',
  key: `switch:${n}`,
  at: n,
  from: { id: 'opus', effort: null },
  to: { id: 'sonnet', effort: null },
});

const done = () => Promise.resolve();

const props = (over: Partial<IFeedProps> = {}): IFeedProps => ({
  items: [row(1), row(2), row(3)] as FeedItem[],
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
  placeKey: 'tab-1',
  ...over,
});

beforeEach(() => forgetPlaces());
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('the place of the reader in a chat', () => {
  it('is the row at the top edge and how far under it its top stands, not pixels of the scroll', () => {
    const rows = [
      { key: 'a', top: -400, bottom: -10 },
      { key: 'b', top: -10, bottom: 300 },
      { key: 'c', top: 300, bottom: 900 },
    ];
    expect(placeAmong(rows, 0)).toEqual({ k: PLACE.at, key: 'b', offset: -10 });
    expect(placeAmong([], 0)).toBeNull();
    // Older turns loaded above moved the row 800px down: the scroll goes down as much, the row stands where it stood.
    expect(scrollToPlace({ offset: -10 }, 790, 0)).toBe(800);
  });

  it('is kept per chat; a chat never opened is at the bottom; the oldest go past the limit', () => {
    expect(placeOf('tab-x')).toEqual(AT_BOTTOM);
    rememberPlace('tab-1', { k: PLACE.at, key: 'b', offset: 12 });
    rememberPlace('tab-2', AT_BOTTOM);
    expect(placeOf('tab-1')).toEqual({ k: PLACE.at, key: 'b', offset: 12 });
    expect(placeOf('tab-2')).toEqual(AT_BOTTOM);
    for (let i = 0; i < MAX_PLACES; i += 1) rememberPlace(`more-${i}`, AT_BOTTOM);
    expect(placeOf('tab-1')).toEqual(AT_BOTTOM);
  });

  it('back in the chat, the row the reader left at the top edge stands there again', () => {
    rememberPlace('tab-1', { k: PLACE.at, key: 'switch:2', offset: 20 });
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      const top = this.dataset.key === 'switch:2' ? 500 : 100;
      return { top, bottom: top + 50, left: 0, right: 0, width: 0, height: 50, x: 0, y: top } as DOMRect;
    });
    const view = render(<Feed {...props()} />);
    const scrolled = [...view.container.querySelectorAll<HTMLElement>('*')].map((el) => el.scrollTop);
    // 500 (the row) - 100 (the edge) - 20 (its place under the edge).
    expect(scrolled).toContain(380);
  });
});
