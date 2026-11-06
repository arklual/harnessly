// The Аналитика section's contract. `…/kpis`, `…/kpis/{kpi}/series`, `…/changes`, `…/waste` of
// docs/hub (analytics) map onto these shapes; until those contracts land the mock (../mock/analytics.ts) is the
// reference and live answers `not_ready`.
import { addDays } from '../dates';
import type { SignalGo } from './overview';

export type KpiId =
  | 'moneyPerMr'
  | 'yield'
  | 'timeToMr'
  | 'mrPerPerson'
  | 'autonomy'
  | 'waiting'
  | 'parallel'
  | 'toolErrors'
  | 'reread'
  | 'sessionStart'
  | 'cache'
  | 'skills'
  | 'cacheWrite1h'
  | 'mrSurvival'
  | 'waitingP90';

export type KpiGroup = 'result' | 'agent' | 'env';

/** How a value is written and how a change is told: `rel` as a percent of the old value, `pts` in points, `abs` as the plain difference. */
export type KpiFormat = 'usd' | 'pct' | 'minutes' | 'count' | 'tokens' | 'perMonth' | 'perWeek';
export type DeltaKind = 'rel' | 'pts' | 'abs';

export interface Kpi {
  id: KpiId;
  group: KpiGroup;
  title: string;
  /** One line under the chart's title when this KPI is selected. */
  hint: string;
  format: KpiFormat;
  delta: DeltaKind;
  /** Which way is better. */
  better: 'up' | 'down';
  /** A change smaller than this (in the units of `delta`) is told as «как раньше». */
  noise: number;
  /** Null: too few sessions for a number. */
  value: number | null;
  prev: number | null;
  /** The small chart of the tile: by day of the period, null where there was too little to count. */
  series: (number | null)[];
  prevSeries: (number | null)[];
}

/** A calendar week, Monday to Sunday, as `YYYY-MM-DD`. */
export interface Week {
  from: string;
  to: string;
}

export interface ChangeRef {
  id: string;
  title: string;
  at: string;
}

/** A week of the picker's strip: the money spent in it makes the bar, the unfinished week is marked. */
export interface FrameWeek extends Week {
  /** Null/absent: no number yet, the bar is drawn at the minimum. */
  spendUsd?: number | null;
  /** The week is still going (the last one of the strip). */
  current?: boolean;
}

/** What the pickers need before any number: the weeks to choose from and the latest change for the preset. */
export interface AnalyticsFrame {
  weeks: FrameWeek[];
  /** The last day with data: the big chart ends here. Absent: the period's last day. */
  today?: string;
  defaultPeriod: Week;
  lastChange: ChangeRef | null;
}

/**
 * The weeks of the current month, as the screen opens on the same period as «Обзор». A week belongs to the month its
 * Thursday is in (so «ноябрь» does not start with a week that is mostly October); the strip's last week is the current
 * one. Nothing of the month yet: the last week.
 */
export const monthPeriod = (weeks: readonly Week[]): Week => {
  const last = weeks[weeks.length - 1];
  if (!last) throw new Error('no weeks');
  const month = (w: Week): string => addDays(w.from, 3).slice(0, 7);
  const ref = month(last);
  const mine = weeks.filter((w) => month(w) === ref);
  return { from: (mine[0] ?? last).from, to: last.to };
};

export interface AnalyticsQuery {
  /** Of the selected weeks: the first Monday and the last Sunday. */
  period: Week;
  compare: Week;
}

export interface Summary {
  spendUsd: number;
  sessions: number;
  mrs: number;
}

/** One line of a hover bubble: «Фронт · 14%». */
export interface DetailRow {
  label: string;
  value: string;
}

export type SignalTone = 'high' | 'mid';
export interface Signal {
  id: string;
  tone: SignalTone;
  title: string;
  text: string;
  /** Null: nothing to do but read. */
  action: SignalGo | null;
  /** The numbers behind the card, for its hover bubble. Absent: the bubble is not drawn. */
  detail?: DetailRow[];
}

export type ChangeKind = 'tool' | 'model' | 'plugins';
export type Verdict = 'better' | 'worse' | 'noise';

export interface Effect {
  kpi: KpiId;
  title: string;
  format: KpiFormat;
  before: number;
  after: number;
  verdict: Verdict;
}

export interface Change {
  id: string;
  kind: ChangeKind;
  title: string;
  at: string;
  /** «окружение тенанта · Дима» */
  meta: string;
  effects: Effect[];
}

export interface WasteItem {
  id: string;
  title: string;
  text: string;
  usd: number;
  tone: 'violet' | 'rose' | 'amber' | 'teal';
  /** Where the number comes from, for the row's hover bubble. */
  detail?: DetailRow[];
}

export interface Waste {
  wasteUsd: number;
  totalUsd: number;
  items: WasteItem[];
}

export interface PeerRow {
  id: string;
  name: string;
  /** One cell per column; `score` -1 (worse than the average) … 1 (better). */
  cells: { text: string; score: number; mean?: string }[];
}

export interface Peers {
  title: string;
  sub: string;
  columns: string[];
  rows: PeerRow[];
}

export interface AnalyticsData {
  /** «Все команды тенанта», «Команда Фронт», «Только твоя работа». */
  scopeLabel: string;
  summary: Summary;
  kpis: Kpi[];
  signals: Signal[];
  changes: Change[];
  waste: Waste | null;
  peers: Peers | null;
}

export interface SeriesLine {
  id: string;
  name: string;
  /** Null: too few sessions that day, a gap in the line. */
  values: (number | null)[];
}

export interface Marker {
  id: string;
  kind: ChangeKind;
  title: string;
  /** Index into `days`. */
  index: number;
  /** The icon the hub picked: a written edit, a new version of a tool, anything else. Absent: told from `kind`. */
  icon?: 'edit' | 'announce' | 'rocket';
  /** «окружение организации · Аня»: the layer and the author, under the title in the chart's bubble. */
  detail?: string;
}

export interface KpiChart {
  kpi: KpiId;
  /** ISO days of the whole drawn range. */
  days: string[];
  /** Lines by team, or by person for a lead; one «Мои» for a participant. */
  lines: SeriesLine[];
  /** The same value for the compare range, day by day, aligned to the period's first day. */
  compareLine: (number | null)[];
  /** The selected period inside `days`. */
  periodFrom: number;
  periodTo: number;
  markers: Marker[];
  /** The scope's own value day by day (not the sum of `lines`: a share is not a sum), aligned to `days`. */
  total?: (number | null)[];
  /** The same value one comparison back: day `i` holds what `days[i]` held at the compare period's distance. */
  previous?: (number | null)[];
}

/** One finished or running cycle of a limit window (a week of a subscription, five hours of a session). */
export interface LimitCycle {
  /** RFC 3339: when the cycle resets. */
  endsAt: string;
  /** The highest use of the window in the cycle, percent. */
  peakPct: number;
  measures: number;
}

export interface LimitWindow {
  /** «weekly», «5h»: the window's name as the provider calls it. */
  name: string;
  lengthSec: number;
  cycles: LimitCycle[];
  avgPeakPct: number;
  /** How many cycles ran into the limit. */
  fullCycles: number;
}

export interface LimitAccount {
  id: string;
  label: string;
  plan: string;
  windows: LimitWindow[];
}

/** The history of the limit windows of the scope's accounts; null: the viewer may not see money, or the hub has no history. */
export type LimitHistory = LimitAccount[] | null;

export interface AnalyticsApi {
  analyticsFrame(): Promise<AnalyticsFrame>;
  analytics(query: AnalyticsQuery): Promise<AnalyticsData>;
  analyticsChart(query: AnalyticsQuery, kpi: KpiId): Promise<KpiChart>;
  limitWindows(query: AnalyticsQuery): Promise<LimitHistory>;
}
