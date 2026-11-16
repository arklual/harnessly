// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { fireEvent, render } from '@testing-library/react';
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

  it('names no weekday for today, and says nothing when the end is at or after the reset', () => {
    const at = (exhaustAt: number, resetsIn: number) => [
      {
        kind: 'seven_day' as const,
        window: win(93, resetsIn, forecast({ status: 'exhausts_before_reset', exhaustAt })),
      },
    ];
    const noon = new Date();
    noon.setHours(12, 0, 0, 0);
    const n = noon.getTime() / 1000;
    expect(runsOutLine(at(n + 3600, 5 * 86400), n)).toMatch(/^Lasts until \d\d:\d\d$/);
    expect(runsOutLine(at(n + 6 * 86400, 5 * 86400), n)).toBeNull();
    expect(runsOutLine(at(n + 5 * 86400, 5 * 86400), n)).toBeNull();
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

  const plan = (over: object = {}) => ({
    fiveHour: null,
    sevenDay: win(7, 5 * 86400),
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
  const claude = plan({ extra: [{ id: 'f', title: 'Fable', window: win(58, 5 * 86400) }] });
  const view = (limits: ReturnType<typeof plan>, others?: never) =>
    ({ state: 'ready', provider: 'claude', limits, spend: null, others }) as LimitsView;
  const other = (key: string, name: string, usedPct: number) => ({
    key,
    name,
    provider: null,
    limits: plan({ sevenDay: win(usedPct, 5 * 86400) }),
  });

  it("shows a model's own windows only: Sonnet has the week, Fable adds its own when it presses", () => {
    const sonnet = render(<LimitsBrief view={view(claude)} model="claude-sonnet-5-5" />);
    expect(sonnet.container.querySelectorAll('.hx-limb__win')).toHaveLength(1);
    expect(sonnet.container.textContent).not.toContain('Fable');
    sonnet.unmount();
    const fable = render(<LimitsBrief view={view(claude)} model="claude-fable-5-2" />);
    // 93% left of the week, 42% of Fable's: only the nearer one, nothing presses
    expect(fable.container.querySelectorAll('.hx-limb__win')).toHaveLength(1);
    expect(fable.container.textContent).toContain('Fable');
  });

  it('adds the second window only when it presses (25% or less left)', () => {
    const tight = plan({ fiveHour: win(80, 3600), sevenDay: win(80, 5 * 86400) });
    const { container } = render(<LimitsBrief view={view(tight)} model="gpt-5" />);
    expect(container.querySelectorAll('.hx-limb__win')).toHaveLength(2);
  });

  it('tells the others in one line that opens on a click, and lifts one at 10% or less', () => {
    const v = view(claude, [other('c', 'Codex', 41), other('k', 'Kimi', 52)] as never);
    const { container } = render(<LimitsBrief view={v} model="claude-sonnet-5-5" />);
    expect(container.querySelector('.hx-limo__sum')?.textContent).toBe('Others: Codex 59% · Kimi 48%');
    const sum = container.querySelector('.hx-limo__sum') as HTMLElement;
    expect(sum.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(sum);
    expect(sum.getAttribute('aria-expanded')).toBe('true');
    const low = view(claude, [other('c', 'Codex', 95), other('k', 'Kimi', 52)] as never);
    const warm = render(<LimitsBrief view={low} model="claude-sonnet-5-5" />);
    expect(warm.container.querySelector('.hx-limb__warm')?.textContent).toContain('Codex');
    expect(warm.container.querySelector('.hx-limo__sum')?.textContent).toBe('Others: Kimi 48%');
  });
});
