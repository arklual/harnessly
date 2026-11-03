// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest';

import { niceCeil, niceStep, yTicks } from './scale';

describe('niceStep', () => {
  it.each([
    [1, 1],
    [1.1, 2],
    [2, 2],
    [2.2, 2.5],
    [3, 5],
    [5, 5],
    [7, 10],
    [10, 10],
    [23, 25],
    [0.3, 0.5],
    [4200, 5000],
  ])('%d → %d', (x, expected) => {
    expect(niceStep(x)).toBeCloseTo(expected, 10);
  });

  it('falls back to 1 for non-positive or invalid input', () => {
    expect(niceStep(0)).toBe(1);
    expect(niceStep(-5)).toBe(1);
    expect(niceStep(Number.NaN)).toBe(1);
  });

  it('is never smaller than its argument', () => {
    for (const x of [0.013, 0.7, 1.9, 3.3, 8, 42, 99, 1234, 56789])
      expect(niceStep(x)).toBeGreaterThanOrEqual(x);
  });
});

describe('niceCeil', () => {
  it('rounds up to a multiple of a nice step', () => {
    expect(niceCeil(100)).toBe(100);
    expect(niceCeil(95)).toBe(100);
    expect(niceCeil(101)).toBe(150);
    expect(niceCeil(7)).toBe(8);
  });

  it('is never below the value', () => {
    for (const v of [0.4, 1, 3.7, 12, 99, 101, 5000, 123456]) expect(niceCeil(v)).toBeGreaterThanOrEqual(v);
  });

  it('never returns less than one step for zero', () => {
    expect(niceCeil(0)).toBe(1);
  });

  it('respects the division count', () => {
    expect(niceCeil(100, 5)).toBe(100);
    expect(niceCeil(100, 2)).toBe(100);
    expect(niceCeil(90, 2)).toBe(100);
  });
});

describe('yTicks', () => {
  it('lists multiples of the step up to max, without zero', () => {
    expect(yTicks(100, 25)).toEqual([25, 50, 75, 100]);
  });

  it('is tolerant to float drift on the last tick', () => {
    const ticks = yTicks(0.5, 0.1);
    expect(ticks).toHaveLength(5);
    expect(ticks.at(-1)).toBeCloseTo(0.5, 10);
  });

  it('returns no ticks when max is below one step', () => {
    expect(yTicks(5, 10)).toEqual([]);
  });

  it('guards against a huge max with a small step', () => {
    expect(yTicks(1e9, 1)).toEqual([]);
    expect(yTicks(1e12, 0.001, 24).length).toBeLessThanOrEqual(24);
  });

  it('never produces more than the limit', () => {
    for (const limit of [1, 5, 24, 100]) {
      const ticks = yTicks(10_000, 7, limit);
      expect(ticks.length).toBeLessThanOrEqual(limit);
    }
    expect(yTicks(24, 1, 24)).toHaveLength(24);
    expect(yTicks(25, 1, 24)).toEqual([]);
  });

  it('rejects a non-positive or invalid step', () => {
    expect(yTicks(100, 0)).toEqual([]);
    expect(yTicks(100, -5)).toEqual([]);
    expect(yTicks(100, Number.NaN)).toEqual([]);
  });
});
