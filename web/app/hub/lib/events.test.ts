import { describe, expect, it } from 'vitest';

import { type BusEvent, chainOf, domains, KEEP, lag, matches, merge } from './events';

const ev = (seq: number, over: Partial<BusEvent> = {}): BusEvent => ({
  v: 1,
  seq,
  id: `e${seq}`,
  owner: 'u1',
  visibility: 'team',
  type: 'chat.message',
  source: 'telegram-account',
  fingerprint: `f${seq}`,
  at: '2026-11-21T09:00:00Z',
  received: '2026-11-21T09:00:01Z',
  via: ['connector'],
  ...over,
});

describe('the live list of the journal', () => {
  it('keeps the newest first and drops repeats of a reconnect', () => {
    const l = merge([ev(3), ev(1)], [ev(2), ev(3), ev(4)]);
    expect(l.map((e) => e.seq)).toEqual([4, 3, 2, 1]);
  });
  it('keeps at most KEEP events', () => {
    const many = Array.from({ length: KEEP + 5 }, (_, i) => ev(i + 1));
    const l = merge([], many);
    expect(l).toHaveLength(KEEP);
    expect(l[0]?.seq).toBe(KEEP + 5);
  });
  it('finds by every word in type, source, actor, thread and facts', () => {
    const e = ev(1, { actor: '@ann', thread: 'tg:42', facts: { direct: 'true' } });
    expect(matches(e, 'chat ann')).toBe(true);
    expect(matches(e, 'tg:42 direct')).toBe(true);
    expect(matches(e, 'mail')).toBe(false);
  });
  it('chains the events of a thread, oldest first, or of a fingerprint without one', () => {
    const list = [
      ev(5, { thread: 't' }),
      ev(4),
      ev(3, { thread: 't' }),
      ev(2, { fingerprint: 'x' }),
      ev(1, { fingerprint: 'x' }),
    ];
    expect(chainOf(list, list[0]!).map((e) => e.seq)).toEqual([3, 5]);
    expect(chainOf(list, list[3]!).map((e) => e.seq)).toEqual([1, 2]);
  });
  it('counts the domains and the lag of a subscription', () => {
    expect(domains([ev(1), ev(2, { type: 'mail.received' }), ev(3)])).toEqual([
      ['chat', 2],
      ['mail', 1],
    ]);
    expect(
      lag(
        {
          id: 's',
          owner: '',
          consumer: 'rules',
          filter: {},
          cursor: 7,
          state: 'active',
          created: '',
          updated: '',
        },
        10,
      ),
    ).toBe(3);
  });
});
