// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useRef, useState } from 'react';

import { overviewGranularity, type VizGranularity, type VizTimeRange } from '../model/time';
import type { VizBucketFrame } from '../model/types';
import type { VizTimeSource } from './source';

export const VIZ_DATA_STATUS = {
  /** Ни одного кадра ещё нет: скелетон. */
  loading: 'loading',
  ready: 'ready',
  /** Кадр на экране, догружается новый (детализация, метрика, окно). */
  refreshing: 'refreshing',
  /** Кадра нет, запрос упал. */
  error: 'error',
  /** Кадр на экране, а обновить его не вышло. */
  staleError: 'stale-error',
} as const;

export type VizDataStatus = (typeof VIZ_DATA_STATUS)[keyof typeof VIZ_DATA_STATUS];

export interface UseVizTimeDataOptions<Metric extends string> {
  readonly metric: Metric;
  readonly granularity: VizGranularity;
  /** Окно на экране. Запрос идёт с запасом `overscan` ширины окна с каждой стороны. */
  readonly range: VizTimeRange;
  readonly overscan?: number;
  /** Пауза перед запросом, пока окно ещё едет, мс. Первый запрос — без паузы. */
  readonly debounce?: number;
  readonly enabled?: boolean;
}

export interface VizTimeData<Id extends string> {
  /** Последний пришедший кадр — даже пока ждём другой детализации: график перетечёт в новый. */
  readonly frame: VizBucketFrame<Id> | null;
  readonly status: VizDataStatus;
  readonly error: unknown;
  /** Детализация, которую сейчас ждём. */
  readonly pending: VizGranularity | null;
  retry(): void;
}

interface Entry<Id extends string> {
  readonly frame: VizBucketFrame<Id>;
  /** Период, который мы спрашивали: кадр может обрываться на «сейчас», а покрытие — нет. */
  readonly asked: VizTimeRange;
}

interface Inflight {
  readonly key: string;
  readonly asked: VizTimeRange;
  readonly controller: AbortController;
}

const within = (outer: VizTimeRange, inner: VizTimeRange) =>
  outer.start <= inner.start + 1 && outer.end >= inner.end - 1;

type State<Id extends string> = Omit<VizTimeData<Id>, 'retry'>;

/**
 * Кадр для окна. Спрашивает источник, только когда метрика, детализация или окно вышли за то,
 * что уже есть; отменяет устаревший запрос; держит прошлый кадр, пока не пришёл новый.
 */
export function useVizTimeData<Id extends string, Metric extends string>(
  source: VizTimeSource<Id, Metric>,
  options: UseVizTimeDataOptions<Metric>,
): VizTimeData<Id> {
  const { metric, granularity, range, overscan = 1, debounce = 120, enabled = true } = options;
  const [state, setState] = useState<State<Id>>({
    frame: null,
    status: VIZ_DATA_STATUS.loading,
    error: null,
    pending: granularity,
  });
  const cache = useRef(new Map<string, Entry<Id>>());
  const inflight = useRef<Inflight | null>(null);
  const hasFrame = useRef(false);
  const [attempt, setAttempt] = useState(0);
  const handledAttempt = useRef(0);
  /** Что спрашивали в прошлый раз: источник сменился, а окно и метрика те же — кадр не мигает «обновляется». */
  const lastAsk = useRef<{
    source: VizTimeSource<Id, Metric>;
    key: string;
    start: number;
    end: number;
  } | null>(null);
  const retry = useCallback(() => setAttempt((n) => n + 1), []);

  useEffect(() => {
    cache.current.clear();
    inflight.current?.controller.abort();
    inflight.current = null;
  }, [source]);

  useEffect(() => {
    if (!enabled) return;
    const extent = source.extent;
    const want = { start: Math.max(extent.start, range.start), end: Math.min(extent.end, range.end) };
    const key = `${metric}|${granularity}`;
    const forced = attempt !== handledAttempt.current;
    handledAttempt.current = attempt;
    const last = lastAsk.current;
    const swapped =
      hasFrame.current &&
      !!last &&
      last.source !== source &&
      last.key === key &&
      last.start === range.start &&
      last.end === range.end;
    lastAsk.current = { source, key, start: range.start, end: range.end };
    const cached = cache.current.get(key);
    if (!forced && cached && within(cached.asked, want)) {
      inflight.current?.controller.abort();
      inflight.current = null;
      hasFrame.current = true;
      setState((s) =>
        s.frame === cached.frame && s.status === VIZ_DATA_STATUS.ready
          ? s
          : { frame: cached.frame, status: VIZ_DATA_STATUS.ready, error: null, pending: null },
      );
      return;
    }
    if (!forced && inflight.current?.key === key && within(inflight.current.asked, want)) return;
    if (!swapped)
      setState((s) => ({
        ...s,
        pending: granularity,
        status: s.frame ? VIZ_DATA_STATUS.refreshing : VIZ_DATA_STATUS.loading,
      }));
    const span = range.end - range.start;
    const asked = {
      start: Math.max(extent.start, range.start - span * overscan),
      end: Math.min(extent.end, range.end + span * overscan),
    };
    const timer = setTimeout(
      () => {
        inflight.current?.controller.abort();
        const controller = new AbortController();
        inflight.current = { key, asked, controller };
        source.load({ metric, granularity, range: asked, signal: controller.signal }).then(
          (frame) => {
            if (controller.signal.aborted) return;
            cache.current.set(key, { frame, asked });
            inflight.current = null;
            hasFrame.current = true;
            setState({ frame, status: VIZ_DATA_STATUS.ready, error: null, pending: null });
          },
          (error: unknown) => {
            if (controller.signal.aborted || (error instanceof Error && error.name === 'AbortError')) return;
            inflight.current = null;
            setState((s) => ({
              ...s,
              status: s.frame ? VIZ_DATA_STATUS.staleError : VIZ_DATA_STATUS.error,
              error,
              pending: null,
            }));
          },
        );
      },
      hasFrame.current && !forced && !swapped ? debounce : 0,
    );
    return () => clearTimeout(timer);
  }, [source, metric, granularity, range.start, range.end, overscan, debounce, enabled, attempt]);

  useEffect(() => () => inflight.current?.controller.abort(), []);

  return { ...state, retry };
}

/** Кадр всей истории для полосы «весь период»: одна детализация на всё, один запрос. */
export function useVizOverviewData<Id extends string, Metric extends string>(
  source: VizTimeSource<Id, Metric>,
  metric: Metric,
  enabled = true,
): VizTimeData<Id> {
  return useVizTimeData(source, {
    metric,
    granularity: overviewGranularity(source.extent),
    range: source.extent,
    overscan: 0,
    debounce: 0,
    enabled,
  });
}
