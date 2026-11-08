// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest';

import {
  formatBucket,
  formatCompact,
  formatDay,
  formatDelta,
  formatNumber,
  formatRange,
  formatSpan,
  formatTime,
  formatValue,
  plural,
  VIZ_UNIT_COUNT,
  VIZ_UNIT_PERCENT,
  VIZ_UNIT_RUB,
} from './format';
import { DAY, HOUR, WEEK } from './time';

/** ru-RU отделяет тысячи неразрывным или узким пробелом: сравниваем с обычным. */
const plain = (s: string) => s.replace(/[\u00a0\u202f]/g, ' ');

/** Московская полночь 7 октября 2026 (среда). */
const WED = Date.UTC(2026, 9, 6, 21);
const MON = Date.UTC(2026, 9, 4, 21);

const TICKETS = ['билет', 'билета', 'билетов'] as const;

describe('plural', () => {
  it.each([
    [0, 'билетов'],
    [1, 'билет'],
    [2, 'билета'],
    [4, 'билета'],
    [5, 'билетов'],
    [11, 'билетов'],
    [12, 'билетов'],
    [14, 'билетов'],
    [21, 'билет'],
    [22, 'билета'],
    [25, 'билетов'],
    [101, 'билет'],
    [111, 'билетов'],
    [112, 'билетов'],
    [1002, 'билета'],
  ])('%i → %s', (n, expected) => {
    expect(plural(n, TICKETS)).toBe(expected);
  });

  it('rounds and ignores sign', () => {
    expect(plural(-1, TICKETS)).toBe('билет');
    expect(plural(1.2, TICKETS)).toBe('билет');
    expect(plural(4.6, TICKETS)).toBe('билетов');
  });
});

describe('formatNumber', () => {
  it('groups thousands and rounds', () => {
    expect(plain(formatNumber(1234567))).toBe('1 234 567');
    expect(formatNumber(2.6)).toBe('3');
  });

  it('keeps one decimal when asked, with a comma', () => {
    expect(formatNumber(2.46, 1)).toBe('2,5');
  });
});

describe('formatCompact', () => {
  it('writes small numbers as is', () => {
    expect(plain(formatCompact(820))).toBe('820');
    expect(plain(formatCompact(0))).toBe('0');
    expect(plain(formatCompact(12))).toBe('12');
  });

  it('keeps a decimal below ten', () => {
    expect(formatCompact(5.4)).toBe('5,4');
  });

  it('abbreviates thousands', () => {
    expect(plain(formatCompact(1200))).toBe('1,2 тыс');
    expect(plain(formatCompact(1000))).toBe('1 тыс');
    expect(plain(formatCompact(48_000))).toBe('48 тыс');
    expect(plain(formatCompact(48_400))).toBe('48 тыс');
  });

  it('abbreviates millions', () => {
    expect(plain(formatCompact(1_500_000))).toBe('1,5 млн');
    expect(plain(formatCompact(12_000_000))).toBe('12 млн');
  });

  it('keeps the sign', () => {
    expect(plain(formatCompact(-1200))).toBe('-1,2 тыс');
  });
});

describe('formatValue', () => {
  it('formats counts short and long', () => {
    expect(plain(formatValue(124, VIZ_UNIT_COUNT))).toBe('124');
    expect(plain(formatValue(48_000, VIZ_UNIT_COUNT))).toBe('48 тыс');
    expect(plain(formatValue(1234, VIZ_UNIT_COUNT, { long: true }))).toBe('1 234');
  });

  it('adds the word in long form when forms are given', () => {
    const unit = { kind: 'count', forms: TICKETS } as const;
    expect(plain(formatValue(1, unit, { long: true }))).toBe('1 билет');
    expect(plain(formatValue(2, unit, { long: true }))).toBe('2 билета');
    expect(plain(formatValue(5, unit, { long: true }))).toBe('5 билетов');
    expect(plain(formatValue(11, unit, { long: true }))).toBe('11 билетов');
    expect(plain(formatValue(21, unit, { long: true }))).toBe('21 билет');
    // Короткая запись слов не добавляет.
    expect(plain(formatValue(5, unit))).toBe('5');
  });

  it('formats currency short and long', () => {
    expect(plain(formatValue(248_000, VIZ_UNIT_RUB))).toBe('248 тыс ₽');
    expect(plain(formatValue(248_000, VIZ_UNIT_RUB, { long: true }))).toBe('248 000 ₽');
    expect(plain(formatValue(820, VIZ_UNIT_RUB))).toBe('820 ₽');
  });

  it('formats percent with one decimal', () => {
    expect(formatValue(12.34, VIZ_UNIT_PERCENT)).toBe('12,3%');
    expect(formatValue(50, VIZ_UNIT_PERCENT)).toBe('50%');
  });

  it('renders a dash for a gap or non-finite value in every unit', () => {
    for (const unit of [VIZ_UNIT_COUNT, VIZ_UNIT_RUB, VIZ_UNIT_PERCENT]) {
      expect(formatValue(Number.NaN, unit)).toBe('—');
      expect(formatValue(Number.POSITIVE_INFINITY, unit, { long: true })).toBe('—');
    }
  });

  it('does not turn a real zero into a dash', () => {
    expect(formatValue(0, VIZ_UNIT_COUNT)).toBe('0');
    expect(plain(formatValue(0, VIZ_UNIT_RUB, { long: true }))).toBe('0 ₽');
  });
});

describe('formatDelta', () => {
  it('marks growth with an up arrow and plus', () => {
    expect(formatDelta(24)).toBe('↑ +24%');
    expect(formatDelta(23.6)).toBe('↑ +24%');
  });

  it('marks decline with a down arrow and a real minus sign', () => {
    expect(formatDelta(-42)).toBe('↓ −42%');
    expect(formatDelta(-42)).toContain('−');
  });

  it('treats zero as growth', () => {
    expect(formatDelta(0)).toBe('↑ +0%');
  });

  it('renders a dash when there is no baseline', () => {
    expect(formatDelta(Number.NaN)).toBe('—');
    expect(formatDelta(Number.POSITIVE_INFINITY)).toBe('—');
  });
});

describe('date and time helpers', () => {
  it('formats day and time in Moscow time', () => {
    expect(formatDay(WED)).toBe('7 окт');
    expect(formatTime(WED + 14 * HOUR + 5 * 60_000)).toBe('14:05');
    expect(formatTime(WED)).toBe('00:00');
  });

  it('formats spans', () => {
    expect(formatSpan(44 * HOUR)).toBe('1 д 20 ч');
    expect(formatSpan(2 * DAY)).toBe('2 д');
    expect(formatSpan(5 * HOUR)).toBe('5 ч');
    expect(formatSpan(0)).toBe('0 ч');
  });
});

describe('formatBucket', () => {
  it('labels an hour with weekday and time range', () => {
    expect(formatBucket(WED + 14 * HOUR, 'hour', HOUR)).toBe('7 окт, ср · 14:00–15:00');
  });

  it('wraps the hour range over midnight', () => {
    expect(formatBucket(WED + 23 * HOUR, 'hour', HOUR)).toBe('7 окт, ср · 23:00–00:00');
  });

  it('labels a day with the full weekday', () => {
    expect(formatBucket(WED, 'day', DAY)).toBe('7 окт, среда');
  });

  it('labels a week as first day – last day', () => {
    expect(formatBucket(MON, 'week', WEEK)).toBe('5 окт – 11 окт');
  });

  it('respects a different zone', () => {
    const utc = { offset: () => 0 };
    expect(formatBucket(Date.UTC(2026, 9, 7, 14), 'hour', HOUR, utc)).toBe('7 окт, ср · 14:00–15:00');
  });
});

describe('formatRange', () => {
  it('shows start, end and length', () => {
    expect(formatRange(WED + 14 * HOUR, WED + DAY + 10 * HOUR)).toBe('7 окт 14:00 → 8 окт 10:00 · 20 ч');
  });

  it('collapses a whole-day range to day names', () => {
    expect(formatRange(WED, WED + 2 * DAY)).toBe('7 окт → 8 окт · 2 д');
  });

  it('shows only the end time within one day', () => {
    expect(formatRange(WED + 10 * HOUR, WED + 14 * HOUR)).toBe('7 окт 10:00 → 14:00 · 4 ч');
  });
});

describe('formatCompact на границе тысяч и миллионов', () => {
  it('999 999 пишет миллионами, а не «1 000 тыс»', () => {
    expect(formatCompact(999_999).replace(/\s/g, ' ')).toBe('1 млн');
  });
});
