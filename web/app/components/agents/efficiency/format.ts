import { locale, t } from '@/lib/i18n';

/** Number and date formats follow the page language. */
const TAG = locale() === 'ru' ? 'ru-RU' : 'en-US';
const MONTHS = t('efficiency.format.months').split(' ');

const nf = new Intl.NumberFormat(TAG);
const comma = (v: number, digits: number) =>
  locale() === 'ru' ? v.toFixed(digits).replace('.', ',') : v.toFixed(digits);

export const NO_DATA = '—';

export const n0 = (v: number | null): string => (v === null ? NO_DATA : nf.format(Math.round(v)));

export const money = (v: number | null): string => {
  if (v === null) return NO_DATA;
  return v >= 100 ? `$${nf.format(Math.round(v))}` : `$${comma(v, v >= 10 ? 1 : 2)}`;
};

/** Token counts the way the stats screen prints them: 1.2M, 340K. */
export const tokens = (v: number | null): string => {
  if (v === null) return NO_DATA;
  if (v >= 1e9) return t('efficiency.format.billions', { value: comma(v / 1e9, 1) });
  if (v >= 1e6) return t('efficiency.format.millions', { value: comma(v / 1e6, 1) });
  if (v >= 1e3) return t('efficiency.format.thousands', { value: comma(v / 1e3, 1) });
  return nf.format(Math.round(v));
};

/** Compact form for chart labels: 411K, 1.2M. */
export const compact = (v: number | null): string => {
  if (v === null) return NO_DATA;
  if (v >= 1e9) return t('efficiency.format.billions', { value: comma(v / 1e9, 1) });
  if (v >= 1e6) return `${comma(v / 1e6, 1)}M`;
  if (v >= 1e3) return `${Math.round(v / 1e3)}K`;
  return nf.format(Math.round(v));
};

/** A share stored as a fraction (0.25 → 25%). */
export const pct = (v: number | null, digits = 0): string =>
  v === null ? NO_DATA : `${comma(v * 100, digits)}%`;

export const minutes = (v: number | null): string => {
  if (v === null) return NO_DATA;
  return v >= 90
    ? t('efficiency.format.hours', { value: comma(v / 60, 1) })
    : t('efficiency.format.minutes', { value: nf.format(Math.round(v)) });
};

export const seconds = (v: number | null): string => {
  if (v === null) return NO_DATA;
  return v >= 120
    ? t('efficiency.format.minutes', { value: Math.round(v / 60) })
    : t('efficiency.format.seconds', { value: Math.round(v) });
};

export const millis = (v: number | null): string => {
  if (v === null) return NO_DATA;
  return v >= 1000
    ? t('efficiency.format.seconds', { value: comma(v / 1000, 1) })
    : t('efficiency.format.millis', { value: Math.round(v) });
};

export const decimal = (v: number | null, digits = 1): string => (v === null ? NO_DATA : comma(v, digits));

export const shortDay = (day: string): string =>
  `${Number(day.slice(8, 10))} ${MONTHS[Number(day.slice(5, 7)) - 1]}`;

export const dayLabel = (day: string): string => `${day.slice(8, 10)}.${day.slice(5, 7)}`;

export const dateTime = (iso: string | null): string => {
  if (!iso) return NO_DATA;
  return new Date(iso).toLocaleString(TAG, {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    timeZone: 'Europe/Moscow',
  });
};

export const clock = (iso: string | null): string => {
  if (!iso) return NO_DATA;
  return new Date(iso).toLocaleTimeString(TAG, {
    hour: '2-digit',
    minute: '2-digit',
    timeZone: 'Europe/Moscow',
  });
};

/** Hypothesis bases and targets come in any unit: whole tokens, dollars, shares in percent. */
export const plainValue = (v: number | null): string => {
  if (v === null) return NO_DATA;
  if (Math.abs(v) >= 10_000) return compact(v);
  return Number.isInteger(v) ? nf.format(v) : comma(v, 2).replace(/0+$/, '').replace(/[.,]$/, '');
};
