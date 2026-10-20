import { useMemo, useState } from 'react';
import { cx } from '@/components/agents/ui';
import type { IEffDay, TEffMarkerKind } from '@/types/efficiency';
import { dayLabel, shortDay } from '@/components/agents/efficiency/format';
import {
  AXIS_PAD,
  MARKER_LABEL,
  METRICS,
  aaBand,
  chartMax,
  currentVsBaseline,
  deltaText,
  indexAtX,
  linePath,
  seriesOf,
  xAt,
  type IMarkerDay,
  type IMetric,
  type TVerdict,
} from '@/components/agents/efficiency/monitor-math';
import { Chip } from '@/components/agents/efficiency/primitives';
import useWidth from '@/components/agents/efficiency/use-width';
import { t } from '@/lib/i18n';

export const MARKER_FILL: Record<TEffMarkerKind, string> = {
  model: 'fill-(--t-accent-purple)',
  claude: 'fill-(--t-text-secondary)',
  plugins: 'fill-(--t-status-success)',
  memory: 'fill-(--t-status-warning)',
  rule: 'fill-(--t-accent-blue)',
  eval: 'fill-(--t-accent-teal)',
};

export const VERDICT_TONE: Record<TVerdict, 'ok' | 'bad' | 'flat'> = {
  good: 'ok',
  bad: 'bad',
  flat: 'flat',
  wait: 'flat',
};
const LANE_HEIGHT = 34;
const tickAnchor = (k: number, count: number): 'start' | 'middle' | 'end' => {
  if (k === 0) return 'start';
  return k === count - 1 ? 'end' : 'middle';
};
const ROW_HEIGHT = 58;

export const MarkerShape = ({ kind, x, y }: { kind: TEffMarkerKind; x: number; y: number }) => {
  const fill = MARKER_FILL[kind];
  if (kind === 'model')
    return <path className={fill} d={`M${x} ${y - 8} L${x + 7} ${y} L${x} ${y + 8} L${x - 7} ${y}Z`} />;
  if (kind === 'claude') return <rect className={fill} x={x - 1.5} y={y - 9} width={3} height={18} rx={1} />;
  if (kind === 'plugins') return <circle className={fill} cx={x} cy={y} r={5.5} />;
  if (kind === 'memory') return <rect className={fill} x={x - 5} y={y - 5} width={10} height={10} rx={1.5} />;
  if (kind === 'eval') return <circle className={cx(fill, 'opacity-80')} cx={x} cy={y} r={4} />;
  return <path className={fill} d={`M${x} ${y - 9} L${x + 7} ${y + 8} L${x - 7} ${y + 8}Z`} />;
};

export const MarkerLegend = () => (
  <div className="mb-3 flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-(--t-text-secondary)">
    {(Object.keys(MARKER_LABEL) as TEffMarkerKind[]).map((kind) => (
      <span key={kind} className="inline-flex items-center gap-1.5">
        <svg viewBox="0 0 16 20" className="h-4 w-3.5" aria-hidden="true">
          <MarkerShape kind={kind} x={8} y={10} />
        </svg>
        {MARKER_LABEL[kind]}
      </span>
    ))}
  </div>
);

interface IRowProps {
  metric: IMetric;
  days: IEffDay[];
  marks: IMarkerDay[];
  width: number;
  hover: number | null;
  setHover: (index: number | null) => void;
}

const MetricRow = ({ metric, days, marks, width, hover, setHover }: IRowProps) => {
  const values = useMemo(() => seriesOf(days, metric.key), [days, metric.key]);
  const legend = useMemo(() => currentVsBaseline(metric, days), [metric, days]);
  const band = useMemo(() => aaBand(values), [values]);
  const max = chartMax(metric, values);
  const y = (v: number) => ROW_HEIGHT - 6 - (v / max) * (ROW_HEIGHT - 14);
  const x = (i: number) => xAt(i, width, days.length);
  const shown = hover === null ? legend.current : values[hover];
  const last = values.findLastIndex((v) => v !== null);
  const barWidth = Math.max(2, (width - 2 * AXIS_PAD) / days.length - 3);
  const { effect } = legend;
  const hasEffect = effect.verdict !== 'wait';

  return (
    <div className="grid gap-x-4 gap-y-1 border-t border-(--t-border-subtle) py-2.5 md:grid-cols-[190px_minmax(0,1fr)]">
      <div className="flex items-baseline justify-between gap-2 md:block">
        <span className="block text-[12px] text-(--t-text-secondary)" title={metric.hint}>
          {metric.name}
        </span>
        <span className="flex items-baseline gap-2">
          <span className="text-xl font-bold tabular-nums">{metric.format(shown)}</span>
          {hover === null && hasEffect && (
            <Chip tone={VERDICT_TONE[effect.verdict]}>
              <span title={t('efficiency.monitor.deltaTitle')}>
                {deltaText(metric, legend.baseline, legend.current)}
                {effect.verdict === 'flat' && ` · ${t('efficiency.monitor.noise')}`}
              </span>
            </Chip>
          )}
        </span>
      </div>
      <svg
        viewBox={`0 0 ${width} ${ROW_HEIGHT}`}
        className="block w-full touch-pan-y"
        height={ROW_HEIGHT}
        role="img"
        aria-label={t('efficiency.monitor.metricByDay', { metric: metric.name })}
        onPointerMove={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          setHover(indexAtX(e.clientX - r.left, r.width, days.length));
        }}
        onPointerLeave={() => setHover(null)}
      >
        {band && (
          <rect
            className="fill-(--t-surface-input)"
            x={0}
            width={width}
            y={y(band.high)}
            height={Math.max(1, y(band.low) - y(band.high))}
          />
        )}
        {metric.line && (
          <g>
            <line
              className="stroke-(--t-text-muted)"
              x1={0}
              x2={width}
              y1={y(metric.line.value)}
              y2={y(metric.line.value)}
              strokeDasharray="4 4"
            />
            <text
              className="fill-(--t-text-muted)"
              x={width - 4}
              y={y(metric.line.value) - 3}
              fontSize={10}
              textAnchor="end"
            >
              {metric.line.label}
            </text>
          </g>
        )}
        {marks.map((m) => (
          <line
            key={m.day}
            className="stroke-(--t-border-strong)"
            x1={x(m.index)}
            x2={x(m.index)}
            y1={0}
            y2={ROW_HEIGHT}
          />
        ))}
        {metric.kind === 'bar' ? (
          values.map(
            (v, i) =>
              v !== null &&
              v > 0 && (
                <rect
                  key={days[i].day}
                  className="fill-(--t-accent-purple) opacity-80"
                  x={x(i) - barWidth / 2}
                  y={y(v)}
                  width={barWidth}
                  height={ROW_HEIGHT - 6 - y(v)}
                  rx={1.5}
                />
              ),
          )
        ) : (
          <path
            className="fill-none stroke-(--t-accent-purple)"
            d={linePath(values, x, y)}
            strokeWidth={2}
            strokeLinejoin="round"
            strokeLinecap="round"
          />
        )}
        {metric.kind === 'line' && last >= 0 && (
          <circle className="fill-(--t-accent-purple)" cx={x(last)} cy={y(values[last] as number)} r={3.2} />
        )}
        {hover !== null && (
          <line className="stroke-(--t-text-secondary)" x1={x(hover)} x2={x(hover)} y1={0} y2={ROW_HEIGHT} />
        )}
        {hover !== null && values[hover] !== null && (
          <circle
            className="fill-(--t-text) stroke-(--t-surface)"
            cx={x(hover)}
            cy={y(values[hover] as number)}
            r={3.5}
            strokeWidth={1.5}
          />
        )}
      </svg>
    </div>
  );
};

interface IMonitorProps {
  days: IEffDay[];
  marks: IMarkerDay[];
  onOpenDay: (day: IMarkerDay) => void;
}

/** Every metric on one time axis. Harness changes are vertical marks through all rows; the grey band is the first two weeks' spread. */
const Monitor = ({ days, marks, onOpenDay }: IMonitorProps) => {
  const [widthRef, width] = useWidth();
  const [hover, setHover] = useState<number | null>(null);
  const hoverMarks = hover === null ? [] : marks.filter((m) => m.index === hover).flatMap((m) => m.markers);
  const ticks =
    days.length > 1
      ? [0, Math.round((days.length - 1) / 3), Math.round(((days.length - 1) * 2) / 3), days.length - 1]
      : [0];

  return (
    <div>
      <MarkerLegend />
      <p className="mb-1 min-h-5 text-[12px] text-(--t-text-secondary)" aria-live="polite">
        {hover === null ? (
          t('efficiency.monitor.hoverHint')
        ) : (
          <>
            <b className="text-(--t-text)">{shortDay(days[hover].day)}</b>
            {hoverMarks.length > 0 && ` · ${hoverMarks.map((m) => m.title).join('; ')}`}
          </>
        )}
      </p>
      <div className="grid gap-x-4 gap-y-1 py-2.5 md:grid-cols-[190px_minmax(0,1fr)]">
        <div>
          <span className="block text-[12px] text-(--t-text-secondary)">
            {t('efficiency.monitor.harnessChanges')}
          </span>
          <span className="text-xl font-bold tabular-nums">
            {marks.reduce((a, m) => a + m.markers.length, 0)}
          </span>
        </div>
        <div ref={widthRef}>
          <svg
            viewBox={`0 0 ${width} ${LANE_HEIGHT}`}
            className="block w-full"
            height={LANE_HEIGHT}
            role="group"
            aria-label={t('efficiency.monitor.harnessChangesByDay')}
          >
            <line
              className="stroke-(--t-border-strong)"
              x1={0}
              x2={width}
              y1={LANE_HEIGHT / 2}
              y2={LANE_HEIGHT / 2}
            />
            {marks.map((m) => (
              <g
                key={m.day}
                className="cursor-pointer outline-none focus-visible:opacity-70"
                tabIndex={0}
                role="button"
                aria-label={`${shortDay(m.day)}: ${m.markers.map((mk) => mk.title).join('; ')}`}
                onClick={() => onOpenDay(m)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    onOpenDay(m);
                  }
                }}
              >
                <rect
                  x={xAt(m.index, width, days.length) - 9}
                  y={0}
                  width={18}
                  height={LANE_HEIGHT}
                  className="fill-transparent"
                />
                <MarkerShape kind={m.lead.kind} x={xAt(m.index, width, days.length)} y={LANE_HEIGHT / 2} />
                {m.markers.length > 1 && (
                  <text
                    className="fill-(--t-text-secondary)"
                    x={xAt(m.index, width, days.length) + 7}
                    y={10}
                    fontSize={9}
                  >
                    +{m.markers.length - 1}
                  </text>
                )}
                <title>{`${shortDay(m.day)} · ${m.markers.map((mk) => mk.title).join('; ')}`}</title>
              </g>
            ))}
          </svg>
        </div>
      </div>
      {METRICS.map((metric) => (
        <MetricRow
          key={metric.key}
          metric={metric}
          days={days}
          marks={marks}
          width={width}
          hover={hover}
          setHover={setHover}
        />
      ))}
      <div className="grid gap-x-4 md:grid-cols-[190px_minmax(0,1fr)]">
        <span className="hidden md:block" />
        <svg viewBox={`0 0 ${width} 18`} className="block w-full" height={18} aria-hidden="true">
          {ticks.map((i, k) => (
            <text
              key={i}
              className="fill-(--t-text-muted)"
              x={xAt(i, width, days.length)}
              y={13}
              fontSize={11}
              textAnchor={tickAnchor(k, ticks.length)}
            >
              {dayLabel(days[i].day)}
            </text>
          ))}
        </svg>
      </div>
    </div>
  );
};

export default Monitor;
