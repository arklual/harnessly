// SPDX-License-Identifier: Apache-2.0
import { HOUR } from '../model/time';
import type { VizTimeExtent } from '../model/types';
import { createLocalSource, type VizTimeSource } from './source';

/** Метрика источника из `dailySource`: все ряды лежат под одним именем. */
export const DAILY_METRIC = 'v';

/**
 * Источник из дневных рядов: каждое значение дня встаёт в полдень, остальные часы суток — нули.
 * Дни после `now` остаются пропусками. `firstDay` — полночь первого дня ряда в мс.
 */
export function dailySource<Id extends string>(
  extent: VizTimeExtent,
  firstDay: number,
  now: number,
  daily: Readonly<Record<Id, ArrayLike<number>>>,
): VizTimeSource<Id, typeof DAILY_METRIC> {
  const length = Math.max(1, Math.ceil((now - extent.start) / HOUR));
  const values = {} as Record<Id, Float64Array>;
  for (const [id, days] of Object.entries<ArrayLike<number>>(daily)) {
    const hourly = new Float64Array(length).fill(Number.NaN);
    for (let i = 0; i < days.length; i++) {
      const v = days[i];
      const day = firstDay + i * 24 * HOUR;
      if (v !== v || day >= now) continue;
      const from = Math.round((day - extent.start) / HOUR);
      for (let h = 0; h < 24; h++) if (from + h >= 0 && from + h < length) hourly[from + h] = 0;
      const noon = Math.min(day + 12 * HOUR, now - HOUR);
      const at = Math.round((noon - extent.start) / HOUR);
      if (at >= 0 && at < length) hourly[at] = v;
    }
    values[id as Id] = hourly;
  }
  return createLocalSource<Id, typeof DAILY_METRIC>({
    hourly: { v: { start: extent.start, length, values } },
    extent: { ...extent, now },
    latency: 200,
    fail: () => false,
  });
}
