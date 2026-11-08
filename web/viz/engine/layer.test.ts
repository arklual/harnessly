// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest';

import { DAY, HOUR } from '../model/time';
import type { VizBucketFrame } from '../model/types';
import { columnGranularityThresholds } from '../ui/wave-board';
import { buildLayer } from './layer';

const day = (values: number[]): VizBucketFrame => ({
  granularity: 'day',
  start: 0,
  step: DAY,
  length: values.length,
  values: { a: values },
});
const hours = (values: number[]): VizBucketFrame => ({
  granularity: 'hour',
  start: 0,
  step: HOUR,
  length: values.length,
  values: { a: values },
});

describe('buildLayer: смена детализации у столбиков', () => {
  it('день распадается на часы: каждый час начинает с 1/24 показанного дня', () => {
    const prev = buildLayer(day([240]), ['a']);
    prev.shown[0].set([240]);
    const next = buildLayer(hours(new Array(24).fill(3)), ['a'], prev);
    expect(Array.from(next.shown[0])).toEqual(new Array(24).fill(10));
    expect(Array.from(next.target[0])).toEqual(new Array(24).fill(3));
  });

  it('часы сливаются в день: день начинает с суммы показанных часов', () => {
    const prev = buildLayer(hours(new Array(48).fill(2)), ['a']);
    prev.shown[0].set(new Array(48).fill(2));
    const next = buildLayer(day([10, 20]), ['a'], prev);
    expect(Array.from(next.shown[0])).toEqual([48, 48]);
  });

  it('пропуски в старом слое не превращаются в NaN нового, пропуск нового остаётся пропуском', () => {
    const prev = buildLayer(hours([Number.NaN, 4, ...new Array(22).fill(1)]), ['a']);
    prev.shown[0].set([Number.NaN, 4, ...new Array(22).fill(1)]);
    const next = buildLayer(day([Number.NaN]), ['a'], prev);
    expect(next.shown[0][0]).toBeNaN();
    const filled = buildLayer(day([30]), ['a'], prev);
    expect(filled.shown[0][0]).toBe(26);
  });
});

describe('columnGranularityThresholds', () => {
  it('стопка: столбик от 10 px на днях и от 14 px на часах', () => {
    expect(columnGranularityThresholds('stack', 6)).toEqual({
      hour: { enter: 14, leave: 11 },
      day: { enter: 10, leave: 8 },
    });
  });

  it('группа: отрезок должен вместить все видимые серии рядом', () => {
    const six = columnGranularityThresholds('overlay', 6);
    const two = columnGranularityThresholds('overlay', 2);
    expect(six.day?.enter).toBe(34);
    expect(two.day?.enter).toBe(14);
    expect(six.hour!.enter).toBeGreaterThan(six.day!.enter);
  });
});
