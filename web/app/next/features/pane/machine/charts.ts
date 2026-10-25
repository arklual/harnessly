// The design's chart helpers (lib/hx/charts.js) for the Machine tool: pure functions that take numbers and return what
// the markup draws, so a chart is the same drawing in the design and here. The numbers reach the CSS as custom
// properties (`vars.ts`), the paths as `d`. Where the design spaces points evenly, these place them by time: a machine
// that has run for ten minutes draws ten minutes, not a stretched hour.
import { locale, t } from '@/next/i18n';

/** Custom properties of one element: `--v`, `--c`, `--x`… (the name without the dashes). */
export type TVars = Readonly<Record<string, string | number>>;

/** The viewBox of a plot (.hx-ch-plot svg) and of a mini line (.hx-ch-sline svg). */
export const PLOT = { w: 600, h: 160 } as const;
export const SLINE = { w: 120, h: 32 } as const;

const MINUS = '−';
const clamp = (x: number, a: number, b: number) => Math.min(b, Math.max(a, x));
const r1 = (x: number) => Math.round(x * 10) / 10;
const r3 = (x: number) => Math.round(x * 1000) / 1000;
const pct = (x: number) => `${r1(x)}%`;

/** A series' colour: a slot 1…6 of the palette (.hx-ch-s1…s6), wrapping. */
export const slot = (n: number): string => `var(--hx-ch-s${((n - 1) % 6) + 1})`;

export const TONE = { ok: 'ok', warn: 'warn', bad: 'bad' } as const;
export type TTone = (typeof TONE)[keyof typeof TONE];
const TONE_COLOR: Record<TTone, string> = {
  ok: 'var(--hx-success)',
  warn: 'var(--hx-warning)',
  bad: 'var(--hx-danger)',
};

/** ok / warn / bad by thresholds, as .hx-meter and .hx-lvl are coloured. */
export const tone = (value: number, warn: number, bad: number): TTone =>
  value >= bad ? TONE.bad : value >= warn ? TONE.warn : TONE.ok;

// ---- numbers in the page's language

const formats = new Map<string, Intl.NumberFormat>();
export const num = (value: number, digits = 0): string => {
  const key = `${locale()}:${digits}`;
  let f = formats.get(key);
  if (!f) {
    f = new Intl.NumberFormat(locale(), { maximumFractionDigits: digits, minimumFractionDigits: 0 });
    formats.set(key, f);
  }
  return f.format(value).replace('-', MINUS);
};

const UNITS = ['b', 'kb', 'mb', 'gb', 'tb'] as const;

/** Bytes as a number and its unit (1 KB = 1024 B): «1,4» «МБ». Below a megabyte whole numbers, a few gigabytes one digit. */
export const bytesParts = (n: number, digits?: number): { value: string; unit: string } => {
  let i = 0;
  let v = Math.abs(n);
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024;
    i += 1;
  }
  const d = digits ?? (v < 10 && i > 1 ? 1 : 0);
  return { value: (n < 0 ? MINUS : '') + num(v, i < 2 ? 0 : d), unit: t(`next.machine.unit.${UNITS[i]}`) };
};

export const bytes = (n: number, digits?: number): string => {
  const { value, unit } = bytesParts(n, digits);
  return `${value} ${unit}`;
};

export const rateParts = (n: number): { value: string; unit: string } => {
  const { value, unit } = bytesParts(n);
  return { value, unit: t('next.machine.perSec', { unit }) };
};

export const rate = (n: number): string => {
  const { value, unit } = rateParts(n);
  return `${value} ${unit}`;
};

export const percent = (value: number): string => `${num(value)}%`;

// ---- the chart pieces

export interface IDelta {
  text: string;
  /** What the change means, not which way it went: good, bad or flat. */
  tone: 'good' | 'bad' | 'flat';
  dir: 'up' | 'down' | 'flat';
}

/** The change from `prev` to `cur` in whole points; `unit` follows the figure. `good: 'down'`: a rise is bad (the processor). */
export const delta = (cur: number, prev: number, unit: string, good: 'up' | 'down'): IDelta => {
  const d = Math.round(cur) - Math.round(prev);
  if (d === 0) return { text: `0${unit}`, tone: 'flat', dir: 'flat' };
  const up = d > 0;
  return {
    text: `${up ? '+' : MINUS}${num(Math.abs(d))}${unit}`,
    tone: up === (good === 'up') ? 'good' : 'bad',
    dir: up ? 'up' : 'down',
  };
};

/** A line through points; `smooth`: a monotone cubic spline (no overshoot past the data), else a polyline. */
export const curve = (pts: readonly (readonly [number, number])[], smooth: boolean, cmd = 'M'): string => {
  const n = pts.length;
  const f = (p: readonly [number, number]) => `${r1(p[0])} ${r1(p[1])}`;
  if (n === 0) return '';
  let d = cmd + f(pts[0]);
  if (n === 1) return d;
  if (!smooth || n === 2) {
    for (let i = 1; i < n; i += 1) d += `L${f(pts[i])}`;
    return d;
  }
  const dx: number[] = [];
  const m: number[] = [];
  for (let i = 0; i < n - 1; i += 1) {
    dx[i] = pts[i + 1][0] - pts[i][0] || 1e-6;
    m[i] = (pts[i + 1][1] - pts[i][1]) / dx[i];
  }
  const tg: number[] = new Array<number>(n);
  tg[0] = m[0];
  tg[n - 1] = m[n - 2];
  for (let i = 1; i < n - 1; i += 1) tg[i] = m[i - 1] * m[i] <= 0 ? 0 : (m[i - 1] + m[i]) / 2;
  for (let i = 0; i < n - 1; i += 1) {
    if (m[i] === 0) {
      tg[i] = 0;
      tg[i + 1] = 0;
      continue;
    }
    const a = tg[i] / m[i];
    const b = tg[i + 1] / m[i];
    const s = a * a + b * b;
    if (s > 9) {
      const k = 3 / Math.sqrt(s);
      tg[i] = k * a * m[i];
      tg[i + 1] = k * b * m[i];
    }
  }
  for (let i = 0; i < n - 1; i += 1) {
    const h = dx[i] / 3;
    d += `C${r1(pts[i][0] + h)} ${r1(pts[i][1] + tg[i] * h)} ${r1(pts[i + 1][0] - h)} ${r1(pts[i + 1][1] - tg[i + 1] * h)} ${f(pts[i + 1])}`;
  }
  return d;
};

const niceStep = (raw: number): number => {
  if (!(raw > 0)) return 1;
  const p = 10 ** Math.floor(Math.log10(raw));
  const f = raw / p;
  const k = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10;
  return k * p;
};

/** The axis: min and max, and `ticks` values from the top down. Without a given max it is the next round step. */
export const axis = (
  dmin: number,
  dmax: number,
  ticks: number,
  fixed: { min?: number; max?: number } = {},
): { min: number; max: number; vals: number[] } => {
  const min = fixed.min ?? Math.min(0, dmin);
  let max = fixed.max;
  if (max === undefined) max = min + niceStep((dmax - min) / (ticks - 1)) * (ticks - 1);
  if (!(max > min)) max = min + 1;
  const vals: number[] = [];
  for (let i = 0; i < ticks; i += 1) vals.push(max - ((max - min) * i) / (ticks - 1));
  return { min, max, vals };
};

export interface IPlotSeries {
  name: string;
  values: readonly number[];
  slot: number;
  format: (value: number) => string;
}

export interface IPlotOptions {
  series: readonly IPlotSeries[];
  /** When each point was taken, ms; the plot runs from `from` to `to`, and a point stands where its time is. */
  times: readonly number[];
  from: number;
  to: number;
  titles: readonly string[];
  xs: readonly string[];
  min?: number;
  max?: number;
  yTicks: number;
  yFormat: (value: number) => string;
  refs?: readonly { value: number; label: string }[];
}

export interface IPlot {
  /** The grid rows of the frame (`--rows`). */
  rows: number;
  layers: { lineD: string; areaD: string; c: string; label: string }[];
  cols: {
    key: number;
    vars: TVars;
    side: 'l' | 'r';
    title: string;
    rows: { label: string; value: string; c: string }[];
    dots: { c: string; y: string }[];
  }[];
  ys: string[];
  xs: readonly string[];
  refs: { y: string; label: string }[];
}

/** Lines over soft areas for series that share their points (.hx-ch-plot): paths, hit columns with their tooltips, axes. */
export const plot = (o: IPlotOptions): IPlot => {
  const n = o.times.length;
  const out: IPlot = { rows: o.yTicks - 1, layers: [], cols: [], ys: [], xs: o.xs, refs: [] };
  const flat = o.series.flatMap((s) => s.values);
  const ax = axis(flat.length ? Math.min(...flat) : 0, flat.length ? Math.max(...flat) : 1, o.yTicks, {
    min: o.min,
    max: o.max,
  });
  const span = Math.max(1, o.to - o.from);
  const at = (time: number) => clamp((time - o.from) / span, 0, 1);
  const X = (i: number) => at(o.times[i]) * PLOT.w;
  const Y = (v: number) => (1 - clamp((v - ax.min) / (ax.max - ax.min), 0, 1)) * PLOT.h;
  const step = n > 1 ? Math.min(...o.times.slice(1).map((time, i) => time - o.times[i])) : span;

  o.series.forEach((s) => {
    const top = s.values.map((v, i) => [X(i), Y(v)] as const);
    const lineD = curve(top, true);
    const areaD = n ? `${lineD}L${r1(X(n - 1))} ${PLOT.h}L${r1(X(0))} ${PLOT.h}Z` : '';
    out.layers.push({ lineD, areaD, c: slot(s.slot), label: s.name });
  });
  for (let i = 0; i < n; i += 1) {
    const x = at(o.times[i]) * 100;
    out.cols.push({
      key: o.times[i],
      vars: { x: pct(x), w: pct((step / span) * 100) },
      side: x < 50 ? 'r' : 'l',
      title: o.titles[i] ?? '',
      rows: o.series.map((s) => ({ label: s.name, value: s.format(s.values[i]), c: slot(s.slot) })),
      dots: o.series.map((s) => ({ c: slot(s.slot), y: pct((Y(s.values[i]) / PLOT.h) * 100) })),
    });
  }
  out.ys = ax.vals.map((v) => o.yFormat(v));
  out.refs = (o.refs ?? [])
    .filter((r) => r.value >= ax.min && r.value <= ax.max)
    .map((r) => ({ y: pct((Y(r.value) / PLOT.h) * 100), label: r.label }));
  return out;
};

/** The mini line of a tile (.hx-ch-sline): the path, the area under it and where its end dot stands. */
export const sline = (
  values: readonly number[],
  min?: number,
  max?: number,
): { lineD: string; areaD: string; end: TVars } => {
  const n = values.length;
  if (!n) return { lineD: '', areaD: '', end: {} };
  const ax = axis(Math.min(...values), Math.max(...values), 2, { min, max });
  const pad = 2;
  const pts = values.map(
    (v, i) =>
      [
        n > 1 ? (i / (n - 1)) * SLINE.w : SLINE.w / 2,
        pad + (1 - clamp((v - ax.min) / (ax.max - ax.min), 0, 1)) * (SLINE.h - pad * 2),
      ] as const,
  );
  const lineD = curve(pts, true);
  const last = pts[n - 1];
  return {
    lineD,
    areaD: `${lineD}L${r1(last[0])} ${SLINE.h}L${r1(pts[0][0])} ${SLINE.h}Z`,
    end: { x: pct((last[0] / SLINE.w) * 100), y: pct((last[1] / SLINE.h) * 100) },
  };
};

export interface IHeatCell {
  vars: TVars;
  tip: string;
}

/** Cells whose saturation is the value (.hx-ch-heat): one per core. */
export const heat = (
  values: readonly number[],
  max: number,
  label: (i: number) => string,
  format: (v: number) => string,
): IHeatCell[] =>
  values.map((v, i) => ({ vars: { v: r3(clamp(v / max, 0, 1)) }, tip: `${label(i)} · ${format(v)}` }));

export interface IStackItem<I extends string = string> {
  id: I;
  label: string;
  value: number;
  /** A palette slot; none for the free part. */
  slot?: number;
  free?: boolean;
}

/** A segmented bar and its legend (.hx-ch-stack): each part's weight, colour and size in words. */
export const stack = <I extends string>(
  items: readonly IStackItem<I>[],
  total: number,
  format: (v: number) => string,
): { parts: { id: I; vars: TVars; label: string; text: string; free: boolean }[] } => ({
  parts: items.map((x, i) => ({
    id: x.id,
    vars: {
      w: r3((x.value / (total || 1)) * 100),
      c: x.free ? 'var(--hx-fg-3)' : slot(x.slot ?? 1),
      i,
    },
    label: x.label,
    text: format(x.value),
    free: Boolean(x.free),
  })),
});

export interface IListItem {
  key: string;
  label: string;
  sub: string;
  value: number;
  text: string;
  tone: '' | 'bad';
}

/** Rows with a bar behind them (.hx-ch-list): the bar is the value's share of `max`. */
export const list = (items: readonly IListItem[], max: number) =>
  items.map((x, i) => ({ ...x, vars: { v: r3(clamp(x.value / max, 0, 1)), i } satisfies TVars }));

export interface IKpiInput {
  label: string;
  value: string;
  unit: string;
  index: number;
  sub: string;
  delta?: IDelta;
  spark?: readonly number[];
  sparkMin?: number;
  sparkMax?: number;
  /** A colour by level, or a palette slot. */
  tone?: TTone;
  slot?: number;
}

export interface IKpi {
  label: string;
  value: string;
  unit: string;
  sub: string;
  delta: IDelta | null;
  vars: TVars;
  spark: { lineD: string; areaD: string; end: TVars } | null;
}

/** A tile: the figure, its change, a few words and a mini line. */
export const kpi = (o: IKpiInput): IKpi => ({
  label: o.label,
  value: o.value,
  unit: o.unit,
  sub: o.sub,
  delta: o.delta ?? null,
  vars: { c: o.tone ? TONE_COLOR[o.tone] : slot(o.slot ?? 1), i: o.index },
  spark: o.spark && o.spark.length > 1 ? sline(o.spark, o.sparkMin, o.sparkMax) : null,
});
