import { describe, expect, it } from 'vitest';
import { alertsDue, stepOf, ALERT_STEPS, type TAlertMemory } from '@/next/features/limits/alerts';
import { ringOf, WINDOW, type LimitsView, type ProviderLimits } from '@/next/features/limits/model';

const NOW = 1_800_000_000;

const limits = (fiveLeft: number | null, weekLeft: number | null): ProviderLimits => ({
  fiveHour: fiveLeft === null ? null : { usedPct: 100 - fiveLeft, resetsAt: NOW + 3600, forecast: null },
  sevenDay: weekLeft === null ? null : { usedPct: 100 - weekLeft, resetsAt: NOW + 3 * 86400, forecast: null },
  month: null,
  balance: null,
  estimate: false,
  unofficial: false,
  updatedAt: null,
  plan: null,
  error: null,
  rejected: false,
});

const view = (fiveLeft: number | null, weekLeft: number | null): LimitsView => ({
  state: 'ready',
  provider: 'claude',
  limits: limits(fiveLeft, weekLeft),
  spend: null,
});

const steps = (memory: TAlertMemory, v: LimitsView, now = NOW) => {
  const r = alertsDue(v, memory, now);
  return { told: r.due.map((a) => `${a.kind}:${a.step}`), memory: r.memory };
};

describe('stepOf', () => {
  it('is the lowest step not under what is left', () => {
    const five = ALERT_STEPS[WINDOW.fiveHour] ?? [];
    expect(stepOf(21, five)).toBeNull();
    expect(stepOf(20, five)).toBe(20);
    expect(stepOf(18, five)).toBe(20);
    expect(stepOf(15, five)).toBe(15);
    expect(stepOf(3, five)).toBe(5);
    expect(stepOf(0, five)).toBe(0);
    const week = ALERT_STEPS[WINDOW.sevenDay] ?? [];
    expect(stepOf(5, week)).toBeNull();
    expect(stepOf(4, week)).toBe(4);
    expect(stepOf(1, week)).toBe(1);
  });
});

describe('alertsDue', () => {
  it('tells nothing while plenty is left', () => {
    expect(steps({}, view(60, 51)).told).toEqual([]);
  });

  it('tells each step once, and only the one a jump lands on', () => {
    let s = steps({}, view(19, 4));
    expect(s.told).toEqual(['five_hour:20', 'seven_day:4']);
    s = steps(s.memory, view(17, 4));
    expect(s.told).toEqual([]);
    s = steps(s.memory, view(4, 3));
    expect(s.told).toEqual(['five_hour:5', 'seven_day:3']);
    s = steps(s.memory, view(4, 3));
    expect(s.told).toEqual([]);
    s = steps(s.memory, view(0, 0));
    expect(s.told).toEqual(['five_hour:0', 'seven_day:0']);
  });

  it('forgets a window that has reset, and a new window starts over', () => {
    const first = steps({}, view(10, null));
    expect(first.told).toEqual(['five_hour:10']);
    const later = NOW + 3600 + 60;
    const next: LimitsView = {
      state: 'ready',
      provider: 'claude',
      limits: {
        ...limits(null, null),
        fiveHour: { usedPct: 85, resetsAt: later + 5 * 3600, forecast: null },
      },
      spend: null,
    };
    const second = steps(first.memory, next, later);
    expect(second.told).toEqual(['five_hour:15']);
    expect(Object.keys(second.memory)).toHaveLength(1);
  });

  it('keeps a reading that drifts the reset by minutes the same window', () => {
    const first = steps({}, view(10, null));
    const drift: LimitsView = {
      state: 'ready',
      provider: 'claude',
      limits: {
        ...limits(null, null),
        fiveHour: { usedPct: 91, resetsAt: NOW + 3600 + 120, forecast: null },
      },
      spend: null,
    };
    expect(steps(first.memory, drift).told).toEqual([]);
  });

  it('tells nothing of a view that failed', () => {
    expect(alertsDue({ state: 'failed', provider: 'claude' }, {}, NOW).due).toEqual([]);
  });
});

describe('ringOf', () => {
  it('is the spent share of the five hours with its level', () => {
    expect(ringOf(view(60, 51), NOW)).toEqual({ used: 40, level: 'ok' });
    expect(ringOf(view(20, 51), NOW)).toEqual({ used: 80, level: 'low' });
    expect(ringOf(view(3, 51), NOW)).toEqual({ used: 97, level: 'out' });
    expect(ringOf(view(null, 51), NOW)).toBeNull();
    expect(ringOf(null, NOW)).toBeNull();
  });
});
