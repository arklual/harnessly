// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest';

import { computeEffect, defaultEffectWindow } from './effect';
import { layoutMarkers, pxPerDay, visiblePriority } from './markers';
import { DAY, HOUR } from './time';
import type { VizBucketFrame, VizMarker } from './types';

/** Московская полночь 1 октября 2026. */
const S = Date.UTC(2026, 8, 30, 21);

/** 10 суток: серия `a` продаёт по 10 в день, с 6-го дня — по 20; `b` — по 5. */
const frame: VizBucketFrame<'a' | 'b'> = {
  granularity: 'day',
  start: S,
  step: DAY,
  length: 10,
  values: {
    a: [10, 10, 10, 10, 10, 20, 20, 20, 20, 20],
    b: [5, 5, 5, 5, 5, 5, 5, 5, 5, 5],
  },
};

const NOW = S + 10 * DAY;
const marker = (patch: Partial<VizMarker<'a' | 'b'>> = {}): VizMarker<'a' | 'b'> => ({
  id: 'm',
  t: S + 5 * DAY,
  kind: 'price',
  label: 'Цена',
  ...patch,
});

describe('defaultEffectWindow', () => {
  it('is a day on hourly data and three days otherwise', () => {
    expect(defaultEffectWindow(HOUR)).toBe(DAY);
    expect(defaultEffectWindow(DAY)).toBe(3 * DAY);
    expect(defaultEffectWindow(7 * DAY)).toBe(3 * DAY);
  });
});

describe('computeEffect', () => {
  it('compares the daily average before and after the marker for an affected series', () => {
    const effect = computeEffect(frame, marker({ series: ['a'] }), NOW);
    expect(effect.markerId).toBe('m');
    expect(effect.window).toBe(3 * DAY);
    expect(effect.rows).toHaveLength(1);
    const [row] = effect.rows;
    expect(row.id).toBe('a');
    expect(row.before).toBeCloseTo(10, 10);
    expect(row.after).toBeCloseTo(20, 10);
    expect(row.deltaPct).toBeCloseTo(100, 10);
    expect(effect.partial).toBe(false);
    expect(effect.afterEnd).toBe(marker().t + 3 * DAY);
  });

  it('returns one row per affected series', () => {
    const effect = computeEffect(frame, marker({ series: ['a', 'b'] }), NOW);
    expect(effect.rows.map((r) => r.id)).toEqual(['a', 'b']);
    expect(effect.rows[1].deltaPct).toBeCloseTo(0, 10);
  });

  it('returns a single total row when the marker touches everything', () => {
    for (const series of [undefined, [] as const]) {
      const effect = computeEffect(frame, marker({ series }), NOW);
      expect(effect.rows).toHaveLength(1);
      const [row] = effect.rows;
      expect(row.id).toBeNull();
      expect(row.before).toBeCloseTo(15, 10);
      expect(row.after).toBeCloseTo(25, 10);
      expect(row.deltaPct).toBeCloseTo((25 / 15 - 1) * 100, 10);
    }
  });

  it('ignores marker series that are not in the frame', () => {
    const effect = computeEffect(frame, marker({ series: ['a', 'ghost' as 'a'] }), NOW);
    expect(effect.rows.map((r) => r.id)).toEqual(['a']);
  });

  it('is partial and clips afterEnd to "now" for a recent change', () => {
    const now = marker().t + DAY;
    const effect = computeEffect(frame, marker({ series: ['a'] }), now);
    expect(effect.afterEnd).toBe(now);
    expect(effect.partial).toBe(true);
    expect(effect.rows[0].after).toBeCloseTo(20, 10);
    expect(effect.rows[0].deltaPct).toBeCloseTo(100, 10);
  });

  it('leaves "after" and the delta empty until an hour has passed', () => {
    const now = marker().t + 30 * 60_000;
    const [row] = computeEffect(frame, marker({ series: ['a'] }), now).rows;
    expect(row.before).toBeCloseTo(10, 10);
    expect(row.after).toBeNaN();
    expect(row.deltaPct).toBeNaN();
    expect(computeEffect(frame, marker(), now).partial).toBe(true);
  });

  it('gives a defined "after" exactly at one hour', () => {
    const [row] = computeEffect(frame, marker({ series: ['a'] }), marker().t + HOUR).rows;
    expect(row.after).toBeCloseTo(20, 10);
  });

  it('has no delta when there were no sales before the change', () => {
    const [row] = computeEffect(frame, marker({ t: S, series: ['a'] }), NOW).rows;
    expect(row.before).toBe(0);
    expect(row.after).toBeGreaterThan(0);
    expect(row.deltaPct).toBeNaN();
  });

  it('reports a decline as a negative delta', () => {
    const falling: VizBucketFrame<'a' | 'b'> = {
      ...frame,
      values: { ...frame.values, a: [20, 20, 20, 20, 20, 10, 10, 10, 10, 10] },
    };
    const [row] = computeEffect(falling, marker({ series: ['a'] }), NOW).rows;
    expect(row.deltaPct).toBeCloseTo(-50, 10);
  });

  it('uses a custom window', () => {
    const effect = computeEffect(frame, marker({ series: ['a'] }), NOW, DAY);
    expect(effect.window).toBe(DAY);
    expect(effect.afterEnd).toBe(marker().t + DAY);
    expect(effect.rows[0].before).toBeCloseTo(10, 10);
    expect(effect.rows[0].after).toBeCloseTo(20, 10);
  });

  it('defaults the window to one day on hourly frames', () => {
    const hours: VizBucketFrame<'a'> = {
      granularity: 'hour',
      start: S,
      step: HOUR,
      length: 96,
      values: { a: Array.from({ length: 96 }, (_, i) => (i < 48 ? 1 : 2)) },
    };
    const effect = computeEffect(hours, { id: 'x', t: S + 48 * HOUR, kind: 'k', label: 'x' }, S + 96 * HOUR);
    expect(effect.window).toBe(DAY);
    expect(effect.rows[0].before).toBeCloseTo(24, 10);
    expect(effect.rows[0].after).toBeCloseTo(48, 10);
  });
});

describe('visiblePriority', () => {
  it('reveals priorities as the scale grows (30 and 70 px/day by default)', () => {
    expect(visiblePriority(0)).toBe(1);
    expect(visiblePriority(29.9)).toBe(1);
    expect(visiblePriority(30)).toBe(2);
    expect(visiblePriority(69.9)).toBe(2);
    expect(visiblePriority(70)).toBe(3);
    expect(visiblePriority(1000)).toBe(3);
  });

  it('accepts custom thresholds', () => {
    const reveal = { 2: 10, 3: 20 } as const;
    expect(visiblePriority(9, reveal)).toBe(1);
    expect(visiblePriority(10, reveal)).toBe(2);
    expect(visiblePriority(20, reveal)).toBe(3);
  });
});

describe('pxPerDay', () => {
  it('converts window and width to a scale', () => {
    expect(pxPerDay(10 * DAY, 1000)).toBe(100);
    expect(pxPerDay(DAY / 2, 100)).toBe(200);
  });

  it('survives an empty window', () => {
    expect(Number.isFinite(pxPerDay(0, 100))).toBe(true);
  });
});

describe('layoutMarkers', () => {
  const at = (id: string, x: number, priority?: 1 | 2 | 3): VizMarker => ({
    id,
    t: x,
    kind: 'k',
    label: id,
    priority,
  });
  const xOf = (t: number) => t;

  it('shows distant markers and hides neighbours closer than minGap, counting them', () => {
    const slots = layoutMarkers([at('a', 0), at('b', 5), at('c', 10), at('d', 100)], xOf, {
      pxPerDay: 10,
      minGap: 20,
    });
    expect(slots.get('a')).toEqual({ visible: true, hidden: 2 });
    expect(slots.get('b')).toEqual({ visible: false, hidden: 0 });
    expect(slots.get('c')).toEqual({ visible: false, hidden: 0 });
    expect(slots.get('d')).toEqual({ visible: true, hidden: 0 });
  });

  it('places markers left to right, so the earlier one wins within a priority', () => {
    const slots = layoutMarkers([at('late', 5), at('early', 0)], xOf, { pxPerDay: 10, minGap: 20 });
    expect(slots.get('early')?.visible).toBe(true);
    expect(slots.get('late')?.visible).toBe(false);
  });

  it('keeps both when the distance equals minGap', () => {
    const slots = layoutMarkers([at('a', 0), at('b', 20)], xOf, { pxPerDay: 10, minGap: 20 });
    expect(slots.get('a')?.visible).toBe(true);
    expect(slots.get('b')?.visible).toBe(true);
  });

  it('lets a higher priority marker win over a lower one', () => {
    const slots = layoutMarkers([at('low', 0, 2), at('high', 5, 1)], xOf, { pxPerDay: 30, minGap: 20 });
    expect(slots.get('high')).toEqual({ visible: true, hidden: 1 });
    expect(slots.get('low')?.visible).toBe(false);
  });

  it('hides secondary priorities when zoomed out and reveals them on zoom', () => {
    const markers = [at('p1', 0, 1), at('p2', 100, 2), at('p3', 200, 3)];
    const far = layoutMarkers(markers, xOf, { pxPerDay: 10, minGap: 20 });
    expect([...far.values()].map((s) => s.visible)).toEqual([true, false, false]);
    const mid = layoutMarkers(markers, xOf, { pxPerDay: 30, minGap: 20 });
    expect([...mid.values()].map((s) => s.visible)).toEqual([true, true, false]);
    const near = layoutMarkers(markers, xOf, { pxPerDay: 70, minGap: 20 });
    expect([...near.values()].map((s) => s.visible)).toEqual([true, true, true]);
  });

  it('a hidden-by-priority marker does not count towards a neighbour', () => {
    const slots = layoutMarkers([at('p1', 0, 1), at('p3', 5, 3)], xOf, { pxPerDay: 10, minGap: 20 });
    expect(slots.get('p1')).toEqual({ visible: true, hidden: 0 });
  });

  it('always shows the marker in focus, even a low-priority one', () => {
    const markers = [at('p1', 0, 1), at('p3', 100, 3)];
    const slots = layoutMarkers(markers, xOf, { pxPerDay: 10, minGap: 20, focusId: 'p3' });
    expect(slots.get('p3')?.visible).toBe(true);
  });

  it('lets the focus marker displace a neighbour', () => {
    const markers = [at('p1', 0, 1), at('p3', 5, 3)];
    const slots = layoutMarkers(markers, xOf, { pxPerDay: 10, minGap: 20, focusId: 'p3' });
    expect(slots.get('p3')).toEqual({ visible: true, hidden: 1 });
    expect(slots.get('p1')?.visible).toBe(false);
  });

  it('treats a missing priority as 1 and keeps an entry for every marker', () => {
    const slots = layoutMarkers([at('a', 0), at('b', 100, 3)], xOf, { pxPerDay: 10, minGap: 20 });
    expect([...slots.keys()]).toEqual(['a', 'b']);
    expect(slots.get('a')?.visible).toBe(true);
  });

  it('uses the custom reveal thresholds', () => {
    const slots = layoutMarkers([at('p2', 0, 2)], xOf, { pxPerDay: 5, minGap: 20, reveal: { 2: 5, 3: 10 } });
    expect(slots.get('p2')?.visible).toBe(true);
  });

  it('returns an empty map for no markers', () => {
    expect(layoutMarkers([], xOf, { pxPerDay: 10, minGap: 20 }).size).toBe(0);
  });
});
