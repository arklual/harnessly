import { describe, expect, it } from 'vitest';
import {
  CARD_LEVEL,
  balanceForecastOf,
  cardForecastOf,
  cardLevelOf,
  cardWindowsOf,
  paceMarkOf,
  readAccountSpend,
  runOutOf,
  spendKey,
} from '@/next/features/limits/card-model';
import { pinLocale } from '@/next/i18n';
import type { LimitWindow, ProviderLimits } from '@/next/features/limits/model';

pinLocale('ru');
const H = 3600;
const NOW = 1_800_000_000;
const win = (usedPct: number, resetsIn: number): LimitWindow => ({
  usedPct,
  resetsAt: NOW + resetsIn,
  forecast: null,
});
const limits = (over: Partial<ProviderLimits>): ProviderLimits => ({
  fiveHour: null,
  sevenDay: null,
  month: null,
  balance: null,
  estimate: false,
  unofficial: false,
  updatedAt: null,
  plan: null,
  error: null,
  rejected: false,
  ...over,
});

describe('card levels', () => {
  it('colours only what runs out', () => {
    expect(cardLevelOf(64, false)).toBe(CARD_LEVEL.ok);
    expect(cardLevelOf(39, false)).toBe(CARD_LEVEL.warn);
    expect(cardLevelOf(14, false)).toBe(CARD_LEVEL.bad);
    expect(cardLevelOf(80, true)).toBe(CARD_LEVEL.bad);
  });
});

describe('the pace', () => {
  it('runs out before the reset when spent faster than the window goes by', () => {
    // 81% of five hours gone in 4.3 h: 19% left at 18.8%/h lasts about an hour, the reset is in 0.7 h.
    expect(runOutOf('five_hour', win(81, 0.7 * H), NOW)).toEqual({ k: 'lasts' });
    // 60% used in one hour: the rest goes in 40 minutes, the reset is in four hours.
    const out = runOutOf('five_hour', win(60, 4 * H), NOW);
    expect(out.k).toBe('at');
    if (out.k === 'at') expect(Math.round((out.at - NOW) / 60)).toBe(40);
  });

  it('says nothing over the first twentieth of a window', () => {
    expect(runOutOf('seven_day', win(5, 7 * 24 * H - 600), NOW)).toEqual({ k: 'unknown' });
  });

  it('marks where what is left should be to last to the reset', () => {
    expect(paceMarkOf('seven_day', win(19, 3.5 * 24 * H), NOW)).toBe(50);
    expect(paceMarkOf('seven_day', win(19, -1), NOW)).toBeNull();
  });

  it('tells the soonest end in one line', () => {
    const windows = cardWindowsOf(limits({ fiveHour: win(60, 4 * H), sevenDay: win(30, 3 * 24 * H) }), NOW);
    expect(cardForecastOf(windows, NOW)).toEqual({ text: 'Кончится через 40 мин', level: CARD_LEVEL.bad });
    const calm = cardWindowsOf(limits({ sevenDay: win(20, 3 * 24 * H) }), NOW);
    expect(cardForecastOf(calm, NOW)).toEqual({ text: 'Хватит до сброса', level: CARD_LEVEL.ok });
    expect(cardForecastOf([], NOW)).toBeNull();
  });
});

describe('spend by account', () => {
  const day = (back: number) => new Date((NOW - back * 24 * H) * 1000).toISOString().slice(0, 10);
  const doc = {
    runtimes: [
      {
        machineId: 'm1',
        report: {
          timezone: 'UTC',
          usage: [
            [day(0), 'claude', 'opus', 'p', 0, 0, 0, 0, 10, 1],
            [day(1), 'claude', 'opus', 'p', 0, 0, 0, 0, 5, 1],
            [day(9), 'claude', 'opus', 'p', 0, 0, 0, 0, 100, 1],
            [day(0), 'codex', 'gpt', 'p', 0, 0, 0, 0, 2, 1, 'Рабочий'],
          ],
        },
      },
    ],
  };

  it('sums today and the last seven days by agent and account', () => {
    const spend = readAccountSpend(doc, 'm1', NOW * 1000);
    expect(spend?.get(spendKey('claude', ''))).toMatchObject({ today: 10, week: 15 });
    expect(spend?.get(spendKey('codex', 'Рабочий'))?.today).toBe(2);
  });

  it('has none for a machine without a report', () => {
    expect(readAccountSpend({ runtimes: [] }, 'm1', NOW * 1000)).toBeNull();
    expect(readAccountSpend({}, 'm1', NOW * 1000)).toBeNull();
  });

  it('tells how long a balance lasts at the week’s pace, and nothing without spend', () => {
    expect(balanceForecastOf(49.58, { today: 1, week: 7.9, days: [] })?.text).toBe(
      'Хватит примерно на 6 недель',
    );
    expect(balanceForecastOf(49.58, null)).toBeNull();
  });
});
