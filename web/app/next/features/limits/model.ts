// What is left of an agent's subscription, as the machine says it (the platform route `…/machines`, the one the current
// interface reads): the five-hour and the weekly window of each agent, with the machine's own forecast. Pure: the
// answer goes through a reader that takes nothing it cannot read as a number, and the words and levels come from it.
// No figure is ever made up: a window the machine does not report is not there, and the view says so.
import { locale, t } from '@/next/i18n';
import type { Provider } from '@/next/protocol/v2';

/**
 * The agent whose quota a chat's chip shows: none for a chat on a vendor's profile (Kimi, GLM, Grok), whose limits are
 * not Anthropic's or Codex's and are not read here.
 */
export const limitsProviderOf = (
  chat: { provider: Provider; profile?: string | null } | null | undefined,
): Provider | null => (!chat || chat.profile ? null : chat.provider);

export const WINDOW = { fiveHour: 'five_hour', sevenDay: 'seven_day' } as const;
export type WindowKind = (typeof WINDOW)[keyof typeof WINDOW];

export const FORECAST = {
  exhausts: 'exhausts_before_reset',
  lasts: 'lasts_until_reset',
  insufficient: 'insufficient_data',
  stale: 'stale',
  exhausted: 'exhausted',
} as const;
export type ForecastStatus = (typeof FORECAST)[keyof typeof FORECAST];

export const CONFIDENCE = ['high', 'medium', 'low'] as const;
export type Confidence = (typeof CONFIDENCE)[number];

/** How much is left: plenty, running low (a quarter), out (a twentieth). */
export const LEVEL = { ok: 'ok', low: 'low', out: 'out' } as const;
export type Level = (typeof LEVEL)[keyof typeof LEVEL];
const LOW_AT = 25;
const OUT_AT = 5;

export interface LimitForecast {
  status: ForecastStatus;
  /** Epoch seconds. */
  exhaustAt: number | null;
  exhaustRange: [number, number | null] | null;
  usedAtReset: number | null;
  confidence: Confidence | null;
  /** The machine's words for why there is no forecast. */
  reason: string | null;
  lastAt: number | null;
  resetsAt: number | null;
  /** Follows the person's usual hours, not only the last pace. */
  profile: boolean;
}

export interface LimitWindow {
  usedPct: number;
  /** Epoch seconds. */
  resetsAt: number;
  forecast: LimitForecast | null;
}

export interface ProviderLimits {
  fiveHour: LimitWindow | null;
  sevenDay: LimitWindow | null;
  /** Epoch ms of the machine's last reading. */
  updatedAt: number | null;
  plan: string | null;
  /** The last failure of the machine's own reading, shown beside the values it did not refresh. */
  error: string | null;
}

export type MachineLimits = Record<Provider, ProviderLimits | null>;

// ---- the reader

type Rec = Record<string, unknown>;

const rec = (v: unknown): Rec | null =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Rec) : null;
const finite = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);
const text = (v: unknown): string | null => (typeof v === 'string' && v !== '' ? v : null);

const STATUSES: readonly string[] = Object.values(FORECAST);

const readForecast = (v: unknown): LimitForecast | null => {
  const f = rec(v);
  if (!f || typeof f.status !== 'string' || !STATUSES.includes(f.status)) return null;
  const range = Array.isArray(f.exhaustRange) ? f.exhaustRange : null;
  const first = range ? finite(range[0]) : null;
  const confidence = CONFIDENCE.find((level) => level === f.confidence) ?? null;
  return {
    status: f.status as ForecastStatus,
    exhaustAt: finite(f.exhaustAt),
    exhaustRange: range && first !== null ? [first, finite(range[1])] : null,
    usedAtReset: finite(f.usedAtReset),
    confidence,
    reason: text(f.reason),
    lastAt: finite(f.lastAt),
    resetsAt: finite(f.resetsAt),
    profile: f.method === 'profile',
  };
};

/** A window without a reading or a reset is not a window. */
const readWindow = (v: unknown): LimitWindow | null => {
  const w = rec(v);
  if (!w) return null;
  const usedPct = finite(w.usedPct);
  const resetsAt = finite(w.resetsAt);
  if (usedPct === null || resetsAt === null) return null;
  return { usedPct, resetsAt, forecast: readForecast(w.forecast) };
};

const readProvider = (v: unknown): ProviderLimits | null => {
  const p = rec(v);
  if (!p) return null;
  const updated = finite(p.updatedAt);
  return {
    fiveHour: readWindow(p.fiveHour),
    sevenDay: readWindow(p.sevenDay),
    updatedAt: updated === null ? null : updated * 1000,
    plan: text(p.plan),
    error: text(p.error),
  };
};

/**
 * The limits of the machine in the answer of `…/machines`. The runtime answers for itself; a gateway lists every machine
 * of the person, so the machine is found by its id, or is the only one listed. Throws on an answer that is not a list.
 */
export const readMachineLimits = (doc: unknown, machine: string): MachineLimits | null => {
  const list = rec(doc)?.machines;
  if (!Array.isArray(list)) throw new Error('no machines in the answer');
  const mine = list.find((m) => rec(m)?.id === machine) ?? (list.length === 1 ? list[0] : undefined);
  const limits = rec(rec(mine)?.limits);
  if (!limits) return null;
  return { claude: readProvider(limits.claude), codex: readProvider(limits.codex) };
};

// ---- what to draw

export const windowsOf = (limits: ProviderLimits | null): { kind: WindowKind; window: LimitWindow }[] => {
  if (!limits) return [];
  const all = [
    { kind: WINDOW.fiveHour, window: limits.fiveHour },
    { kind: WINDOW.sevenDay, window: limits.sevenDay },
  ];
  return all.flatMap(({ kind, window }) => (window ? [{ kind, window }] : []));
};

/** What is left of a window, whole per cent. */
export const leftOf = (window: LimitWindow): number =>
  Math.max(0, Math.min(100, Math.round(100 - window.usedPct)));

export const levelOf = (left: number): Level => {
  if (left <= OUT_AT) return LEVEL.out;
  if (left <= LOW_AT) return LEVEL.low;
  return LEVEL.ok;
};

/** What the forecast says will be left at the reset, whole per cent; null when it does not say. */
export const leftAtReset = (window: LimitWindow): number | null => {
  const used = window.forecast?.usedAtReset;
  return used === null || used === undefined ? null : Math.max(0, Math.min(100, Math.round(100 - used)));
};

const WINDOW_SHORT_KEY = {
  [WINDOW.fiveHour]: 'next.limits.fiveShort',
  [WINDOW.sevenDay]: 'next.limits.sevenShort',
} as const;
const WINDOW_NAME_KEY = {
  [WINDOW.fiveHour]: 'next.limits.fiveHour',
  [WINDOW.sevenDay]: 'next.limits.sevenDay',
} as const;
export const windowShort = (kind: WindowKind): string => t(WINDOW_SHORT_KEY[kind]);
export const windowName = (kind: WindowKind): string => t(WINDOW_NAME_KEY[kind]);

// ---- time and words

const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** «2 ч 5 мин», «2 ч», «3 д 4 ч»: a unit that is zero is not told. */
export const span = (seconds: number): string => {
  const total = Math.max(0, Math.round(seconds / MINUTE));
  const d = Math.floor(total / (DAY / MINUTE));
  const h = Math.floor((total % (DAY / MINUTE)) / (HOUR / MINUTE));
  const m = total % (HOUR / MINUTE);
  if (d > 0 && h > 0) return t('next.limits.dur.days', { d, h });
  if (d > 0) return t('next.limits.dur.daysOnly', { d });
  if (h > 0 && m > 0) return t('next.limits.dur.hours', { h, m });
  if (h > 0) return t('next.limits.dur.hoursOnly', { h });
  return t('next.limits.dur.minutes', { m });
};

/** «16:50», and with the weekday «сб 13:30» for the weekly window. */
export const clockOf = (epochSec: number, withDay: boolean): string => {
  const date = new Date(epochSec * 1000);
  const hm = new Intl.DateTimeFormat(locale(), { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
  if (!withDay) return hm.format(date);
  return `${new Intl.DateTimeFormat(locale(), { weekday: 'short' }).format(date)} ${hm.format(date)}`;
};

export const FORECAST_TONE = { neutral: 'neutral', warning: 'warning', danger: 'danger' } as const;
export type ForecastTone = (typeof FORECAST_TONE)[keyof typeof FORECAST_TONE];

export interface ForecastText {
  tone: ForecastTone;
  text: string;
  /** The range, the confidence and the basis; null when there is none. */
  detail: string | null;
}

/** The forecast is told to this precision: a guess to the minute is a lie. */
const ROUND: Record<WindowKind, number> = { five_hour: 5 * MINUTE, seven_day: HOUR };

const join = (parts: (string | null)[]): string | null => parts.filter(Boolean).join(' · ') || null;

const runsOutText = (kind: WindowKind, provider: string, at: string, reset: string | null): string => {
  if (kind === WINDOW.fiveHour) return t('next.limits.fc.runsOutFive', { provider, at });
  if (reset === null) return t('next.limits.fc.runsOutWeek', { provider, at });
  return t('next.limits.fc.runsOutWeekReset', { provider, at, reset });
};

/** The forecast line of one window: when it runs out, that it lasts, that it is out, or why there is nothing yet. */
export const describeForecast = (
  forecast: LimitForecast,
  kind: WindowKind,
  provider: string,
): ForecastText => {
  const weekly = kind === WINDOW.sevenDay;
  const at = (epochSec: number) => clockOf(Math.round(epochSec / ROUND[kind]) * ROUND[kind], weekly);
  const reset = forecast.resetsAt === null ? null : clockOf(forecast.resetsAt, weekly);
  const confidence = forecast.confidence
    ? t('next.limits.fc.confidence', { level: t(`next.limits.fc.${forecast.confidence}`) })
    : null;
  const basis = forecast.profile ? t('next.limits.fc.profile') : null;

  switch (forecast.status) {
    case FORECAST.exhausts: {
      const range = forecast.exhaustRange;
      const rangeText = range ? `${at(range[0])}–${range[1] === null ? '…' : at(range[1])}` : null;
      return {
        tone: FORECAST_TONE.warning,
        text: runsOutText(kind, provider, at(forecast.exhaustAt ?? 0), reset),
        detail: join([rangeText, confidence, basis]),
      };
    }
    case FORECAST.lasts: {
      const lasts = reset === null ? t('next.limits.fc.lasts') : t('next.limits.fc.lastsAt', { reset });
      const used =
        forecast.usedAtReset === null
          ? null
          : t('next.limits.fc.usedAtReset', { percent: Math.round(forecast.usedAtReset) });
      return {
        tone: FORECAST_TONE.neutral,
        text: join([lasts, used]) ?? lasts,
        detail: join([confidence, basis]),
      };
    }
    case FORECAST.exhausted:
      return {
        tone: FORECAST_TONE.danger,
        text: reset === null ? t('next.limits.fc.exhausted') : t('next.limits.fc.exhaustedAt', { reset }),
        detail: null,
      };
    case FORECAST.stale:
      return {
        tone: FORECAST_TONE.neutral,
        text:
          forecast.lastAt === null
            ? t('next.limits.fc.stale')
            : t('next.limits.fc.staleSince', { at: clockOf(forecast.lastAt, weekly) }),
        detail: null,
      };
    default:
      return {
        tone: FORECAST_TONE.neutral,
        text: t('next.limits.fc.insufficient', { reason: forecast.reason ?? t('next.limits.fc.fewSamples') }),
        detail: null,
      };
  }
};

/** What the composer's chip is given: the agent's limits on the machine, or that they could not be got. */
export type LimitsView =
  | { state: 'ready'; provider: Provider; limits: ProviderLimits | null }
  | { state: 'failed'; provider: Provider; onRetry?: () => Promise<void> };
