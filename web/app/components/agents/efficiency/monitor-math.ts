import type { IEffDay, IEffMarker, TEffMarkerKind } from '@/types/efficiency';
import { compact, decimal, minutes, money, pct } from '@/components/agents/efficiency/format';
import { t } from '@/lib/i18n';

export const BASE_DAYS = 14;
export const EFFECT_DAYS = 7;

export type TVerdict = 'good' | 'bad' | 'flat' | 'wait';
export type TMetricKey = Exclude<keyof IEffDay, 'day' | 'sessions' | 'modelCalls' | 'mrs' | 'sleepHours'>;

export interface IMetric {
  key: TMetricKey;
  name: string;
  hint: string;
  better: 'up' | 'down';
  format: (v: number | null) => string;
  kind: 'line' | 'bar';
  /** Dashed reference line: a target or a threshold, in the metric's own unit. */
  line?: { value: number; label: string };
  /** Metric stored as a 0–1 share: a shift is read in percentage points, noise floor is 5 points. */
  share?: boolean;
}

export const METRICS: IMetric[] = [
  {
    key: 'avgCtx',
    name: t('efficiency.monitorMath.avgCtxName'),
    hint: t('efficiency.monitorMath.avgCtxHint'),
    better: 'down',
    format: compact,
    kind: 'line',
    line: { value: 250_000, label: t('efficiency.monitorMath.avgCtxLine') },
  },
  {
    key: 'firstCtxP50',
    name: t('efficiency.monitorMath.firstCtxP50Name'),
    hint: t('efficiency.monitorMath.firstCtxP50Hint'),
    better: 'down',
    format: compact,
    kind: 'line',
    line: { value: 40_000, label: t('efficiency.monitorMath.firstCtxP50Line') },
  },
  {
    key: 'carryShare',
    name: t('efficiency.monitorMath.carryShareName'),
    hint: t('efficiency.monitorMath.carryShareHint'),
    better: 'down',
    format: (v) => pct(v),
    kind: 'line',
    share: true,
  },
  {
    key: 'toolErrorRate',
    name: t('efficiency.monitorMath.toolErrorRateName'),
    hint: t('efficiency.monitorMath.toolErrorRateHint'),
    better: 'down',
    format: (v) => pct(v, 1),
    kind: 'line',
    share: true,
    line: { value: 0.02, label: t('efficiency.monitorMath.toolErrorRateLine') },
  },
  {
    key: 'guardStopRate',
    name: t('efficiency.monitorMath.guardStopRateName'),
    hint: t('efficiency.monitorMath.guardStopRateHint'),
    better: 'down',
    format: (v) => pct(v, 1),
    kind: 'line',
    share: true,
  },
  {
    key: 'redGateRate',
    name: t('efficiency.monitorMath.redGateRateName'),
    hint: t('efficiency.monitorMath.redGateRateHint'),
    better: 'down',
    format: (v) => pct(v, 1),
    kind: 'line',
    share: true,
  },
  {
    key: 'subagentSonnetShare',
    name: t('efficiency.monitorMath.subagentSonnetShareName'),
    hint: t('efficiency.monitorMath.subagentSonnetShareHint'),
    better: 'up',
    format: (v) => pct(v),
    kind: 'line',
    share: true,
    line: { value: 0.8, label: t('efficiency.monitorMath.subagentSonnetShareLine') },
  },
  {
    key: 'gateVisibleShare',
    name: t('efficiency.monitorMath.gateVisibleShareName'),
    hint: t('efficiency.monitorMath.gateVisibleShareHint'),
    better: 'up',
    format: (v) => pct(v),
    kind: 'line',
    share: true,
  },
  {
    key: 'humanReplyP90Min',
    name: t('efficiency.monitorMath.humanReplyP90MinName'),
    hint: t('efficiency.monitorMath.humanReplyP90MinHint'),
    better: 'down',
    format: minutes,
    kind: 'line',
  },
  {
    key: 'ciPollsPerMr',
    name: t('efficiency.monitorMath.ciPollsPerMrName'),
    hint: t('efficiency.monitorMath.ciPollsPerMrHint'),
    better: 'down',
    format: (v) => decimal(v),
    kind: 'line',
  },
  {
    key: 'costUsd',
    name: t('efficiency.monitorMath.costUsdName'),
    hint: t('efficiency.monitorMath.costUsdHint'),
    better: 'down',
    format: money,
    kind: 'bar',
  },
];

export const MARKER_LABEL: Record<TEffMarkerKind, string> = {
  model: t('efficiency.monitorMath.markerModel'),
  claude: 'Claude Code',
  plugins: t('efficiency.monitorMath.markerPlugins'),
  memory: t('efficiency.monitorMath.markerMemory'),
  rule: t('efficiency.monitorMath.markerRule'),
  eval: t('efficiency.monitorMath.markerEval'),
};

/** When several changes land on one day the lane shows the one that most likely moved the numbers. */
const MARKER_PRIORITY: TEffMarkerKind[] = ['rule', 'model', 'memory', 'plugins', 'eval', 'claude'];

export const seriesOf = (days: IEffDay[], key: TMetricKey): (number | null)[] => days.map((d) => d[key]);

const present = (values: (number | null)[]): number[] => values.filter((v): v is number => v !== null);

export const median = (values: (number | null)[]): number | null => {
  const sorted = present(values).sort((a, b) => a - b);
  if (sorted.length === 0) return null;
  const mid = sorted.length >> 1;
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
};

const mad = (values: (number | null)[]): number | null => {
  const m = median(values);
  return m === null ? null : median(present(values).map((v) => Math.abs(v - m)));
};

export interface IBand {
  low: number;
  high: number;
}

/** A/A band: the spread of the metric over the first two weeks of the period, before the changes under review. */
export const aaBand = (values: (number | null)[]): IBand | null => {
  const sorted = present(values.slice(0, BASE_DAYS)).sort((a, b) => a - b);
  if (sorted.length < 3) return null;
  return {
    low: sorted[Math.floor((sorted.length - 1) * 0.25)],
    high: sorted[Math.ceil((sorted.length - 1) * 0.75)],
  };
};

export interface IMarkerDay {
  day: string;
  index: number;
  markers: IEffMarker[];
  /** The marker the lane draws when the day holds several. */
  lead: IEffMarker;
}

/** Markers that fall into the plotted window, grouped by day, aligned to the day's index on the axis. Dates outside it (plans, other periods) are dropped. */
export const markerDays = (days: string[], markers: IEffMarker[]): IMarkerDay[] => {
  const index = new Map(days.map((d, i) => [d, i]));
  const byDay = new Map<string, IEffMarker[]>();
  for (const marker of markers) {
    const day = marker.at.slice(0, 10);
    if (!index.has(day)) continue;
    byDay.set(day, [...(byDay.get(day) ?? []), marker]);
  }
  return [...byDay.entries()]
    .map(([day, list]) => ({
      day,
      index: index.get(day) as number,
      markers: list,
      lead: [...list].sort((a, b) => MARKER_PRIORITY.indexOf(a.kind) - MARKER_PRIORITY.indexOf(b.kind))[0],
    }))
    .sort((a, b) => a.index - b.index);
};

/** The week before (dir = -1) or after (dir = 1) a day, the day itself excluded. */
export const windowAround = (values: (number | null)[], index: number, dir: -1 | 1): number[] => {
  const out: number[] = [];
  for (let step = 1; step <= EFFECT_DAYS; step++) {
    const v = values[index + dir * step];
    if (v !== undefined && v !== null) out.push(v);
  }
  return out;
};

export interface IEffect {
  before: number | null;
  after: number | null;
  verdict: TVerdict;
}

/** "What moved": median of the week before against the week after. A shift inside 1.5× the A/A spread is noise. */
export const judge = (
  metric: IMetric,
  before: (number | null)[],
  after: (number | null)[],
  baseline: (number | null)[],
): IEffect => {
  const b = median(before);
  const a = median(after);
  if (b === null || a === null || present(before).length < 2 || present(after).length < 2)
    return { before: b, after: a, verdict: 'wait' };
  const noise = Math.max(mad(baseline.slice(0, BASE_DAYS)) ?? 0, Math.abs(b) * 0.05, metric.share ? 0.05 : 0);
  const diff = a - b;
  if (Math.abs(diff) <= 1.5 * noise) return { before: b, after: a, verdict: 'flat' };
  return { before: b, after: a, verdict: (metric.better === 'down') === diff < 0 ? 'good' : 'bad' };
};

export const effectAt = (metric: IMetric, days: IEffDay[], index: number): IEffect => {
  const values = seriesOf(days, metric.key);
  return judge(metric, windowAround(values, index, -1), windowAround(values, index, 1), values);
};

export const VERDICT_LABEL: Record<TVerdict, string> = {
  good: t('efficiency.monitorMath.verdictGood'),
  bad: t('efficiency.monitorMath.verdictBad'),
  flat: t('efficiency.monitorMath.verdictFlat'),
  wait: t('efficiency.monitorMath.verdictWait'),
};

/** +89 pp for shares, −21% for the rest. */
export const deltaText = (metric: IMetric, before: number | null, after: number | null): string => {
  if (before === null || after === null) return '—';
  const sign = (v: number) => (v >= 0 ? '+' : '−');
  if (metric.share)
    return t('efficiency.monitorMath.points', {
      sign: sign(after - before),
      value: Math.abs(Math.round((after - before) * 100)),
    });
  if (before === 0) return after === 0 ? '0' : `+${decimal(after)}`;
  return `${sign(after - before)}${Math.abs(Math.round(((after - before) / Math.abs(before)) * 100))}%`;
};

export interface ILegend {
  metric: IMetric;
  current: number | null;
  baseline: number | null;
  effect: IEffect;
}

/** The label column of the monitor: the last week against the first two weeks. */
export const currentVsBaseline = (metric: IMetric, days: IEffDay[]): ILegend => {
  const values = seriesOf(days, metric.key);
  const current = median(values.slice(-EFFECT_DAYS));
  const baseline = median(values.slice(0, BASE_DAYS));
  return {
    metric,
    current,
    baseline,
    effect: judge(metric, values.slice(0, BASE_DAYS), values.slice(-EFFECT_DAYS), values),
  };
};

/** Metrics worth showing in the change pane: those that moved first, noise and gaps after. */
export const changeEffects = (days: IEffDay[], index: number): { metric: IMetric; effect: IEffect }[] => {
  const order: TVerdict[] = ['bad', 'good', 'flat', 'wait'];
  return METRICS.filter((m) => m.kind === 'line')
    .map((metric) => ({ metric, effect: effectAt(metric, days, index) }))
    .sort((a, b) => order.indexOf(a.effect.verdict) - order.indexOf(b.effect.verdict));
};

/** Other changes within ±3 days: they are the confounders of any shift read off the lane. */
export const nearbyMarkers = (all: IMarkerDay[], day: IMarkerDay, radius = 3): IEffMarker[] =>
  all
    .filter((other) => other.day !== day.day && Math.abs(other.index - day.index) <= radius)
    .flatMap((other) => other.markers);

export const AXIS_PAD = 6;

export const xAt = (index: number, width: number, count: number): number =>
  AXIS_PAD + (count <= 1 ? 0 : (index / (count - 1)) * (width - 2 * AXIS_PAD));

/** The day under the pointer; px is measured from the left edge of a chart `width` pixels wide. */
export const indexAtX = (px: number, width: number, count: number): number => {
  if (count <= 1) return 0;
  return Math.max(
    0,
    Math.min(count - 1, Math.round(((px - AXIS_PAD) / (width - 2 * AXIS_PAD)) * (count - 1))),
  );
};

/** SVG path of a series; a day without data breaks the line instead of dropping it to zero. */
export const linePath = (
  values: (number | null)[],
  x: (i: number) => number,
  y: (v: number) => number,
): string => {
  let d = '';
  let pen = false;
  values.forEach((v, i) => {
    if (v === null) {
      pen = false;
      return;
    }
    d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)} ${y(v).toFixed(1)} `;
    pen = true;
  });
  return d.trim();
};

/** Upper edge of a metric's chart: room above the highest point or the reference line. */
export const chartMax = (metric: IMetric, values: (number | null)[]): number => {
  const top = Math.max(...present(values), metric.line?.value ?? 0) * 1.12;
  return Number.isFinite(top) && top > 0 ? top : 1;
};
