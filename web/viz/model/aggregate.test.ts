// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest';

import {
  bucketHasData,
  bucketIndex,
  bucketize,
  frameSum,
  prefixSums,
  rangeSum,
  type VizHourlySeries,
} from './aggregate';
import { DAY, fixedOffsetZone, HOUR, WEEK } from './time';
import type { VizBucketFrame } from './types';

/** Московская полночь 6 октября 2026 (вторник). */
const TUE = Date.UTC(2026, 9, 5, 21);
/** Понедельник 5 октября 2026, 00:00 по Москве. */
const MON = Date.UTC(2026, 9, 4, 21);

const filled = (n: number, v: number) => Array.from({ length: n }, () => v);

function hourly(start: number, a: number[], extra: Record<string, number[]> = {}): VizHourlySeries<string> {
  return { start, length: a.length, values: { a, ...extra } };
}

describe('prefixSums / rangeSum', () => {
  it('sums any slice in constant time', () => {
    const p = prefixSums([1, 2, 3, 4]);
    expect(rangeSum(p, 0, 4)).toBe(10);
    expect(rangeSum(p, 1, 3)).toBe(5);
  });

  it('returns NaN for an empty or inverted range', () => {
    const p = prefixSums([1, 2, 3]);
    expect(rangeSum(p, 2, 2)).toBeNaN();
    expect(rangeSum(p, 3, 1)).toBeNaN();
  });

  it('clamps indexes to the series', () => {
    const p = prefixSums([1, 2, 3]);
    expect(rangeSum(p, -5, 2)).toBe(3);
    expect(rangeSum(p, 1, 99)).toBe(5);
    expect(rangeSum(p, 10, 20)).toBeNaN();
  });

  it('treats NaN as zero in the sum but counts it as a gap', () => {
    const p = prefixSums([1, Number.NaN, 3]);
    expect(p.gaps[3]).toBe(1);
    expect(rangeSum(p, 0, 3)).toBe(4);
  });
});

describe('bucketize', () => {
  it('collapses hours into Moscow days aligned to local midnight', () => {
    const frame = bucketize(hourly(TUE, filled(48, 1)), 'day');
    expect(frame.granularity).toBe('day');
    expect(frame.step).toBe(DAY);
    expect(frame.start).toBe(TUE);
    expect(frame.length).toBe(2);
    expect(Array.from(frame.values.a)).toEqual([24, 24]);
  });

  it('keeps hours as is', () => {
    const frame = bucketize(hourly(TUE + 5 * HOUR, [3, 1, 4]), 'hour');
    expect(frame.start).toBe(TUE + 5 * HOUR);
    expect(frame.length).toBe(3);
    expect(Array.from(frame.values.a)).toEqual([3, 1, 4]);
  });

  it('aligns day buckets by the zone, not by UTC', () => {
    const series = hourly(TUE, filled(48, 1));
    const utc = bucketize(series, 'day', fixedOffsetZone(0));
    expect(utc.start).toBe(Date.UTC(2026, 9, 5));
    expect(utc.length).toBe(3);
    // Полный сутки внутри данных суммируются целиком.
    expect(utc.values.a[1]).toBe(24);
  });

  it('rounds the first bucket down to the start of the day and the count up', () => {
    const series = hourly(TUE + 10 * HOUR, filled(30, 1));
    const frame = bucketize(series, 'day');
    expect(frame.start).toBe(TUE);
    expect(frame.length).toBe(2);
  });

  it('starts weekly buckets on Monday', () => {
    const series = hourly(MON + 2 * DAY, filled(14 * 24, 1));
    const frame = bucketize(series, 'week');
    expect(frame.start).toBe(MON);
    expect(frame.step).toBe(WEEK);
    expect(frame.length).toBe(3);
    // Среднюю неделю данные покрывают целиком.
    expect(frame.values.a[1]).toBe(168);
  });

  it('buckets every series of the frame', () => {
    const frame = bucketize(hourly(TUE, filled(24, 1), { b: filled(24, 2) }), 'day');
    expect(Array.from(frame.values.a)).toEqual([24]);
    expect(Array.from(frame.values.b)).toEqual([48]);
  });

  it('returns an empty frame for an empty series', () => {
    const frame = bucketize(hourly(TUE, []), 'day');
    expect(frame.length).toBe(0);
    expect(frame.values.a).toHaveLength(0);
  });

  describe('gap rules', () => {
    it('sums the remaining hours when a single hour is missing', () => {
      const a = filled(24, 1);
      a[10] = Number.NaN;
      expect(bucketize(hourly(TUE, a), 'day').values.a[0]).toBe(23);
    });

    it('keeps a real zero as a zero, not a gap', () => {
      expect(bucketize(hourly(TUE, filled(24, 0)), 'day').values.a[0]).toBe(0);
    });

    it('returns the sum when exactly half of the hours are missing', () => {
      const a = filled(24, 1);
      for (let i = 0; i < 12; i++) a[i] = Number.NaN;
      expect(bucketize(hourly(TUE, a), 'day').values.a[0]).toBe(12);
    });

    it('returns NaN when more than half of the hours are missing', () => {
      const a = filled(24, 1);
      for (let i = 0; i < 13; i++) a[i] = Number.NaN;
      expect(bucketize(hourly(TUE, a), 'day').values.a[0]).toBeNaN();
    });

    it('applies the rule per bucket', () => {
      const a = filled(48, 1);
      for (let i = 24; i < 48; i++) a[i] = Number.NaN;
      const frame = bucketize(hourly(TUE, a), 'day');
      expect(frame.values.a[0]).toBe(24);
      expect(frame.values.a[1]).toBeNaN();
    });

    it('applies the rule per series', () => {
      const b = filled(24, Number.NaN);
      const frame = bucketize(hourly(TUE, filled(24, 1), { b }), 'day');
      expect(frame.values.a[0]).toBe(24);
      expect(frame.values.b[0]).toBeNaN();
    });
  });
});

describe('bucketIndex', () => {
  const frame = { start: 1000, step: DAY };

  it('finds the bucket that contains the moment', () => {
    expect(bucketIndex(frame, 1000)).toBe(0);
    expect(bucketIndex(frame, 1000 + DAY - 1)).toBe(0);
    expect(bucketIndex(frame, 1000 + DAY)).toBe(1);
    expect(bucketIndex(frame, 1000 + 5.5 * DAY)).toBe(5);
  });

  it('goes negative before the frame', () => {
    expect(bucketIndex(frame, 999)).toBe(-1);
    expect(bucketIndex(frame, 1000 - 2 * DAY)).toBe(-2);
  });
});

describe('bucketHasData', () => {
  const frame: VizBucketFrame<'a' | 'b'> = {
    granularity: 'day',
    start: 0,
    step: DAY,
    length: 3,
    values: { a: [1, Number.NaN, Number.NaN], b: [Number.NaN, 0, Number.NaN] },
  };

  it('is true when any series has a value (zero included)', () => {
    expect(bucketHasData(frame, 0)).toBe(true);
    expect(bucketHasData(frame, 1)).toBe(true);
  });

  it('is false when all series have a gap', () => {
    expect(bucketHasData(frame, 2)).toBe(false);
  });
});

describe('frameSum', () => {
  const frame: VizBucketFrame<'a'> = {
    granularity: 'day',
    start: 0,
    step: DAY,
    length: 3,
    values: { a: [10, 20, 30] },
  };

  it('sums whole buckets', () => {
    expect(frameSum(frame, 'a', 0, 3 * DAY)).toBe(60);
    expect(frameSum(frame, 'a', DAY, 2 * DAY)).toBe(20);
  });

  it('takes a proportional share of partially covered buckets', () => {
    expect(frameSum(frame, 'a', 0, DAY / 2)).toBeCloseTo(5, 10);
    expect(frameSum(frame, 'a', DAY / 2, DAY + DAY / 2)).toBeCloseTo(5 + 10, 10);
    expect(frameSum(frame, 'a', DAY / 4, DAY / 2)).toBeCloseTo(2.5, 10);
  });

  it('ignores the part outside the frame', () => {
    expect(frameSum(frame, 'a', -5 * DAY, DAY)).toBe(10);
    expect(frameSum(frame, 'a', 2 * DAY, 10 * DAY)).toBe(30);
    expect(frameSum(frame, 'a', 5 * DAY, 6 * DAY)).toBe(0);
    expect(frameSum(frame, 'a', -2 * DAY, -DAY)).toBe(0);
  });

  it('skips gaps instead of poisoning the sum', () => {
    const gappy: VizBucketFrame<'a'> = { ...frame, values: { a: [10, Number.NaN, 30] } };
    expect(frameSum(gappy, 'a', 0, 3 * DAY)).toBe(40);
  });

  it('returns 0 for an unknown series or an empty/inverted window', () => {
    expect(frameSum(frame, 'zzz', 0, 3 * DAY)).toBe(0);
    expect(frameSum(frame, 'a', DAY, DAY)).toBe(0);
    expect(frameSum(frame, 'a', 2 * DAY, DAY)).toBe(0);
  });

  it('works on an hourly frame', () => {
    const hours: VizBucketFrame<'a'> = {
      granularity: 'hour',
      start: 0,
      step: HOUR,
      length: 4,
      values: { a: [4, 4, 4, 4] },
    };
    expect(frameSum(hours, 'a', HOUR, 3 * HOUR)).toBe(8);
    expect(frameSum(hours, 'a', HOUR / 2, 2 * HOUR)).toBeCloseTo(6, 10);
  });
});
