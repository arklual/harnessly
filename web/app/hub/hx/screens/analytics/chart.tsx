// The big chart of the selected KPI on the chart library (vizloom): one line per team, or this period against the
// comparison. The module is loaded on its own (the library is large); the hover is the library's own canvas work, no
// state of this screen changes while the pointer moves.
import { Megaphone, Pencil, Rocket } from 'lucide-react';
import { type ReactNode, useMemo } from 'react';
import { TimeSeriesChart, type MarkerEffectContext } from 'vizloom/charts';
import {
  intlZone,
  seriesColor,
  TooltipContent,
  VizProvider,
  type ColorSlot,
  type Series,
  type SeriesFrame,
  type TimeMarker,
  type TooltipContext,
  type ValueUnit,
} from 'vizloom/core';
import 'vizloom/styles.css';

import type { Kpi, KpiChart, KpiFormat, Marker } from '../../../org/api/analytics';
import { parseDay } from '../../../org/dates';
import { inRubles, shownOfUsd } from '../../../org/money';
import { DASH, effectNote, effectWindows, formatChart, meanOf, ru, shiftOf, VERDICT_WORD } from './calc';
import { teamSlot } from './colors';

export type ChartMode = 'teams' | 'cmp';

const HOUR = 36e5;
const DAY = 864e5;
const ZONE = intlZone('Europe/Moscow');
const CHART_HEIGHT = 360;

const UNIT: Record<KpiFormat, ValueUnit> = {
  usd: { kind: 'currency', currency: 'USD' }, // in rubles once the hub's rate is known, see unitOf
  pct: { kind: 'percent' },
  minutes: { kind: 'count', forms: ['минута', 'минуты', 'минут'] },
  count: { kind: 'count', forms: ['сессия', 'сессии', 'сессий'] },
  tokens: { kind: 'count', forms: ['токен', 'токена', 'токенов'] },
  perMonth: { kind: 'count', forms: ['MR', 'MR', 'MR'] },
  perWeek: { kind: 'count', forms: ['MR', 'MR', 'MR'] },
};

/** The axis counts what the lines hold: dollars turned to the hub's rubles are rubles. */
const unitOf = (format: KpiFormat): ValueUnit =>
  format === 'usd' && inRubles() && shownOfUsd(1) !== null
    ? { kind: 'currency', currency: 'RUB' }
    : UNIT[format];

/** A money line in the currency shown, so the axis and the hover agree with the tiles. */
const shownLine = (format: KpiFormat, values: Values): Values => {
  if (format !== 'usd' || shownOfUsd(1) === null) return values;
  return values.map((v) => (v === null ? null : (shownOfUsd(v) ?? v)));
};

type MarkerKind = 'start' | 'announce' | 'edit';

/** The icon a change gets on the chart: the one the hub picked, else told from the kind of the change. */
const iconOf = (m: Pick<Marker, 'icon' | 'kind'>): MarkerKind => {
  if (m.icon === 'rocket') return 'start';
  if (m.icon) return m.icon;
  return m.kind === 'tool' ? 'start' : m.kind === 'model' ? 'announce' : 'edit';
};

const MARKER_ICON: Record<MarkerKind, typeof Rocket> = { start: Rocket, announce: Megaphone, edit: Pencil };

const markerIcon = (m: { kind: string }): ReactNode => {
  const Icon = MARKER_ICON[m.kind as MarkerKind] ?? Pencil;
  return <Icon size={12} aria-hidden="true" />;
};

type Values = (number | null)[];

interface Line {
  id: string;
  label: string;
  slot: ColorSlot;
  values: Values;
}

/** The scope's value day by day, and the same one comparison back; an older answer is told from its lines. */
const pairOf = (chart: KpiChart): { now: Values; prev: Values } => {
  const n = chart.days.length;
  const blank = (): Values => Array.from({ length: n }, () => null);
  const now = chart.total ?? (chart.lines.length === 1 ? (chart.lines[0]?.values ?? blank()) : blank());
  if (chart.previous) return { now, prev: chart.previous };
  const prev = blank();
  chart.compareLine.forEach((v, i) => {
    const at = chart.periodFrom + i;
    if (at < n) prev[at] = v;
  });
  return { now, prev };
};

const dayStart = (iso: string): number => parseDay(iso).getTime() - 3 * HOUR;

/** One value a day for the daily frame of the library; a missing day is a gap. */
const daily = (values: Values): Float64Array =>
  Float64Array.from(values, (v) => (v === null ? Number.NaN : v));

interface Built {
  frame: SeriesFrame<string>;
  series: Series<string>[];
  markers: TimeMarker<string, MarkerKind>[];
  unit: ValueUnit;
  tooltip: (ctx: TooltipContext<string>) => ReactNode;
  effect: (ctx: MarkerEffectContext<string, MarkerKind>) => ReactNode;
}

const build = (chart: KpiChart, kpi: Kpi, mode: ChartMode, order: readonly string[]): Built => {
  const n = chart.days.length;
  const first = dayStart(chart.days[0] ?? '1970-01-01');
  const teams = mode === 'teams';
  const { now, prev } = pairOf(chart);
  const lines: Line[] = teams
    ? chart.lines.map((l) => ({
        id: `l:${l.id}`,
        label: l.name,
        slot: teamSlot(order, l.name),
        values: l.values,
      }))
    : [
        { id: 'now', label: 'Этот период', slot: 0, values: now },
        { id: 'prev', label: 'Период сравнения', slot: 'other', values: prev },
      ];
  const values: Record<string, Float64Array> = {};
  for (const l of lines) values[l.id] = daily(shownLine(kpi.format, l.values));
  const frame: SeriesFrame<string> = { granularity: 'day', start: first, step: DAY, length: n, values };
  const series: Series<string>[] = lines.map((l) => ({ id: l.id, label: l.label, slot: l.slot }));
  const ids = lines.map((l) => l.id);
  const days = new Map<string, number>();
  const markers: TimeMarker<string, MarkerKind>[] = chart.markers.map((m) => {
    days.set(m.id, m.index);
    return {
      id: m.id,
      t: first + m.index * DAY + 12 * HOUR,
      kind: iconOf(m),
      priority: 1,
      series: teams ? ids : ['now'],
      label: m.title,
      detail: m.detail,
    };
  });
  const lowIsBetter = kpi.better === 'down';
  const f = (v: number | null | undefined) => formatChart(kpi.format, v);
  // The library hands the hover the values it draws, already in the shown currency: back to dollars for the formatter.
  const rate = kpi.format === 'usd' ? (shownOfUsd(1) ?? 1) : 1;
  const fDrawn = (v: number) => f(v / rate);

  const tooltip = (ctx: TooltipContext<string>) => {
    const rows = lines.map((l) => ({
      id: l.id,
      label: l.label,
      value: Number.isFinite(ctx.values[l.id]) ? fDrawn(ctx.values[l.id] as number) : DASH,
      color: seriesColor(l.slot),
      raw: ctx.values[l.id],
    }));
    if (teams) rows.sort((a, b) => (b.raw ?? 0) - (a.raw ?? 0));
    const shown = rows.filter((r) => Number.isFinite(r.raw));
    const avg = shown.reduce((s, r) => s + (r.raw ?? 0), 0) / Math.max(1, shown.length);
    const a = ctx.values.now;
    const b = ctx.values.prev;
    const diff = a !== undefined && b !== undefined ? a - b : Number.NaN;
    const sum: { id: string; label: string; value: string; strong: boolean }[] = [];
    if (teams && shown.length > 0) {
      sum.push({ id: 'avg', label: 'В среднем по командам', value: fDrawn(avg), strong: true });
    }
    if (!teams && Number.isFinite(diff)) {
      const sign = diff > 0 ? '+' : diff < 0 ? '−' : '';
      sum.push({ id: 'diff', label: 'Разница', value: `${sign}${fDrawn(Math.abs(diff))}`, strong: true });
    }
    return <TooltipContent title={ctx.title} rows={[...rows, ...sum]} />;
  };

  // What the change did: the days before it against as many after, by the daily means of every line on the chart.
  const effect = (ctx: MarkerEffectContext<string, MarkerKind>) => {
    const x = ctx.marker;
    const d = days.get(x.id) ?? 0;
    const { before: wb, after: wa } = effectWindows(d, n);
    const rows = lines
      .filter((l) => teams || l.id === 'now')
      .flatMap((l) => {
        const before = meanOf(l.values, d - wb, d);
        const after = meanOf(l.values, d, d + wa);
        if (before === null || after === null) return [];
        const s = shiftOf(before, after, lowIsBetter);
        return [{ id: l.id, label: l.label, color: seriesColor(l.slot), before, after, ...s }];
      });
    const word = { good: VERDICT_WORD.better, bad: VERDICT_WORD.worse, flat: VERDICT_WORD.noise };
    const day = new Date(x.t).toLocaleDateString('ru-RU', {
      day: 'numeric',
      month: 'long',
      timeZone: 'Europe/Moscow',
    });
    const table = rows.map((r) => ({
      id: r.id,
      label: r.label,
      value: `${f(r.before)} → ${f(r.after)}`,
      color: r.color,
      note: (
        <span className="anx-delta" data-tone={r.tone}>
          {r.tone === 'flat'
            ? word.flat
            : `${r.pct > 0 ? '↑' : '↓'} ${ru(Math.abs(r.pct), 0)}% · ${word[r.tone]}`}
        </span>
      ),
    }));
    return (
      <TooltipContent
        title={
          <>
            {markerIcon(x)} {x.label}
          </>
        }
        subtitle={x.detail ? `${day} · ${x.detail}` : day}
        rows={table}
        footer={
          <>
            <div className="anx-eff__h">{`${kpi.title} — до и после`}</div>
            {rows.length === 0 && <div>Нет данных до или после</div>}
            <div>{effectNote(wb, wa)}</div>
          </>
        }
      />
    );
  };

  return { frame, series, markers, unit: unitOf(kpi.format), tooltip, effect };
};

export interface AnalyticsChartProps {
  chart: KpiChart;
  kpi: Kpi;
  mode: ChartMode;
  /** The tenant's teams in their order: a team keeps its colour on every card. */
  teamOrder: readonly string[];
}

const AnalyticsChart = ({ chart, kpi, mode, teamOrder }: AnalyticsChartProps) => {
  const b = useMemo(() => build(chart, kpi, mode, teamOrder), [chart, kpi, mode, teamOrder]);
  return (
    <VizProvider locale="ru" zone={ZONE}>
      <TimeSeriesChart
        title={kpi.title}
        series={b.series}
        frame={b.frame}
        kind="area"
        granularity="day"
        unit={b.unit}
        height={CHART_HEIGHT}
        points="none"
        peak={false}
        gradient={0.3}
        markers={b.markers}
        renderMarkerIcon={markerIcon}
        markerLabel={(x) => x.label}
        tooltip={b.tooltip}
        renderEffect={b.effect}
        navigator
        zoomControls
      />
    </VizProvider>
  );
};

export default AnalyticsChart;
