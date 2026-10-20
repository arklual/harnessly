/**
 * When a subscription window runs out. Pure: samples and spend history in, verdict out.
 *
 * With the person's transcript history the forecast follows their usual hours: the current pace fades within about an
 * hour into what they usually spend at those hours, nights and weekends included. Percent per dollar is calibrated on
 * the window itself. The range is the quieter and the busier quartile of the same hours in the past. A backtest on
 * our own history chose the details: a five-hour window is one stretch of work and follows the typical (median) hour,
 * a week adds up many hours and follows the average one; scaling by how busy the last day was only made it worse.
 *
 * Without that history it falls back to the pace alone. Percentages arrive as whole numbers, so the rate is the median
 * of pairwise slopes (Theil–Sen): one-point steps do not throw it off, and the quartiles of the slopes give a range.
 * Everything the samples cannot support is reported as `insufficient_data`, never guessed.
 */

import { t } from '@/lib/i18n';
import {
  averageRate,
  costBetween,
  historyDays,
  usualRate,
  type IActivity,
  type IUsualRate,
} from '@/lib/hl-limits-activity';

/** One measurement of a window: epoch seconds, percent used, reset time (epoch seconds). */
export interface ILimitSample {
  t: number;
  u: number;
  r: number;
}

export const FORECAST_STATUS = {
  exhaustsBeforeReset: 'exhausts_before_reset',
  lastsUntilReset: 'lasts_until_reset',
  insufficientData: 'insufficient_data',
  stale: 'stale',
  exhausted: 'exhausted',
} as const;

export type TForecastStatus = (typeof FORECAST_STATUS)[keyof typeof FORECAST_STATUS];

export const FORECAST_CONFIDENCE = { high: 'high', medium: 'medium', low: 'low' } as const;

export type TForecastConfidence = (typeof FORECAST_CONFIDENCE)[keyof typeof FORECAST_CONFIDENCE];

export interface IForecast {
  status: TForecastStatus;
  /** Median burn rate, percentage points per hour. */
  ratePctPerHour: number | null;
  /** Epoch seconds. */
  exhaustAt: number | null;
  /** Earliest and latest exhaustion (fast and slow quartile); the latest is null when the slow pace never runs out. */
  exhaustRange: [number, number | null] | null;
  usedAtReset: number | null;
  confidence: TForecastConfidence | null;
  samples: number;
  spanSec: number;
  /** Why there is no forecast, in Russian; null otherwise. */
  reason: string | null;
  /** Time of the last sample, epoch seconds; null without samples. */
  lastAt: number | null;
  resetsAt: number | null;
  /**
   * Absent on older runtimes: `profile` follows the person's usual hours, `pace` extends the last samples.
   */
  method?: TForecastMethod;
  /** Days of transcript history behind a `profile` forecast. */
  historyDays?: number;
  /** Projected use of a `profile` forecast, `[epoch seconds, percent]` from the last sample to the reset or to 100%. */
  path?: [number, number][];
}

export const FORECAST_METHOD = { profile: 'profile', pace: 'pace' } as const;

export type TForecastMethod = (typeof FORECAST_METHOD)[keyof typeof FORECAST_METHOD];

export interface IWindowSpec {
  /** Window length, seconds. */
  period: number;
  /** How far back the pace is measured. */
  lookback: number;
  /** Shortest time between two samples of one slope. */
  minGap: number;
  /** Shortest span of samples to forecast from. */
  minSpan: number;
  /** A last sample older than this makes the forecast stale. */
  fresh: number;
  /** Samples are thinned to the last one per bucket before pairing. */
  bucket: number;
}

export const WINDOW_SPECS = {
  five_hour: {
    period: 5 * 3600,
    lookback: 90 * 60,
    minGap: 5 * 60,
    minSpan: 20 * 60,
    fresh: 10 * 60,
    bucket: 60,
  },
  seven_day: {
    period: 7 * 86400,
    lookback: 24 * 3600,
    minGap: 60 * 60,
    minSpan: 6 * 3600,
    fresh: 2 * 3600,
    bucket: 10 * 60,
  },
} as const satisfies Record<string, IWindowSpec>;

/** Samples of one window whose reset time moved by at most this much belong to the same window. */
const SAME_RESET_SEC = 120;
/** A drop larger than one measurement step means a reset or a glitch: the pace is measured after it. */
const DROP_PCT = 1;
const IDLE_RATE_PCT_PER_HOUR = 0.1;

const EMPTY: Omit<IForecast, 'status'> = {
  ratePctPerHour: null,
  exhaustAt: null,
  exhaustRange: null,
  usedAtReset: null,
  confidence: null,
  samples: 0,
  spanSec: 0,
  reason: null,
  lastAt: null,
  resetsAt: null,
};

/** "20 min", "6 h", "1 h 30 min". */
export const formatSpan = (seconds: number): string => {
  const minutes = Math.round(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  if (!hours) return t('lib.hlLimitsForecast.minutes', { minutes });
  if (!rest) return t('lib.hlLimitsForecast.hours', { hours });
  return t('lib.hlLimitsForecast.hoursMinutes', { hours, minutes: rest });
};

const quantile = (sorted: readonly number[], q: number): number => {
  const position = (sorted.length - 1) * q;
  const low = Math.floor(position);
  const high = Math.ceil(position);
  return sorted[low] + (sorted[high] - sorted[low]) * (position - low);
};

const lastPerBucket = (samples: readonly ILimitSample[], bucket: number): ILimitSample[] => {
  const result: ILimitSample[] = [];
  for (const sample of samples) {
    const previous = result.at(-1);
    if (previous && Math.floor(previous.t / bucket) === Math.floor(sample.t / bucket))
      result[result.length - 1] = sample;
    else result.push(sample);
  }
  return result;
};

const slopesPerHour = (samples: readonly ILimitSample[], minGap: number): number[] => {
  const slopes: number[] = [];
  for (let i = 0; i < samples.length; i += 1) {
    for (let j = i + 1; j < samples.length; j += 1) {
      const dt = samples[j].t - samples[i].t;
      if (dt >= minGap) slopes.push(((samples[j].u - samples[i].u) / dt) * 3600);
    }
  }
  return slopes.sort((a, b) => a - b);
};

const confidenceOf = (
  count: number,
  span: number,
  spread: number,
  age: number,
  spec: IWindowSpec,
): TForecastConfidence => {
  if (count >= 12 && span >= 2 * spec.minSpan && spread <= 0.35 && age <= spec.fresh / 3)
    return FORECAST_CONFIDENCE.high;
  if (spread <= 0.8) return FORECAST_CONFIDENCE.medium;
  return FORECAST_CONFIDENCE.low;
};

const HOUR = 3600;
const DAY = 86400;
/** The current pace fades into the usual one with this time constant. */
const MOMENTUM_TAU_SEC = HOUR;
/** Transcripts read this recently tell the current pace better than whole-percent samples do. */
const MOMENTUM_FRESH_SEC = 15 * 60;
/** Past windows weigh as much as this many points of the current one when calibrating percent per dollar. */
const PRIOR_PCT = 10;
/** Spread of the runs is measured against at least this many points of spend, so a quiet window is not «unsure». */
const SPREAD_FLOOR_PCT = 5;
const MIN_CALIBRATION_PCT = 3;

interface IPace {
  series: ILimitSample[];
  span: number;
  /** Quartiles of the slopes, percentage points per hour; null when the samples do not give a pace. */
  rates: { r25: number; r50: number; r75: number } | null;
  /** Rise over the series is within one measurement step. */
  flat: boolean;
}

/** The samples of the current window the pace is measured on, and the slopes between them. */
const measurePace = (
  all: readonly ILimitSample[],
  last: ILimitSample,
  now: number,
  spec: IWindowSpec,
): IPace => {
  const reset = last.r;
  const windowStart = reset - spec.period;
  let series = all.filter(
    (x) => Math.abs(x.r - reset) <= SAME_RESET_SEC && x.t >= Math.max(windowStart, now - spec.lookback),
  );
  let dropAt = -1;
  for (let i = 1; i < series.length; i += 1) {
    if (series[i].u < series[i - 1].u - DROP_PCT) dropAt = i;
  }
  if (dropAt > 0) series = series.slice(dropAt);
  // The window really starts at zero: a window opened within the lookback gets that point for free.
  if (windowStart >= now - spec.lookback && series.length > 0 && series[0].t > windowStart) {
    series = [{ t: windowStart, u: 0, r: reset }, ...series];
  }
  series = lastPerBucket(series, spec.bucket);
  const span = series.length > 0 ? series[series.length - 1].t - series[0].t : 0;
  const flat = series.length === 0 || last.u - series[0].u <= DROP_PCT;
  if (series.length < 4 || span < spec.minSpan || flat) return { series, span, rates: null, flat };
  const slopes = slopesPerHour(series, spec.minGap);
  if (slopes.length === 0) return { series, span, rates: null, flat };
  return {
    series,
    span,
    rates: { r25: quantile(slopes, 0.25), r50: quantile(slopes, 0.5), r75: quantile(slopes, 0.75) },
    flat,
  };
};

interface ICalibration {
  pctPerUsd: number;
  /** Backed by past windows or by a good part of the current one. */
  strong: boolean;
}

/**
 * Percent of the window per dollar of API-equivalent spend. A window starts at zero, so its use at any moment is
 * exactly what was spent since its start; past windows steady the ratio while the current one is still young. A
 * window that began before the history is measured by its rise since the history began.
 */
const calibrate = (
  all: readonly ILimitSample[],
  last: ILimitSample,
  spec: IWindowSpec,
  activity: IActivity,
): ICalibration | null => {
  const groups: ILimitSample[][] = [];
  for (const sample of all) {
    const group = groups.at(-1);
    if (group && Math.abs(group[0].r - sample.r) <= SAME_RESET_SEC) group.push(sample);
    else groups.push([sample]);
  }
  const measuredOf = (group: readonly ILimitSample[]) => {
    const known = group.filter((x) => x.t <= activity.asOf);
    const at = known.at(-1);
    if (!at) return null;
    const start = group[0].r - spec.period;
    if (start >= activity.firstAt) return { used: at.u, cost: costBetween(activity, start, at.t) };
    // A new machine's history begins inside the window: the rise over the part it covers calibrates just as well.
    const from = known.find((x) => x.t >= activity.firstAt);
    if (!from || from === at || at.u < from.u) return null;
    return { used: at.u - from.u, cost: costBetween(activity, from.t, at.t) };
  };
  let pastUsed = 0;
  let pastCost = 0;
  let current: { used: number; cost: number } | null = null;
  for (const group of groups) {
    const measured = measuredOf(group);
    if (!measured) continue;
    if (Math.abs(group[0].r - last.r) <= SAME_RESET_SEC) current = measured;
    else if (measured.used >= MIN_CALIBRATION_PCT && measured.cost > 0) {
      pastUsed += measured.used;
      pastCost += measured.cost;
    }
  }
  const past = pastCost > 0 && pastUsed >= 2 * MIN_CALIBRATION_PCT ? pastUsed / pastCost : null;
  if (past !== null) {
    const pctPerUsd = current ? (current.used + PRIOR_PCT) / (current.cost + PRIOR_PCT / past) : past;
    return Number.isFinite(pctPerUsd) && pctPerUsd > 0 ? { pctPerUsd, strong: true } : null;
  }
  if (!current || current.used < MIN_CALIBRATION_PCT || current.cost <= 0) return null;
  return { pctPerUsd: current.used / current.cost, strong: current.used >= PRIOR_PCT };
};

interface IRun {
  exhaustAt: number | null;
  /** Use at the reset without the 100% cap, so the spread of the runs stays visible. */
  usedAtReset: number;
  firstRate: number;
  path: [number, number][];
}

/** Forecast from the person's usual hours; null when the history cannot support one. */
const forecastByProfile = (
  all: readonly ILimitSample[],
  last: ILimitSample,
  now: number,
  spec: IWindowSpec,
  activity: IActivity,
  pace: IPace,
  measured: Omit<IForecast, 'status'>,
): IForecast | null => {
  const days = historyDays(activity);
  if (days < 1) return null;
  const calibration = calibrate(all, last, spec, activity);
  if (!calibration) return null;
  const k = calibration.pctPerUsd;
  const fallback = averageRate(activity);
  const usualAt = (t: number) =>
    usualRate(activity, t) ?? { mean: fallback, median: fallback, low: fallback, high: fallback };
  const central = spec.period <= DAY ? 'median' : 'mean';

  const momentum =
    activity.asOf >= now - MOMENTUM_FRESH_SEC
      ? k * costBetween(activity, activity.asOf - HOUR, activity.asOf)
      : (pace.rates?.r50 ?? null);

  const reset = last.r;
  const step = spec.period <= DAY ? 10 * 60 : HOUR;
  const run = (pick: keyof IUsualRate): IRun => {
    let used = last.u;
    let exhaustAt: number | null = null;
    let firstRate = 0;
    const path: [number, number][] = [[last.t, last.u]];
    for (let from = last.t; from < reset; from += step) {
      const to = Math.min(reset, from + step);
      const mid = (from + to) / 2;
      const carry = momentum === null ? 0 : Math.exp(-(mid - last.t) / MOMENTUM_TAU_SEC);
      const rate = carry * (momentum ?? 0) + (1 - carry) * k * usualAt(mid)[pick];
      if (from === last.t) firstRate = rate;
      const next = used + (rate * (to - from)) / HOUR;
      if (exhaustAt === null && next >= 100) exhaustAt = from + ((100 - used) / rate) * HOUR;
      used = next;
      if (exhaustAt === null) path.push([to, Math.round(used * 10) / 10]);
    }
    if (exhaustAt !== null) path.push([exhaustAt, 100]);
    return { exhaustAt, usedAtReset: used, firstRate, path };
  };
  const expected = run(central);
  const busy = run('high');
  const quiet = run('low');

  const spent = (x: IRun) => x.usedAtReset - last.u;
  const spread = (spent(busy) - spent(quiet)) / Math.max(spent(expected), SPREAD_FLOOR_PCT);
  let confidence: TForecastConfidence = FORECAST_CONFIDENCE.low;
  if (days >= 14 && calibration.strong && spread <= 0.8) confidence = FORECAST_CONFIDENCE.high;
  else if (days >= 3 && spread <= 1.6) confidence = FORECAST_CONFIDENCE.medium;

  const common = {
    ...measured,
    ratePctPerHour: Math.round(expected.firstRate * 100) / 100,
    confidence,
    method: FORECAST_METHOD.profile,
    historyDays: Math.floor(days),
    path: expected.path,
  };
  const exhaustAt = expected.exhaustAt;
  if (exhaustAt === null) {
    return {
      ...common,
      status: FORECAST_STATUS.lastsUntilReset,
      usedAtReset: Math.min(100, expected.usedAtReset),
    };
  }
  // A skewed hour can put the average above the busy quartile, so the range is widened to hold the forecast itself.
  return {
    ...common,
    status: FORECAST_STATUS.exhaustsBeforeReset,
    exhaustAt,
    exhaustRange: [
      Math.min(busy.exhaustAt ?? exhaustAt, exhaustAt),
      quiet.exhaustAt === null ? null : Math.max(quiet.exhaustAt, exhaustAt),
    ],
  };
};

/**
 * `all` must be sorted by time. `now` in epoch seconds. `activity` is the person's spend history; without it, or
 * when it cannot be calibrated, the forecast extends the pace of the last samples.
 */
export const forecastWindow = (
  all: readonly ILimitSample[],
  now: number,
  spec: IWindowSpec,
  activity: IActivity | null = null,
): IForecast => {
  const last = all.at(-1);
  if (!last)
    return {
      ...EMPTY,
      status: FORECAST_STATUS.insufficientData,
      reason: t('lib.hlLimitsForecast.reasonNoSamples'),
    };
  const base = { ...EMPTY, lastAt: last.t, resetsAt: last.r };
  if (last.r <= now)
    return {
      ...base,
      status: FORECAST_STATUS.insufficientData,
      reason: t('lib.hlLimitsForecast.reasonWindowReset'),
    };
  if (last.u >= 100) return { ...base, status: FORECAST_STATUS.exhausted, usedAtReset: 100 };
  if (now - last.t > spec.fresh)
    return { ...base, status: FORECAST_STATUS.stale, reason: t('lib.hlLimitsForecast.reasonNoFresh') };

  const pace = measurePace(all, last, now, spec);
  const measured = { ...base, samples: pace.series.length, spanSec: pace.span };
  const profiled = activity ? forecastByProfile(all, last, now, spec, activity, pace, measured) : null;
  if (profiled) return profiled;

  const reset = last.r;
  const byPace = { ...measured, method: FORECAST_METHOD.pace };
  if (pace.series.length < 4 || pace.span < spec.minSpan) {
    return {
      ...byPace,
      status: FORECAST_STATUS.insufficientData,
      reason: t('lib.hlLimitsForecast.reasonNeedSpan', { span: formatSpan(spec.minSpan) }),
    };
  }
  const lasts = (rate: number, confidence: TForecastConfidence): IForecast => ({
    ...byPace,
    status: FORECAST_STATUS.lastsUntilReset,
    ratePctPerHour: rate,
    usedAtReset: Math.min(100, last.u + (Math.max(0, rate) * (reset - last.t)) / 3600),
    confidence,
  });
  if (pace.flat) {
    if (pace.span >= 2 * spec.minSpan) return lasts(0, FORECAST_CONFIDENCE.medium);
    return {
      ...byPace,
      status: FORECAST_STATUS.insufficientData,
      reason: t('lib.hlLimitsForecast.reasonFlat'),
    };
  }
  if (!pace.rates) {
    return {
      ...byPace,
      status: FORECAST_STATUS.insufficientData,
      reason: t('lib.hlLimitsForecast.reasonNeedSpan', { span: formatSpan(spec.minSpan) }),
    };
  }
  const { r25, r50, r75 } = pace.rates;
  if (r50 <= IDLE_RATE_PCT_PER_HOUR) return lasts(r50, FORECAST_CONFIDENCE.medium);

  const hoursLeft = (rate: number) => ((100 - last.u) / rate) * 3600;
  const exhaustAt = last.t + hoursLeft(r50);
  const spread = (r75 - r25) / r50;
  const confidence = confidenceOf(pace.series.length, pace.span, spread, now - last.t, spec);
  if (exhaustAt >= reset) return lasts(r50, confidence);
  return {
    ...byPace,
    status: FORECAST_STATUS.exhaustsBeforeReset,
    ratePctPerHour: r50,
    exhaustAt,
    exhaustRange: [last.t + hoursLeft(r75), r25 > 0 ? last.t + hoursLeft(r25) : null],
    confidence,
  };
};

// --- Texts ------------------------------------------------------------------------------------------------------

export type TForecastWindowKind = 'five_hour' | 'seven_day';

/** Formats epoch seconds in the viewer's time zone; `withDay` adds a short weekday ("Sat 13:30"). */
export type TFormatClock = (epochSec: number, withDay: boolean) => string;

const ROUND_SEC: Record<TForecastWindowKind, number> = { five_hour: 5 * 60, seven_day: 3600 };
/** A forecast this close and at least this sure turns the header chip into a warning. */
const WARN_WITHIN_SEC: Record<TForecastWindowKind, number> = { five_hour: 3 * 3600, seven_day: 24 * 3600 };

const CONFIDENCE_TEXT: Record<TForecastConfidence, string> = {
  high: t('lib.hlLimitsForecast.confidenceHigh'),
  medium: t('lib.hlLimitsForecast.confidenceMedium'),
  low: t('lib.hlLimitsForecast.confidenceLow'),
};

export const FORECAST_TONE = { neutral: 'neutral', warning: 'warning', danger: 'danger' } as const;
export type TForecastTone = (typeof FORECAST_TONE)[keyof typeof FORECAST_TONE];

export interface IForecastText {
  tone: TForecastTone;
  text: string;
  /** Range and confidence, or null. */
  detail: string | null;
}

const roundTo = (epochSec: number, step: number) => Math.round(epochSec / step) * step;

/** Whether the forecast is sure and close enough to warn about in the header. */
export const isForecastWarning = (
  forecast: IForecast | null | undefined,
  kind: TForecastWindowKind,
  now: number,
): boolean => {
  if (!forecast) return false;
  if (forecast.status === FORECAST_STATUS.exhausted) return true;
  if (forecast.status !== FORECAST_STATUS.exhaustsBeforeReset || forecast.exhaustAt === null) return false;
  if (forecast.confidence !== FORECAST_CONFIDENCE.high && forecast.confidence !== FORECAST_CONFIDENCE.medium)
    return false;
  return forecast.exhaustAt - now <= WARN_WITHIN_SEC[kind];
};

/** The forecast line for one window, e.g. "Claude limit (5 h) runs out ≈ by 16:50". */
export const describeForecast = (
  forecast: IForecast,
  kind: TForecastWindowKind,
  providerName: string,
  clock: TFormatClock,
): IForecastText => {
  const weekly = kind === 'seven_day';
  const at = (epochSec: number) => clock(roundTo(epochSec, ROUND_SEC[kind]), weekly);
  const reset = forecast.resetsAt === null ? null : clock(forecast.resetsAt, weekly);
  const confidence = forecast.confidence
    ? t('lib.hlLimitsForecast.confidence', { level: CONFIDENCE_TEXT[forecast.confidence] })
    : null;
  const basis = forecast.method === FORECAST_METHOD.profile ? t('lib.hlLimitsForecast.basisProfile') : null;
  switch (forecast.status) {
    case FORECAST_STATUS.exhaustsBeforeReset: {
      const exhaustAt = forecast.exhaustAt ?? 0;
      const range = forecast.exhaustRange;
      const rangeText = range ? `${at(range[0])}–${range[1] === null ? '…' : at(range[1])}` : null;
      const text = weekly
        ? reset
          ? t('lib.hlLimitsForecast.weeklyRunsOutBeforeReset', {
              provider: providerName,
              at: at(exhaustAt),
              reset,
            })
          : t('lib.hlLimitsForecast.weeklyRunsOut', { provider: providerName, at: at(exhaustAt) })
        : t('lib.hlLimitsForecast.fiveHourRunsOut', { provider: providerName, at: at(exhaustAt) });
      return {
        tone: FORECAST_TONE.warning,
        text,
        detail: [rangeText, confidence, basis].filter(Boolean).join(' · ') || null,
      };
    }
    case FORECAST_STATUS.lastsUntilReset: {
      const used =
        forecast.usedAtReset === null
          ? ''
          : ` · ${t('lib.hlLimitsForecast.usedAtReset', { percent: Math.round(forecast.usedAtReset) })}`;
      return {
        tone: FORECAST_TONE.neutral,
        text: `${reset ? t('lib.hlLimitsForecast.lastsUntilResetAt', { reset }) : t('lib.hlLimitsForecast.lastsUntilReset')}${used}`,
        detail: [confidence, basis].filter(Boolean).join(' · ') || null,
      };
    }
    case FORECAST_STATUS.exhausted:
      return {
        tone: FORECAST_TONE.danger,
        text: reset
          ? t('lib.hlLimitsForecast.exhaustedResetAt', { reset })
          : t('lib.hlLimitsForecast.exhausted'),
        detail: null,
      };
    case FORECAST_STATUS.stale:
      return {
        tone: FORECAST_TONE.neutral,
        text:
          forecast.lastAt === null
            ? t('lib.hlLimitsForecast.stale')
            : t('lib.hlLimitsForecast.staleSince', { at: clock(forecast.lastAt, weekly) }),
        detail: null,
      };
    default:
      return {
        tone: FORECAST_TONE.neutral,
        text: t('lib.hlLimitsForecast.insufficient', {
          reason: forecast.reason ?? t('lib.hlLimitsForecast.reasonFewSamples'),
        }),
        detail: null,
      };
  }
};

/** "2 h 5 min", "14 h 44 min", "3 d 4 h". */
export const formatDuration = (seconds: number): string => {
  const total = Math.max(0, Math.round(seconds / 60));
  const days = Math.floor(total / 1440);
  const hours = Math.floor((total % 1440) / 60);
  const minutes = total % 60;
  if (days) return t('lib.hlLimitsForecast.daysHours', { days, hours });
  if (hours) return t('lib.hlLimitsForecast.hoursMinutes', { hours, minutes });
  return t('lib.hlLimitsForecast.minutes', { minutes });
};

/** codexbar's window average: "Window average: 44% over pace, runs out in 14 h 44 min". */
export const describePace = (pace: {
  deltaPercent: number;
  willLastToReset: boolean;
  etaSeconds: number | null;
}): string => {
  const delta = Math.round(Math.abs(pace.deltaPercent));
  const balance =
    pace.deltaPercent > 0
      ? t('lib.hlLimitsForecast.paceDeficit', { delta })
      : t('lib.hlLimitsForecast.paceReserve', { delta });
  if (pace.willLastToReset || pace.etaSeconds === null)
    return t('lib.hlLimitsForecast.paceLasts', { balance });
  return t('lib.hlLimitsForecast.paceRunsOut', { balance, eta: formatDuration(pace.etaSeconds) });
};
