// SPDX-License-Identifier: Apache-2.0
/** «Круглые» множители шага сетки. */
const NICE_FRACTIONS = [1, 2, 2.5, 5, 10] as const;

/** Шаг делений «красивого» ряда: 1, 2, 2,5, 5 × 10ⁿ. */
export function niceStep(x: number): number {
  if (!(x > 0)) return 1;
  const p = 10 ** Math.floor(Math.log10(x));
  const f = x / p;
  return (NICE_FRACTIONS.find((n) => f <= n) ?? 10) * p;
}

/** Верх оси: кратен шагу примерно четырёх делений и не ниже `v`. */
export function niceCeil(v: number, divisions = 4): number {
  const step = niceStep(v / divisions);
  return Math.max(step, Math.ceil(v / step) * step);
}

/** Деления оси Y от шага до верха. Ноль рисуется отдельно, как базовая линия. */
export function yTicks(max: number, step: number, limit = 24): number[] {
  if (!(step > 0) || max / step > limit) return [];
  const out: number[] = [];
  for (let v = step; v <= max * 1.001; v += step) out.push(v);
  return out;
}
