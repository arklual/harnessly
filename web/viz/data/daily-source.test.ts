// SPDX-License-Identifier: Apache-2.0
import { afterEach, describe, expect, it, vi } from 'vitest';

import { DAY, HOUR } from '../model/time';
import { dailySource, DAILY_METRIC } from './daily-source';

const FIRST_DAY = Date.UTC(2026, 9, 5, 21);
const EXTENT = { start: FIRST_DAY, end: FIRST_DAY + 4 * DAY, now: FIRST_DAY + 4 * DAY };

describe('dailySource', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('puts every daily value at noon and keeps the rest of the day at zero', async () => {
    const source = dailySource(EXTENT, FIRST_DAY, EXTENT.now, { a: [5, 7] });
    const hour = await source.load({
      metric: DAILY_METRIC,
      granularity: 'hour',
      range: { start: FIRST_DAY, end: FIRST_DAY + 2 * DAY },
      signal: new AbortController().signal,
    });
    expect(hour.length).toBe(96);
    expect(hour.values.a[12]).toBe(5);
    expect(hour.values.a[11]).toBe(0);
    expect(hour.values.a[24 + 12]).toBe(7);
  });

  it('sums back into the original daily numbers', async () => {
    const source = dailySource(EXTENT, FIRST_DAY, EXTENT.now, { a: [5, 7, 0, 3] });
    const day = await source.load({
      metric: DAILY_METRIC,
      granularity: 'day',
      range: { start: FIRST_DAY, end: FIRST_DAY + 4 * DAY },
      signal: new AbortController().signal,
    });
    expect(Array.from(day.values.a)).toEqual([5, 7, 0, 3]);
  });

  it('leaves days after now as gaps and clamps the last value before now', async () => {
    const now = FIRST_DAY + 2 * DAY + 6 * HOUR;
    const source = dailySource({ ...EXTENT, now }, FIRST_DAY, now, { a: [1, 1, 9, 9] });
    expect(source.extent.now).toBe(now);
    const hour = await source.load({
      metric: DAILY_METRIC,
      granularity: 'hour',
      range: { start: FIRST_DAY, end: FIRST_DAY + 4 * DAY },
      signal: new AbortController().signal,
    });
    expect(hour.length).toBe(2 * 24 + 6);
    expect(hour.values.a[2 * 24 + 5]).toBe(9);
  });

  it('skips NaN days', async () => {
    const source = dailySource(EXTENT, FIRST_DAY, EXTENT.now, { a: [Number.NaN, 4] });
    const hour = await source.load({
      metric: DAILY_METRIC,
      granularity: 'hour',
      range: { start: FIRST_DAY, end: FIRST_DAY + 2 * DAY },
      signal: new AbortController().signal,
    });
    expect(Number.isNaN(hour.values.a[12])).toBe(true);
    expect(hour.values.a[24 + 12]).toBe(4);
  });
});
