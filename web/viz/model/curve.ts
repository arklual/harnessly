// SPDX-License-Identifier: Apache-2.0
/**
 * Плавная кривая через точки: монотонные касательные Фрича–Карлсона. Между двумя точками
 * кривая не выходит за их значения — волна не уходит ниже нуля и не рисует выдуманный пик.
 * Пропуск (`NaN`) рвёт кривую.
 */

/** Касательные в единицах «разность значений на отрезок»; пишутся в `out`. */
export function monotoneTangents(y: ArrayLike<number>, out: Float64Array, length = y.length): Float64Array {
  for (let i = 0; i < length; i++) {
    const v = y[i];
    if (v !== v) {
      out[i] = 0;
      continue;
    }
    const dl = i > 0 ? v - y[i - 1] : Number.NaN;
    const dr = i < length - 1 ? y[i + 1] - v : Number.NaN;
    if (dl !== dl && dr !== dr) out[i] = 0;
    else if (dl !== dl) out[i] = dr * 0.5;
    else if (dr !== dr) out[i] = dl * 0.5;
    else out[i] = dl * dr <= 0 ? 0 : (2 * dl * dr) / (dl + dr);
  }
  return out;
}

/**
 * Значение в дробной позиции `u` (0 — первая точка, `length − 1` — последняя).
 * `smooth` от 0 (ломаная) до 1 (монотонная кривая) — промежуточные значения смешивают обе.
 */
export function evalCurve(
  y: ArrayLike<number>,
  tangents: ArrayLike<number>,
  u: number,
  smooth: number,
  length = y.length,
): number {
  if (length < 2 || !(u >= 0 && u <= length - 1)) return Number.NaN;
  const k = Math.min(Math.floor(u), length - 2);
  const f = u - k;
  const y0 = y[k];
  const y1 = y[k + 1];
  if (y0 !== y0 || y1 !== y1) return Number.NaN;
  const lin = y0 + (y1 - y0) * f;
  if (smooth <= 0) return lin;
  const f2 = f * f;
  const f3 = f2 * f;
  const h =
    (2 * f3 - 3 * f2 + 1) * y0 +
    (f3 - 2 * f2 + f) * tangents[k] +
    (-2 * f3 + 3 * f2) * y1 +
    (f3 - f2) * tangents[k + 1];
  return lin + (h - lin) * smooth;
}
