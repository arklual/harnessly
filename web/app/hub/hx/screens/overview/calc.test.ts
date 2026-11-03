import { describe, expect, it } from 'vitest';

import {
  barRects,
  donutSlices,
  limitView,
  pctCss,
  pctOf,
  personBars,
  rankDesc,
  relativeWidths,
  splitRows,
  stripParts,
  teamBar,
  toneOfShare,
  windowIsHot,
} from './calc';

describe('barRects', () => {
  it('gives the largest bar 38 and an empty day 3, the last bar is recent', () => {
    const r = barRects([10, 0, 5]);
    expect(r[0]).toMatchObject({ y: 2, h: 38, recent: false });
    expect(r[1]).toMatchObject({ y: 37, h: 3 });
    expect(r[2]).toMatchObject({ recent: true });
    expect(r[2]!.h).toBeCloseTo(19);
  });
  it('lays 14 bars on a 7.143 step, 5.14 wide', () => {
    const r = barRects(Array.from({ length: 14 }, (_, i) => i));
    expect(r[1]!.x - r[0]!.x).toBeCloseTo(7.143, 3);
    expect(r[0]!.w).toBeCloseTo(5.14, 2);
  });
  it('draws minimal bars when everything is zero and nothing for no data', () => {
    expect(barRects([0, 0]).map((b) => b.h)).toEqual([3, 3]);
    expect(barRects([])).toEqual([]);
  });
});

describe('stripParts, shares and widths', () => {
  it('splits used and rest and clamps', () => {
    expect(stripParts(12.7, 30)).toEqual({ used: 12.7, rest: 17.3 });
    expect(stripParts(40, 30)).toEqual({ used: 30, rest: 0 });
    expect(stripParts(-1, 30)).toEqual({ used: 0, rest: 30 });
  });
  it('pctOf and pctCss', () => {
    expect(pctOf(118, 312)).toBe(38);
    expect(pctOf(1, 0)).toBe(0);
    expect(pctCss(0.37)).toBe('37.0%');
    expect(pctCss(-1)).toBe('0.0%');
  });
  it('rankDesc keeps the order of equals', () => {
    expect(rankDesc([3, 9, 3, 7])).toEqual([1, 3, 0, 2]);
  });
  it('relativeWidths scales to the largest', () => {
    expect(relativeWidths([8.1, 4.05, 0])).toEqual(['100.0%', '50.0%', '0.0%']);
    expect(relativeWidths([0, 0])).toEqual(['0.0%', '0.0%']);
  });
});

describe('donutSlices', () => {
  const s = donutSlices([118, 96, 54, 44]);
  it('takes shares of the half ring', () => {
    expect(s.map((x) => x.pct)).toEqual([38, 31, 17, 14]);
    expect(s[0]!.from).toBe(180);
    expect(s[0]!.to).toBeCloseTo(112.92, 2);
  });
  it("draws the first slice in the board's geometry, to a hundredth", () => {
    expect(s[0]!.d).toBe(
      'M 20.50 100.00 A 4 4 0 0 1 16.60 96.00 A 83.5 83.5 0 0 1 63.83 24.74 A 4 4 0 0 1 69.04 26.78 L 70.20 29.54 A 4 4 0 0 1 68.12 34.89 A 72.5 72.5 0 0 0 27.61 96.00 A 4 4 0 0 1 23.50 100.00 Z',
    );
  });
  it('shares of tokens', () => {
    expect(donutSlices([28, 26, 36, 10]).map((x) => x.pct)).toEqual([28, 26, 36, 10]);
  });
  it('has no path for zero and survives an empty total', () => {
    expect(donutSlices([5, 0])[1]!.d).toBe('');
    expect(donutSlices([0, 0]).map((x) => x.share)).toEqual([0, 0]);
  });
});

describe('tones', () => {
  it('toneOfShare: 90 risk, 75 warn', () => {
    expect(toneOfShare(0.9)).toBe('risk');
    expect(toneOfShare(0.89)).toBe('warn');
    expect(toneOfShare(0.75)).toBe('warn');
    expect(toneOfShare(0.74)).toBe('ok');
  });
  it('windowIsHot from 80', () => {
    expect(windowIsHot(80)).toBe(true);
    expect(windowIsHot(79)).toBe(false);
  });
  it('teamBar needs a limit', () => {
    expect(teamBar(110, 120)).toMatchObject({ pct: 92, w: '91.7%', tone: 'risk' });
    expect(teamBar(130, 120)!.w).toBe('100.0%');
    expect(teamBar(10, null)).toBeNull();
    expect(teamBar(10, 0)).toBeNull();
  });
});

describe('personBars', () => {
  const rows = personBars(
    [
      { userId: 'free', spent: 9, limit: null },
      { userId: 'a', spent: 12, limit: 30 },
      { userId: 'b', spent: 28, limit: 30 },
      { userId: 'idle', spent: 1, limit: 20 },
    ],
    2,
  );
  it('puts the most used limit first and a person without a limit last', () => {
    expect(rows.map((r) => r.userId)).toEqual(['b', 'a', 'idle', 'free']);
  });
  it('marks hot, idle and the cap', () => {
    expect(rows[0]).toMatchObject({ hot: true, idle: false, w: '93.3%', cap: '100.0%' });
    expect(rows[2]).toMatchObject({ hot: false, idle: true });
    expect(rows[3]!.cap).toBeNull();
  });
});

describe('limitView', () => {
  it('computes widths of 30 тыс limit', () => {
    const v = limitView({ spent: 12.7, limit: 30, forecast: 13.4, warnPct: 80 });
    expect(v.usedW).toBe('42.3%');
    expect(v.forecastW).toBe('44.7%');
    expect(v.warnX).toBe('80%');
    expect(v.left).toBeCloseTo(17.3);
    expect(v.over).toBe(false);
    expect(v.willRunOut).toBe(false);
  });
  it('flags an overrun and clamps', () => {
    const v = limitView({ spent: 35, limit: 30, forecast: 50, warnPct: 120 });
    expect(v).toMatchObject({ usedW: '100.0%', over: true, willRunOut: true, left: 0, warnX: '100%' });
  });
});

describe('splitRows', () => {
  it('gives widths with a decimal and whole percents', () => {
    const r = splitRows([8.9, 3.1, 0.4, 0.3]);
    expect(r.map((x) => x.pct)).toEqual([70, 24, 3, 2]);
    expect(r[0]!.w).toBe('70.1%');
    expect(r[1]!.w).toBe('24.4%');
    expect(r[2]!.w).toBe('3.1%');
    expect(r[3]!.w).toBe('2.4%');
  });
  it('zero total gives zero widths', () => {
    expect(splitRows([0, 0])).toEqual([
      { pct: 0, w: '0.0%' },
      { pct: 0, w: '0.0%' },
    ]);
  });
});
