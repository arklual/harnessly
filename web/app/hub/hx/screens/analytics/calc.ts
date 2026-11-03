// The numbers of Аналитика as small pure functions: how a KPI is written, how it moved, what the sparkline and the
// before/after bars look like, how waste and peers are told. Nothing here knows React or the API client.
import type { Effect, Kpi, KpiFormat, PeerRow, Verdict, Waste } from '../../../org/api/analytics';

const nf = (min: number, max: number): Intl.NumberFormat =>
  new Intl.NumberFormat('ru-RU', { minimumFractionDigits: min, maximumFractionDigits: max });
const cache = new Map<string, Intl.NumberFormat>();
/** A number the Russian way: decimal comma, a non-breaking space between thousands. */
export const ru = (v: number, min = 0, max = min): string => {
  const key = `${min}:${max}`;
  let f = cache.get(key);
  if (!f) {
    f = nf(min, max);
    cache.set(key, f);
  }
  return f.format(v);
};

export const DASH = '—';
const ok = (v: number | null | undefined): v is number => typeof v === 'number' && Number.isFinite(v);

/** «2 ч 10 мин», «50 мин», «3 ч». */
export const minutesText = (m: number): string => {
  const total = Math.round(m);
  if (total < 60) return `${total} мин`;
  const h = Math.floor(total / 60);
  const rest = total % 60;
  return rest === 0 ? `${h} ч` : `${h} ч ${String(rest).padStart(2, '0')} мин`;
};

/** A KPI's value on its tile. Null or not a number: «—». */
export const formatKpi = (format: KpiFormat, v: number | null | undefined): string => {
  if (!ok(v)) return DASH;
  switch (format) {
    case 'usd':
      return `$${ru(v, 1)}`;
    case 'pct':
      return `${ru(v, 0, v < 10 ? 1 : 0)}%`;
    case 'minutes':
      return minutesText(v);
    case 'tokens':
      return v >= 1000 ? `${ru(v / 1000, 0)}K` : ru(v, 0);
    case 'count':
    case 'perMonth':
    case 'perWeek':
      return ru(v, 0, 1);
  }
};

/** The same value on the big chart's axis and bubbles: a share of a dollar and a second digit are worth showing. */
export const formatChart = (format: KpiFormat, v: number | null | undefined): string => {
  if (!ok(v)) return DASH;
  if (format === 'perMonth' || format === 'perWeek') return ru(v, 2);
  return formatKpi(format, v);
};

/** The value in a «до → после» line: whole dollars stay whole («$19 → $18,5»). */
export const formatEffect = (format: KpiFormat, v: number | null | undefined): string => {
  if (!ok(v)) return DASH;
  if (format === 'usd') return `$${ru(v, 0, 1)}`;
  return formatKpi(format, v);
};

/** «было $21,0 / 59% / 2 ч 33 мин / 6,1 в месяц / 2,1 сессии / 50K токенов». */
export const wasText = (kpi: Pick<Kpi, 'format' | 'prev'>): string => {
  if (!ok(kpi.prev)) return `было ${DASH}`;
  const v = formatKpi(kpi.format, kpi.prev);
  switch (kpi.format) {
    case 'perMonth':
      return `было ${v} в месяц`;
    case 'perWeek':
      return `было ${v} в неделю`;
    case 'tokens':
      return `было ${v} токенов`;
    case 'count':
      return `было ${v} сессии`;
    default:
      return `было ${v}`;
  }
};

export type Tone = 'good' | 'bad' | 'flat';
export interface DeltaText {
  tone: Tone;
  /** «↓ 12%», «↑ 5 п.», «↑ 0,3», «как раньше»; «—» without a number to compare. */
  text: string;
}

/** How far the value went from the old one, in the units the KPI is told in (percent of old, points, plain). */
export const changeSize = (kpi: Pick<Kpi, 'value' | 'prev' | 'delta'>): number | null => {
  if (!ok(kpi.value) || !ok(kpi.prev)) return null;
  const diff = kpi.value - kpi.prev;
  if (kpi.delta !== 'rel') return diff;
  return kpi.prev === 0 ? null : (diff / Math.abs(kpi.prev)) * 100;
};

/** The change chip of a tile: the arrow and size, the tone by which way is better, «как раньше» inside the noise. */
export const deltaOf = (kpi: Pick<Kpi, 'value' | 'prev' | 'delta' | 'better' | 'noise'>): DeltaText => {
  const size = changeSize(kpi);
  if (size === null) return { tone: 'flat', text: DASH };
  const abs = Math.abs(size);
  if (!(abs >= kpi.noise) || Math.round(abs * 10) === 0) return { tone: 'flat', text: 'как раньше' };
  const arrow = size > 0 ? '↑' : '↓';
  const good = kpi.better === 'up' ? size > 0 : size < 0;
  const body =
    kpi.delta === 'rel' ? `${ru(abs, 0)}%` : kpi.delta === 'pts' ? `${ru(abs, 0, 1)} п.` : ru(abs, 0, 1);
  return { tone: good ? 'good' : 'bad', text: `${arrow} ${body}` };
};

export const SPARK_W = 200;
export const SPARK_H = 44;
/** Both lines share one scale, padded by this share of the range so the line never touches the edge. */
const SPARK_PAD = 0.15;

export interface SparkPaths {
  /** The comparison, drawn dashed. */
  prev: string;
  /** The filled area under the period's line. */
  area: string;
  now: string;
}

const line = (ys: readonly (number | null)[], y: (v: number) => number): string => {
  const n = ys.length;
  const parts: string[] = [];
  ys.forEach((v, i) => {
    if (!ok(v)) return;
    const x = n > 1 ? (i / (n - 1)) * SPARK_W : 0;
    parts.push(`${parts.length === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y(v).toFixed(1)}`);
  });
  return parts.join(' ');
};

/** The tile's sparkline, in a 200 by 44 box: the period solid with an area, the comparison on the same scale. */
export const sparkPaths = (
  series: readonly (number | null)[],
  prevSeries: readonly (number | null)[],
): SparkPaths => {
  const all = [...series, ...prevSeries].filter(ok);
  if (all.length === 0) return { prev: '', area: '', now: '' };
  const lo = Math.min(...all);
  const hi = Math.max(...all);
  const range = hi - lo;
  const y = (v: number): number =>
    range === 0
      ? SPARK_H / 2
      : SPARK_H * (1 - (v - (lo - SPARK_PAD * range)) / (range * (1 + 2 * SPARK_PAD)));
  const now = line(series, y);
  return {
    prev: line(prevSeries, y),
    now,
    area: now ? `${now} L${SPARK_W} ${SPARK_H} L0 ${SPARK_H} Z` : '',
  };
};

/** The chip of a «до и после» line. */
export const VERDICT_WORD: Record<Verdict, string> = {
  better: 'лучше',
  worse: 'хуже',
  noise: 'в пределах шума',
};
export const VERDICT_TONE: Record<Verdict, Tone> = { better: 'good', worse: 'bad', noise: 'flat' };

/** The before/after bars: the larger value is the full width. */
export const barWidths = (before: number, after: number): [number, number] => {
  const top = Math.max(Math.abs(before), Math.abs(after));
  if (!(top > 0)) return [0, 0];
  return [Math.round((Math.abs(before) / top) * 100), Math.round((Math.abs(after) / top) * 100)];
};

/** A change card's colour: a worse number wins over a better one, no moved number is quiet. */
export const changeTone = (effects: readonly Pick<Effect, 'verdict'>[]): Tone => {
  if (effects.some((e) => e.verdict === 'worse')) return 'bad';
  if (effects.some((e) => e.verdict === 'better')) return 'good';
  return 'flat';
};

/** Inside this many percent a before/after move on the chart is the noise of the day. */
export const FLAT_PCT = 3;

export interface Shift {
  pct: number;
  tone: Tone;
}

/** The move of a metric between two averages, by which way is better. */
export const shiftOf = (before: number, after: number, lowIsBetter: boolean): Shift => {
  const pct = before === 0 ? 0 : ((after - before) / Math.abs(before)) * 100;
  if (Math.abs(pct) < FLAT_PCT) return { pct, tone: 'flat' };
  return { pct, tone: (lowIsBetter ? pct < 0 : pct > 0) ? 'good' : 'bad' };
};

/** The mean of `values[from, to)`, skipping the days without a number; null when none is left. */
export const meanOf = (values: readonly (number | null)[], from: number, to: number): number | null => {
  const xs = values.slice(Math.max(0, from), Math.max(0, to)).filter(ok);
  return xs.length === 0 ? null : xs.reduce((s, v) => s + v, 0) / xs.length;
};

/** «1 день», «2 дня», «7 дней». */
export const daysWord = (n: number): string => `${n} ${n === 1 ? 'день' : n < 5 ? 'дня' : 'дней'}`;

/** How much of a change's neighbourhood the chart averages: a week each side, less at the edges of the data. */
export const effectWindows = (dayIndex: number, total: number): { before: number; after: number } => ({
  before: Math.max(1, Math.min(7, dayIndex)),
  after: Math.max(1, Math.min(7, total - dayIndex)),
});

/** The foot of the effect bubble. */
export const effectNote = (before: number, after: number): string =>
  `Среднее за ${daysWord(before)} до и ${daysWord(after)} после${Math.min(before, after) < 7 ? ' — меньше недели данных' : ''}`;

export interface WasteView {
  /** Share of the waste, a segment of the bar, in percent. */
  segment: number;
  /** Share of everything spent, in whole percent. */
  share: number;
}

export const wasteView = (waste: Pick<Waste, 'wasteUsd' | 'totalUsd'>, usd: number): WasteView => ({
  segment: waste.wasteUsd > 0 ? (usd / waste.wasteUsd) * 100 : 0,
  share: waste.totalUsd > 0 ? Math.round((usd / waste.totalUsd) * 100) : 0,
});

export const money = (v: number): string => `$${ru(v, 0)}`;

/** «$1 080 из $3 450 за период — 31% расхода». */
export const wasteLead = (waste: Pick<Waste, 'wasteUsd' | 'totalUsd'>): string => {
  const share = waste.totalUsd > 0 ? Math.round((waste.wasteUsd / waste.totalUsd) * 100) : 0;
  return `${money(waste.wasteUsd)} из ${money(waste.totalUsd)} за период — ${share}% расхода`;
};

export type Heat = 'g2' | 'g1' | 'n' | 'r1' | 'r2';

/** A peer cell's shade from its score (+1 well above the average … −1 well below). */
export const heatOf = (score: number): Heat => {
  if (score >= 0.75) return 'g2';
  if (score >= 0.25) return 'g1';
  if (score > -0.25) return 'n';
  if (score > -0.75) return 'r1';
  return 'r2';
};

export const HEAT_NOTE: Record<Heat, string> = {
  g2: 'заметно лучше среднего',
  g1: 'чуть лучше среднего',
  n: 'около среднего',
  r1: 'чуть хуже среднего',
  r2: 'заметно хуже среднего',
};

/** The peer table's header and the row's cell, kept together so a short row never shifts the columns. */
export const peerCells = (row: PeerRow, columns: readonly string[]): PeerRow['cells'] =>
  columns.map((_, i) => row.cells[i] ?? { text: DASH, score: 0 });

/** The page's lead: «$810 · 302 сессии · 44 MR». */
export const totalsText = (s: { spendUsd: number; sessions: number; mrs: number }): string =>
  `${money(s.spendUsd)} · ${s.sessions} ${plural(s.sessions, 'сессия', 'сессии', 'сессий')} · ${s.mrs} MR`;

export const plural = (n: number, one: string, few: string, many: string): string => {
  const m100 = n % 100;
  const m10 = n % 10;
  if (m100 >= 11 && m100 <= 14) return many;
  if (m10 === 1) return one;
  if (m10 >= 2 && m10 <= 4) return few;
  return many;
};
