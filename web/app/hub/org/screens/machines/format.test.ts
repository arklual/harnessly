import { describe, expect, it } from 'vitest';

import { ago, gb, plural, priceOf, rub, strip } from './format';

describe('machine formats', () => {
  it('declines Russian counts', () => {
    expect([1, 2, 5, 11, 21, 22].map((n) => plural(n, 'ядро', 'ядра', 'ядер'))).toEqual([
      'ядро',
      'ядра',
      'ядер',
      'ядер',
      'ядро',
      'ядра',
    ]);
  });
  it('says how long ago', () => {
    const now = Date.parse('2026-10-31T12:00:00Z');
    expect(ago('2026-10-31T10:00:00Z', now)).toBe('2 ч');
    expect(ago('2026-10-19T12:00:00Z', now)).toBe('12 дн');
    expect(ago('2026-10-31T11:20:00Z', now)).toBe('40 мин');
    expect(ago(null, now)).toBe('');
  });
  it('prices and sizes', () => {
    expect(priceOf({ core: 450, ramGb: 120, diskGb: 8 }, 32, 128, 500)).toBe(33760);
    expect(gb(1000)).toBe('1 ТБ');
    expect(gb(500)).toBe('500 ГБ');
    expect(rub(33760).replace(/\s/g, ' ')).toBe('33 760 ₽');
  });
  it('splits the strip by state', () => {
    expect(strip({ online: 2, idle: 1, offline: 1 }).map((s) => s.pct)).toEqual([50, 25, 25]);
  });
});
