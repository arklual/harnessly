// The provider cards of Settings → «Провайдеры» (the board «Лимиты — карточки провайдеров», variant A): what
// a window's bar, its pace mark and the card's one-line forecast say, and what each account spent. Pure: every figure
// comes from the machine's limits or its report of spend; what neither says is not drawn.
import {
  WINDOW,
  FORECAST,
  clockOf,
  leftOf,
  span,
  windowsOf,
  type LimitWindow,
  type ProviderLimits,
  type WindowKind,
} from '@/next/features/limits/model';
import { locale, plural, t } from '@/next/i18n';

/** A card colours only what runs out: yellow under 40% left, red under 15% or when it ends before the reset. */
export const CARD_LEVEL = { ok: 'ok', warn: 'warn', bad: 'bad' } as const;
export type CardLevel = (typeof CARD_LEVEL)[keyof typeof CARD_LEVEL];
const WARN_UNDER = 40;
const BAD_UNDER = 15;

const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** How long each window is: the pace is measured against it. */
export const WINDOW_SECONDS: Readonly<Record<WindowKind, number>> = {
  five_hour: 5 * HOUR,
  seven_day: 7 * DAY,
  month: 30 * DAY,
};

/** Too little of a window has passed for its pace to say anything: the first twentieth of it. */
const PACE_FROM = 0.05;

/** When a window runs out: at a moment before its reset, not before it, or unknown. */
export type RunOut = { k: 'at'; at: number } | { k: 'lasts' } | { k: 'unknown' };

/**
 * When the window runs out at the pace it is spent: the machine's own forecast where it gives one (Claude, Codex),
 * else the share used over the share of the window gone by. Seconds since the epoch.
 */
export const runOutOf = (kind: WindowKind, window: LimitWindow, nowSec: number): RunOut => {
  const left = leftOf(window);
  if (left <= 0) return { k: 'at', at: nowSec };
  const fc = window.forecast;
  if (fc?.status === FORECAST.exhausts && fc.exhaustAt !== null) return { k: 'at', at: fc.exhaustAt };
  if (fc?.status === FORECAST.lasts) return { k: 'lasts' };
  if (fc?.status === FORECAST.exhausted) return { k: 'at', at: nowSec };
  const length = WINDOW_SECONDS[kind];
  const toReset = window.resetsAt - nowSec;
  const gone = length - toReset;
  if (toReset <= 0 || gone < length * PACE_FROM || window.usedPct <= 0) return { k: 'unknown' };
  const perSecond = window.usedPct / gone;
  const at = nowSec + left / perSecond;
  return at < window.resetsAt ? { k: 'at', at } : { k: 'lasts' };
};

/** Where what is left should be now to last to the reset spending evenly: the share of the window still ahead, %. */
export const paceMarkOf = (kind: WindowKind, window: LimitWindow, nowSec: number): number | null => {
  const ahead = (window.resetsAt - nowSec) / WINDOW_SECONDS[kind];
  if (!Number.isFinite(ahead) || ahead <= 0 || ahead >= 1) return null;
  return Math.round(ahead * 100);
};

export const cardLevelOf = (left: number, endsBeforeReset: boolean): CardLevel => {
  if (left < BAD_UNDER || endsBeforeReset) return CARD_LEVEL.bad;
  if (left < WARN_UNDER) return CARD_LEVEL.warn;
  return CARD_LEVEL.ok;
};

const WINDOW_LABEL: Readonly<Record<WindowKind, string>> = {
  five_hour: 'next.limits.fiveHour',
  seven_day: 'next.limits.sevenDay',
  month: 'next.limits.month',
};

/** «сброс через 2 ч» for five hours, «сброс в пн, 09:00» for a week, the date for a month. */
export const resetText = (kind: WindowKind, resetsAt: number, nowSec: number): string => {
  if (kind === WINDOW.fiveHour) return t('next.limits.card.resetIn', { span: span(resetsAt - nowSec) });
  if (kind === WINDOW.sevenDay) return t('next.limits.card.resetAt', { at: clockOf(resetsAt, true) });
  return t('next.limits.card.resetAt', {
    at: new Intl.DateTimeFormat(locale(), { day: 'numeric', month: 'short' }).format(
      new Date(resetsAt * 1000),
    ),
  });
};

export interface CardWindow {
  kind: WindowKind;
  label: string;
  left: number;
  level: CardLevel;
  reset: string;
  /** The pace mark, %; only on the week. */
  pace: number | null;
  runOut: RunOut;
}

export const cardWindowsOf = (limits: ProviderLimits | null, nowSec: number): CardWindow[] =>
  windowsOf(limits, nowSec).map(({ kind, window }) => {
    const left = leftOf(window);
    const runOut = runOutOf(kind, window, nowSec);
    return {
      kind,
      label: t(WINDOW_LABEL[kind]),
      left,
      level: cardLevelOf(left, runOut.k === 'at'),
      reset: resetText(kind, window.resetsAt, nowSec),
      pace: kind === WINDOW.sevenDay ? paceMarkOf(kind, window, nowSec) : null,
      runOut,
    };
  });

export interface CardForecast {
  text: string;
  level: CardLevel;
}

/** «Кончится через 25 мин», «Кончится в чт 15:00»: within a day by the span, later by the clock. */
const endsText = (at: number, nowSec: number): string => {
  if (at <= nowSec) return t('next.limits.card.over');
  if (at - nowSec < DAY) return t('next.limits.card.endsIn', { span: span(at - nowSec) });
  return t('next.limits.card.endsAt', { at: clockOf(at, true) });
};

/**
 * The card's one line: the soonest end among all its windows, else «Хватит до сброса» once every window is known to
 * last; null when nothing can be told (no windows, or a pace too young to read).
 */
export const cardForecastOf = (windows: readonly CardWindow[], nowSec: number): CardForecast | null => {
  if (windows.length === 0) return null;
  const ends = windows.flatMap((w) => (w.runOut.k === 'at' ? [w.runOut.at] : []));
  if (ends.length > 0) return { text: endsText(Math.min(...ends), nowSec), level: CARD_LEVEL.bad };
  if (windows.some((w) => w.runOut.k === 'unknown')) return null;
  const least = Math.min(...windows.map((w) => w.left));
  return { text: t('next.limits.card.lasts'), level: cardLevelOf(least, false) };
};

// ---- spend by account (the machine's report of spend)

/** What one account spent, USD at API prices: today, the last seven days with today, and each of those days. */
export interface AccountSpend {
  today: number;
  week: number;
  /** Seven days, the oldest first. */
  days: number[];
}

/** The spend of every account by spendKey. */
export type AccountSpendMap = ReadonlyMap<string, AccountSpend>;

/** The spend of the CLI's own sign-in: the report names no account for it. */
export const OWN_ACCOUNT = '';
export const spendKey = (provider: string, account: string): string => `${provider}/${account}`;

type Rec = Record<string, unknown>;
const rec = (v: unknown): Rec | null =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Rec) : null;

/** The day in the report's zone, YYYY-MM-DD, of a moment. */
const dayIn = (ms: number, zone: string): string => {
  try {
    return new Intl.DateTimeFormat('en-CA', {
      timeZone: zone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(new Date(ms));
  } catch {
    return new Date(ms).toISOString().slice(0, 10);
  }
};

/** The seven days to today, oldest first, as keys of the report. */
const lastWeek = (today: string): string[] => {
  const base = Date.parse(`${today}T12:00:00Z`);
  return Array.from({ length: 7 }, (_, i) =>
    new Date(base - (6 - i) * DAY * 1000).toISOString().slice(0, 10),
  );
};

/**
 * The spend of every account of the machine from its report of usage (`…/stats`: the runtimes, each with its report):
 * the rows of the last seven days by agent and account. Null when the answer has no report of this machine.
 */
export const readAccountSpend = (
  doc: unknown,
  machine: string,
  nowMs: number,
): Map<string, AccountSpend> | null => {
  const runtimes = rec(doc)?.runtimes;
  if (!Array.isArray(runtimes)) return null;
  const mine =
    runtimes.find((r) => rec(r)?.machineId === machine) ?? (runtimes.length === 1 ? runtimes[0] : undefined);
  const report = rec(rec(mine)?.report);
  if (!report || !Array.isArray(report.usage)) return null;
  const zone = typeof report.timezone === 'string' && report.timezone ? report.timezone : 'UTC';
  const days = lastWeek(dayIn(nowMs, zone));
  const at = new Map(days.map((d, i) => [d, i]));
  const out = new Map<string, AccountSpend>();
  for (const row of report.usage) {
    if (!Array.isArray(row)) continue;
    const [day, provider, , , , , , , cost, , account] = row as unknown[];
    const i = typeof day === 'string' ? at.get(day) : undefined;
    if (i === undefined || typeof provider !== 'string' || typeof cost !== 'number' || !Number.isFinite(cost))
      continue;
    const key = spendKey(provider, typeof account === 'string' ? account : OWN_ACCOUNT);
    const entry = out.get(key) ?? { today: 0, week: 0, days: [0, 0, 0, 0, 0, 0, 0] };
    entry.days[i] += cost;
    entry.week += cost;
    if (i === days.length - 1) entry.today += cost;
    out.set(key, entry);
  }
  return out;
};

/** «$38», «$1,20»: whole dollars from ten up, cents below. */
export const usd = (amount: number): string =>
  new Intl.NumberFormat(locale(), {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: amount >= 10 ? 0 : 2,
    minimumFractionDigits: amount >= 10 ? 0 : 2,
  }).format(amount);

/** How long a balance lasts at the last week's pace: «Хватит примерно на 6 недель»; null without spend to judge by. */
export const balanceForecastOf = (amount: number, spend: AccountSpend | null): CardForecast | null => {
  if (!spend || spend.week <= 0) return null;
  const days = amount / (spend.week / 7);
  if (days < 1) return { text: t('next.limits.card.balanceDay'), level: CARD_LEVEL.bad };
  if (days < 14)
    return {
      text: plural(Math.floor(days), 'next.limits.card.balanceDays'),
      level: days < 3 ? CARD_LEVEL.bad : CARD_LEVEL.warn,
    };
  return { text: plural(Math.floor(days / 7), 'next.limits.card.balanceWeeks'), level: CARD_LEVEL.ok };
};
