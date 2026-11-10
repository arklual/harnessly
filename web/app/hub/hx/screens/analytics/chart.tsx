// The big chart of the selected KPI on the chart library's wave board: one line per team, or this period against the
// comparison. The module is loaded on its own (the library is large); the hover is the library's own canvas work, no
// state of this screen changes while the pointer moves.
import { type ReactNode, useMemo } from 'react';

import {
  createSource,
  formatBucketTitle,
  renderMarkerIcon,
  VizWave,
  type VizMarker,
  type VizSeries,
  type VizUnit,
  type WaveEffectContext,
  type WaveEngineOptions,
  type WaveTooltipContext,
} from '@viz';

import type { Kpi, KpiChart, KpiFormat, Marker } from '../../../org/api/analytics';
import { parseDay } from '../../../org/dates';
import { inRubles, shownOfUsd } from '../../../org/money';
import { DASH, effectNote, effectWindows, formatChart, meanOf, ru, shiftOf, VERDICT_WORD } from './calc';
import { teamSlot } from './colors';

export type ChartMode = 'teams' | 'cmp';

const HOUR = 36e5;
const DAY = 864e5;

const UNIT: Record<KpiFormat, VizUnit> = {
  usd: { kind: 'currency', currency: 'USD' }, // in rubles once the hub's rate is known, see unitOf
  pct: { kind: 'percent' },
  minutes: { kind: 'count', forms: ['минута', 'минуты', 'минут'] },
  count: { kind: 'count', forms: ['сессия', 'сессии', 'сессий'] },
  tokens: { kind: 'count', forms: ['токен', 'токена', 'токенов'] },
  perMonth: { kind: 'count', forms: ['MR', 'MR', 'MR'] },
  perWeek: { kind: 'count', forms: ['MR', 'MR', 'MR'] },
};

/** The axis counts what the lines hold: dollars turned to the hub's rubles are rubles. */
const unitOf = (format: KpiFormat): VizUnit =>
  format === 'usd' && inRubles() && shownOfUsd(1) !== null
    ? { kind: 'currency', currency: 'RUB' }
    : UNIT[format];

/** A money line in the currency shown, so the axis and the hover agree with the tiles. */
const shownLine = (format: KpiFormat, values: Values): Values => {
  if (format !== 'usd' || shownOfUsd(1) === null) return values;
  return values.map((v) => (v === null ? null : (shownOfUsd(v) ?? v)));
};

const OPTIONS: Partial<WaveEngineOptions> = {
  mode: 'overlay',
  zones: false,
  points: 'none',
  peak: false,
  gradient: 0.3,
};

/** The icon a change gets on the chart: the one the hub picked, else told from the kind of the change. */
const iconOf = (m: Pick<Marker, 'icon' | 'kind'>): 'start' | 'announce' | 'edit' => {
  if (m.icon === 'rocket') return 'start';
  if (m.icon) return m.icon;
  return m.kind === 'tool' ? 'start' : m.kind === 'model' ? 'announce' : 'edit';
};

type Values = (number | null)[];

interface Line {
  id: string;
  label: string;
  slot: VizSeries['slot'];
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

/** One value a day on the hourly grid of the library: the number at noon, zeros round it, a missing day is a gap. */
const hourly = (values: Values): Float64Array => {
  const a = new Float64Array(values.length * 24);
  values.forEach((v, d) => {
    for (let h = 0; h < 24; h += 1) a[d * 24 + h] = v === null ? Number.NaN : 0;
    if (v !== null) a[d * 24 + 12] = v;
  });
  return a;
};

const Swatch = ({ color }: { color: string }) => (
  <svg className="viz-swatch" viewBox="0 0 10 10" aria-hidden="true">
    <circle cx="5" cy="5" r="4" fill={color} stroke={color} strokeWidth="1.5" />
  </svg>
);

interface Built {
  metric: string;
  source: ReturnType<typeof createSource>;
  series: VizSeries[];
  markers: VizMarker[];
  unit: VizUnit;
  tooltip: (ctx: WaveTooltipContext) => ReactNode;
  effect: (ctx: WaveEffectContext) => ReactNode;
}

const build = (chart: KpiChart, kpi: Kpi, mode: ChartMode, order: readonly string[]): Built => {
  const n = chart.days.length;
  const first = dayStart(chart.days[0] ?? '1970-01-01');
  const end = first + n * DAY;
  const teams = mode === 'teams';
  const metric = `${kpi.id}-${mode}`;
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
  for (const l of lines) values[l.id] = hourly(shownLine(kpi.format, l.values));
  const source = createSource<string, string>({
    hourly: { [metric]: { start: first, length: n * 24, values } },
    extent: { start: first, end, now: end },
    latency: 0,
  });
  const series: VizSeries[] = lines.map((l) => ({ id: l.id, label: l.label, slot: l.slot }));
  const ids = lines.map((l) => l.id);
  const days = new Map<string, number>();
  const markers: VizMarker[] = chart.markers.map((m) => {
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

  const tooltip = (ctx: WaveTooltipContext) => {
    const rows = ctx.values.slice();
    if (teams) rows.sort((a, b) => (b.value || 0) - (a.value || 0));
    const shown = rows.filter((r) => Number.isFinite(r.value));
    const avg = shown.reduce((s, r) => s + r.value, 0) / Math.max(1, shown.length);
    const a = ctx.values.find((r) => r.id === 'now')?.value;
    const b = ctx.values.find((r) => r.id === 'prev')?.value;
    const diff = a !== undefined && b !== undefined ? a - b : Number.NaN;
    return (
      <div className="viz-tip">
        <div className="viz-tip__title">
          <span>{formatBucketTitle(ctx.start, ctx.granularity, ctx.step)}</span>
        </div>
        {rows.map((r) => (
          <div key={r.id} className="viz-tip__row" data-focus={r.id === ctx.seriesId || undefined}>
            <Swatch color={r.color} />
            <span className="viz-tip__label">{r.label}</span>
            <b className="viz-tip__value">{Number.isFinite(r.value) ? fDrawn(r.value) : DASH}</b>
          </div>
        ))}
        {teams
          ? shown.length > 0 && (
              <div className="viz-tip__total">
                <span>В среднем по командам</span>
                <b>{fDrawn(avg)}</b>
              </div>
            )
          : Number.isFinite(diff) && (
              <div className="viz-tip__total">
                <span>Разница</span>
                <b>{`${diff > 0 ? '+' : diff < 0 ? '−' : ''}${fDrawn(Math.abs(diff))}`}</b>
              </div>
            )}
        {ctx.markers.map((x) => (
          <div key={x.id} className="viz-tip__event">
            {renderMarkerIcon(x)}
            <span>{`Изменение: ${x.label}`}</span>
          </div>
        ))}
      </div>
    );
  };

  // What the change did: the days before it against as many after, by the daily means of every line on the chart.
  const effect = (ctx: WaveEffectContext) => {
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
        const color = ctx.series.find((c) => c.id === l.id)?.color;
        return [{ id: l.id, label: l.label, color, before, after, ...s }];
      });
    const word = { good: VERDICT_WORD.better, bad: VERDICT_WORD.worse, flat: VERDICT_WORD.noise };
    return (
      <div className="viz-tip viz-tip--effect">
        <div className="viz-tip__kicker">
          {renderMarkerIcon(x)}
          <span>
            {new Date(x.t).toLocaleDateString('ru-RU', {
              day: 'numeric',
              month: 'long',
              timeZone: 'Europe/Moscow',
            })}
          </span>
        </div>
        <div className="viz-tip__title">{x.label}</div>
        {x.detail && <div className="viz-tip__subtitle">{x.detail}</div>}
        <div className="anx-eff__h">{`${kpi.title} — до и после`}</div>
        {rows.map((r) => (
          <div key={r.id} className="viz-tip__effect">
            <Swatch color={r.color ?? 'currentColor'} />
            <span className="viz-tip__label">{r.label}</span>
            <span className="viz-tip__change">{`${f(r.before)} → ${f(r.after)}`}</span>
            <span className="anx-delta" data-tone={r.tone}>
              {r.tone === 'flat'
                ? word.flat
                : `${r.pct > 0 ? '↑' : '↓'} ${ru(Math.abs(r.pct), 0)}% · ${word[r.tone]}`}
            </span>
          </div>
        ))}
        {rows.length === 0 && <div className="viz-tip__note">Нет данных до или после</div>}
        <div className="viz-tip__note">{effectNote(wb, wa)}</div>
      </div>
    );
  };

  return { metric, source, series, markers, unit: unitOf(kpi.format), tooltip, effect };
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
  const options = useMemo(() => ({ ...OPTIONS, unit: b.unit }), [b.unit]);
  return (
    <VizWave
      source={b.source}
      metric={b.metric}
      series={b.series}
      options={options}
      granularity="day"
      markers={b.markers}
      renderMarkerIcon={renderMarkerIcon}
      markerLabel={(x) => x.label}
      renderTooltip={b.tooltip}
      renderEffect={b.effect}
      navigator
      zoomControls
      size="l"
    />
  );
};

export default AnalyticsChart;
