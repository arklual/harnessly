// SPDX-License-Identifier: Apache-2.0
const lerp = (a: number, b: number, t: number) => a + (b - a) * t;

/**
 * Верх и низ каждой серии в одной точке времени.
 * `stack` (0…1) плавно переводит наложение в стопку, `percent` (0…1) — стопку в доли от суммы:
 * так смена вида анимируется без отдельной логики под каждый режим.
 * Возвращает сумму видимых значений.
 */
export function stackAt(
  values: ArrayLike<number>,
  stack: number,
  percent: number,
  tops: Float64Array,
  bottoms: Float64Array,
): number {
  const n = values.length;
  let total = 0;
  for (let i = 0; i < n; i++) {
    const v = values[i];
    if (v === v) total += v;
  }
  let cum = 0;
  for (let i = 0; i < n; i++) {
    const v = values[i];
    if (v !== v) {
      tops[i] = Number.NaN;
      bottoms[i] = Number.NaN;
      continue;
    }
    let bottom = cum * stack;
    let top = lerp(v, cum + v, stack);
    if (percent > 0) {
      const pb = total > 0 ? (cum / total) * 100 : 0;
      const pt = total > 0 ? ((cum + v) / total) * 100 : 0;
      bottom = lerp(bottom, pb, percent);
      top = lerp(top, pt, percent);
    }
    cum += v;
    tops[i] = top;
    bottoms[i] = bottom;
  }
  return total;
}
