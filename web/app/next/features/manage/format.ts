import { locale } from '@/next/i18n';
import type { HubCurrency } from '@/next/features/manage/hub-types';

const tag = () => (locale() === 'ru' ? 'ru-RU' : 'en-US');

export const hubMoney = (v: number, currency: HubCurrency): string =>
  new Intl.NumberFormat(tag(), { style: 'currency', currency, maximumFractionDigits: 0 }).format(v);

/** 12 700 → «12,7 тыс ₽» style short money for big headline numbers. */
export const hubMoneyShort = (v: number, currency: HubCurrency): string => {
  const symbol = currency === 'RUB' ? '₽' : '$';
  if (v >= 1000) {
    const k = new Intl.NumberFormat(tag(), { maximumFractionDigits: 1 }).format(v / 1000);
    return `${k}${locale() === 'ru' ? ' тыс' : 'K'} ${symbol}`;
  }
  return `${new Intl.NumberFormat(tag()).format(Math.round(v))} ${symbol}`;
};

export const usd = (v: number): string =>
  `$${new Intl.NumberFormat(tag(), { maximumFractionDigits: v >= 100 ? 0 : v >= 10 ? 1 : 2 }).format(v)}`;

export const clockOf = (iso: string): string =>
  new Intl.DateTimeFormat(tag(), { hour: '2-digit', minute: '2-digit' }).format(new Date(iso));

export const dayOf = (iso: string): string =>
  new Intl.DateTimeFormat(tag(), { day: 'numeric', month: 'long' }).format(new Date(iso));

/** «сегодня в 12:40», or «21 октября в 12:40». */
export const stamp = (iso: string, now = Date.now()): string => {
  const d = new Date(iso);
  const today = new Date(now);
  const same = d.toDateString() === today.toDateString();
  return same
    ? `${locale() === 'ru' ? 'сегодня в' : 'today at'} ${clockOf(iso)}`
    : `${dayOf(iso)} ${clockOf(iso)}`;
};

export const number = (v: number): string =>
  new Intl.NumberFormat(tag(), { maximumFractionDigits: 1 }).format(v);
