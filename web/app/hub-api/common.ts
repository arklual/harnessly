// What the parts of the new hub's contract share (docs/hub/analytics.md, «Money and periods»).

// An amount: kept in USD, shown in the organization's currency at the month's rate.
export type Money = { usd: number; shown: number };

// The rate the shown amounts of an answer use: the Central Bank's rate on the 1st of the month, or an admin's correction.
export type Currency = {
  code: 'RUB' | 'USD';
  perUsd: number;
  rateDate: string;
  source: 'cbr' | 'manual' | 'none';
};

// [from, to) in the organization's time zone, as calendar days.
export type Period = { from: string; to: string; tz: string };

export type PersonRef = { userId: string; name: string };
