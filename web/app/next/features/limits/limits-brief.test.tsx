// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/react';
import LimitsBrief, { runsOutLine, slotsOf } from '@/next/features/limits/limits-brief';
import type { LimitForecast, LimitsView, LimitWindow } from '@/next/features/limits/model';

const now = Math.floor(Date.now() / 1000);
const forecast = (over: Partial<LimitForecast>): LimitForecast => ({
  status: 'lasts_until_reset',
  exhaustAt: null,
  exhaustRange: null,
  usedAtReset: null,
  confidence: 'medium',
  reason: null,
  lastAt: null,
  resetsAt: null,
  profile: true,
  ...over,
});
const win = (usedPct: number, resetsIn: number, f: LimitForecast | null = null): LimitWindow => ({
  usedPct,
  resetsAt: now + resetsIn,
  forecast: f,
});

describe('the quiet limits card (HRN-1496)', () => {
  it('says when the limit runs out only when it is before the reset', () => {
    const runsOut = win(50, 5 * 86400, forecast({ status: 'exhausts_before_reset', exhaustAt: now + 86400 }));
    expect(runsOutLine([{ kind: 'seven_day', window: runsOut }])).toMatch(/^Lasts until /);
    const lasts = win(50, 5 * 86400, forecast({}));
    expect(runsOutLine([{ kind: 'seven_day', window: lasts }])).toBeNull();
  });

  it("shows only the windows the plan has, a model's own week after the plan's (HRN-1538)", () => {
    const limits = {
      fiveHour: win(11, -60 * 3600),
      sevenDay: win(45, 5 * 86400),
      month: null,
      balance: null,
      estimate: false,
      unofficial: false,
      updatedAt: null,
      plan: null,
      error: null,
      rejected: false,
      extra: [{ id: 'seven_day_model_fable', title: 'Fable', window: win(20, 5 * 86400) }],
    };
    const slots = slotsOf(limits, now);
    expect(slots.map((s) => [s.key, s.label, s.left])).toEqual([
      ['seven_day', 'week', 55],
      ['seven_day_model_fable', 'Fable · week', 80],
    ]);
  });

  it('draws a line a subscription, colour only on a low number, no confidence words', () => {
    const view: LimitsView = {
      state: 'ready',
      provider: 'claude',
      limits: {
        fiveHour: win(80, 3600),
        sevenDay: win(50, 5 * 86400, forecast({ status: 'exhausts_before_reset', exhaustAt: now + 86400 })),
        month: null,
        balance: null,
        estimate: false,
        unofficial: false,
        updatedAt: null,
        plan: null,
        error: null,
        rejected: false,
      },
      spend: null,
    };
    const { container } = render(<LimitsBrief view={view} />);
    expect(container.querySelectorAll('.hx-limb')).toHaveLength(1);
    expect(container.querySelector('.hx-limb__win[data-level="low"] .hx-limb__pct')?.textContent).toBe('20%');
    expect(container.textContent).toContain('Lasts until');
    expect(container.textContent).not.toMatch(/confidence/i);
  });
});
