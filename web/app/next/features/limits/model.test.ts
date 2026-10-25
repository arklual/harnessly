import { describe, expect, it } from 'vitest';
import {
  describeForecast,
  FORECAST,
  FORECAST_TONE,
  leftAtReset,
  leftOf,
  LEVEL,
  levelOf,
  readMachineLimits,
  span,
  WINDOW,
  windowsOf,
  type LimitForecast,
} from '@/next/features/limits/model';

const window5 = {
  usedPct: 6.2,
  resetsAt: 1_800_000_000,
  forecast: {
    status: 'lasts_until_reset',
    exhaustAt: null,
    exhaustRange: null,
    usedAtReset: 31.4,
    confidence: 'medium',
    reason: null,
    lastAt: 1_799_999_000,
    resetsAt: 1_800_000_000,
    method: 'profile',
    samples: 9,
  },
};

const claude = {
  fiveHour: window5,
  sevenDay: { usedPct: 19, resetsAt: 1_800_400_000 },
  updatedAt: 1_799_999_000,
  plan: 'Claude Max 20x',
  error: null,
};

describe('the machines answer', () => {
  it('reads both windows of an agent, the forecast and the plan, and nothing for an agent that reports none', () => {
    const limits = readMachineLimits({ machines: [{ id: 'm1', limits: { claude, codex: null } }] }, 'm1');
    expect(limits?.codex).toBeNull();
    expect(limits?.claude?.plan).toBe('Claude Max 20x');
    expect(limits?.claude?.updatedAt).toBe(1_799_999_000_000);
    expect(limits?.claude?.fiveHour?.forecast?.status).toBe(FORECAST.lasts);
    expect(limits?.claude?.fiveHour?.forecast?.profile).toBe(true);
    expect(limits?.claude?.sevenDay?.forecast).toBeNull();
  });

  it('finds the machine by id in a gateway list, and takes the only one a runtime lists for itself', () => {
    const list = {
      machines: [
        { id: 'a', limits: { claude: { ...claude, plan: 'A' }, codex: null } },
        { id: 'b', limits: { claude: { ...claude, plan: 'B' }, codex: null } },
      ],
    };
    expect(readMachineLimits(list, 'b')?.claude?.plan).toBe('B');
    expect(readMachineLimits(list, 'c')).toBeNull();
    expect(readMachineLimits({ machines: [list.machines[0]] }, 'local')?.claude?.plan).toBe('A');
  });

  it('does not make a window of a reading it cannot read as numbers', () => {
    const limits = readMachineLimits(
      {
        machines: [
          {
            id: 'm',
            limits: {
              claude: { fiveHour: { usedPct: null, resetsAt: 1 }, sevenDay: { usedPct: '40', resetsAt: 2 } },
              codex: { fiveHour: { usedPct: 10, resetsAt: 5, forecast: { status: 'nonsense' } } },
            },
          },
        ],
      },
      'm',
    );
    expect(windowsOf(limits?.claude ?? null)).toEqual([]);
    expect(windowsOf(limits?.codex ?? null)).toHaveLength(1);
    expect(limits?.codex?.fiveHour?.forecast).toBeNull();
  });

  it('refuses an answer that is not a list of machines', () => {
    expect(() => readMachineLimits({ error: 'x' }, 'm')).toThrow();
  });
});

describe('what is left', () => {
  it('is the rest of the window in whole per cent, with the level of how much is low', () => {
    expect(leftOf({ usedPct: 6.2, resetsAt: 0, forecast: null })).toBe(94);
    expect(leftOf({ usedPct: 120, resetsAt: 0, forecast: null })).toBe(0);
    expect(levelOf(94)).toBe(LEVEL.ok);
    expect(levelOf(25)).toBe(LEVEL.low);
    expect(levelOf(5)).toBe(LEVEL.out);
  });

  it('keeps the order of the windows and what the forecast leaves at the reset', () => {
    const limits = readMachineLimits({ machines: [{ id: 'm', limits: { claude, codex: null } }] }, 'm');
    expect(windowsOf(limits?.claude ?? null).map((w) => w.kind)).toEqual([WINDOW.fiveHour, WINDOW.sevenDay]);
    expect(leftAtReset(limits!.claude!.fiveHour!)).toBe(69);
    expect(leftAtReset(limits!.claude!.sevenDay!)).toBeNull();
  });

  it('tells a span in days, hours and minutes', () => {
    expect(span(40 * 60)).toBe('40 min');
    expect(span(3 * 3600 + 10 * 60)).toBe('3 h 10 min');
    expect(span(2 * 3600)).toBe('2 h');
    expect(span(3 * 86400 + 4 * 3600)).toBe('3 d 4 h');
    expect(span(2 * 86400)).toBe('2 d');
  });
});

const forecast = (over: Partial<LimitForecast>): LimitForecast => ({
  status: FORECAST.insufficient,
  exhaustAt: null,
  exhaustRange: null,
  usedAtReset: null,
  confidence: null,
  reason: null,
  lastAt: null,
  resetsAt: 1_800_000_000,
  profile: false,
  ...over,
});

describe('the forecast in words', () => {
  it('warns when the window runs out before the reset', () => {
    const text = describeForecast(
      forecast({ status: FORECAST.exhausts, exhaustAt: 1_799_990_000, confidence: 'high' }),
      WINDOW.fiveHour,
      'Claude',
    );
    expect(text.tone).toBe(FORECAST_TONE.warning);
    expect(text.text).toMatch(/^Claude limit \(5 h\) runs out/);
    expect(text.detail).toBe('high confidence');
  });

  it('says it lasts, with what is used by the reset', () => {
    const text = describeForecast(
      forecast({ status: FORECAST.lasts, usedAtReset: 31.4 }),
      WINDOW.sevenDay,
      'Claude',
    );
    expect(text.tone).toBe(FORECAST_TONE.neutral);
    expect(text.text).toContain('Will last until the reset at');
    expect(text.text).toContain('≈ 31% used at the reset');
  });

  it('says a limit is used up in the danger tone', () => {
    expect(describeForecast(forecast({ status: FORECAST.exhausted }), WINDOW.fiveHour, 'Claude').tone).toBe(
      FORECAST_TONE.danger,
    );
  });

  it("gives the machine's reason when there is no forecast, never a made-up one", () => {
    expect(describeForecast(forecast({ reason: 'no samples' }), WINDOW.fiveHour, 'Claude').text).toBe(
      'No forecast yet: no samples',
    );
    expect(describeForecast(forecast({}), WINDOW.fiveHour, 'Claude').text).toBe(
      'No forecast yet: too few samples',
    );
  });
});
