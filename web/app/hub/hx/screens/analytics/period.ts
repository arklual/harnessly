// The period and the comparison, chosen as ranges of the weeks on the picker's strip. Pure: the picker's clicks are
// `pickWeek` / `pickPreset`, the URL is `encodePeriod` / `decodePeriod`, nothing here touches the DOM.
import {
  type AnalyticsQuery,
  type ChangeRef,
  type FrameWeek,
  monthPeriod,
  type Week,
} from '../../../org/api/analytics';
import {
  addDays,
  dayOfMonth,
  daysBetween,
  isoDay,
  monthShort,
  parseDay,
  rangeLabel,
} from '../../../org/dates';
import { ru } from './calc';

/** First and last week index, both included. */
export type Range = readonly [number, number];

export interface PickerState {
  a: Range;
  b: Range;
  /** Which of the two the next click on a week changes. */
  edit: 'a' | 'b';
  /** The first click of a range waits here for the second. */
  anchor: number | null;
  /** The comparison follows the period (the same length right before) until it is chosen by hand. */
  auto: boolean;
  /** Days that are not whole weeks («Сегодня», «7 дней», chosen dates): they replace the weeks of `a` and `b`. */
  free: FreePeriod | null;
}

/** A period of days and its comparison; both ends of each are included. */
export interface FreePeriod {
  a: Week;
  b: Week;
}

export const sameRange = (x: Range, y: Range): boolean => x[0] === y[0] && x[1] === y[1];

/** The same number of weeks right before; it stops at the first week of the strip. */
export const prevOf = (r: Range): Range => {
  const n = r[1] - r[0] + 1;
  return [Math.max(0, r[0] - n), Math.max(0, r[0] - 1)];
};

export const initialState = (a: Range, b: Range | null = null): PickerState => ({
  a,
  b: b ?? prevOf(a),
  edit: 'a',
  anchor: null,
  auto: b === null,
  free: null,
});

/** A click on week `i`: the first click of a range, then the last one (the same week twice is one week). */
export const pickWeek = (s: PickerState, i: number): PickerState => {
  const key = s.edit;
  if (s.anchor === null) {
    const next: PickerState = { ...s, [key]: [i, i] as Range, anchor: i, free: null };
    return key === 'a' && s.auto ? { ...next, b: prevOf([i, i]) } : next;
  }
  const r: Range = [Math.min(s.anchor, i), Math.max(s.anchor, i)];
  const next: PickerState = { ...s, [key]: r, anchor: null, free: null };
  if (key === 'a' && s.auto) return { ...next, b: prevOf(r) };
  return key === 'b' ? { ...next, auto: false } : next;
};

/** Which part of the pair the clicks change; a half-made range is dropped. */
export const setEdit = (s: PickerState, edit: 'a' | 'b'): PickerState => ({ ...s, edit, anchor: null });

export const pickPreset = (s: PickerState, p: Preset): PickerState => ({
  ...s,
  a: p.a,
  b: p.b,
  anchor: null,
  auto: false,
  free: null,
});

export const hintOf = (s: PickerState): string => {
  if (s.anchor !== null) return 'Нажми на последнюю неделю отрезка — или на ту же, если нужна одна неделя';
  return s.edit === 'a'
    ? 'Нажми на неделю. Чтобы взять несколько недель подряд — на первую и последнюю'
    : 'Нажми на неделю для сравнения. По умолчанию — столько же недель сразу перед периодом';
};

/** The weeks of the current month on the strip: what «Этот месяц» is, as on «Обзор». */
export const monthRange = (weeks: readonly FrameWeek[]): Range | null => {
  if (weeks.length === 0) return null;
  const m = monthPeriod(weeks);
  const i = weeks.findIndex((w) => w.from === m.from);
  return i < 0 ? null : [i, weeks.length - 1];
};

/** A range as a person says it: a name where there is one, and the dates always. */
export interface RangeName {
  title: string;
  dates: string;
}

const WEEK_WORD = ['неделя', 'недели', 'недель'] as const;

/** «Этот месяц», «Эта неделя», «Прошлая неделя»; any other range is named by its length: «4 недели». */
export const nameOf = (weeks: readonly FrameWeek[], r: Range, opts: { month?: boolean } = {}): RangeName => {
  const dates = labelOf(weeks, r);
  const last = weeks.length - 1;
  const month = opts.month ? monthRange(weeks) : null;
  const n = r[1] - r[0] + 1;
  if (month && sameRange(r, month)) return { title: 'Этот месяц', dates };
  if (n === 1 && r[0] === last && weeks[last]?.current) return { title: 'Эта неделя', dates };
  if (n === 1 && r[0] === lastFullWeek(weeks)) return { title: 'Прошлая неделя', dates };
  if (n === 1) return { title: '1 неделя', dates };
  const word =
    n % 10 === 1 && n % 100 !== 11
      ? WEEK_WORD[0]
      : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 12 || n % 100 > 14)
        ? WEEK_WORD[1]
        : WEEK_WORD[2];
  return { title: `${n} ${word}`, dates };
};

/** What the comparison is called: the same length right before the period, or what was chosen by hand. */
export const compareNameOf = (weeks: readonly FrameWeek[], s: PickerState): RangeName =>
  s.free
    ? s.auto
      ? { title: 'Предыдущий период', dates: freeName(s.free.b).dates }
      : freeName(s.free.b)
    : s.auto
      ? { title: 'Предыдущий период', dates: labelOf(weeks, s.b) }
      : nameOf(weeks, s.b);

const weekAt = (weeks: readonly FrameWeek[], i: number): FrameWeek => {
  const w = weeks[i];
  if (!w) throw new Error(`week ${i} is not on the strip`);
  return w;
};

/** «19–25 окт», «28 сен – 4 окт». */
export const labelOf = (weeks: readonly FrameWeek[], r: Range): string =>
  rangeLabel(weekAt(weeks, r[0]).from, weekAt(weeks, r[1]).to);

export const spanOf = (weeks: readonly FrameWeek[], r: Range): Week => ({
  from: weekAt(weeks, r[0]).from,
  to: weekAt(weeks, r[1]).to,
});

export const queryOf = (weeks: readonly FrameWeek[], s: PickerState): AnalyticsQuery =>
  s.free
    ? { period: s.free.a, compare: s.free.b }
    : {
        period: spanOf(weeks, s.a),
        compare: spanOf(weeks, s.b),
      };

/** The quick choices of a period in days; the numbers come by day, so «24 ч» is the one day that is going. */
export const QUICK = [
  { id: '1', days: 1, label: 'Сегодня' },
  { id: '7', days: 7, label: '7 дн' },
  { id: '30', days: 30, label: '30 дн' },
] as const;

const DAY_WORD = ['день', 'дня', 'дней'] as const;

const dayWord = (n: number): string =>
  n % 10 === 1 && n % 100 !== 11
    ? DAY_WORD[0]
    : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 12 || n % 100 > 14)
      ? DAY_WORD[1]
      : DAY_WORD[2];

/** The same number of days right before `a`. */
export const prevDays = (a: Week): Week => {
  const n = daysBetween(a.from, a.to) + 1;
  return { from: addDays(a.from, -n), to: addDays(a.from, -1) };
};

/** The last `days` days up to `today` (included) against the same number right before. */
export const lastDays = (days: number, today: string = isoDay(new Date())): FreePeriod => {
  const a = { from: addDays(today, 1 - days), to: today };
  return { a, b: prevDays(a) };
};

export const pickFree = (s: PickerState, free: FreePeriod, auto = false): PickerState => ({
  ...s,
  free,
  auto,
  anchor: null,
});

/** The name of a period of days: «24 часа», «7 дней», «12 дней». */
export const freeName = (w: Week): RangeName => {
  const n = daysBetween(w.from, w.to) + 1;
  return { title: n === 1 ? 'Сегодня' : `${n} ${dayWord(n)}`, dates: rangeLabel(w.from, w.to) };
};

export interface Preset {
  id: string;
  label: string;
  a: Range;
  b: Range;
}

/** The last week that is over: the one still going is not a choice for a preset. */
export const lastFullWeek = (weeks: readonly FrameWeek[]): number => {
  const n = weeks.length;
  return weeks[n - 1]?.current ? n - 2 : n - 1;
};

export const presetsOf = (weeks: readonly FrameWeek[], change: ChangeRef | null): Preset[] => {
  const last = lastFullWeek(weeks);
  const out: Preset[] = [];
  if (last >= 1) {
    out.push({
      id: 'last',
      label: 'Прошлая неделя против позапрошлой',
      a: [last, last],
      b: [last - 1, last - 1],
    });
  }
  if (last >= 7) {
    out.push({
      id: 'four',
      label: '4 недели против 4 до них',
      a: [last - 3, last],
      b: [last - 7, last - 4],
    });
  }
  if (change) {
    const after = weeks.findIndex((w) => parseDay(w.from).getTime() > parseDay(change.at).getTime());
    if (after >= 1 && after <= last) {
      out.push({
        id: 'change',
        label: `Неделя после «${change.title}» против недели до`,
        a: [after, after],
        b: [after - 1, after - 1],
      });
    }
  }
  return out;
};

export interface WeekCell {
  index: number;
  inA: boolean;
  inB: boolean;
  now: boolean;
  anchor: boolean;
  /** 0..1 of the tallest bar. */
  h: number;
  day: string;
  /** The month, on the first week and wherever it changes. */
  month: string;
  title: string;
}

/** The smallest bar: a week without a number still has something to press. */
const MIN_BAR = 0.04;

export const weekCells = (weeks: readonly FrameWeek[], s: PickerState): WeekCell[] => {
  const top = Math.max(0, ...weeks.map((w) => w.spendUsd ?? 0));
  return weeks.map((w, i) => {
    const spend = w.spendUsd ?? null;
    const prev = weeks[i - 1];
    const range = rangeLabel(w.from, w.to);
    const money = spend === null ? '' : ` · $${ru(spend, 0)}`;
    return {
      index: i,
      inA: !s.free && i >= s.a[0] && i <= s.a[1],
      inB: !s.free && i >= s.b[0] && i <= s.b[1],
      now: w.current === true,
      anchor: s.anchor === i,
      h: spend === null || top === 0 ? MIN_BAR : Math.max(MIN_BAR, spend / top),
      day: `${dayOfMonth(w.from)}–${dayOfMonth(w.to)}`,
      month: !prev || monthShort(prev.from) !== monthShort(w.from) ? monthShort(w.from) : '',
      title: `${range}${money}${w.current ? ' · неделя ещё идёт' : ''}`,
    };
  });
};

const rangeParam = (w: Week): string => `${w.from}_${w.to}`;

export interface PeriodParams {
  p: string | null;
  /** Null: the comparison is the same length right before. */
  c: string | null;
}

/** The URL's `p` and `c`: the dates of the period, and of the comparison when it was chosen by hand. */
export const encodePeriod = (weeks: readonly FrameWeek[], s: PickerState): PeriodParams =>
  s.free ? { p: rangeParam(s.free.a), c: s.auto ? null : rangeParam(s.free.b) } : encodeWeeks(weeks, s);

const encodeWeeks = (weeks: readonly FrameWeek[], s: PickerState): PeriodParams => ({
  p: rangeParam(spanOf(weeks, s.a)),
  c: s.auto ? null : rangeParam(spanOf(weeks, s.b)),
});

const dayRangeOfParam = (v: string | null): Week | null => {
  const m = v ? /^(\d{4}-\d{2}-\d{2})_(\d{4}-\d{2}-\d{2})$/.exec(v) : null;
  if (!m?.[1] || !m[2]) return null;
  const from = m[1];
  const to = m[2];
  const ok = !Number.isNaN(parseDay(from).getTime()) && !Number.isNaN(parseDay(to).getTime());
  return ok && from <= to && daysBetween(from, to) < 400 ? { from, to } : null;
};

const rangeOfParam = (weeks: readonly FrameWeek[], v: string | null): Range | null => {
  const m = v ? /^(\d{4}-\d{2}-\d{2})_(\d{4}-\d{2}-\d{2})$/.exec(v) : null;
  if (!m) return null;
  const i = weeks.findIndex((w) => w.from === m[1]);
  const j = weeks.findIndex((w) => w.to === m[2]);
  return i >= 0 && j >= i ? [i, j] : null;
};

/** The picker's state from the URL; what is not a range of the strip's weeks falls back to the default period. */
export const decodePeriod = (
  weeks: readonly FrameWeek[],
  params: PeriodParams,
  fallback: Week,
): PickerState => {
  const def = rangeOfParam(weeks, rangeParam(fallback)) ?? [
    Math.max(0, lastFullWeek(weeks)),
    Math.max(0, lastFullWeek(weeks)),
  ];
  const a = rangeOfParam(weeks, params.p);
  const b = rangeOfParam(weeks, params.c);
  // Dates that are not whole weeks of the strip are a period of days; a day range reads from the address as it is.
  const fa = a ? null : dayRangeOfParam(params.p);
  if (fa) {
    const fb = dayRangeOfParam(params.c);
    return { ...initialState(def), free: { a: fa, b: fb ?? prevDays(fa) }, auto: fb === null };
  }
  return initialState(a ?? def, b);
};

/** The name of the chosen period, whole weeks or days. */
export const periodNameOf = (weeks: readonly FrameWeek[], s: PickerState): RangeName =>
  s.free ? freeName(s.free.a) : nameOf(weeks, s.a, { month: true });

/** Which quick choice is on (its id), or null for weeks and chosen dates. */
export const quickOn = (s: PickerState, today: string = isoDay(new Date())): string | null => {
  if (!s.free) return null;
  const q = QUICK.find((x) => {
    const f = lastDays(x.days, today);
    return f.a.from === s.free?.a.from && f.a.to === s.free.a.to;
  });
  return q?.id ?? null;
};

/** A day typed into a date field moves one end of the period or of its comparison; a reversed pair swaps. */
export const setFreeDay = (s: PickerState, key: 'a' | 'b', end: 'from' | 'to', day: string): PickerState => {
  const cur = s.free ?? lastDays(7);
  const w = { ...cur[key], [end]: day };
  const fixed = w.from <= w.to ? w : { from: w.to, to: w.from };
  const next = { ...cur, [key]: fixed };
  return {
    ...s,
    anchor: null,
    auto: key === 'a' ? true : false,
    free: key === 'a' ? { a: fixed, b: prevDays(fixed) } : next,
  };
};
