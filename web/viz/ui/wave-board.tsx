// SPDX-License-Identifier: Apache-2.0
import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react';

import { classNames } from '../internal/cx';
import { useVizOverviewData, useVizTimeData, VIZ_DATA_STATUS } from '../data/use-viz-time-data';
import type { VizTimeSource } from '../data/source';
import {
  type VizTimeEngine,
  WAVE_STATUS,
  type WaveEngineOptions,
  type WaveStatus,
} from '../engine/time-engine';
import { vizSeriesColor } from '../model/palette';
import type { GranularityThresholds, VizGranularity, VizTimeRange } from '../model/time';
import type { VizBucketFrame, VizMarker, VizSeries, VizStackMode, VizZone } from '../model/types';
import {
  useElementWidth,
  useVizGranularity,
  useVizSeriesState,
  useVizTheme,
  useVizViewport,
  useVizViewportTarget,
} from './hooks';
import { VizCard } from './viz-card';
import { VizLegend } from './viz-legend';
import { VizNavigator } from './viz-navigator';
import { VizStatus } from './viz-status';
import { VizZoomControls, type VizZoomLabels } from './viz-zoom-controls';
import {
  type VizChartSeries,
  createColumnEngine,
  createWaveEngine,
  type VizEngineFactory,
  VizTimeChart,
  type WaveChartLabels,
  type WaveChartSize,
  type WaveEffectContext,
  type WaveSummaryContext,
  type WaveTooltipContext,
} from './wave-chart';

import '../viz.css';

export interface VizStateCopy {
  readonly title: string;
  readonly description?: string;
}

export interface VizLiveEvent<Id extends string = string> {
  readonly key: string;
  readonly id: Id;
  readonly t: number;
  readonly text?: string;
}

export interface WaveBoardLabels {
  readonly empty: VizStateCopy;
  readonly error: VizStateCopy;
  readonly staleError: string;
  readonly retry: string;
  readonly event: string;
  readonly navigator: string;
  readonly legend: string;
}

export const DEFAULT_WAVE_BOARD_LABELS: WaveBoardLabels = {
  empty: { title: 'Данных пока нет', description: 'График появится с первыми данными.' },
  error: {
    title: 'Не удалось загрузить данные',
    description: 'Произошла ошибка на нашей стороне. Попробуйте ещё раз.',
  },
  staleError: 'Не удалось обновить — показаны прошлые данные',
  retry: 'Повторить',
  event: 'мероприятие',
  navigator: 'Весь период',
  legend: 'Серии графика',
};

export interface WaveBoardProps<
  Id extends string = string,
  Metric extends string = string,
  Kind extends string = string,
> {
  readonly title?: ReactNode;
  /** Рядом с названием: «?» с объяснением метрики. */
  readonly info?: ReactNode;
  readonly description?: ReactNode;
  /** Свои действия в шапке — перед кнопками зума: переключатель метрики, меню. */
  readonly actions?: ReactNode;
  readonly source: VizTimeSource<Id, Metric>;
  readonly metric: Metric;
  readonly series: readonly VizSeries<Id>[];
  readonly options?: Partial<WaveEngineOptions>;
  /** Скрытые с самого начала. */
  readonly initialHidden?: readonly Id[];
  /** Окно на старте. По умолчанию — вся история. */
  readonly initialRange?: VizTimeRange;
  /** Окно снаружи: при смене значения график переходит к нему. */
  readonly range?: VizTimeRange;
  /** Серия, подсвеченная снаружи (например, строкой таблицы). Легенда не перебивает. */
  readonly focus?: Id | null;
  readonly onFocusChange?: (id: Id | null) => void;
  /** Новые события: отрезок вспыхивает, при `text` над ним всплывает подпись. Ключ `key` не повторяется. */
  readonly live?: readonly VizLiveEvent<Id>[];
  /** Детализация вручную; по умолчанию — по ширине окна. */
  readonly granularity?: VizGranularity;
  readonly granularityThresholds?: Partial<Record<VizGranularity, GranularityThresholds>>;
  readonly zones?: readonly VizZone[];
  readonly markers?: readonly VizMarker<Id, Kind>[];
  readonly renderMarkerIcon?: (marker: VizMarker<Id, Kind>) => ReactNode;
  readonly markerLabel?: (marker: VizMarker<Id, Kind>) => string;
  readonly renderTooltip?: ((context: WaveTooltipContext<Id, Kind>) => ReactNode) | null;
  readonly renderSummary?: (context: WaveSummaryContext<Id>) => ReactNode;
  readonly renderEffect?: ((context: WaveEffectContext<Id, Kind>) => ReactNode) | null;
  readonly onZoneDraft?: (range: VizTimeRange | null, done: boolean) => void;
  readonly onZoneRemove?: (id: string) => void;
  /** Полоса «весь период» под графиком. */
  readonly navigator?: boolean;
  readonly zoomControls?: boolean;
  readonly size?: WaveChartSize;
  readonly labels?: Partial<WaveBoardLabels>;
  readonly chartLabels?: Partial<WaveChartLabels>;
  /** Пауза подсказки наведения после закрытия закреплённой кликом, мс (`WaveChart.tooltipDelay`). */
  readonly tooltipDelay?: number;
  readonly zoomLabels?: Partial<VizZoomLabels>;
  /** Карточка или блок без рамки внутри карточки страницы. */
  readonly variant?: 'card' | 'plain';
  readonly className?: string;
}

/** Все значения кадра — пропуски или нули: показываем «данных нет», а не плоскую линию. */
export function frameIsEmpty(frame: VizBucketFrame): boolean {
  for (const values of Object.values<ArrayLike<number>>(frame.values)) {
    for (let k = 0; k < values.length; k++) if (values[k] > 0) return false;
  }
  return true;
}

/** Высота оси Y и подписи слева съедают около 60 px — по ним детализация не считается. */
const AXIS_ALLOWANCE = 60;

export type ColumnBoardProps<
  Id extends string = string,
  Metric extends string = string,
  Kind extends string = string,
> = WaveBoardProps<Id, Metric, Kind>;

/**
 * Пороги детализации столбиков. Столбик уже ~4 px не читается, а в группе в отрезок встают все
 * видимые серии — поэтому чем больше серий рядом, тем позже график переходит на дни и часы.
 */
export function columnGranularityThresholds(
  mode: VizStackMode,
  visibleSeries: number,
): Partial<Record<VizGranularity, GranularityThresholds>> {
  const side = mode === 'overlay' ? Math.max(1, visibleSeries) : 1;
  const enter = Math.max(10, side * 5 + 4);
  return { hour: { enter: enter + 4, leave: enter + 1 }, day: { enter, leave: enter - 2 } };
}

/**
 * Готовый блок доски с волновым графиком: карточка с названием, легенда, график, полоса
 * «весь период», кнопки зума и состояния загрузки. Данные спрашивает у источника сам.
 */
export function WaveBoard<
  Id extends string = string,
  Metric extends string = string,
  Kind extends string = string,
>(props: WaveBoardProps<Id, Metric, Kind>) {
  return <VizTimeBoard<Id, Metric, Kind> {...props} createEngine={createWaveEngine} />;
}

/** Тот же блок со столбчатым графиком: группой, стопкой и долями. */
export function ColumnBoard<
  Id extends string = string,
  Metric extends string = string,
  Kind extends string = string,
>(props: ColumnBoardProps<Id, Metric, Kind>) {
  return (
    <VizTimeBoard<Id, Metric, Kind>
      thresholdsFor={columnGranularityThresholds}
      {...props}
      createEngine={createColumnEngine}
    />
  );
}

/**
 * Готовый блок доски: карточка с названием, легенда, график времени (волны или столбики), полоса «весь период»,
 * кнопки зума и состояния загрузки. Данные спрашивает у источника сам: подбирает детализацию
 * под окно, отменяет устаревшие запросы, держит прошлый кадр, пока идёт новый.
 */
export function VizTimeBoard<
  Id extends string = string,
  Metric extends string = string,
  Kind extends string = string,
>(
  props: WaveBoardProps<Id, Metric, Kind> & {
    readonly createEngine: VizEngineFactory;
    /** Пороги детализации по режиму и числу видимых серий (столбики); иначе — `granularityThresholds`. */
    readonly thresholdsFor?: (
      mode: VizStackMode,
      visibleSeries: number,
    ) => Partial<Record<VizGranularity, GranularityThresholds>>;
  },
) {
  const {
    title,
    info,
    description,
    actions,
    source,
    metric,
    options,
    initialHidden,
    initialRange,
    zones,
    markers,
    renderMarkerIcon,
    navigator = true,
    zoomControls = true,
    size,
    variant,
    className,
  } = props;
  const labels = { ...DEFAULT_WAVE_BOARD_LABELS, ...props.labels };
  const theme = useVizTheme();
  const extent = source.extent;
  const viewport = useVizViewport({ extent, initial: props.range ?? initialRange });
  const rangeKey = props.range ? `${props.range.start}|${props.range.end}` : null;
  const rangeSeen = useRef(rangeKey);
  useEffect(() => {
    if (!props.range || rangeSeen.current === rangeKey) return;
    rangeSeen.current = rangeKey;
    viewport.setRange(props.range);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rangeKey, viewport]);
  const target = useVizViewportTarget(viewport);
  const [measure, width] = useElementWidth<HTMLDivElement>();
  const [engine, setEngine] = useState<VizTimeEngine | null>(null);
  const [selectedMarker, setSelectedMarker] = useState<string | null>(null);

  const seenLive = useRef(new Set((props.live ?? []).map((e) => e.key)));
  useEffect(() => {
    if (!engine || !props.live) return;
    const fresh = props.live.filter((e) => !seenLive.current.has(e.key));
    if (fresh.length === 0) return;
    for (const e of fresh) seenLive.current.add(e.key);
    engine.pulse(fresh.map((e) => ({ id: e.id, t: e.t })));
    const labelled = fresh.flatMap((e) => (e.text ? [{ id: e.id, t: e.t, text: e.text }] : []));
    if (labelled.length > 0) engine.float(labelled);
  }, [engine, props.live]);

  const series = useMemo<VizChartSeries<Id>[]>(
    () =>
      props.series.map((s, i) => ({ id: s.id, label: s.label, color: vizSeriesColor(s.slot ?? i, theme) })),
    [props.series, theme],
  );
  const ids = useMemo(() => series.map((s) => s.id), [series]);
  const state = useVizSeriesState(ids, initialHidden);
  const focus = state.focus ?? props.focus ?? null;
  const setFocus = (id: Id | null) => {
    state.setFocus(id);
    props.onFocusChange?.(id);
  };
  const visibleSeries = ids.length - state.hidden.size;
  const thresholds =
    props.granularityThresholds ?? props.thresholdsFor?.(options?.mode ?? 'overlay', visibleSeries);
  const granularity = useVizGranularity(target, Math.max(0, width - AXIS_ALLOWANCE), {
    fixed: props.granularity,
    thresholds,
  });
  const data = useVizTimeData(source, { metric, granularity, range: target });
  const overview = useVizOverviewData(source, metric, navigator);

  let status: WaveStatus = WAVE_STATUS.ready;
  if (data.status === VIZ_DATA_STATUS.loading) status = WAVE_STATUS.loading;
  else if (data.status === VIZ_DATA_STATUS.error) status = WAVE_STATUS.error;
  else if (data.frame && frameIsEmpty(data.frame)) status = WAVE_STATUS.empty;

  let overlay: ReactNode = null;
  if (status === WAVE_STATUS.error)
    overlay = <VizStatus kind="error" {...labels.error} onRetry={data.retry} retryLabel={labels.retry} />;
  else if (status === WAVE_STATUS.empty) overlay = <VizStatus kind="empty" {...labels.empty} />;

  const interactive = status === WAVE_STATUS.ready || status === WAVE_STATUS.empty;

  return (
    <VizCard
      title={title}
      info={info}
      description={description}
      variant={variant}
      className={classNames('viz-board', className)}
      actions={
        actions || zoomControls ? (
          <>
            {actions}
            {zoomControls ? (
              <VizZoomControls viewport={viewport} disabled={!interactive} labels={props.zoomLabels} />
            ) : null}
          </>
        ) : undefined
      }
      legend={
        <VizLegend
          items={series}
          hidden={state.hidden}
          focus={focus}
          onToggle={state.toggle}
          onSolo={state.solo}
          onFocus={setFocus}
          loading={status === WAVE_STATUS.loading}
          aria-label={labels.legend}
        />
      }
      footer={
        navigator ? (
          <VizNavigator
            viewport={viewport}
            series={series}
            frame={overview.frame}
            ready={!!overview.frame && interactive}
            refreshing={overview.status === VIZ_DATA_STATUS.refreshing}
            hidden={state.hidden}
            zones={zones}
            showZones={options?.zones ?? true}
            now={extent.now}
            eventAt={extent.event ?? null}
            eventLabel={labels.event}
            markers={markers}
            renderMarkerIcon={renderMarkerIcon}
            onMarkerSelect={(m) => setSelectedMarker(m.id)}
            alignTo={engine}
            timeZone={options?.timeZone}
            slow={options?.slow}
            aria-label={labels.navigator}
          />
        ) : undefined
      }
    >
      {data.status === VIZ_DATA_STATUS.staleError ? (
        <div className="viz-board__stale" role="status">
          <span>{labels.staleError}</span>
          <button type="button" className="viz-board__retry" onClick={data.retry}>
            {labels.retry}
          </button>
        </div>
      ) : null}
      <div ref={measure}>
        <VizTimeChart<Id, Kind>
          createEngine={props.createEngine}
          series={series}
          frame={data.frame}
          status={status}
          refreshing={data.status === VIZ_DATA_STATUS.refreshing}
          viewport={viewport}
          options={options}
          hidden={state.hidden}
          focus={focus}
          onSeriesHover={props.onFocusChange}
          scaleKey={String(metric)}
          zones={zones}
          markers={markers}
          renderMarkerIcon={renderMarkerIcon}
          markerLabel={props.markerLabel}
          now={extent.now}
          renderTooltip={props.renderTooltip}
          renderSummary={props.renderSummary}
          renderEffect={props.renderEffect}
          onZoneDraft={props.onZoneDraft}
          onZoneRemove={props.onZoneRemove}
          selectedMarker={selectedMarker}
          onSelectedMarkerChange={setSelectedMarker}
          labels={props.chartLabels}
          tooltipDelay={props.tooltipDelay}
          size={size}
          overlay={overlay}
          engineRef={setEngine}
          aria-label={typeof title === 'string' ? title : labels.legend}
        />
      </div>
    </VizCard>
  );
}
