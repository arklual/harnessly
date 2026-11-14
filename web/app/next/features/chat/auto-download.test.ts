// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  claimDownload,
  deliveryKey,
  freshDeliveries,
  useAutoDownload,
} from '@/next/features/chat/auto-download';
import type { FeedItem, TurnOutput } from '@/next/features/chat/types';

const file = (ref: string, sha256 = 'a'): TurnOutput => ({
  kind: 'delivery',
  op: 'created',
  title: ref,
  ref,
  name: ref,
  sha256,
});

const turn = (id: string, outputs: TurnOutput[], endedAt: number | null): FeedItem =>
  ({
    kind: 'turn',
    key: id,
    turn: { id, startedAt: 0, endedAt, outputs, files: [] },
  }) as unknown as FeedItem;

describe('freshDeliveries', () => {
  it('gives each hand-over once and leaves history that ended before the page subscribed', () => {
    const seen = new Set<string>();
    const items = [turn('old', [file('/old.zip')], 50), turn('now', [file('/a.pdf'), file('/b.csv')], null)];
    expect(freshDeliveries(items, seen, 100).map((o) => o.ref)).toEqual(['/a.pdf', '/b.csv']);
    expect(freshDeliveries(items, seen, 100)).toEqual([]);
    // The same file handed over again after a change is a new one.
    expect(freshDeliveries([turn('now', [file('/a.pdf', 'b')], 200)], seen, 100).map((o) => o.ref)).toEqual([
      '/a.pdf',
    ]);
  });
});

describe('claimDownload', () => {
  beforeEach(() => localStorage.clear());
  it('lets one tab of the browser take a file', () => {
    const key = deliveryKey(file('/a.pdf'));
    expect(claimDownload('tab-c1', key, 1000)).toBe(true);
    expect(claimDownload('tab-c1', key, 2000)).toBe(false);
    expect(claimDownload('tab-c2', key, 2000)).toBe(true);
  });
});

describe('useAutoDownload', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it('saves a new live hand-over, never what the chat had when it opened', () => {
    const save = vi.fn();
    const hrefOf = (o: TurnOutput) => `/dl${o.ref}`;
    const history = [turn('t1', [file('/old.zip')], null)];
    const { rerender } = renderHook(
      (items: FeedItem[]) =>
        useAutoDownload({ conv: 'tab-c', items, subscribedAt: 100, live: true, hrefOf, save }),
      { initialProps: history },
    );
    vi.runAllTimers();
    expect(save).not.toHaveBeenCalled();
    rerender([...history, turn('t2', [file('/new.html'), file('/data.csv')], null)]);
    vi.runAllTimers();
    expect(save.mock.calls).toEqual([['/dl/new.html'], ['/dl/data.csv']]);
    rerender([...history, turn('t2', [file('/new.html'), file('/data.csv')], 300)]);
    vi.runAllTimers();
    expect(save).toHaveBeenCalledTimes(2);
  });

  it('waits for the live snapshot', () => {
    const save = vi.fn();
    const items = [turn('t1', [file('/a.zip')], null)];
    const { rerender } = renderHook(
      (live: boolean) =>
        useAutoDownload({
          conv: 'tab-c',
          items,
          subscribedAt: live ? 1 : null,
          live,
          hrefOf: (o) => o.ref,
          save,
        }),
      { initialProps: false },
    );
    rerender(true);
    vi.runAllTimers();
    expect(save).not.toHaveBeenCalled();
  });
});
