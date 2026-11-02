// The big chart of the selected KPI (ECharts, its own chunk: the screen loads it lazily). Two views: lines by team
// (or by person), and the period over the compare period. A range slider under the plot is the minimap; the change
// markers sit above the plot. Colours are resolved to rgb() — zrender cannot do oklch.
import { useEffect, useMemo, useRef } from 'react';
import { LineChart } from 'echarts/charts';
import { DataZoomComponent, GridComponent, MarkPointComponent, TooltipComponent } from 'echarts/components';
import * as echarts from 'echarts/core';
import type { EChartsCoreOption } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';

import { oklchToRgb, withAlpha } from '../../../../components/agents/stats/chart-theme';
import type { KpiChart, KpiFormat } from '../../api/analytics';
import { dayOfMonth, monthShort } from '../../dates';
import { lineColors } from './chart-colors';
import { formatValue } from './format';

echarts.use([
  LineChart,
  GridComponent,
  TooltipComponent,
  DataZoomComponent,
  MarkPointComponent,
  CanvasRenderer,
]);

export type ChartMode = 'teams' | 'compare';
/** What the buttons beside the legend call: zoom the plot by a factor (<1 in, >1 out). */
export interface ZoomHandle {
  by: (factor: number) => void;
}
/** The visible part of the range when the chart opens: the last five weeks. */
const INITIAL_DAYS = 35;

const MARK_GLYPH = { tool: 'Т', model: 'М', plugins: 'П' } as const;

const css = (name: string): string =>
  oklchToRgb(getComputedStyle(document.documentElement).getPropertyValue(name));

const dayLabel = (iso: string): string => `${dayOfMonth(iso)} ${monthShort(iso)}`;

interface Props {
  chart: KpiChart;
  format: KpiFormat;
  /** Whether the lines add up (money) — then they are stacked. */
  stacked: boolean;
  mode: ChartMode;
  dark: boolean;
  zoomRef: { current: ZoomHandle | null };
  ariaLabel: string;
  compareName: string;
  periodName: string;
}

/** A day none of the lines knows stays a gap; a mean goes over the lines that know it. */
const total = (lines: KpiChart['lines'], i: number, stacked: boolean): number | null => {
  const known = lines.flatMap((l) => {
    const v = l.values[i];
    return v === null || v === undefined ? [] : [v];
  });
  if (known.length === 0) return null;
  const sum = known.reduce((s, v) => s + v, 0);
  return stacked ? sum : sum / known.length;
};

const knownOnly = (values: (number | null)[]): number[] => values.flatMap((v) => (v === null ? [] : [v]));

export default function KpiChartView({
  chart,
  format,
  stacked,
  mode,
  dark,
  zoomRef,
  ariaLabel,
  compareName,
  periodName,
}: Props) {
  const host = useRef<HTMLDivElement>(null);
  const inst = useRef<echarts.ECharts | null>(null);
  const span = useRef({ start: 0, end: 100 });

  const option = useMemo<EChartsCoreOption>(() => {
    const colors = lineColors(dark);
    const text = css('--k-text-secondary');
    const grid = css('--k-border');
    const muted = css('--k-text-muted');
    const surface = css('--k-surface');
    const fmt = (v: number) => formatValue(format, v);
    const base = {
      animationDuration: 500,
      animationEasing: 'cubicOut' as const,
      textStyle: { fontFamily: 'inherit', color: text },
      tooltip: {
        trigger: 'axis',
        backgroundColor: surface,
        borderColor: grid,
        textStyle: { color: css('--k-text-primary'), fontSize: 12 },
        valueFormatter: (v: unknown) => (typeof v === 'number' ? fmt(v) : '—'),
      },
    };
    if (mode === 'compare') {
      const from = chart.periodFrom;
      const n = chart.days.length - from;
      const now = Array.from({ length: n }, (_, i) => total(chart.lines, from + i, stacked));
      const cmp = chart.compareLine.slice(0, n);
      const labels = Array.from({ length: n }, (_, i) => dayLabel(chart.days[from + i] ?? ''));
      const peak = Math.max(...knownOnly(now), ...knownOnly(cmp), 1);
      return {
        ...base,
        grid: { left: 48, right: 16, top: 24, bottom: 28 },
        xAxis: {
          type: 'category',
          data: labels,
          boundaryGap: false,
          axisLine: { lineStyle: { color: grid } },
          axisLabel: { color: muted, fontSize: 11 },
          axisTick: { show: false },
        },
        yAxis: {
          type: 'value',
          max: peak * 1.15,
          axisLabel: { color: muted, fontSize: 11, formatter: (v: number) => fmt(v) },
          splitLine: { lineStyle: { color: grid } },
        },
        series: [
          {
            name: compareName,
            type: 'line',
            data: cmp,
            smooth: true,
            showSymbol: false,
            lineStyle: { color: muted, type: 'dashed', width: 2 },
            itemStyle: { color: muted },
          },
          {
            name: periodName,
            type: 'line',
            data: now,
            smooth: true,
            showSymbol: false,
            lineStyle: { color: colors[0], width: 2.5 },
            itemStyle: { color: colors[0] },
            areaStyle: { color: withAlpha(colors[0] ?? '#6d49f4', 0.12) },
          },
        ],
      };
    }
    const peak = Math.max(
      1,
      ...chart.days.map((_, i) =>
        stacked
          ? chart.lines.reduce((s, l) => s + (l.values[i] ?? 0), 0)
          : Math.max(...chart.lines.map((l) => l.values[i] ?? 0)),
      ),
    );
    const max = peak * 1.18;
    const open = Math.max(0, (1 - INITIAL_DAYS / chart.days.length) * 100);
    const first = chart.lines[0];
    return {
      ...base,
      grid: { left: 48, right: 16, top: 36, bottom: 64 },
      xAxis: {
        type: 'category',
        data: chart.days.map(dayLabel),
        boundaryGap: false,
        axisLine: { lineStyle: { color: grid } },
        axisLabel: { color: muted, fontSize: 11 },
        axisTick: { show: false },
      },
      yAxis: {
        type: 'value',
        max,
        axisLabel: { color: muted, fontSize: 11, formatter: (v: number) => fmt(v) },
        splitLine: { lineStyle: { color: grid } },
      },
      dataZoom: [
        { type: 'inside', start: open, end: 100 },
        {
          type: 'slider',
          start: open,
          end: 100,
          height: 28,
          bottom: 8,
          borderColor: grid,
          fillerColor: withAlpha(colors[0] ?? '#6d49f4', 0.14),
          handleSize: '90%',
          textStyle: { color: muted },
          dataBackground: {
            lineStyle: { color: muted, opacity: 0.4 },
            areaStyle: { color: grid, opacity: 0.4 },
          },
        },
      ],
      series: chart.lines.map((l, i) => ({
        name: l.name,
        type: 'line',
        data: l.values,
        stack: stacked ? 'all' : undefined,
        smooth: true,
        showSymbol: false,
        lineStyle: { color: colors[i % colors.length], width: 2 },
        itemStyle: { color: colors[i % colors.length] },
        areaStyle: { color: withAlpha(colors[i % colors.length] ?? '#6d49f4', stacked ? 0.16 : 0.07) },
        emphasis: { focus: 'series' },
        markPoint:
          i === 0 && first
            ? {
                animation: false,
                silent: false,
                symbol: 'circle',
                symbolSize: 18,
                itemStyle: { color: surface, borderColor: muted, borderWidth: 1.2 },
                label: { color: text, fontSize: 10, fontWeight: 600 },
                tooltip: { formatter: (p: { name?: string }) => p.name ?? '' },
                data: chart.markers.map((m) => ({
                  name: m.title,
                  coord: [dayLabel(chart.days[m.index] ?? ''), max],
                  label: { show: true, formatter: MARK_GLYPH[m.kind] },
                })),
              }
            : undefined,
      })),
    };
  }, [chart, format, stacked, mode, dark, compareName, periodName]);

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const instance = echarts.init(el, undefined, { renderer: 'canvas' });
    inst.current = instance;
    instance.on('datazoom', () => {
      const dz = (instance.getOption() as { dataZoom?: { start: number; end: number }[] }).dataZoom?.[0];
      if (dz) span.current = { start: dz.start, end: dz.end };
    });
    zoomRef.current = {
      by: (factor) => {
        const { start, end } = span.current;
        const mid = (start + end) / 2;
        const half = Math.min(50, Math.max(4, ((end - start) / 2) * factor));
        const next = { start: Math.max(0, mid - half), end: Math.min(100, mid + half) };
        span.current = next;
        instance.dispatchAction({ type: 'dataZoom', ...next });
      },
    };
    const ro = new ResizeObserver(() => instance.resize());
    ro.observe(el);
    return () => {
      zoomRef.current = null;
      ro.disconnect();
      instance.dispose();
      inst.current = null;
    };
  }, [zoomRef]);

  useEffect(() => {
    inst.current?.setOption(option, { notMerge: true });
    const dz = (option as { dataZoom?: { start: number; end: number }[] }).dataZoom?.[0];
    span.current = dz ? { start: dz.start, end: dz.end } : { start: 0, end: 100 };
  }, [option]);

  return <div ref={host} className="an-plot" role="img" aria-label={ariaLabel} />;
}
