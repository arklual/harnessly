import { describe, expect, it } from 'vitest';

import { createOrgApi } from '../../api';
import type { Kpi } from '../../api/analytics';
import { deltaOf, formatValue, minutesText } from './format';
import { defaultCompare, presetsOf, weekCount } from './period';

const kpi = (over: Partial<Kpi>): Kpi => ({
  id: 'yield',
  group: 'result',
  title: 'x',
  hint: '',
  format: 'pct',
  delta: 'pts',
  better: 'up',
  noise: 2,
  value: 64,
  prev: 59,
  series: [],
  prevSeries: [],
  ...over,
});

describe('numbers', () => {
  it('writes values the Russian way', () => {
    expect(formatValue('usd', 18.5)).toBe('$18,5');
    expect(formatValue('pct', 3.4)).toBe('3,4%');
    expect(formatValue('tokens', 23_000)).toBe('23K');
    expect(minutesText(130)).toBe('2 ч 10 мин');
    expect(minutesText(38)).toBe('38 мин');
  });

  it('tells a change by direction and by which way is better', () => {
    expect(deltaOf(kpi({}))).toEqual({ tone: 'good', text: '↑ 5 п.' });
    expect(deltaOf(kpi({ better: 'down' })).tone).toBe('bad');
    expect(
      deltaOf(kpi({ delta: 'rel', better: 'down', value: 18.5, prev: 21, noise: 3, format: 'usd' })),
    ).toEqual({
      tone: 'good',
      text: '↓ 12%',
    });
  });

  it('calls a change inside the noise «как раньше»', () => {
    expect(deltaOf(kpi({ value: 50, prev: 49, delta: 'rel', noise: 5 }))).toEqual({ tone: 'flat', text: '' });
  });
});

describe('periods', () => {
  it('compares with the same number of weeks right before', () => {
    expect(defaultCompare({ from: '2026-10-19', to: '2026-10-25' })).toEqual({
      from: '2026-10-12',
      to: '2026-10-18',
    });
    const four = defaultCompare({ from: '2026-09-28', to: '2026-10-25' });
    expect(weekCount(four)).toBe(4);
    expect(four.to).toBe('2026-09-27');
  });

  it('offers the ready choices over full weeks', async () => {
    const frame = await createOrgApi('mock', 'admin').analyticsFrame();
    const presets = presetsOf(frame);
    expect(presets.map((p) => p.id)).toEqual(['last-week', 'four-weeks', 'after-change']);
    expect(weekCount(presets[1]?.period ?? { from: '', to: '' })).toBe(4);
    expect(frame.defaultPeriod.from).toBe('2026-10-19');
  });
});

describe('what each role gets', () => {
  const q = {
    period: { from: '2026-10-19', to: '2026-10-25' },
    compare: { from: '2026-10-12', to: '2026-10-18' },
  };
  it('gives a participant their own work, without money of others or peers', async () => {
    const d = await createOrgApi('mock', 'member').analytics(q);
    expect(d.kpis).toHaveLength(12);
    expect(d.waste).toBeNull();
    expect(d.peers).toBeNull();
    expect(d.signals).toEqual([]);
  });
  it('gives an admin teams next to each other and the waste', async () => {
    const d = await createOrgApi('mock', 'admin').analytics(q);
    expect(d.peers?.title).toBe('Команды рядом');
    expect(d.waste?.items.length).toBeGreaterThan(0);
  });
  it('draws a lead their people, an admin their teams', async () => {
    const lead = await createOrgApi('mock', 'lead').analyticsChart(q, 'moneyPerMr');
    const admin = await createOrgApi('mock', 'admin').analyticsChart(q, 'moneyPerMr');
    expect(admin.lines.map((l) => l.name)).toContain('Фронт');
    expect(lead.lines.map((l) => l.name)).not.toContain('Фронт');
  });
});
