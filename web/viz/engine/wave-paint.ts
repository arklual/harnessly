// SPDX-License-Identifier: Apache-2.0
import { formatBucket, formatDay, formatNumber, formatValue } from '../model/format';
import { stackAt } from '../model/stack';
import { rgba } from './colors';
import {
  clamp,
  dot,
  easeInOut,
  easeOut,
  glideTo,
  lerp,
  paintChart,
  type ChartDataPainter,
} from './chart-paint';
import type { VizChartRenderer, VizPoint } from './renderer';
import type { Sample, VizTimeEngine } from './time-engine';

/** Непрерывные куски без пропусков (NaN). */
function runs(arr: Float64Array, fn: (a: number, b: number) => void): void {
  let start = -1;
  for (let j = 0; j <= arr.length; j++) {
    const ok = j < arr.length && arr[j] === arr[j];
    if (ok && start < 0) start = j;
    if (!ok && start >= 0) {
      fn(start, j - 1);
      start = -1;
    }
  }
}

function paintSeries(e: VizTimeEngine, S: Sample, dim: number): void {
  const { ctx, colors: C, options: o } = e;
  const p = e.plot;
  const xs = Array.from(S.ts, (t) => e.xOf(t));
  const yb = p.top + p.height;
  const stacked = e.stackMix > 0.5;
  const order = e.series.map((_, i) => i).filter((i) => e.series[i].w > 0.002);
  const peaks = new Map(
    order.map((i) => {
      let top = Number.POSITIVE_INFINITY;
      const tp = S.tops[i];
      const rv = e.series[i].rv;
      for (let j = 0; j < tp.length; j++) if (tp[j] === tp[j]) top = Math.min(top, e.yOf(tp[j] * rv));
      return [i, top] as const;
    }),
  );
  // В наложении сначала высокие заливки, потом низкие — низкие не тонут; серия в фокусе последней.
  const fillOrder = stacked ? order : [...order].sort((a, b) => (peaks.get(a) ?? 0) - (peaks.get(b) ?? 0));
  if (!stacked) fillOrder.sort((a, b) => e.series[a].f - e.series[b].f);
  for (const i of fillOrder) {
    const s = e.series[i];
    const tp = S.tops[i];
    const bt = S.bottoms[i];
    const rv = s.rv;
    const alpha = dim * s.e * Math.min(1, s.w * 1.4) * (1 + 1.3 * s.f);
    if (alpha < 0.005 || o.gradient <= 0) continue;
    const py = Math.min(peaks.get(i) ?? yb, yb - 1);
    const grad = ctx.createLinearGradient(0, py, 0, yb);
    grad.addColorStop(0, rgba(s.color, o.gradient * (stacked ? 0.62 : 0.5) * alpha));
    grad.addColorStop(1, rgba(s.color, (stacked ? o.gradient * 0.16 : 0) * alpha));
    ctx.fillStyle = grad;
    ctx.beginPath();
    runs(tp, (a, b) => {
      ctx.moveTo(xs[a], e.yOf(tp[a] * rv));
      for (let j = a + 1; j <= b; j++) ctx.lineTo(xs[j], e.yOf(tp[j] * rv));
      for (let j = b; j >= a; j--) ctx.lineTo(xs[j], e.yOf(bt[j] * rv));
      ctx.closePath();
    });
    ctx.fill();
  }
  const lineOrder = [...fillOrder].sort((a, b) => e.series[a].e - e.series[b].e);
  ctx.lineJoin = 'round';
  ctx.lineCap = 'round';
  const eps = e.ymax * 0.004;
  const flat = e.stackMix < 0.5;
  for (const i of lineOrder) {
    const s = e.series[i];
    const tp = S.tops[i];
    const rv = s.rv;
    const alpha = dim * lerp(0.22, 1, s.e) * Math.min(1, s.w * 1.4);
    if (alpha < 0.005) continue;
    const trace = () => {
      ctx.beginPath();
      runs(tp, (a, b) => {
        ctx.moveTo(xs[a], e.yOf(tp[a] * rv));
        for (let j = a + 1; j <= b; j++) {
          const y = e.yOf(tp[j] * rv);
          // Нулевой хвост в наложении не рисуем линией: десяток серий не сливается в полосу у оси.
          if (flat && tp[j] <= eps && tp[j - 1] <= eps) ctx.moveTo(xs[j], y);
          else ctx.lineTo(xs[j], y);
        }
      });
    };
    if (stacked) {
      trace();
      ctx.strokeStyle = rgba(C.surface, alpha * 0.9 * e.stackMix);
      ctx.lineWidth = o.lineWidth + 2.5;
      ctx.stroke();
    }
    trace();
    ctx.strokeStyle = rgba(s.color, alpha);
    ctx.lineWidth = o.lineWidth * (1 + 0.75 * s.f);
    ctx.stroke();
  }
}

function paintDots(e: VizTimeEngine, S: Sample, dim: number, now: number, few: boolean): boolean {
  const L = e.layer;
  if (!L) return false;
  const { ctx, colors: C } = e;
  const p = e.plot;
  const mode = e.options.points;
  // «auto»: точки проявляются, когда отрезки шире 30 px; одна-две точки видны всегда.
  let a = clamp((S.bucketPx - 30) / 18, 0, 1);
  if (mode === 'always' || few) a = 1;
  else if (mode === 'none') a = 0;
  a *= easeInOut(e.morph) * dim * (e.reveal >= 1 ? 1 : clamp(e.reveal * 2 - 1, 0, 1));
  let busy = false;
  if (a > 0.01) {
    for (let k = Math.max(0, S.kA); k <= S.kB; k++) {
      const t = L.c0 + k * L.step;
      const x = e.xOf(t);
      if (x < p.left - 4 || x > p.left + p.width + 4) continue;
      const tp = e.topsAt(t, true);
      for (let s = 0; s < e.series.length; s++) {
        const se = e.series[s];
        const v = tp[s];
        if (!(v > 0) || se.w < 0.05) continue;
        dot(
          ctx,
          C.surface,
          x,
          e.yOf(v * se.rv),
          se.color,
          few ? 4.5 : 2.75,
          2,
          a * lerp(0.3, 1, se.e) * se.w,
        );
      }
      if (few && L.length <= 3) paintDirectLabel(e, k, x, tp, a);
    }
  }
  const life = 1300 * Math.max(e.options.slow, 0.25);
  for (const pulse of e.pulses) {
    const si = e.series.findIndex((s) => s.id === pulse.id);
    if (si < 0 || e.series[si].w < 0.5) continue;
    const x = e.xOf(pulse.t);
    if (x < p.left || x > p.left + p.width) continue;
    const age = (now - pulse.born) / life;
    const y = e.yOf(e.topsAt(pulse.t, true)[si]);
    if (y !== y) continue;
    ctx.beginPath();
    ctx.arc(x, y, 4 + 18 * easeOut(age), 0, Math.PI * 2);
    ctx.strokeStyle = rgba(e.series[si].color, 0.85 * (1 - age));
    ctx.lineWidth = 2;
    ctx.stroke();
    dot(ctx, C.surface, x, y, e.series[si].color, 3.5, 2, 1 - age * age);
    busy = true;
  }
  return busy;
}

/** Одна-две точки: значение и дата прямо у точки — ни легенда, ни тултип не нужны. */
function paintDirectLabel(e: VizTimeEngine, k: number, x: number, tops: Float64Array, a: number): void {
  const L = e.layer;
  if (!L) return;
  const { ctx, colors: C } = e;
  const p = e.plot;
  let best = -1;
  for (let s = 0; s < tops.length; s++) if (tops[s] > 0 && (best < 0 || tops[s] > tops[best])) best = s;
  if (best < 0) return;
  let raw = 0;
  L.target.forEach((arr, s) => {
    if (e.series[s]?.wt > 0 && arr[k] === arr[k]) raw += arr[k];
  });
  const y = e.yOf(tops[best]);
  const atRight = x > p.left + p.width - 150;
  const lx = atRight ? x - 20 : x + 20;
  ctx.beginPath();
  ctx.arc(x, y, 14, 0, Math.PI * 2);
  ctx.fillStyle = rgba(e.series[best].color, 0.14 * a);
  ctx.fill();
  ctx.textAlign = atRight ? 'right' : 'left';
  ctx.font = `650 14px ${e.font}`;
  ctx.fillStyle = rgba(C.text, a);
  const many = e.series.filter((s) => s.wt > 0).length > 1;
  const total = many && e.options.labels.total ? `${e.options.labels.total} ` : '';
  ctx.fillText(total + formatValue(raw, e.options.unit, { long: true }), lx, y - 1);
  ctx.font = `500 12px ${e.font}`;
  ctx.fillStyle = rgba(C.muted, a);
  const start = L.start + k * L.step;
  const live =
    e.now > start && e.now < start + L.step && e.options.labels.current
      ? ` · ${e.options.labels.current}`
      : '';
  ctx.fillText(formatBucket(start, L.granularity, L.step, e.options.timeZone) + live, lx, y + 16);
  ctx.textAlign = 'left';
}

/** Подпись максимума в окне. */
function paintPeak(e: VizTimeEngine, S: Sample): void {
  const L = e.layer;
  if (!L) return;
  const { ctx, colors: C } = e;
  const p = e.plot;
  let best: { v: number; j: number; i: number } | null = null;
  const list =
    e.stackMix > 0.5 ? [e.series.map((s) => s.w > 0.5).lastIndexOf(true)] : e.series.map((_, i) => i);
  for (const i of list) {
    const s = e.series[i];
    if (!s || s.w < 0.5 || s.e < 0.5) continue;
    const tp = S.tops[i];
    for (let j = 0; j < tp.length; j++)
      if (tp[j] === tp[j] && (!best || tp[j] > best.v)) best = { v: tp[j], j, i };
  }
  if (!best || best.v <= 0) return;
  const t = S.ts[best.j];
  const x = e.xOf(t);
  const y = e.yOf(best.v);
  if (x < p.left + 20 || x > p.left + p.width - 20) return;
  const k = clamp(Math.round((t - L.c0) / L.step), 0, L.length - 1);
  let raw = 0;
  if (e.pctMix > 0.5) raw = best.v;
  else if (e.stackMix > 0.5) {
    for (let s = 0; s < L.target.length; s++)
      if (e.series[s]?.w > 0.5 && L.target[s][k] === L.target[s][k]) raw += L.target[s][k];
  } else raw = L.target[best.i][k];
  const value = e.pctMix > 0.5 ? `${formatNumber(raw)}%` : formatValue(raw, e.options.unit);
  const text = [e.options.labels.peak, value, formatDay(L.start + k * L.step, e.options.timeZone)]
    .filter(Boolean)
    .join(' · ');
  ctx.font = `600 11px ${e.font}`;
  const w = ctx.measureText(text).width;
  const side = x - w / 2 < p.left + 4;
  const lx = side ? x + 12 : clamp(x - w / 2, p.left + 2, p.left + p.width - w - 2);
  const ly = side ? y + 4 : Math.max(p.top + 36, y - 12);
  ctx.fillStyle = rgba(C.text2, 0.95);
  ctx.fillText(text, lx, ly);
}

/** Живые продажи: точки на концах линий и всплывающие «+1». */
function paintLive(e: VizTimeEngine, now: number): void {
  const L = e.layer;
  if (!L) return;
  const { ctx, colors: C } = e;
  const p = e.plot;
  const right = p.left + p.width;
  if (e.options.live && L.length > 0) {
    const t = L.c0 + (L.length - 1) * L.step;
    const x = e.xOf(t);
    if (x >= p.left && x <= right) {
      const tp = e.topsAt(t, true);
      e.series.forEach((s, i) => {
        if (tp[i] > 0 && s.w >= 0.5) dot(ctx, C.surface, x, e.yOf(tp[i]), s.color, 3, 2, lerp(0.3, 1, s.e));
      });
    }
  }
  ctx.font = `600 12px ${e.font}`;
  const life = 1600 * Math.max(e.options.slow, 0.25);
  for (const f of e.floats) {
    const si = e.series.findIndex((s) => s.id === f.id);
    if (si < 0 || e.series[si].w < 0.5) continue;
    const x = e.xOf(f.t);
    if (x < p.left || x > right + 2) continue;
    const age = (now - f.born) / life;
    const top = e.topsAt(f.t, true)[si];
    const y = e.yOf(top === top ? top : 0) - 14 - 30 * easeOut(age);
    const a = age < 0.15 ? age / 0.15 : 1 - (age - 0.15) / 0.85;
    const lx = Math.min(x + 10, right - ctx.measureText(f.text).width - 2);
    ctx.beginPath();
    ctx.arc(lx - 7, y - 4, 3, 0, Math.PI * 2);
    ctx.fillStyle = rgba(e.series[si].color, a);
    ctx.fill();
    ctx.fillStyle = rgba(C.text, a);
    ctx.fillText(f.text, lx, y);
  }
}

/** Перекрестье: вертикаль и точки серий, плавно переезжает между отрезками. */
function paintCrosshair(e: VizTimeEngine, dt: number): boolean {
  const L = e.layer;
  const h = e.hover;
  if (!L || !h) return false;
  const { ctx, colors: C } = e;
  const p = e.plot;
  const k = clamp(h.k, 0, L.length - 1);
  const t = L.c0 + k * L.step;
  const { x, busy } = glideTo(e, e.xOf(t), dt);
  if (x < p.left - 2 || x > p.left + p.width + 2) return busy;
  ctx.fillStyle = rgba(C.text, 0.4);
  ctx.fillRect(Math.round(x) - 0.5, p.top, 1, p.height);
  const tops = busy ? e.topsAt(e.tOf(x)) : e.topsAt(t, true);
  e.series.forEach((s, i) => {
    if (tops[i] !== tops[i] || s.w < 0.5) return;
    const y = e.yOf(tops[i]);
    const big = e.pick === s.id;
    if (s.e > 0.5) {
      ctx.beginPath();
      ctx.arc(x, y, big ? 12 : 9, 0, Math.PI * 2);
      ctx.fillStyle = rgba(s.color, big ? 0.24 : 0.16);
      ctx.fill();
    }
    dot(ctx, C.surface, x, y, s.color, big ? 5.5 : 4.25, 2.25, lerp(0.35, 1, s.e));
  });
  return busy;
}

const samples = new WeakMap<VizTimeEngine, Sample | null>();

/** Волны: монотонные кривые с градиентной заливкой, точки на узлах, перекрестье с точками серий. */
const WAVE_PAINTER: ChartDataPainter = {
  prepare(e) {
    const S = e.sample();
    samples.set(e, S);
    const L = e.layer;
    let max = S ? S.max : 0;
    // Одна-две точки: держим место под подпись значения у точки.
    if (L && e.sparse)
      for (let k = 0; k < L.length; k++)
        for (const v of e.topsAt(L.c0 + k * L.step, true)) if (v > max) max = v;
    return max;
  },
  data(e, dt, now, dim) {
    const S = samples.get(e);
    if (!S) return false;
    const { ctx } = e;
    const p = e.plot;
    const few = e.sparse;
    const r = e.reveal;
    const clipX = p.left + p.width * easeInOut(Math.min(1, r * 1.12));
    ctx.save();
    ctx.beginPath();
    ctx.rect(p.left - 6, 0, (r < 1 ? clipX : p.left + p.width + 6) - p.left + 6, p.top + p.height + 2);
    ctx.clip();
    e.series.forEach((s, i) => {
      s.rv = r >= 1 ? 1 : easeOut(clamp(r * 1.35 - i * 0.07, 0, 1));
    });
    paintSeries(e, S, dim);
    ctx.restore();
    const busy = paintDots(e, S, dim, now, few);
    if (e.options.peak && !e.hover && r >= 1 && !few && e.refA < 0.5) paintPeak(e, S);
    paintLive(e, now);
    void dt;
    return busy;
  },
  over: (e, dt) => paintCrosshair(e, dt),
};

/** Серия под точкой: ближайшая линия в 14 px, иначе самая «тесная» заливка, внутри которой точка. */
function hitWave(e: VizTimeEngine, x: number, y: number): string | null {
  const t = e.tOf(x);
  const n = e.series.length;
  const vals = new Float64Array(n);
  const tops = new Float64Array(n);
  const bottoms = new Float64Array(n);
  for (let s = 0; s < n; s++) {
    const w = e.series[s].w;
    vals[s] = w < 1e-3 ? 0 : e.valueAt(s, t) * w;
  }
  stackAt(vals, e.stackMix, e.pctMix, tops, bottoms);
  let best = -1;
  let bestDistance = 14;
  for (let s = 0; s < n; s++) {
    if (e.series[s].wt === 0 || tops[s] !== tops[s]) continue;
    const d = Math.abs(y - e.yOf(tops[s]));
    if (d < bestDistance) {
      bestDistance = d;
      best = s;
    }
  }
  if (best < 0) {
    let area = Number.POSITIVE_INFINITY;
    for (let s = 0; s < n; s++) {
      if (e.series[s].wt === 0 || !(tops[s] > 0)) continue;
      const yt = e.yOf(tops[s]);
      const yb = e.yOf(bottoms[s]);
      if (y >= yt && y <= yb && yb - yt < area) {
        area = yb - yt;
        best = s;
      }
    }
  }
  return best >= 0 ? e.series[best].id : null;
}

/** Якорь подсказки волны: точка серии на линии (или верхняя точка отрезка), за перекрестьем. */
function anchorWave(e: VizTimeEngine, k: number, seriesId: string | null, follow: boolean): VizPoint | null {
  const L = e.layer;
  if (!L) return null;
  const t = L.c0 + k * L.step;
  const x = follow && e.cx !== null ? e.cx : e.xOf(t);
  const gliding = Math.abs(x - e.xOf(t)) > 0.3;
  const tops = gliding ? e.topsAt(e.tOf(x)) : e.topsAt(t, true);
  const si = e.series.findIndex((s) => s.id === seriesId);
  let y = Number.POSITIVE_INFINITY;
  if (si >= 0 && tops[si] === tops[si]) y = e.yOf(tops[si]);
  else
    e.series.forEach((s, i) => (s.w > 0.5 && tops[i] === tops[i] ? (y = Math.min(y, e.yOf(tops[i]))) : null));
  if (!(y < Number.POSITIVE_INFINITY)) y = e.plot.top + e.plot.height - 8;
  return { x, y };
}

export const WAVE_RENDERER: VizChartRenderer = {
  kind: 'wave',
  morph: 'blend',
  paint: (e, dt, now) => paintChart(e, dt, now, WAVE_PAINTER),
  hit: hitWave,
  anchor: anchorWave,
};
