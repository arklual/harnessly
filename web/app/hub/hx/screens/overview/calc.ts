// The arithmetic of «Обзор»: every width, share, tone and angle on the screen comes out of these small pure functions,
// so a number is computed from data in one place and a test can hold it. The formulas are the board's own.

// ── Tiles: the bars of the last days and the strip ──────────────────────────────────────────────────────────────

export interface BarRect {
  x: number;
  y: number;
  w: number;
  h: number;
  /** The last bar (today) is drawn stronger. */
  recent: boolean;
}

/** Heights of the bars in the board's 100×40 box: the largest is 38 tall, an empty day still shows 3. */
const BAR_MAX = 38;
const BAR_MIN = 3;

/** The bars of a tile, oldest first. Each takes a slot of `100/n`, 72% of it is the bar. */
export const barRects = (values: readonly number[]): BarRect[] => {
  const n = values.length;
  if (n === 0) return [];
  const max = Math.max(0, ...values);
  const slot = 100 / n;
  return values.map((v, i) => {
    const h = max > 0 ? Math.min(BAR_MAX, Math.max(BAR_MIN, (Math.max(0, v) / max) * BAR_MAX)) : BAR_MIN;
    return { x: 1 + slot * i, y: 40 - h, w: slot * 0.72, h, recent: i === n - 1 };
  });
};

/** The two parts of a progress strip: what is used and what is left, as weights (tenths of a percent are kept). */
export const stripParts = (used: number, whole: number): { used: number; rest: number } => {
  const u = Math.max(0, Math.min(used, whole));
  return { used: Math.round(u * 10) / 10, rest: Math.round((whole - u) * 10) / 10 };
};

// ── Shares and widths ───────────────────────────────────────────────────────────────────────────────────────────

/** A share of a whole in whole percent: 118 of 312 → 38. */
export const pctOf = (part: number, whole: number): number =>
  whole > 0 ? Math.round((part / whole) * 100) : 0;

/** `width` of a bar as a CSS percent with one digit: `37.0%`. */
export const pctCss = (share: number): string => `${(Math.max(0, share) * 100).toFixed(1)}%`;

/** The indices of the values from the largest to the smallest; equal values keep their order. */
export const rankDesc = (values: readonly number[]): number[] =>
  values
    .map((v, i) => ({ v, i }))
    .sort((a, b) => b.v - a.v || a.i - b.i)
    .map((x) => x.i);

/** The bars of a list scaled to the largest: the largest is 100%. */
export const relativeWidths = (values: readonly number[]): string[] => {
  const max = Math.max(0, ...values);
  return values.map((v) => (max > 0 ? pctCss(v / max) : pctCss(0)));
};

// ── The donut ───────────────────────────────────────────────────────────────────────────────────────────────────

/** The half ring of the board: centre, radii, the corner radius of a slice and half the gap between two slices. */
export const RING = { cx: 100, cy: 100, rOut: 83.5, rIn: 72.5, corner: 4, halfGapDeg: 1 } as const;

const rad = (deg: number) => (deg * Math.PI) / 180;
const deg = (r: number) => (r * 180) / Math.PI;
const f = (n: number) => n.toFixed(2);

/** A point at the angle (180° is the left end of the half ring, 0° the right one) and the radius. */
const at = (angle: number, r: number): string =>
  `${f(RING.cx + r * Math.cos(rad(angle)))} ${f(RING.cy - r * Math.sin(rad(angle)))}`;

export interface Slice {
  /** The share of the whole, 0..1. */
  share: number;
  /** Whole percent. */
  pct: number;
  /** Start and end angle in degrees, the start is the larger. */
  from: number;
  to: number;
  /** The path of the rounded slice; empty for a value of 0. */
  d: string;
}

/** The plain sector, for a slice too thin for round corners. */
const sector = (from: number, to: number): string => {
  const { rOut, rIn } = RING;
  return `M ${at(from, rIn)} L ${at(from, rOut)} A ${rOut} ${rOut} 0 0 1 ${at(to, rOut)} L ${at(to, rIn)} A ${rIn} ${rIn} 0 0 0 ${at(from, rIn)} Z`;
};

/** The path of a slice between two angles, its four corners rounded by `corner`, as the board draws it. */
export const slicePath = (from: number, to: number): string => {
  const { rOut, rIn, corner: c } = RING;
  const aOut = deg(Math.asin(c / rOut));
  const aIn = deg(Math.asin(c / rIn));
  if (from - to < 2 * aIn + 1) return sector(from, to);
  return [
    `M ${at(from, rOut - c)}`,
    `A ${c} ${c} 0 0 1 ${at(from - aOut, rOut)}`,
    `A ${rOut} ${rOut} 0 0 1 ${at(to + aOut, rOut)}`,
    `A ${c} ${c} 0 0 1 ${at(to, rOut - c)}`,
    `L ${at(to, rIn + c)}`,
    `A ${c} ${c} 0 0 1 ${at(to + aIn, rIn)}`,
    `A ${rIn} ${rIn} 0 0 0 ${at(from - aIn, rIn)}`,
    `A ${c} ${c} 0 0 1 ${at(from, rIn + c)}`,
    'Z',
  ].join(' ');
};

/**
 * The slices of the half ring for the values: a slice takes its share of the 180°, and between two neighbours stays a
 * gap of 2° (1° off each). The ends of the ring are not trimmed. A value of 0 has no slice.
 */
export const donutSlices = (values: readonly number[]): Slice[] => {
  const total = values.reduce((a, v) => a + Math.max(0, v), 0);
  let before = 0;
  const last = values.length - 1;
  return values.map((raw, i) => {
    const v = Math.max(0, raw);
    const share = total > 0 ? v / total : 0;
    const from = 180 - before * 180 - (i > 0 ? RING.halfGapDeg : 0);
    before += share;
    const to = 180 - before * 180 + (i < last ? RING.halfGapDeg : 0);
    return { share, pct: Math.round(share * 100), from, to, d: v > 0 ? slicePath(from, to) : '' };
  });
};

// ── «Расход по людям» of a lead ─────────────────────────────────────────────────────────────────────────────────

export type Tone = 'ok' | 'warn' | 'risk';

/** The tone of a spend against a limit: 90% and over is a risk, 75% a warning. */
export const toneOfShare = (share: number): Tone => (share >= 0.9 ? 'risk' : share >= 0.75 ? 'warn' : 'ok');

/** A person from 80% of their own limit is «hot»; a subscription window from 80% is a warning. */
export const HOT_FROM = 0.8;

/** A subscription's window used by `pct` percent is close to the end. */
export const windowIsHot = (pct: number): boolean => pct >= HOT_FROM * 100;

export interface PersonIn {
  userId: string;
  spent: number;
  limit: number | null;
}

export interface PersonBar {
  userId: string;
  /** The spend against the personal limit, 0..∞; 0 without a limit. */
  share: number;
  hot: boolean;
  idle: boolean;
  /** Where the bar ends and where the limit cap stands, as CSS percents of the longest scale. */
  w: string;
  cap: string | null;
}

export interface TeamBar {
  share: number;
  pct: number;
  w: string;
  tone: Tone;
}

/** The team's spend against its limit; null without a limit. */
export const teamBar = (spent: number, limit: number | null): TeamBar | null => {
  if (!limit || limit <= 0) return null;
  const share = spent / limit;
  return {
    share,
    pct: Math.round(share * 100),
    w: `${Math.min(100, share * 100).toFixed(1)}%`,
    tone: toneOfShare(share),
  };
};

/**
 * The rows of the lead's list: the most used limit first (a person without a limit goes last, the biggest spender
 * first), each bar scaled to the longest of all spends and limits. `idleBelow` is the spend under which a person has
 * hardly worked (the row is drawn quieter).
 */
export const personBars = (people: readonly PersonIn[], idleBelow: number): PersonBar[] => {
  const scale = Math.max(1, ...people.flatMap((p) => [p.spent, p.limit ?? 0]));
  const rows = people.map((p) => {
    const share = p.limit && p.limit > 0 ? p.spent / p.limit : 0;
    return {
      userId: p.userId,
      share,
      hot: p.limit !== null && share >= HOT_FROM,
      idle: p.spent < idleBelow,
      w: `${Math.min(100, (p.spent / scale) * 100).toFixed(1)}%`,
      cap: p.limit ? `${((p.limit / scale) * 100).toFixed(1)}%` : null,
      spent: p.spent,
      limited: p.limit !== null && p.limit > 0,
    };
  });
  const order = rows
    .map((r, i) => ({ r, i }))
    .sort((a, b) =>
      a.r.limited !== b.r.limited
        ? a.r.limited
          ? -1
          : 1
        : a.r.limited
          ? b.r.share - a.r.share || a.i - b.i
          : b.r.spent - a.r.spent || a.i - b.i,
    );
  return order.map(({ r }) => ({
    userId: r.userId,
    share: r.share,
    hot: r.hot,
    idle: r.idle,
    w: r.w,
    cap: r.cap,
  }));
};

// ── «Мой лимит» of a participant ────────────────────────────────────────────────────────────────────────────────

export interface LimitView {
  /** What is used and where the month ends at this pace, as CSS percents of the limit. */
  usedW: string;
  forecastW: string;
  /** Where the warning comes, as a CSS percent. */
  warnX: string;
  left: number;
  over: boolean;
  /** The forecast crosses the limit: the agents stop before the reset. */
  willRunOut: boolean;
}

export const limitView = (l: {
  spent: number;
  limit: number;
  forecast: number;
  warnPct: number;
}): LimitView => {
  const whole = l.limit > 0 ? l.limit : 1;
  const w = (x: number) => `${Math.min(100, Math.max(0, (x / whole) * 100)).toFixed(1)}%`;
  return {
    usedW: w(l.spent),
    forecastW: w(Math.max(l.forecast, l.spent)),
    warnX: `${Math.min(100, Math.max(0, l.warnPct))}%`,
    left: Math.max(0, l.limit - l.spent),
    over: l.spent > l.limit,
    willRunOut: l.forecast > l.limit,
  };
};

export interface SplitRow {
  /** Share of the whole in whole percent and as a CSS width of the stacked bar. */
  pct: number;
  w: string;
}

/** «На что ушло»: the parts of the spend as widths of the stacked bar and whole percents. */
export const splitRows = (amounts: readonly number[]): SplitRow[] => {
  const total = amounts.reduce((a, x) => a + Math.max(0, x), 0);
  return amounts.map((x) => ({
    pct: pctOf(Math.max(0, x), total),
    w: total > 0 ? `${((Math.max(0, x) / total) * 100).toFixed(1)}%` : '0.0%',
  }));
};
