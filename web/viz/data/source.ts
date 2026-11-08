// SPDX-License-Identifier: Apache-2.0
import { bucketize, type VizHourlySeries } from '../model/aggregate';
import { MOSCOW_ZONE, type VizGranularity, type VizTimeRange, type VizTimeZone } from '../model/time';
import type { VizBucketFrame, VizTimeExtent } from '../model/types';

/** Запрос окна: какие ряды, какой детализации и за какой период. */
export interface VizTimeRequest<Metric extends string = string> {
  readonly metric: Metric;
  readonly granularity: VizGranularity;
  /** Период с запасом по краям — чтобы сдвиг окна не упирался в край данных. */
  readonly range: VizTimeRange;
  /** Отменяется, когда запрос устарел: сменили метрику, детализацию или ушли окном. */
  readonly signal: AbortSignal;
}

/**
 * Источник временных рядов. Продукт реализует `load` своим запросом к бэкенду; библиотека
 * решает, когда спрашивать, отменяет устаревшие запросы и держит прошлый кадр на экране,
 * пока новый не пришёл. Кадр может покрывать больше запрошенного — например, всю историю.
 */
export interface VizTimeSource<Id extends string = string, Metric extends string = string> {
  readonly extent: VizTimeExtent;
  load(request: VizTimeRequest<Metric>): Promise<VizBucketFrame<Id>>;
}

export interface LocalSourceOptions<Id extends string, Metric extends string> {
  /** Часовые ряды каждой метрики: детализация собирается на клиенте. */
  readonly hourly: Readonly<Record<Metric, VizHourlySeries<Id>>>;
  readonly extent: VizTimeExtent;
  readonly zone?: VizTimeZone;
  /** Искусственная задержка ответа, мс — для сторис и проверки состояний загрузки. */
  readonly latency?: number | ((request: VizTimeRequest<Metric>) => number);
  /** Сбой ответа — для сторис ошибок. */
  readonly fail?: (request: VizTimeRequest<Metric>) => boolean;
}

function abortError(): Error {
  const error = new Error('Запрос отменён');
  error.name = 'AbortError';
  return error;
}

/**
 * Источник поверх данных, которые уже на клиенте: детализации собираются из часов и
 * запоминаются. Отвечает всей историей сразу — окно потом не перезапрашивается.
 */
export function createLocalSource<Id extends string, Metric extends string>(
  options: LocalSourceOptions<Id, Metric>,
): VizTimeSource<Id, Metric> {
  const zone = options.zone ?? MOSCOW_ZONE;
  const cache = new Map<string, VizBucketFrame<Id>>();
  const build = (metric: Metric, granularity: VizGranularity) => {
    const key = `${metric}|${granularity}`;
    let frame = cache.get(key);
    if (!frame) {
      frame = bucketize(options.hourly[metric], granularity, zone);
      cache.set(key, frame);
    }
    return frame;
  };
  return {
    extent: options.extent,
    load(request) {
      const delay = typeof options.latency === 'function' ? options.latency(request) : (options.latency ?? 0);
      return new Promise<VizBucketFrame<Id>>((resolve, reject) => {
        if (request.signal.aborted) {
          reject(abortError());
          return;
        }
        const finish = () => {
          request.signal.removeEventListener('abort', cancel);
          if (options.fail?.(request)) reject(new Error('Источник не ответил'));
          else resolve(build(request.metric, request.granularity));
        };
        const timer = delay > 0 ? setTimeout(finish, delay) : null;
        const cancel = () => {
          if (timer) clearTimeout(timer);
          reject(abortError());
        };
        request.signal.addEventListener('abort', cancel, { once: true });
        if (!timer) queueMicrotask(finish);
      });
    },
  };
}

/** Период, который покрывает кадр. */
export function frameCoverage(frame: VizBucketFrame): VizTimeRange {
  return { start: frame.start, end: frame.start + frame.length * frame.step };
}

/** Короткое имя `createLocalSource` для публичного входа. */
export const createSource = createLocalSource;
