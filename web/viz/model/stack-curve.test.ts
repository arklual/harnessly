// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest';

import { evalCurve, monotoneTangents } from './curve';
import { stackAt } from './stack';

function run(values: number[], stack: number, percent: number) {
  const tops = new Float64Array(values.length);
  const bottoms = new Float64Array(values.length);
  const total = stackAt(values, stack, percent, tops, bottoms);
  return { tops: [...tops], bottoms: [...bottoms], total };
}

describe('stackAt', () => {
  const values = [10, 20, 30];

  it('overlay: every series from zero to its own value', () => {
    const { tops, bottoms, total } = run(values, 0, 0);
    expect(tops).toEqual([10, 20, 30]);
    expect(bottoms).toEqual([0, 0, 0]);
    expect(total).toBe(60);
  });

  it('stack: series sit on top of each other', () => {
    const { tops, bottoms, total } = run(values, 1, 0);
    expect(tops).toEqual([10, 30, 60]);
    expect(bottoms).toEqual([0, 10, 30]);
    expect(total).toBe(60);
  });

  it('stack: the top of the pile equals the total', () => {
    const { tops, total } = run([3, 5, 7, 11], 1, 0);
    expect(tops.at(-1)).toBe(total);
  });

  it('percent: shares of the total, 0…100', () => {
    const { tops, bottoms } = run(values, 1, 1);
    expect(bottoms[0]).toBe(0);
    expect(tops[0]).toBeCloseTo(100 / 6, 10);
    expect(bottoms[1]).toBeCloseTo(100 / 6, 10);
    expect(tops[1]).toBeCloseTo(50, 10);
    expect(bottoms[2]).toBeCloseTo(50, 10);
    expect(tops[2]).toBeCloseTo(100, 10);
  });

  it('percent: band heights add up to 100', () => {
    const { tops, bottoms } = run([7, 13, 29, 1], 1, 1);
    const sum = tops.reduce((acc, top, i) => acc + (top - bottoms[i]), 0);
    expect(sum).toBeCloseTo(100, 10);
  });

  it('percent overrides the stack factor (shares are always stacked)', () => {
    const withOverlay = run(values, 0, 1);
    const withStack = run(values, 1, 1);
    expect(withOverlay.tops).toEqual(withStack.tops);
    expect(withOverlay.bottoms).toEqual(withStack.bottoms);
  });

  it('interpolates between overlay and stack', () => {
    const { tops, bottoms } = run(values, 0.5, 0);
    // top = lerp(v, cum + v), bottom = cum · stack
    expect(tops).toEqual([10, 25, 45]);
    expect(bottoms).toEqual([0, 5, 15]);
  });

  it('interpolates between stack and percent', () => {
    const { tops } = run(values, 1, 0.5);
    expect(tops[2]).toBeCloseTo((60 + 100) / 2, 10);
  });

  it('gives NaN for a gap, skips it in the pile and in the total', () => {
    const { tops, bottoms, total } = run([10, Number.NaN, 30], 1, 0);
    expect(total).toBe(40);
    expect(tops[1]).toBeNaN();
    expect(bottoms[1]).toBeNaN();
    expect(tops[2]).toBe(40);
    expect(bottoms[2]).toBe(10);
  });

  it('percent with an all-zero point does not produce NaN', () => {
    const { tops, bottoms, total } = run([0, 0], 1, 1);
    expect(total).toBe(0);
    expect(tops).toEqual([0, 0]);
    expect(bottoms).toEqual([0, 0]);
  });
});

describe('monotoneTangents', () => {
  const tangents = (y: number[]) => [...monotoneTangents(y, new Float64Array(y.length))];

  it('is zero at local extrema and on flat runs', () => {
    const t = tangents([0, 10, 10, 0]);
    expect(t[1]).toBe(0);
    expect(t[2]).toBe(0);
    expect(tangents([0, 5, 0])[1]).toBe(0);
  });

  it('uses half the single slope at the ends', () => {
    const t = tangents([0, 10, 30]);
    expect(t[0]).toBe(5);
    expect(t[2]).toBe(10);
  });

  it('is the harmonic mean of neighbouring slopes inside a monotone run', () => {
    const t = tangents([0, 1, 5]);
    expect(t[1]).toBeCloseTo((2 * 1 * 4) / (1 + 4), 10);
  });

  it('gives zero tangent to a lonely point and to a gap', () => {
    expect(tangents([5])).toEqual([0]);
    expect(tangents([1, Number.NaN, 3])[1]).toBe(0);
  });

  it('writes into the provided buffer', () => {
    const out = new Float64Array(3);
    expect(monotoneTangents([0, 1, 2], out)).toBe(out);
  });
});

describe('evalCurve', () => {
  const sample = (y: number[], smooth: number, u: number) =>
    evalCurve(y, monotoneTangents(y, new Float64Array(y.length)), u, smooth);

  it('passes through the knots', () => {
    const y = [0, 10, 10, 2, 8, 8, 40];
    y.forEach((v, i) => expect(sample(y, 1, i)).toBeCloseTo(v, 10));
  });

  it('passes through the last knot too', () => {
    expect(sample([1, 2, 3], 1, 2)).toBe(3);
  });

  it('does not overshoot the neighbouring knots (monotone)', () => {
    const y = [0, 0, 5, 100, 100, 3, 3, 90, 2];
    for (let k = 0; k < y.length - 1; k++) {
      const lo = Math.min(y[k], y[k + 1]);
      const hi = Math.max(y[k], y[k + 1]);
      for (let f = 0; f <= 1; f += 0.05) {
        const v = sample(y, 1, k + f);
        expect(v).toBeGreaterThanOrEqual(lo - 1e-9);
        expect(v).toBeLessThanOrEqual(hi + 1e-9);
      }
    }
  });

  it('never goes below zero for non-negative data', () => {
    const y = [0, 0, 0, 50, 0, 0, 0];
    for (let u = 0; u <= y.length - 1; u += 0.05) expect(sample(y, 1, u)).toBeGreaterThanOrEqual(-1e-9);
  });

  it('is non-decreasing across an increasing series', () => {
    const y = [0, 1, 5, 6, 20, 21];
    let prev = -Infinity;
    for (let u = 0; u <= y.length - 1; u += 0.02) {
      const v = sample(y, 1, u);
      expect(v).toBeGreaterThanOrEqual(prev - 1e-9);
      prev = v;
    }
  });

  it('keeps a flat segment exactly flat', () => {
    for (let f = 0; f <= 1; f += 0.1) expect(sample([0, 10, 10, 0], 1, 1 + f)).toBeCloseTo(10, 10);
  });

  it('smooth = 0 is a polyline', () => {
    const y = [0, 10, 4];
    expect(sample(y, 0, 0.5)).toBe(5);
    expect(sample(y, 0, 1.25)).toBe(8.5);
    expect(sample(y, -1, 0.5)).toBe(5);
  });

  it('smooth blends the polyline and the curve', () => {
    const y = [0, 1, 5, 6];
    const t = monotoneTangents(y, new Float64Array(y.length));
    const lin = evalCurve(y, t, 1.5, 0);
    const curve = evalCurve(y, t, 1.5, 1);
    expect(evalCurve(y, t, 1.5, 0.5)).toBeCloseTo((lin + curve) / 2, 10);
  });

  it('breaks the curve on a gap', () => {
    const y = [1, Number.NaN, 3, 4];
    expect(sample(y, 1, 0.5)).toBeNaN();
    expect(sample(y, 1, 1.5)).toBeNaN();
    expect(sample(y, 1, 2.5)).not.toBeNaN();
  });

  it('is NaN outside the series or for fewer than two points', () => {
    expect(sample([1, 2, 3], 1, -0.1)).toBeNaN();
    expect(sample([1, 2, 3], 1, 2.1)).toBeNaN();
    expect(sample([1], 1, 0)).toBeNaN();
    expect(sample([1, 2], 1, Number.NaN)).toBeNaN();
  });
});
