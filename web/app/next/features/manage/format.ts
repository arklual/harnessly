import { locale } from '@/next/i18n';
import type { HubCurrency } from '@/next/features/manage/hub-types';

const tag = () => (locale() === 'ru' ? 'ru-RU' : 'en-US');

/** A date from the hub, or null when it is missing or not a date: a formatter must not throw on it. */
const parse = (iso: string | null | undefined): Date | null => {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
};

export const hubMoney = (v: number, currency: HubCurrency): string => {
  try {
    return new Intl.NumberFormat(tag(), { style: 'currency', currency, maximumFractionDigits: 0 }).format(v);
  } catch {
    return `${number(v)} ${currency}`; // a code Intl does not know
  }
};

/** 12 700 → «12,7 тыс ₽» style short money for big headline numbers. */
export const hubMoneyShort = (v: number, currency: HubCurrency): string => {
  const symbol = currency === 'RUB' ? '₽' : currency === 'USD' ? '$' : currency;
  if (v >= 1000) {
    const k = new Intl.NumberFormat(tag(), { maximumFractionDigits: 1 }).format(v / 1000);
    return `${k}${locale() === 'ru' ? ' тыс' : 'K'} ${symbol}`;
  }
  return `${new Intl.NumberFormat(tag()).format(Math.round(v))} ${symbol}`;
};

export const usd = (v: number): string =>
  `$${new Intl.NumberFormat(tag(), { maximumFractionDigits: v >= 100 ? 0 : v >= 10 ? 1 : 2 }).format(v)}`;

export const clockOf = (iso: string | null | undefined): string => {
  const d = parse(iso);
  return d ? new Intl.DateTimeFormat(tag(), { hour: '2-digit', minute: '2-digit' }).format(d) : '';
};

export const dayOf = (iso: string | null | undefined): string => {
  const d = parse(iso);
  return d ? new Intl.DateTimeFormat(tag(), { day: 'numeric', month: 'long' }).format(d) : '';
};

/** «сегодня в 12:40», or «21 октября в 12:40»; empty when there is no date. */
export const stamp = (iso: string | null | undefined, now = Date.now()): string => {
  const d = parse(iso);
  if (!d) return '';
  const same = d.toDateString() === new Date(now).toDateString();
  return same
    ? `${locale() === 'ru' ? 'сегодня в' : 'today at'} ${clockOf(iso)}`
    : `${dayOf(iso)} ${clockOf(iso)}`;
};

export const number = (v: number): string =>
  new Intl.NumberFormat(tag(), { maximumFractionDigits: 1 }).format(v);
