// SPDX-License-Identifier: Apache-2.0
import { DAY, HOUR, MOSCOW_ZONE, type VizGranularity, type VizTimeZone, wallTime } from './time';

/**
 * Единица значения. Слова единиц — копия продукта: библиотека не знает, что считают
 * «билеты», поэтому формы приходят пропсом (`['билет', 'билета', 'билетов']`).
 */
export type VizUnit =
  | { readonly kind: 'count'; readonly forms?: readonly [one: string, few: string, many: string] }
  | { readonly kind: 'currency'; readonly currency: string }
  | { readonly kind: 'percent' };

export const VIZ_UNIT_COUNT = { kind: 'count' } as const satisfies VizUnit;
export const VIZ_UNIT_RUB = { kind: 'currency', currency: 'RUB' } as const satisfies VizUnit;
export const VIZ_UNIT_PERCENT = { kind: 'percent' } as const satisfies VizUnit;

const LOCALE = 'ru-RU';
const whole = new Intl.NumberFormat(LOCALE, { maximumFractionDigits: 0 });
const tenth = new Intl.NumberFormat(LOCALE, { maximumFractionDigits: 1 });
const currencySymbols = new Map<string, string>();

function currencySymbol(code: string): string {
  let symbol = currencySymbols.get(code);
  if (symbol === undefined) {
    const parts = new Intl.NumberFormat(LOCALE, { style: 'currency', currency: code }).formatToParts(0);
    symbol = parts.find((part) => part.type === 'currency')?.value ?? code;
    currencySymbols.set(code, symbol);
  }
  return symbol;
}

/** Русское множественное число: 1 билет, 2 билета, 5 билетов, 11 билетов. */
export function plural(n: number, forms: readonly [string, string, string]): string {
  const a = Math.abs(Math.round(n)) % 100;
  const b = a % 10;
  if (a > 10 && a < 20) return forms[2];
  if (b > 1 && b < 5) return forms[1];
  if (b === 1) return forms[0];
  return forms[2];
}

export function formatNumber(v: number, fractionDigits: 0 | 1 = 0): string {
  return (fractionDigits ? tenth : whole).format(v);
}

/** Коротко для осей и легенды: 820, 1,2 тыс, 48 тыс, 1,5 млн. */
export function formatCompact(v: number): string {
  const a = Math.abs(v);
  // 999 999 округляется до 1 000 тыс — такое число уже пишем миллионами.
  if (a >= 1e6 || Math.round(a / 1e3) >= 1e3) return `${tenth.format(v / 1e6)} млн`;
  if (a >= 1e4) return `${whole.format(v / 1e3)} тыс`;
  if (a >= 1e3) return `${tenth.format(v / 1e3)} тыс`;
  return a < 10 ? tenth.format(v) : whole.format(v);
}

export interface FormatValueOptions {
  /** Полная запись («124 билета», «248 000 ₽») вместо короткой («124», «248 тыс ₽»). */
  readonly long?: boolean;
}

export function formatValue(v: number, unit: VizUnit, options: FormatValueOptions = {}): string {
  if (!Number.isFinite(v)) return '—';
  if (unit.kind === 'percent') return `${tenth.format(v)}%`;
  if (unit.kind === 'currency')
    return `${options.long ? whole.format(v) : formatCompact(v)} ${currencySymbol(unit.currency)}`;
  if (!options.long) return formatCompact(v);
  return unit.forms ? `${whole.format(v)} ${plural(v, unit.forms)}` : whole.format(v);
}

/** Изменение в процентах со знаком и стрелкой: «↑ +24%», «↓ −42%». Знак и стрелка — не только цвет. */
export function formatDelta(pct: number): string {
  if (!Number.isFinite(pct)) return '—';
  return `${pct >= 0 ? '↑ +' : '↓ −'}${whole.format(Math.abs(pct))}%`;
}

const MONTHS = ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'] as const;
const WEEKDAYS = ['вс', 'пн', 'вт', 'ср', 'чт', 'пт', 'сб'] as const;
const WEEKDAYS_FULL = [
  'воскресенье',
  'понедельник',
  'вторник',
  'среда',
  'четверг',
  'пятница',
  'суббота',
] as const;

const pad2 = (n: number) => String(n).padStart(2, '0');

/** «7 окт». */
export function formatDay(t: number, zone: VizTimeZone = MOSCOW_ZONE): string {
  const w = wallTime(t, zone);
  return `${w.day} ${MONTHS[w.month]}`;
}

/** «14:00». */
export function formatTime(t: number, zone: VizTimeZone = MOSCOW_ZONE): string {
  const w = wallTime(t, zone);
  return `${pad2(w.hours)}:${pad2(w.minutes)}`;
}

/** «1 д 20 ч». */
export function formatSpan(ms: number): string {
  const hours = Math.round(ms / HOUR);
  const d = Math.floor(hours / 24);
  const h = hours % 24;
  return [d ? `${d} д` : '', h ? `${h} ч` : ''].filter(Boolean).join(' ') || '0 ч';
}

/**
 * Подпись отрезка для тултипа: «7 окт, вт · 14:00–15:00», «7 окт, вторник», «30 сен – 6 окт».
 */
export function formatBucket(
  start: number,
  granularity: VizGranularity,
  step: number,
  zone: VizTimeZone = MOSCOW_ZONE,
): string {
  const w = wallTime(start, zone);
  if (granularity === 'hour')
    return `${formatDay(start, zone)}, ${WEEKDAYS[w.weekday]} · ${formatTime(start, zone)}–${formatTime(start + step, zone)}`;
  if (granularity === 'day') return `${formatDay(start, zone)}, ${WEEKDAYS_FULL[w.weekday]}`;
  return `${formatDay(start, zone)} – ${formatDay(start + step - DAY, zone)}`;
}

/** «7 окт 14:00 → 9 окт 10:00 · 1 д 20 ч» — подпись выделенного периода. */
export function formatRange(a: number, b: number, zone: VizTimeZone = MOSCOW_ZONE): string {
  const midnight = (t: number) => {
    const w = wallTime(t, zone);
    return w.hours === 0 && w.minutes === 0;
  };
  const from = midnight(a) ? formatDay(a, zone) : `${formatDay(a, zone)} ${formatTime(a, zone)}`;
  const sameDay = formatDay(a, zone) === formatDay(b - 1, zone);
  let to = `${formatDay(b, zone)} ${formatTime(b, zone)}`;
  if (midnight(b)) to = formatDay(b - DAY, zone);
  else if (sameDay) to = formatTime(b, zone);
  return `${from} → ${to} · ${formatSpan(b - a)}`;
}
