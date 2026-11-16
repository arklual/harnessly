import { describe, expect, it } from 'vitest';
import { markWentOn } from '@/next/bindings/feed-items';
import type { FeedItem, OutgoingItem, TurnItem } from '@/next/features/chat/types';

const turn = (id: string, status: string, code = 'agent-exited', auto?: 'restart' | 'limit'): TurnItem =>
  ({
    kind: 'turn',
    key: id,
    turn: { id, status, failure: status === 'failed' ? { code } : null },
    ...(auto && { auto: { why: auto, own: true } }),
  }) as unknown as TurnItem;

const going = (auto?: 'restart'): OutgoingItem =>
  ({
    kind: 'outgoing',
    key: 'auto-1',
    message: {},
    ...(auto && { auto: { why: auto, own: true } }),
  }) as unknown as OutgoingItem;

// a restart cut the turn short, the runtime goes on by itself — the person sees one status, not a red
// «Ход прервался» with its buttons next to «продолжаю…».
describe('a turn a restart cut short', () => {
  it('shows no failure of its own while the runtime goes on, and keeps the same object', () => {
    const kept = new WeakMap<TurnItem, TurnItem>();
    const cut = turn('t1', 'failed');
    const items: FeedItem[] = [cut, going('restart')];
    markWentOn(items, kept);
    expect((items[0] as TurnItem).wentOn).toBe(true);
    const again: FeedItem[] = [cut, going('restart')];
    markWentOn(again, kept);
    expect(again[0]).toBe(items[0]);
  });

  it('stays quiet once the «go on» became the next turn', () => {
    const items: FeedItem[] = [turn('t1', 'failed'), turn('t2', 'live', '', 'restart')];
    markWentOn(items, new WeakMap());
    expect((items[0] as TurnItem).wentOn).toBe(true);
  });

  it('shows the failure with its buttons when nothing goes on (the «go on» failed and left the feed)', () => {
    const items: FeedItem[] = [turn('t1', 'failed'), going()];
    markWentOn(items, new WeakMap());
    expect((items[0] as TurnItem).wentOn).toBeUndefined();
    const alone: FeedItem[] = [turn('t1', 'failed')];
    markWentOn(alone, new WeakMap());
    expect((alone[0] as TurnItem).wentOn).toBeUndefined();
  });

  it('leaves a usage limit and a going-on after a limit as they are', () => {
    const items: FeedItem[] = [turn('t1', 'failed'), going()];
    items[1] = { ...going(), auto: { why: 'limit', own: true } } as unknown as OutgoingItem;
    markWentOn(items, new WeakMap());
    expect((items[0] as TurnItem).wentOn).toBeUndefined();
  });
});
