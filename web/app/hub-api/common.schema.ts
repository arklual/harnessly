import { day, lit, num, obj, str } from './schema';

export const money = obj({ usd: num, shown: num });
export const currency = obj({
  code: lit('RUB', 'USD'),
  perUsd: num,
  rateDate: day,
  source: lit('cbr', 'manual', 'none'),
});
export const period = obj({ from: day, to: day, tz: str });
export const personRef = obj({ userId: str, name: str });
