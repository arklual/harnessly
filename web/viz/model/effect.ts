// SPDX-License-Identifier: Apache-2.0
import { frameSum } from './aggregate';
import { DAY, HOUR } from './time';
import type { VizBucketFrame, VizMarker } from './types';

export interface VizEffectRow<Id extends string = string> {
  /** Серия или `null` — сумма по всем сериям. */
  readonly id: Id | null;
  /** Среднее в день до изменения. */
  readonly before: number;
  /** Среднее в день после; `NaN`, пока после изменения не прошло и часа. */
  readonly after: number;
  /** Изменение в процентах; `NaN`, если до изменения продаж не было. */
  readonly deltaPct: number;
}

export interface VizEffect<Id extends string = string> {
  readonly markerId: string;
  /** Длина окна сравнения, мс. */
  readonly window: number;
  /** Конец окна «после»: не дальше «сейчас». */
  readonly afterEnd: number;
  /** Окно «после» короче «до»: изменение недавнее. */
  readonly partial: boolean;
  readonly rows: readonly VizEffectRow<Id>[];
}

/**
 * Окно сравнения по умолчанию — 3 дня (на часовой детализации — сутки). Неделя слишком длинная:
 * в «до» попадают соседние события (старт продаж, прошлая цена) и выдают ложный минус.
 */
export function defaultEffectWindow(step: number): number {
  return step <= HOUR ? DAY : 3 * DAY;
}

/**
 * Эффект изменения: среднее в день за окно до и после значка, по затронутым сериям
 * (или по сумме всех, если значок касается всех).
 */
export function computeEffect<Id extends string>(
  frame: VizBucketFrame<Id>,
  marker: VizMarker<Id>,
  now: number,
  window = defaultEffectWindow(frame.step),
): VizEffect<Id> {
  const afterEnd = Math.min(marker.t + window, now);
  const afterDays = Math.max(0, (afterEnd - marker.t) / DAY);
  const beforeDays = window / DAY;
  const ids = Object.keys(frame.values) as Id[];
  const own = marker.series?.length ? marker.series.filter((id) => ids.includes(id)) : null;
  const row = (id: Id | null, list: readonly Id[]): VizEffectRow<Id> => {
    let b = 0;
    let a = 0;
    for (const s of list) {
      b += frameSum(frame, s, marker.t - window, marker.t);
      a += frameSum(frame, s, marker.t, afterEnd);
    }
    const before = b / beforeDays;
    const after = afterEnd - marker.t >= HOUR ? a / afterDays : Number.NaN;
    return {
      id,
      before,
      after,
      deltaPct: before > 0 && after === after ? (after / before - 1) * 100 : Number.NaN,
    };
  };
  return {
    markerId: marker.id,
    window,
    afterEnd,
    partial: afterDays < beforeDays - 0.01,
    rows: own ? own.map((id) => row(id, [id])) : [row(null, ids)],
  };
}
