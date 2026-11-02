import { describe, expect, it } from 'vitest';

import { ago, awayFor, rub, toneOf } from './format';

describe('overview texts', () => {
  it('writes money the way the board does', () => {
    expect(rub(312000).replace(/\s/g, ' ')).toBe('312 тыс ₽');
    expect(rub(10400).replace(/\s/g, ' ')).toBe('10,4 тыс ₽');
  });

  it('says how long ago', () => {
    const now = Date.parse('2026-10-20T12:00:00Z');
    expect(ago('2026-10-20T09:00:00Z', now)).toBe('3 ч назад');
    expect(ago('2026-10-19T09:00:00Z', now)).toBe('вчера');
    expect(ago('2026-10-18T09:00:00Z', now)).toBe('2 дня назад');
    expect(awayFor('2026-10-11T12:00:00Z', now)).toBe('9 дней не заходит');
    expect(awayFor('2026-10-19T12:00:00Z', now)).toBeNull();
  });

  it('marks 80% and above as bad', () => {
    expect(toneOf(79)).toBe('warn');
    expect(toneOf(80)).toBe('bad');
  });
});
