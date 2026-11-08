// SPDX-License-Identifier: Apache-2.0
/**
 * Время досок: детализация, выравнивание отрезков по местному времени, деления оси.
 *
 * Всё считается в миллисекундах UTC, а «местное» время даёт `VizTimeZone`. Часовой пояс —
 * функция смещения, а не строка IANA: так модель работает без Intl в тестах и одинаково
 * в браузере и на сервере. Для Москвы и любого пояса без перехода на летнее время хватает
 * `fixedOffsetZone`.
 */

export const HOUR = 3_600_000;
export const DAY = 24 * HOUR;
export const WEEK = 7 * DAY;

export const VIZ_GRANULARITY = {
  hour: 'hour',
  day: 'day',
  week: 'week',
} as const;

export type VizGranularity = (typeof VIZ_GRANULARITY)[keyof typeof VIZ_GRANULARITY];

/** Длина отрезка каждой детализации. Неделя начинается с понедельника. */
export const GRANULARITY_STEP = {
  hour: HOUR,
  day: DAY,
  week: WEEK,
} as const satisfies Record<VizGranularity, number>;

/** Самая мелкая детализация первой: порядок нужен для выбора по ширине окна. */
export const GRANULARITY_ORDER = ['hour', 'day', 'week'] as const satisfies readonly VizGranularity[];

export interface VizTimeZone {
  /** Смещение местного времени от UTC в миллисекундах в момент `t`. */
  offset(t: number): number;
}

export function fixedOffsetZone(hours: number): VizTimeZone {
  const offset = hours * HOUR;
  return { offset: () => offset };
}

/** Москва: UTC+3 круглый год. Пояс по умолчанию для всех досок. */
export const MOSCOW_ZONE: VizTimeZone = fixedOffsetZone(3);

/** Четверг 1 января 1970 → понедельник 5 января: сдвиг до начала недели. */
const MONDAY_SHIFT = 4 * DAY;

/** Начало отрезка детализации `granularity`, в который попадает момент `t`. */
export function floorTo(t: number, granularity: VizGranularity, zone: VizTimeZone = MOSCOW_ZONE): number {
  const offset = zone.offset(t);
  if (granularity === 'hour') return Math.floor((t + offset) / HOUR) * HOUR - offset;
  if (granularity === 'day') return Math.floor((t + offset) / DAY) * DAY - offset;
  return Math.floor((t + offset - MONDAY_SHIFT) / WEEK) * WEEK + MONDAY_SHIFT - offset;
}

export interface VizWallTime {
  readonly year: number;
  readonly month: number;
  readonly day: number;
  readonly hours: number;
  readonly minutes: number;
  /** 0 — воскресенье, как у `Date#getDay`. */
  readonly weekday: number;
}

/** Части местного времени без Intl: дата сдвигается на смещение пояса и читается как UTC. */
export function wallTime(t: number, zone: VizTimeZone = MOSCOW_ZONE): VizWallTime {
  const d = new Date(t + zone.offset(t));
  return {
    year: d.getUTCFullYear(),
    month: d.getUTCMonth(),
    day: d.getUTCDate(),
    hours: d.getUTCHours(),
    minutes: d.getUTCMinutes(),
    weekday: d.getUTCDay(),
  };
}

export function isMidnight(t: number, zone: VizTimeZone = MOSCOW_ZONE): boolean {
  return floorTo(t, 'day', zone) === t;
}

export interface VizTimeRange {
  readonly start: number;
  readonly end: number;
}

export interface GranularityThresholds {
  /** Минимальная ширина отрезка в пикселях, чтобы перейти на эту детализацию. */
  readonly enter: number;
  /** Ширина, ниже которой с неё уходят. Меньше `enter` — гистерезис против дрожания на границе. */
  readonly leave: number;
}

const DEFAULT_THRESHOLDS = {
  hour: { enter: 9, leave: 7 },
  day: { enter: 7, leave: 5 },
} as const satisfies Partial<Record<VizGranularity, GranularityThresholds>>;

/**
 * Детализация под ширину окна: самая мелкая, у которой отрезок не уже порога.
 * `current` — нынешняя детализация: с неё уходят по порогу `leave`, на новую входят по `enter`.
 */
export function pickGranularity(
  spanMs: number,
  widthPx: number,
  current: VizGranularity | null = null,
  thresholds: Partial<Record<VizGranularity, GranularityThresholds>> = DEFAULT_THRESHOLDS,
): VizGranularity {
  for (const granularity of GRANULARITY_ORDER) {
    const limit = thresholds[granularity];
    if (!limit) continue;
    const bucketPx = (widthPx * GRANULARITY_STEP[granularity]) / Math.max(spanMs, 1);
    if (bucketPx >= (current === granularity ? limit.leave : limit.enter)) return granularity;
  }
  return 'week';
}

/** Детализация для полосы «весь период»: недели только на очень длинной истории. */
export function overviewGranularity(range: VizTimeRange): VizGranularity {
  const span = range.end - range.start;
  if (span <= 6 * DAY) return 'hour';
  if (span <= 400 * DAY) return 'day';
  return 'week';
}

/** Деления оси времени: от часа до месяца. Выбирается самое частое с промежутком не уже `minGapPx`. */
export const TICK_LEVELS = [
  HOUR,
  2 * HOUR,
  3 * HOUR,
  6 * HOUR,
  12 * HOUR,
  DAY,
  2 * DAY,
  WEEK,
  2 * WEEK,
  'month',
] as const;

export type TickLevel = (typeof TICK_LEVELS)[number];

const MONTH_APPROX = 30.4 * DAY;

export function tickLevelFor(spanMs: number, widthPx: number, minGapPx = 64): TickLevel {
  for (const level of TICK_LEVELS) {
    const ms = level === 'month' ? MONTH_APPROX : level;
    if ((widthPx * ms) / Math.max(spanMs, 1) >= minGapPx) return level;
  }
  return 'month';
}

/** Моменты делений уровня `level` внутри `[start, end]`, выровненные по местному времени. */
export function ticksFor(
  level: TickLevel,
  range: VizTimeRange,
  zone: VizTimeZone = MOSCOW_ZONE,
  limit = 400,
): number[] {
  const out: number[] = [];
  if (level === 'month') {
    const w = wallTime(range.start, zone);
    let year = w.year;
    let month = w.month;
    for (let i = 0; i < 60 && out.length < limit; i++) {
      const local = Date.UTC(year, month, 1);
      const t = local - zone.offset(local);
      if (t > range.end) break;
      if (t >= range.start) out.push(t);
      month++;
      if (month > 11) {
        month = 0;
        year++;
      }
    }
    return out;
  }
  const offset = zone.offset(range.start);
  const first =
    level < WEEK
      ? Math.floor((range.start + offset) / level) * level - offset
      : Math.floor((range.start + offset - MONDAY_SHIFT) / level) * level + MONDAY_SHIFT - offset;
  for (let t = first; t <= range.end && out.length < limit; t += level) if (t >= range.start) out.push(t);
  return out;
}
