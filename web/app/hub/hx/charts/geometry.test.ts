import { describe, expect, it } from 'vitest';

import { barRects, isHot, pctText, share, sparkPath } from './geometry';

describe('chart geometry', () => {
  it('draws bars like the board', () => {
    const r = barRects([12, 9, 6, 3, 12, 0.5]);
    expect(r).toHaveLength(6);
    expect(r[0]).toMatchObject({ x: 1, width: 5.14 });
    expect(r[0]?.y).toBeCloseTo(2);
    expect(r[1]?.height).toBeCloseTo(28.5);
    expect(r[5]?.height).toBe(3);
    expect(r[5]?.recent).toBe(true);
    expect(r[4]?.recent).toBe(false);
  });
  it('keeps the last 14 bars and survives an empty or zero series', () => {
    expect(barRects(Array.from({ length: 20 }, (_, i) => i))).toHaveLength(14);
    expect(barRects([])).toEqual([]);
    expect(barRects([0, 0])[0]?.height).toBe(3);
  });
  it('draws the load line from the bottom up', () => {
    expect(sparkPath([])).toBe('');
    expect(sparkPath([0, 100])).toBe('M0.0 24.0 L100.0 2.0');
    expect(sparkPath([50])).toBe('M0.0 13.0');
  });
  it('flags hot loads and prints percents', () => {
    expect(isHot(85)).toBe(true);
    expect(isHot(84)).toBe(false);
    expect(isHot(null)).toBe(false);
    expect(pctText(41)).toBe('41%');
    expect(pctText(0)).toBe('—');
    expect(pctText(null)).toBe('—');
  });
  it('turns counts into shares', () => {
    expect(share(1, 4)).toBe('25.0%');
    expect(share(1, 0)).toBe('0.0%');
    expect(share(5, 4)).toBe('100.0%');
  });
});
