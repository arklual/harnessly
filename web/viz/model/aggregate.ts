// SPDX-License-Identifier: Apache-2.0
import { floorTo, GRANULARITY_STEP, HOUR, MOSCOW_ZONE, type VizGranularity, type VizTimeZone } from './time';
import type { VizBucketFrame } from './types';

/** Часовые значения серий: самый мелкий ряд, из которого собирается любая детализация. */
export interface VizHourlySeries<Id extends string = string> {
  /** Начало первого часа, мс UTC (выровнено по часу). */
  readonly start: number;
  /** Число часов. */
  readonly length: number;
  readonly values: Readonly<Record<Id, ArrayLike<number>>>;
}

/** Префиксные суммы ряда и счётчик пропусков: сумма любого отрезка за O(1). */
export interface PrefixSums {
  readonly sum: Float64Array;
  readonly gaps: Int32Array;
}

export function prefixSums(values: ArrayLike<number>, length = values.length): PrefixSums {
  const sum = new Float64Array(length + 1);
  const gaps = new Int32Array(length + 1);
  for (let i = 0; i < length; i++) {
    const v = values[i];
    const gap = v !== v;
    sum[i + 1] = sum[i] + (gap ? 0 : v);
    gaps[i + 1] = gaps[i] + (gap ? 1 : 0);
  }
  return { sum, gaps };
}

/**
 * Сумма по индексам `[i0, i1)`. Пропуском (`NaN`) считается отрезок, где пропущено больше
 * половины: из одного пустого часа в сутках не получается «нет данных за день».
 */
export function rangeSum(prefix: PrefixSums, i0: number, i1: number): number {
  const n = prefix.sum.length - 1;
  const a = Math.max(0, Math.min(n, i0));
  const b = Math.max(0, Math.min(n, i1));
  if (b <= a) return Number.NaN;
  if (prefix.gaps[b] - prefix.gaps[a] > (b - a) / 2) return Number.NaN;
  return prefix.sum[b] - prefix.sum[a];
}

/** Собирает часовые ряды в отрезки детализации `granularity`, выровненные по местному времени. */
export function bucketize<Id extends string>(
  hourly: VizHourlySeries<Id>,
  granularity: VizGranularity,
  zone: VizTimeZone = MOSCOW_ZONE,
): VizBucketFrame<Id> {
  const step = GRANULARITY_STEP[granularity];
  const end = hourly.start + hourly.length * HOUR;
  const start = floorTo(hourly.start, granularity, zone);
  const length = hourly.length > 0 ? Math.max(0, Math.ceil((end - start) / step)) : 0;
  const values = {} as Record<Id, Float64Array>;
  for (const id of Object.keys(hourly.values) as Id[]) {
    const prefix = prefixSums(hourly.values[id], hourly.length);
    const out = new Float64Array(length);
    for (let k = 0; k < length; k++) {
      const a = start + k * step;
      out[k] = rangeSum(
        prefix,
        Math.round((a - hourly.start) / HOUR),
        Math.round((a + step - hourly.start) / HOUR),
      );
    }
    values[id] = out;
  }
  return { granularity, start, step, length, values };
}

/** Индекс отрезка, в который попадает момент `t` (может выйти за границы кадра). */
export function bucketIndex(frame: Pick<VizBucketFrame, 'start' | 'step'>, t: number): number {
  return Math.floor((t - frame.start) / frame.step);
}

/** Есть ли в отрезке `k` хоть одно значение (не пропуск у всех серий). */
export function bucketHasData(frame: VizBucketFrame, k: number): boolean {
  for (const id of Object.keys(frame.values)) {
    const v = frame.values[id][k];
    if (v === v) return true;
  }
  return false;
}

/**
 * Сумма серии по моментам `[a, b)` по кадру: целые отрезки плюс доли крайних.
 * Нужна для эффекта изменения и сводки, когда исходных часов на клиенте нет.
 */
export function frameSum(frame: VizBucketFrame, id: string, a: number, b: number): number {
  const values = (frame.values as Record<string, ArrayLike<number>>)[id];
  if (!values || b <= a) return 0;
  let total = 0;
  const k0 = Math.max(0, bucketIndex(frame, a));
  const k1 = Math.min(frame.length - 1, bucketIndex(frame, b - 1));
  for (let k = k0; k <= k1; k++) {
    const v = values[k];
    if (v !== v) continue;
    const s = frame.start + k * frame.step;
    const overlap = Math.min(b, s + frame.step) - Math.max(a, s);
    if (overlap > 0) total += (v * overlap) / frame.step;
  }
  return total;
}
