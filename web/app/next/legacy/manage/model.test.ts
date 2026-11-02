import { describe, expect, it } from 'vitest';
import { decidingLimit, historyRows, limitTone, runsOutIn } from '@/next/legacy/manage/model';
import { mockMe } from '@/next/features/manage/mock';

describe('limits', () => {
  const me = mockMe(Date.UTC(2026, 9, 22));
  it('picks the limit marked effective', () => {
    expect(decidingLimit(me.limits)?.id).toBe('l-person');
  });
  it('picks the tightest when none is marked', () => {
    const none = { ...me.limits, limits: me.limits.limits.map((l) => ({ ...l, effective: false })) };
    expect(decidingLimit(none)?.id).toBe('l-team');
  });
  it('tells ok, warn and stop apart', () => {
    const l = me.limits.limits[0]!;
    expect(limitTone({ ...l, used: 10, amount: 100 })).toBe('ok');
    expect(limitTone({ ...l, used: 85, amount: 100 })).toBe('warn');
    expect(limitTone({ ...l, used: 100, amount: 100 })).toBe('stop');
  });
  it('says nothing of the end when nothing was spent or the limit is out', () => {
    expect(runsOutIn(0, 100, 5)).toBeNull();
    expect(runsOutIn(100, 100, 5)).toBeNull();
    expect(runsOutIn(50, 100, 5)).toBe(5);
  });
});

describe('history', () => {
  it('meets local sessions and the hub on the session id; no hub, no pairs', () => {
    const session = { id: 's-1' } as never;
    const hub = mockMe().sessions.sessions;
    expect(historyRows([session], hub)[0]?.hub?.key).toBe('s-1');
    expect(historyRows([session], null)[0]?.hub).toBeNull();
  });
});
