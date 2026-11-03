import { describe, expect, it } from 'vitest';
import type { Kpi } from '../../../org/api/analytics';
import { BOARD_CURVES } from '../../../org/mock/analytics-board';
import {
  barWidths,
  changeTone,
  deltaOf,
  effectNote,
  effectWindows,
  formatChart,
  formatEffect,
  formatKpi,
  heatOf,
  meanOf,
  minutesText,
  shiftOf,
  sparkPaths,
  totalsText,
  wasText,
  wasteLead,
  wasteView,
} from './calc';

type D = Pick<Kpi, 'value' | 'prev' | 'delta' | 'better' | 'noise'>;
const kpi = (o: Partial<D>): D => ({ value: 10, prev: 10, delta: 'rel', better: 'up', noise: 3, ...o });

describe('formatKpi', () => {
  it('writes every format the Russian way', () => {
    expect(formatKpi('usd', 18.5)).toBe('$18,5');
    expect(formatKpi('usd', 21)).toBe('$21,0');
    expect(formatKpi('pct', 64)).toBe('64%');
    expect(formatKpi('pct', 3.4)).toBe('3,4%');
    expect(formatKpi('minutes', 130)).toBe('2 ч 10 мин');
    expect(formatKpi('minutes', 50)).toBe('50 мин');
    expect(formatKpi('tokens', 23_000)).toBe('23K');
    expect(formatKpi('count', 2.4)).toBe('2,4');
    expect(formatKpi('perMonth', 6.6)).toBe('6,6');
  });
  it('shows a dash where there is no number', () => {
    expect(formatKpi('usd', null)).toBe('—');
    expect(formatKpi('pct', Number.NaN)).toBe('—');
    expect(formatChart('perMonth', undefined)).toBe('—');
  });
  it('gives the chart and the change lines their own precision', () => {
    expect(formatChart('perMonth', 6.6)).toBe('6,60');
    expect(formatEffect('usd', 19)).toBe('$19');
    expect(formatEffect('usd', 18.5)).toBe('$18,5');
    expect(formatEffect('tokens', 50_000)).toBe('50K');
  });
  it('tells minutes with hours', () => {
    expect(minutesText(180)).toBe('3 ч');
    expect(minutesText(153)).toBe('2 ч 33 мин');
    expect(minutesText(125)).toBe('2 ч 05 мин');
  });
});

describe('wasText', () => {
  it('adds the unit the tile needs', () => {
    expect(wasText({ format: 'usd', prev: 21 })).toBe('было $21,0');
    expect(wasText({ format: 'perMonth', prev: 6.1 })).toBe('было 6,1 в месяц');
    expect(wasText({ format: 'count', prev: 2.1 })).toBe('было 2,1 сессии');
    expect(wasText({ format: 'tokens', prev: 50_000 })).toBe('было 50K токенов');
    expect(wasText({ format: 'usd', prev: null })).toBe('было —');
  });
});

describe('deltaOf', () => {
  it('tells a relative move in percent and judges it by which way is better', () => {
    expect(deltaOf(kpi({ value: 18.5, prev: 21, better: 'down' }))).toEqual({ tone: 'good', text: '↓ 12%' });
    expect(deltaOf(kpi({ value: 6.6, prev: 6.1 }))).toEqual({ tone: 'good', text: '↑ 8%' });
    expect(deltaOf(kpi({ value: 23_000, prev: 50_000, better: 'down', noise: 5 }))).toEqual({
      tone: 'good',
      text: '↓ 54%',
    });
  });
  it('tells points and plain differences, and a worse move is bad', () => {
    expect(deltaOf(kpi({ value: 64, prev: 59, delta: 'pts', noise: 2 }))).toEqual({
      tone: 'good',
      text: '↑ 5 п.',
    });
    expect(deltaOf(kpi({ value: 3.4, prev: 2.6, delta: 'pts', better: 'down', noise: 0.3 }))).toEqual({
      tone: 'bad',
      text: '↑ 0,8 п.',
    });
    expect(deltaOf(kpi({ value: 2.4, prev: 2.1, delta: 'abs', noise: 0.1 }))).toEqual({
      tone: 'good',
      text: '↑ 0,3',
    });
  });
  it('is «как раньше» inside the noise, and a dash without both numbers', () => {
    expect(deltaOf(kpi({ value: 50, prev: 49, better: 'down', noise: 5 }))).toEqual({
      tone: 'flat',
      text: 'как раньше',
    });
    expect(deltaOf(kpi({ value: null }))).toEqual({ tone: 'flat', text: '—' });
    expect(deltaOf(kpi({ value: 5, prev: 0 }))).toEqual({ tone: 'flat', text: '—' });
  });
  it('treats an infinite noise (too few sessions) as «как раньше»', () => {
    expect(deltaOf(kpi({ value: 20, prev: 10, noise: Number.POSITIVE_INFINITY })).text).toBe('как раньше');
  });
});

describe('sparkPaths', () => {
  it('draws both lines on one scale inside 200 by 44', () => {
    const p = sparkPaths([0, 10], [5, 5]);
    expect(p.now.startsWith('M0.0 ')).toBe(true);
    expect(p.now).toContain('L200.0 ');
    expect(p.area.endsWith('L200 44 L0 44 Z')).toBe(true);
    const ys = [...p.now.matchAll(/ ([\d.]+)/g)].map((m) => Number(m[1]));
    expect(ys[0]).toBeGreaterThan(ys[1] ?? 0);
    expect(Math.min(...ys)).toBeGreaterThan(0);
    expect(Math.max(...ys)).toBeLessThan(44);
  });
  it('keeps the comparison on the same scale as the period', () => {
    const p = sparkPaths([0, 10], [10, 0]);
    expect(p.now.split(' ')[1]).toBe(p.prev.split(' ').at(-1));
  });
  it('draws a flat line in the middle and nothing without numbers', () => {
    expect(sparkPaths([3, 3, 3], []).now).toBe('M0.0 22.0 L100.0 22.0 L200.0 22.0');
    expect(sparkPaths([], [])).toEqual({ prev: '', area: '', now: '' });
    expect(sparkPaths([1, null, 3], []).now).toBe(sparkPaths([1, 3], []).now.replace('L200.0', 'L200.0'));
  });
  it('gives the board its own lines back (first y of the money tile)', () => {
    const c = BOARD_CURVES.moneyPerMr;
    const p = sparkPaths(c.sparkNow, c.sparkPrev);
    expect(p.now.startsWith('M0.0 11.7 L6.9 5.1')).toBe(true);
    expect(p.prev.startsWith('M0.0 10.7 L6.9 12.0')).toBe(true);
  });
});

describe('effects', () => {
  it('makes the larger value the full bar', () => {
    expect(barWidths(2.9, 3.4)).toEqual([85, 100]);
    expect(barWidths(19, 18.5)).toEqual([100, 97]);
    expect(barWidths(50_000, 23_000)).toEqual([100, 46]);
    expect(barWidths(0, 0)).toEqual([0, 0]);
  });
  it('colours a card by its worst effect', () => {
    expect(changeTone([{ verdict: 'noise' }, { verdict: 'worse' }, { verdict: 'better' }])).toBe('bad');
    expect(changeTone([{ verdict: 'noise' }, { verdict: 'better' }])).toBe('good');
    expect(changeTone([{ verdict: 'noise' }])).toBe('flat');
  });
  it('calls a move under three percent noise and judges the rest by which way is better', () => {
    expect(shiftOf(100, 102, true).tone).toBe('flat');
    expect(shiftOf(100, 90, true).tone).toBe('good');
    expect(shiftOf(100, 90, false).tone).toBe('bad');
    expect(shiftOf(0, 5, false)).toEqual({ pct: 0, tone: 'flat' });
  });
  it('averages a week either side of a change and says when there is less', () => {
    expect(effectWindows(20, 30)).toEqual({ before: 7, after: 7 });
    expect(effectWindows(1, 30)).toEqual({ before: 1, after: 7 });
    expect(effectWindows(28, 30)).toEqual({ before: 7, after: 2 });
    expect(effectNote(7, 7)).toBe('Среднее за 7 дней до и 7 дней после');
    expect(effectNote(1, 7)).toBe('Среднее за 1 день до и 7 дней после — меньше недели данных');
    expect(effectNote(7, 2)).toContain('2 дня после');
  });
  it('skips the days without numbers when averaging', () => {
    expect(meanOf([1, null, 3, 5], 0, 3)).toBe(2);
    expect(meanOf([null, null], 0, 2)).toBeNull();
  });
});

describe('waste and peers', () => {
  it('splits the bar by waste and the row by all spend', () => {
    const waste = { wasteUsd: 1080, totalUsd: 3450 };
    const v = wasteView(waste, 370);
    expect(v.segment.toFixed(2)).toBe('34.26');
    expect(v.share).toBe(11);
    expect(wasteView(waste, 140).share).toBe(4);
    expect(wasteView({ wasteUsd: 0, totalUsd: 0 }, 5)).toEqual({ segment: 0, share: 0 });
    expect(wasteLead(waste)).toBe('$1 080 из $3 450 за период — 31% расхода');
  });
  it('shades a cell by its score', () => {
    expect([1, 0.5, 0, -0.5, -1].map(heatOf)).toEqual(['g2', 'g1', 'n', 'r1', 'r2']);
    expect(heatOf(0.74)).toBe('g1');
    expect(heatOf(-0.25)).toBe('r1');
  });
  it('writes the page lead', () => {
    expect(totalsText({ spendUsd: 810, sessions: 302, mrs: 44 })).toBe('$810 · 302 сессии · 44 MR');
    expect(totalsText({ spendUsd: 96, sessions: 41, mrs: 6 })).toBe('$96 · 41 сессия · 6 MR');
  });
});
