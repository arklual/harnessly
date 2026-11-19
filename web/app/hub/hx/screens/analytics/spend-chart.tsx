// The spend by day, stacked by team or model, on the chart library's wave board. Its own module: the library is large.
import { useMemo } from 'react';

import {
  createSource,
  formatBucketTitle,
  VizWave,
  type VizSeries,
  type VizUnit,
  type WaveEngineOptions,
  type WaveTooltipContext,
} from '@viz';

import type { DailySpend } from '../../../org/api/analytics';
import { parseDay } from '../../../org/dates';
import { inRubles, shownOfUsd } from '../../../org/money';
import { money } from './calc';
import { teamSlot } from './colors';

const HOUR = 36e5;
const DAY = 864e5;

const OPTIONS: Partial<WaveEngineOptions> = {
  mode: 'stack',
  zones: false,
  points: 'none',
  peak: false,
  gradient: 0.3,
};

const dayStart = (iso: string): number => parseDay(iso).getTime() - 3 * HOUR;

const unit: VizUnit =
  inRubles() && shownOfUsd(1) !== null
    ? { kind: 'currency', currency: 'RUB' }
    : { kind: 'currency', currency: 'USD' };

export interface SpendChartProps {
  spend: DailySpend;
  /** The chart's lines named for the screen: a team's id is not its name. */
  names: ReadonlyMap<string, string>;
  teamOrder: readonly string[];
}

const SLOTS: readonly VizSeries['slot'][] = [0, 2, 1];

const SpendChart = ({ spend, names, teamOrder }: SpendChartProps) => {
  const built = useMemo(() => {
    const n = spend.days.length;
    const first = dayStart(spend.days[0] ?? '1970-01-01');
    const rate = shownOfUsd(1) ?? 1;
    const lines = spend.lines.map((l, i) => {
      const label = names.get(l.id) ?? (l.id === '' ? 'Без команды' : l.id);
      const slot: VizSeries['slot'] =
        spend.split === 'team' ? teamSlot(teamOrder, label) : (SLOTS[i] ?? 'other');
      return { id: `l:${l.id}`, label, slot, values: l.values };
    });
    const values: Record<string, Float64Array> = {};
    for (const l of lines) {
      const a = new Float64Array(n * 24);
      l.values.forEach((v, d) => {
        a[d * 24 + 12] = v * rate;
      });
      values[l.id] = a;
    }
    const metric = `spend-${spend.split}`;
    const source = createSource<string, string>({
      hourly: { [metric]: { start: first, length: n * 24, values } },
      extent: { start: first, end: first + n * DAY, now: first + n * DAY },
      latency: 0,
    });
    const series: VizSeries[] = lines.map((l) => ({ id: l.id, label: l.label, slot: l.slot }));
    const tooltip = (ctx: WaveTooltipContext) => {
      const rows = ctx.values.filter((r) => Number.isFinite(r.value) && r.value > 0);
      rows.sort((a, b) => b.value - a.value);
      const sum = rows.reduce((s, r) => s + r.value, 0);
      return (
        <div className="viz-tip">
          <div className="viz-tip__title">
            <span>{formatBucketTitle(ctx.start, ctx.granularity, ctx.step)}</span>
          </div>
          {rows.map((r) => (
            <div key={r.id} className="viz-tip__row">
              <svg className="viz-swatch" viewBox="0 0 10 10" aria-hidden="true">
                <circle cx="5" cy="5" r="4" fill={r.color} stroke={r.color} strokeWidth="1.5" />
              </svg>
              <span className="viz-tip__label">{r.label}</span>
              <b className="viz-tip__value">{money(r.value / rate)}</b>
            </div>
          ))}
          <div className="viz-tip__total">
            <span>Всего за день</span>
            <b>{money(sum / rate)}</b>
          </div>
        </div>
      );
    };
    return { metric, source, series, tooltip };
  }, [spend, names, teamOrder]);
  const options = useMemo(() => ({ ...OPTIONS, unit }), []);
  return (
    <VizWave
      source={built.source}
      metric={built.metric}
      series={built.series}
      options={options}
      granularity="day"
      renderTooltip={built.tooltip}
      navigator
      zoomControls
      size="l"
    />
  );
};

export default SpendChart;
