// The currency the shown amounts are in. The hub says it in every answer with money (docs/hub/analytics.md): dollars
// while it knows no rate. A screen writes amounts with this symbol, so a dollar is never drawn as a ruble.
import type { Currency, Money } from '../../hub-api/common';

const SYMBOL: Record<Currency['code'], string> = { RUB: '₽', USD: '$' };

let current: Currency['code'] = 'RUB';
// Rubles per dollar of the last answer that said it; 0 while none has (then a dollar amount stays in dollars).
let perUsd = 0;

export const setShownCurrency = (code: Currency['code'], rate?: number): void => {
  current = code;
  if (rate && rate > 0) perUsd = rate;
};

/** Remembers the currency any answer of the hub carries, so every screen writes money the same way (HRN-670). */
export const noteCurrency = (body: unknown): void => {
  const c = (body as { currency?: Partial<Currency> } | null)?.currency;
  if (c && (c.code === 'RUB' || c.code === 'USD')) setShownCurrency(c.code, c.perUsd);
};

/** A dollar amount in the shown currency, or null when the hub has not said the rate yet. */
export const shownOfUsd = (usd: number): number | null =>
  current === 'USD' ? usd : perUsd > 0 ? usd * perUsd : null;

export const shownSymbol = (): string => SYMBOL[current];

export const inRubles = (): boolean => current === 'RUB';

/**
 * What a part of the spend comes to, for a screen: the work at API prices, token by token (HRN-663). What the
 * subscriptions cost a month is a fact of «Аккаунты», not of the work; it shows there. Paid stands in only for a part
 * that has no list price at all.
 */
export const spentOf = (x: {
  list: Money;
  paid: Money;
}): { shown: number; usd: number; estimated: boolean } =>
  x.list.usd > 0 || x.paid.usd === 0
    ? { shown: x.list.shown, usd: x.list.usd, estimated: false }
    : { shown: x.paid.shown, usd: x.paid.usd, estimated: false };

/** Where the month ends at this pace, in the same money spentOf shows: at API prices while the part has them. */
export const forecastShown = (x: {
  list: Money;
  paid: Money;
  forecast: Money | null;
  forecastList?: Money | null;
}): number | null => {
  const atList = x.list.usd > 0 || x.paid.usd === 0;
  const f = atList ? (x.forecastList ?? null) : x.forecast;
  return f?.shown ?? null;
};
