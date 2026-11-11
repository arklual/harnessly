// Notices do not blink on data updates: a sync that shows a chat for a moment at another event does not take
// its notice away, a recompute with equal data keeps the same item objects, and a refetch keeps the open card's detail.
import { describe, expect, it } from 'vitest';
import { DROP_SETTLE_MS, settledDrops } from '@/next/bindings/notices';
import { stableItems, steadyDetail } from '@/next/bindings/smart-notice';
import { SMART_KIND, type ISmartItem } from '@/next/features/smart-notice/smart-notice';
import type { INotice } from '@/next/stores/notices';

const notice = (chatId: string, key: string): INotice =>
  ({ key, chatId, kind: 'waiting', icon: 'x', title: chatId, text: '', at: 0 }) as INotice;

const item = (key: string, text = 'waits'): ISmartItem => ({
  key,
  kind: SMART_KIND.question,
  chatId: key,
  icon: 'x',
  title: key,
  text,
  at: 1,
  until: null,
});

describe('a notice goes only when its chat has moved on for good', () => {
  it('a moment at another event keeps it; standing there DROP_SETTLE_MS takes it', () => {
    const leaving = new Map<string, number>();
    const list = [notice('a', 'a:1'), notice('b', 'b:1')];
    // a sync shows chat a without its question for a moment
    expect(
      settledDrops(
        leaving,
        list,
        new Map([
          ['a', null],
          ['b', 'b:1'],
        ]),
        1000,
      ).size,
    ).toBe(0);
    // back at the same event: forgotten, nothing dropped
    expect(
      settledDrops(
        leaving,
        list,
        new Map([
          ['a', 'a:1'],
          ['b', 'b:1'],
        ]),
        1200,
      ).size,
    ).toBe(0);
    expect(leaving.size).toBe(0);
    // moved on and stays there
    settledDrops(leaving, list, new Map([['a', 'a:2']]), 2000);
    expect(settledDrops(leaving, list, new Map([['a', 'a:2']]), 2000 + DROP_SETTLE_MS - 1).size).toBe(0);
    expect([...settledDrops(leaving, list, new Map([['a', 'a:2']]), 2000 + DROP_SETTLE_MS)]).toEqual(['a:1']);
    expect(leaving.size).toBe(0);
  });

  it('a chat not known yet keeps its notice', () => {
    expect(settledDrops(new Map(), [notice('a', 'a:1')], new Map(), 0).size).toBe(0);
  });
});

describe('equal data, the same items', () => {
  it('an equal recompute is the same list; a change of one item keeps the others', () => {
    const before = [item('a'), item('b'), item('c')];
    expect(stableItems(before, [item('a'), item('b'), item('c')])).toBe(before);
    const after = stableItems(before, [item('a'), item('b', 'another line'), item('c')]);
    expect(after).not.toBe(before);
    expect(after[0]).toBe(before[0]);
    expect(after[1].text).toBe('another line');
    expect(after[2]).toBe(before[2]);
  });

  it('a refetch of the same item keeps its detail; another item may load', () => {
    const shown = { key: 'q', detail: { k: 'question' as const, text: 'Which?', options: ['A'] } };
    expect(steadyDetail(shown, 'q', { k: 'loading' })).toBe(shown.detail);
    expect(steadyDetail(shown, 'p', { k: 'loading' })).toEqual({ k: 'loading' });
  });
});
