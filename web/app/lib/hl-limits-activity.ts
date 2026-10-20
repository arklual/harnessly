/**
 * How much a person's agents usually spend at a given hour. Pure: transcript costs in, rates out.
 *
 * The spend is the API-equivalent cost of every model call from the person's own Claude or Codex transcripts. The
 * usual rate at a future hour is taken from analogs: the same hour one to six weeks back, and the same hour of the
 * last days of the same kind (a workday for a workday, a weekend day for a weekend day). Nights, weekends and the
 * person's habits come from the data itself, nothing about working hours is assumed.
 */

export const ACTIVITY_SLOT_SEC = 600;
/** Six weeks of analogs and a day of margin; Claude Code deletes transcripts after 30 days by default anyway. */
export const ACTIVITY_DAYS = 43;

export interface IActivity {
  /** Epoch seconds of the first slot. */
  start: number;
  /** `cum[i]` is the cost of the slots before slot `i`, USD. */
  cum: Float64Array;
  /** First call with a cost: before it the history knows nothing. */
  firstAt: number;
  /** Transcripts were read up to this moment: after it the cost is unknown, not zero. */
  asOf: number;
}

/** USD per hour: the weighted mean and quartiles of the analogs. */
export interface IUsualRate {
  mean: number;
  median: number;
  low: number;
  high: number;
}

const DAY = 86400;
const HOUR = 3600;
// All three of us work in Moscow, as in the usage statistics: the weekend is Moscow's Saturday and Sunday.
const TZ_OFFSET_SEC = 3 * HOUR;

interface IAnalog {
  lag: number;
  weight: number;
  /** A day lag only stands for a day of the same kind; a week lag always does. */
  sameKindOnly: boolean;
}

const ANALOGS: readonly IAnalog[] = [
  ...Array.from({ length: 6 }, (_, i) => ({
    lag: (i + 1) * 7 * DAY,
    weight: 2 * 0.8 ** i,
    sameKindOnly: false,
  })),
  ...Array.from({ length: 13 }, (_, i) => i + 1)
    .filter((days) => days % 7 !== 0)
    .map((days) => ({ lag: days * DAY, weight: 0.85 ** (days - 1), sameKindOnly: true })),
];

export const isWeekend = (epochSec: number): boolean => {
  const day = new Date((epochSec + TZ_OFFSET_SEC) * 1000).getUTCDay();
  return day === 0 || day === 6;
};

/** `events` are `[epoch seconds, USD]` in any order; null when nothing of the last weeks cost anything. */
export const buildActivity = (
  events: Iterable<readonly [number, number]>,
  asOf: number,
): IActivity | null => {
  const start = Math.floor((asOf - ACTIVITY_DAYS * DAY) / ACTIVITY_SLOT_SEC) * ACTIVITY_SLOT_SEC;
  const slots = Math.ceil((asOf - start) / ACTIVITY_SLOT_SEC) + 1;
  const cost = new Float64Array(slots);
  let firstAt = Infinity;
  for (const [t, usd] of events) {
    if (!(usd > 0) || t < start || t > asOf) continue;
    cost[Math.floor((t - start) / ACTIVITY_SLOT_SEC)] += usd;
    if (t < firstAt) firstAt = t;
  }
  if (firstAt === Infinity) return null;
  const cum = new Float64Array(slots + 1);
  for (let i = 0; i < slots; i += 1) cum[i + 1] = cum[i] + cost[i];
  return { start, cum, firstAt, asOf };
};

const cumAt = (activity: IActivity, t: number): number => {
  const position =
    (Math.min(Math.max(t, activity.start), activity.asOf) - activity.start) / ACTIVITY_SLOT_SEC;
  const index = Math.min(Math.floor(position), activity.cum.length - 2);
  return activity.cum[index] + (activity.cum[index + 1] - activity.cum[index]) * (position - index);
};

/** Cost between two moments, USD; a slot's cost is spread evenly over it. */
export const costBetween = (activity: IActivity, from: number, to: number): number =>
  to <= from ? 0 : cumAt(activity, to) - cumAt(activity, from);

/** Whole days of history the analogs can draw on. */
export const historyDays = (activity: IActivity): number =>
  Math.max(0, (activity.asOf - activity.firstAt) / DAY);

const weightedQuantile = (
  values: readonly { value: number; weight: number }[],
  total: number,
  q: number,
): number => {
  const target = total * q;
  let acc = 0;
  for (const item of values) {
    acc += item.weight;
    if (acc >= target) return item.value;
  }
  return values[values.length - 1].value;
};

/**
 * The usual spend around moment `t`: every analog is a two-hour span centred on the same moment back then, so a
 * session that ran a little earlier or later that day still counts. Null when no analog is inside the history.
 */
export const usualRate = (activity: IActivity, t: number): IUsualRate | null => {
  const weekend = isWeekend(t);
  const collect = (anyKind: boolean) => {
    const values: { value: number; weight: number }[] = [];
    for (const analog of ANALOGS) {
      const at = t - analog.lag;
      if (at - HOUR < activity.firstAt || at + HOUR > activity.asOf) continue;
      if (analog.sameKindOnly && !anyKind && isWeekend(at) !== weekend) continue;
      values.push({ value: costBetween(activity, at - HOUR, at + HOUR) / 2, weight: analog.weight });
    }
    return values;
  };
  // A young history may not have seen a day of this kind yet: the same hour of any day still knows day from night.
  let values = collect(false);
  if (values.length === 0) values = collect(true);
  if (values.length === 0) return null;
  const total = values.reduce((acc, item) => acc + item.weight, 0);
  const mean = values.reduce((acc, item) => acc + item.value * item.weight, 0) / total;
  values.sort((a, b) => a.value - b.value);
  return {
    mean,
    median: weightedQuantile(values, total, 0.5),
    low: weightedQuantile(values, total, 0.25),
    high: weightedQuantile(values, total, 0.75),
  };
};

/** Average spend over the whole history, USD per hour: the fallback for an hour no analog covers. */
export const averageRate = (activity: IActivity): number => {
  const span = activity.asOf - activity.firstAt;
  return span > 0 ? (costBetween(activity, activity.firstAt, activity.asOf) / span) * HOUR : 0;
};
