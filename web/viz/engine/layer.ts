// SPDX-License-Identifier: Apache-2.0
import { evalCurve, monotoneTangents } from '../model/curve';
import type { VizGranularity } from '../model/time';
import type { VizBucketFrame } from '../model/types';

/**
 * Кадр одной детализации, готовый к рисованию: целевые значения (`target`) и показанные
 * (`shown`), которые плавно к ним подтягиваются. Так новые данные, смена метрики и живые
 * продажи не прыгают, а перетекают.
 */
export interface WaveLayer {
  readonly granularity: VizGranularity;
  /** Порядок серий в массивах ниже. */
  readonly ids: readonly string[];
  readonly step: number;
  /** Начало первого отрезка. */
  readonly start: number;
  /** Середина первого отрезка: точки кривой стоят в серединах. */
  readonly c0: number;
  readonly length: number;
  readonly target: readonly Float64Array[];
  readonly shown: readonly Float64Array[];
  readonly tangents: readonly Float64Array[];
  tangentsDirty: boolean;
  settled: boolean;
}

/**
 * Показанные значения `previous` на отрезках нового кадра другой детализации: сумма старых
 * отрезков с долей перекрытия. День распадается на 24 часа по 1/24 дня, часы сливаются в день.
 */
function regroup(previous: WaveLayer, s: number, frame: VizBucketFrame, k: number): number {
  const shown = previous.shown[s];
  const a = frame.start + k * frame.step;
  const b = a + frame.step;
  const j0 = Math.max(0, Math.floor((a - previous.start) / previous.step));
  const j1 = Math.min(previous.length - 1, Math.floor((b - previous.start - 1) / previous.step));
  let sum = 0;
  for (let j = j0; j <= j1; j++) {
    const v = shown[j];
    if (v !== v) continue;
    const from = Math.max(a, previous.start + j * previous.step);
    const to = Math.min(b, previous.start + (j + 1) * previous.step);
    if (to > from) sum += (v * (to - from)) / previous.step;
  }
  return sum;
}

/**
 * Слой из кадра. `previous` той же детализации отдаёт показанные значения по совпадающим
 * отрезкам — новые отрезки вырастают от нуля, остальные доезжают до новых значений.
 * `previous` другой детализации — значения пересобираются по времени (`regroup`).
 */
export function buildLayer(
  frame: VizBucketFrame,
  ids: readonly string[],
  previous?: WaveLayer | null,
): WaveLayer {
  const values = frame.values as Readonly<Record<string, ArrayLike<number>>>;
  const target: Float64Array[] = [];
  const shown: Float64Array[] = [];
  const tangents: Float64Array[] = [];
  const reuse =
    previous && previous.granularity === frame.granularity && previous.step === frame.step ? previous : null;
  const regroupFrom = previous && !reuse && previous.length > 0 ? previous : null;
  const shift = reuse ? Math.round((frame.start - reuse.start) / frame.step) : 0;
  ids.forEach((id) => {
    const src = values[id];
    const t = new Float64Array(frame.length);
    for (let k = 0; k < frame.length; k++) t[k] = src && k < src.length ? Number(src[k]) : Number.NaN;
    const d = Float64Array.from(t);
    const old = reuse ? reuse.shown[reuse.ids.indexOf(id)] : undefined;
    const from = regroupFrom ? regroupFrom.ids.indexOf(id) : -1;
    if (regroupFrom && from >= 0) {
      for (let k = 0; k < frame.length; k++) if (t[k] === t[k]) d[k] = regroup(regroupFrom, from, frame, k);
    } else if (reuse && old) {
      for (let k = 0; k < frame.length; k++) {
        if (t[k] !== t[k]) continue;
        const j = k + shift;
        const v = j >= 0 && j < old.length ? old[j] : Number.NaN;
        d[k] = v === v ? v : 0;
      }
    }
    target.push(t);
    shown.push(d);
    tangents.push(new Float64Array(frame.length));
  });
  return {
    granularity: frame.granularity,
    ids: [...ids],
    step: frame.step,
    start: frame.start,
    c0: frame.start + frame.step / 2,
    length: frame.length,
    target,
    shown,
    tangents,
    tangentsDirty: true,
    settled: false,
  };
}

/** Подтягивает показанные значения к целевым. Возвращает, движется ли ещё что-то. */
export function easeLayer(layer: WaveLayer, dt: number, tau: number): boolean {
  if (layer.settled) return false;
  const k = tau <= 0 ? 1 : 1 - Math.exp(-dt / tau);
  let moving = false;
  let changed = false;
  for (let s = 0; s < layer.target.length; s++) {
    const target = layer.target[s];
    const shown = layer.shown[s];
    for (let i = 0; i < layer.length; i++) {
      const tv = target[i];
      const sv = shown[i];
      if (tv !== tv) {
        if (sv === sv) {
          shown[i] = Number.NaN;
          changed = true;
        }
        continue;
      }
      if (sv !== sv) {
        shown[i] = tv;
        changed = true;
        continue;
      }
      const diff = tv - sv;
      if (diff === 0) continue;
      changed = true;
      if (Math.abs(diff) <= Math.max(1e-3, Math.abs(tv) * 2e-4)) shown[i] = tv;
      else {
        shown[i] = sv + diff * k;
        moving = true;
      }
    }
  }
  if (changed) layer.tangentsDirty = true;
  if (!moving) layer.settled = true;
  return moving;
}

export function refreshTangents(layer: WaveLayer): void {
  if (!layer.tangentsDirty) return;
  layer.tangentsDirty = false;
  for (let s = 0; s < layer.shown.length; s++)
    monotoneTangents(layer.shown[s], layer.tangents[s] as Float64Array, layer.length);
}

/** Показанное значение серии `s` в момент `t` (между точками — по кривой). */
export function layerValue(layer: WaveLayer, s: number, t: number, smooth: number): number {
  const shown = layer.shown[s];
  if (!shown) return Number.NaN;
  return evalCurve(shown, layer.tangents[s], (t - layer.c0) / layer.step, smooth, layer.length);
}

/** Точное значение в узле `k` (без интерполяции). */
export function layerKnot(layer: WaveLayer, s: number, k: number): number {
  const shown = layer.shown[s];
  return shown && k >= 0 && k < layer.length ? shown[k] : Number.NaN;
}

/** Сколько отрезков, где есть хоть одно значение. */
export function layerFilled(layer: WaveLayer): number {
  let count = 0;
  for (let k = 0; k < layer.length; k++) {
    for (const target of layer.target) {
      if (target[k] === target[k]) {
        count++;
        break;
      }
    }
  }
  return count;
}
