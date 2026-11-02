// The currency the shown amounts are in. The hub says it in every answer with money (docs/hub/analytics.md): dollars
// while it knows no rate. A screen writes amounts with this symbol, so a dollar is never drawn as a ruble.
import type { Currency, Money } from '../../hub-api/common';

const SYMBOL: Record<Currency['code'], string> = { RUB: '₽', USD: '$' };

let current: Currency['code'] = 'RUB';

export const setShownCurrency = (code: Currency['code']): void => {
  current = code;
};

export const shownSymbol = (): string => SYMBOL[current];

export const inRubles = (): boolean => current === 'RUB';

/**
 * What a part of the spend comes to, for a screen. Paid is the truth; a subscription with no month's price has paid 0
 * by contract (docs/hub/analytics.md), so then it is the amount at list prices, and the screen says it is an estimate.
 */
export const spentOf = (x: {
  list: Money;
  paid: Money;
}): { shown: number; usd: number; estimated: boolean } =>
  x.paid.usd === 0 && x.list.usd > 0
    ? { shown: x.list.shown, usd: x.list.usd, estimated: true }
    : { shown: x.paid.shown, usd: x.paid.usd, estimated: false };
