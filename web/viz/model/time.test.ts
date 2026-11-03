// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest';

import {
  DAY,
  fixedOffsetZone,
  floorTo,
  HOUR,
  isMidnight,
  MOSCOW_ZONE,
  overviewGranularity,
  pickGranularity,
  tickLevelFor,
  ticksFor,
  WEEK,
  wallTime,
} from './time';

/** Московская полночь 7 октября 2026 (среда) = 6 октября 21:00 UTC. */
const WED_MIDNIGHT = Date.UTC(2026, 9, 6, 21);
/** Понедельник 5 октября 2026, 00:00 по Москве. */
const MON_MIDNIGHT = Date.UTC(2026, 9, 4, 21);

describe('floorTo', () => {
  it('floors to the hour in the zone', () => {
    const t = WED_MIDNIGHT + 14 * HOUR + 37 * 60_000 + 12_000;
    expect(floorTo(t, 'hour')).toBe(WED_MIDNIGHT + 14 * HOUR);
  });

  it('floors to local midnight, not UTC midnight', () => {
    const t = WED_MIDNIGHT + 23 * HOUR + 59 * 60_000;
    expect(floorTo(t, 'day')).toBe(WED_MIDNIGHT);
    // 22:00 UTC на 6 октября — это уже 7 октября в Москве.
    expect(floorTo(Date.UTC(2026, 9, 6, 22), 'day')).toBe(WED_MIDNIGHT);
    expect(floorTo(Date.UTC(2026, 9, 6, 20, 59), 'day')).toBe(WED_MIDNIGHT - DAY);
  });

  it('is idempotent on boundaries', () => {
    expect(floorTo(WED_MIDNIGHT, 'day')).toBe(WED_MIDNIGHT);
    expect(floorTo(WED_MIDNIGHT, 'hour')).toBe(WED_MIDNIGHT);
    expect(floorTo(MON_MIDNIGHT, 'week')).toBe(MON_MIDNIGHT);
  });

  it('starts the week on Monday at local midnight', () => {
    expect(floorTo(WED_MIDNIGHT + 5 * HOUR, 'week')).toBe(MON_MIDNIGHT);
    // Воскресенье 11 октября, 23:59 по Москве — ещё прежняя неделя.
    const sunday = MON_MIDNIGHT + 7 * DAY - 60_000;
    expect(floorTo(sunday, 'week')).toBe(MON_MIDNIGHT);
    // Понедельник 12 октября, 00:00 — уже следующая.
    expect(floorTo(MON_MIDNIGHT + WEEK, 'week')).toBe(MON_MIDNIGHT + WEEK);
    expect(wallTime(floorTo(WED_MIDNIGHT, 'week')).weekday).toBe(1);
  });

  it('respects the zone offset', () => {
    const utc = fixedOffsetZone(0);
    const t = Date.UTC(2026, 9, 7, 10, 30);
    expect(floorTo(t, 'day', utc)).toBe(Date.UTC(2026, 9, 7));
    expect(floorTo(t, 'day', MOSCOW_ZONE)).toBe(Date.UTC(2026, 9, 6, 21));
    expect(floorTo(t, 'week', utc)).toBe(Date.UTC(2026, 9, 5));
  });

  it('works for moments before the epoch', () => {
    const t = Date.UTC(1969, 11, 30, 12);
    const day = floorTo(t, 'day', fixedOffsetZone(0));
    expect(day).toBe(Date.UTC(1969, 11, 30));
    expect(wallTime(floorTo(t, 'week', fixedOffsetZone(0)), fixedOffsetZone(0)).weekday).toBe(1);
  });
});

describe('isMidnight', () => {
  it('detects local midnight only', () => {
    expect(isMidnight(WED_MIDNIGHT)).toBe(true);
    expect(isMidnight(WED_MIDNIGHT + HOUR)).toBe(false);
    expect(isMidnight(Date.UTC(2026, 9, 7))).toBe(false);
  });
});

describe('wallTime', () => {
  it('reads local parts', () => {
    expect(wallTime(WED_MIDNIGHT + 14 * HOUR + 5 * 60_000)).toEqual({
      year: 2026,
      month: 9,
      day: 7,
      hours: 14,
      minutes: 5,
      weekday: 3,
    });
  });
});

describe('pickGranularity', () => {
  const width = 1000;
  const spanOfHours = (hours: number) => hours * HOUR;
  const spanOfDays = (days: number) => days * DAY;

  it('picks the finest granularity whose bucket is wide enough to enter', () => {
    // 100 часов на 1000 px: час = 10 px ≥ 9.
    expect(pickGranularity(spanOfHours(100), width)).toBe('hour');
    // 120 часов: час = 8,3 px < 9, сутки = 200 px.
    expect(pickGranularity(spanOfHours(120), width)).toBe('day');
    // 140 суток: сутки = 7,1 px ≥ 7.
    expect(pickGranularity(spanOfDays(140), width)).toBe('day');
    // 150 суток: 6,7 px < 7 → недели.
    expect(pickGranularity(spanOfDays(150), width)).toBe('week');
  });

  it('stays on the current granularity until the leave threshold', () => {
    // Час 8,3 px: между leave (7) и enter (9).
    expect(pickGranularity(spanOfHours(120), width, null)).toBe('day');
    expect(pickGranularity(spanOfHours(120), width, 'hour')).toBe('hour');
    // Ниже leave (6,7 px) уходят и с часа.
    expect(pickGranularity(spanOfHours(150), width, 'hour')).toBe('day');
  });

  it('does the same for days (enter 7, leave 5)', () => {
    expect(pickGranularity(spanOfDays(150), width, null)).toBe('week');
    expect(pickGranularity(spanOfDays(150), width, 'day')).toBe('day');
    expect(pickGranularity(spanOfDays(200), width, 'day')).toBe('day');
    expect(pickGranularity(spanOfDays(210), width, 'day')).toBe('week');
  });

  it('does not flicker at the boundary: enter is stricter than leave', () => {
    let current = pickGranularity(spanOfHours(100), width);
    expect(current).toBe('hour');
    // Окно чуть-чуть раздвигается и сжимается вокруг границы 111 часов.
    for (const hours of [112, 118, 114, 119, 113]) {
      current = pickGranularity(spanOfHours(hours), width, current);
      expect(current).toBe('hour');
    }
  });

  it('accepts custom thresholds', () => {
    const thresholds = { hour: { enter: 20, leave: 15 } };
    // Час = 10 px < 20 → день не описан в порогах → неделя.
    expect(pickGranularity(spanOfHours(100), width, null, thresholds)).toBe('week');
    expect(pickGranularity(spanOfHours(40), width, null, thresholds)).toBe('hour');
  });

  it('survives a zero span', () => {
    expect(pickGranularity(0, width)).toBe('hour');
  });
});

describe('overviewGranularity', () => {
  it('chooses by history length', () => {
    expect(overviewGranularity({ start: 0, end: 5 * DAY })).toBe('hour');
    expect(overviewGranularity({ start: 0, end: 60 * DAY })).toBe('day');
    expect(overviewGranularity({ start: 0, end: 800 * DAY })).toBe('week');
  });
});

describe('tickLevelFor', () => {
  it('picks the finest level whose spacing is at least minGapPx', () => {
    // Сутки на 960 px: час = 40 px < 64, 2 часа = 80 px.
    expect(tickLevelFor(DAY, 960)).toBe(2 * HOUR);
    // Неделя на 700 px: 6 часов = 25 px, 12 часов = 50, сутки = 100.
    expect(tickLevelFor(WEEK, 700)).toBe(DAY);
    // 90 суток на 900 px: 2 суток = 20 px, неделя = 70 px.
    expect(tickLevelFor(90 * DAY, 900)).toBe(WEEK);
  });

  it('falls back to months for very long spans', () => {
    expect(tickLevelFor(5 * 365 * DAY, 600)).toBe('month');
  });

  it('honours a custom minimum gap', () => {
    expect(tickLevelFor(DAY, 960, 30)).toBe(HOUR);
  });
});

describe('ticksFor', () => {
  it('aligns day ticks to local midnight', () => {
    const range = { start: WED_MIDNIGHT + 10 * HOUR, end: WED_MIDNIGHT + 3 * DAY + 10 * HOUR };
    const ticks = ticksFor(DAY, range);
    expect(ticks).toEqual([WED_MIDNIGHT + DAY, WED_MIDNIGHT + 2 * DAY, WED_MIDNIGHT + 3 * DAY]);
    expect(ticks.every((t) => isMidnight(t))).toBe(true);
  });

  it('includes ticks exactly on the range edges', () => {
    const ticks = ticksFor(DAY, { start: WED_MIDNIGHT, end: WED_MIDNIGHT + 2 * DAY });
    expect(ticks).toEqual([WED_MIDNIGHT, WED_MIDNIGHT + DAY, WED_MIDNIGHT + 2 * DAY]);
  });

  it('aligns multi-hour ticks to the local clock', () => {
    const ticks = ticksFor(6 * HOUR, { start: WED_MIDNIGHT + HOUR, end: WED_MIDNIGHT + DAY });
    expect(ticks).toEqual([
      WED_MIDNIGHT + 6 * HOUR,
      WED_MIDNIGHT + 12 * HOUR,
      WED_MIDNIGHT + 18 * HOUR,
      WED_MIDNIGHT + DAY,
    ]);
  });

  it('aligns week ticks to Mondays', () => {
    const ticks = ticksFor(WEEK, { start: WED_MIDNIGHT, end: WED_MIDNIGHT + 21 * DAY });
    expect(ticks).toEqual([MON_MIDNIGHT + WEEK, MON_MIDNIGHT + 2 * WEEK, MON_MIDNIGHT + 3 * WEEK]);
    expect(ticks.every((t) => wallTime(t).weekday === 1)).toBe(true);
  });

  it('produces month ticks on the first of the month, local midnight', () => {
    const ticks = ticksFor('month', { start: Date.UTC(2026, 8, 15), end: Date.UTC(2026, 11, 15) });
    expect(ticks).toEqual([
      Date.UTC(2026, 9, 1) - 3 * HOUR,
      Date.UTC(2026, 10, 1) - 3 * HOUR,
      Date.UTC(2026, 11, 1) - 3 * HOUR,
    ]);
    for (const t of ticks) {
      expect(wallTime(t).day).toBe(1);
      expect(isMidnight(t)).toBe(true);
    }
  });

  it('rolls month ticks over the year boundary', () => {
    const ticks = ticksFor('month', { start: Date.UTC(2026, 10, 15), end: Date.UTC(2027, 1, 15) });
    expect(ticks.map((t) => wallTime(t).month)).toEqual([11, 0, 1]);
    expect(ticks.map((t) => wallTime(t).year)).toEqual([2026, 2027, 2027]);
  });

  it('keeps a month tick that lies before range start out', () => {
    const start = Date.UTC(2026, 9, 1) - 3 * HOUR + 1;
    const ticks = ticksFor('month', { start, end: start + 40 * DAY });
    expect(ticks[0]).toBe(Date.UTC(2026, 10, 1) - 3 * HOUR);
  });

  it('does not exceed the limit', () => {
    expect(ticksFor(HOUR, { start: 0, end: 1000 * DAY }, MOSCOW_ZONE, 50)).toHaveLength(50);
    expect(ticksFor('month', { start: 0, end: 100 * 365 * DAY }, MOSCOW_ZONE, 5)).toHaveLength(5);
  });

  it('returns nothing for an empty range between ticks', () => {
    expect(ticksFor(DAY, { start: WED_MIDNIGHT + HOUR, end: WED_MIDNIGHT + 2 * HOUR })).toEqual([]);
  });
});
