// SPDX-License-Identifier: Apache-2.0
import { formatNumber, formatValue } from '../model/format';
import { stackAt } from '../model/stack';
import { rgba } from './colors';
import { clamp, dot, easeOut, glideTo, lerp, paintChart, type ChartDataPainter } from './chart-paint';
import { layerKnot } from './layer';
import type { VizChartRenderer, VizPoint } from './renderer';
import type { VizTimeEngine } from './time-engine';

/** Самый широкий столбик, px: на крупном масштабе отрезок шире — столбик не раздувается. */
const MAX_BAR = 72;
/** Зазор между соседними столбиками в группе и между частями стопки, px. */
const GAP = 2;
/** Скругление верхних углов, px. */
const RADIUS = 4;
/**
 * Самая низкая видимая часть столбика, px. Маленькие значения в стопке иначе превращаются в
 * волоски по 1 px, и столбик рябит полосками; ноль частью не рисуется вовсе.
 */
const MIN_PART = 3;
/** Подписи значений над столбиками появляются, когда столбик шире, px. */
const LABEL_MIN_WIDTH = 26;

/** Столбик отрезка: где стоит и какой ширины. */
interface Column {
  readonly x0: number;
  readonly x1: number;
  /** Левый край и ширина столбика внутри отрезка. */
  readonly bx: number;
  readonly bw: number;
}

/** Часть столбика одной серии. */
interface Segment {
  readonly x: number;
  readonly w: number;
  readonly top: number;
  readonly bottom: number;
}

function column(e: VizTimeEngine, k: number): Column | null {
  const L = e.layer;
  if (!L) return null;
  const x0 = e.xOf(L.start + k * L.step);
  const x1 = e.xOf(L.start + (k + 1) * L.step);
  const full = x1 - x0;
  const bw = Math.max(1, Math.min(MAX_BAR, full - Math.max(GAP, full * 0.22)));
  return { x0, x1, bx: (x0 + x1) / 2 - bw / 2, bw };
}

/**
 * Части столбика отрезка `k` по сериям. Группа (наложение) — серии рядом, у скрытой ширина
 * уходит в ноль; стопка и доли — одна колонка. Переход — плавное смешение по `stackMix`.
 */
function segments(e: VizTimeEngine, k: number, col: Column): (Segment | null)[] {
  const L = e.layer;
  const n = e.series.length;
  const out: (Segment | null)[] = new Array(n).fill(null);
  if (!L) return out;
  const vals = new Float64Array(n);
  const tops = new Float64Array(n);
  const bottoms = new Float64Array(n);
  let missing = 0;
  for (let s = 0; s < n; s++) {
    const v = layerKnot(L, s, k);
    if (v !== v) missing++;
    const w = e.series[s].w;
    vals[s] = w < 1e-3 || v !== v ? 0 : v * w;
  }
  if (missing === n) return out;
  stackAt(vals, e.stackMix, e.pctMix, tops, bottoms);
  let weight = 0;
  for (const s of e.series) weight += s.w;
  const shown = e.series.filter((s) => s.w > 0.02).length;
  const gaps = shown > 1 ? GAP * (shown - 1) : 0;
  const inner = Math.max(0, col.bw - gaps);
  let gx = col.bx;
  const m = e.stackMix;
  for (let s = 0; s < n; s++) {
    const w = e.series[s].w;
    const gw = weight > 0 ? (inner * w) / weight : 0;
    if (w > 0.002)
      out[s] = { x: lerp(gx, col.bx, m), w: lerp(gw, col.bw, m), top: tops[s], bottom: bottoms[s] };
    gx += gw + (w > 0.02 ? GAP : 0);
  }
  return out;
}

/** Часть столбика в пикселях холста — с минимальной высотой. */
interface Box {
  readonly x: number;
  readonly w: number;
  readonly yTop: number;
  readonly yBottom: number;
  /** Значение части (для подписи). */
  readonly value: number;
}

/**
 * Части столбика отрезка в пикселях. Ненулевая часть не ниже `MIN_PART`: в стопке нижние части
 * подталкивают верхние, в группе каждый столбик растёт от основания сам. `g` — рост при появлении.
 */
function boxes(e: VizTimeEngine, k: number, col: Column, g: number): (Box | null)[] {
  const segs = segments(e, k, col);
  const base = e.plot.top + e.plot.height;
  const m = e.stackMix;
  const out: (Box | null)[] = segs.map(() => null);
  // Стопка: снизу вверх по порядку частей, каждая — не ниже минимума плюс зазор.
  const order = segs
    .map((seg, s) => ({ seg, s }))
    .filter((x): x is { seg: Segment; s: number } => !!x.seg && x.seg.top - x.seg.bottom > 0)
    .sort((a, b) => a.seg.bottom - b.seg.bottom);
  let cursor = base;
  for (const { seg, s } of order) {
    const value = seg.top - seg.bottom;
    const raw = e.yOf(seg.bottom * g) - e.yOf(seg.top * g);
    // Тип скрывают или показывают — минимум и зазор тают вместе с ним, а не держат пустую
    // щель до последнего кадра.
    const presence = Math.min(1, g * 4, e.series[s].w * 1.5);
    const gap = cursor < base ? GAP * presence : 0;
    const stackH = Math.max(raw, (MIN_PART + gap) * presence);
    const stackBottom = cursor;
    const stackTop = cursor - stackH;
    cursor = stackTop;
    // Группа: от основания, не ниже минимума.
    const groupTop = base - Math.max(base - e.yOf(value * g), MIN_PART * presence);
    out[s] = {
      x: seg.x,
      w: seg.w,
      yTop: lerp(groupTop, stackTop, m),
      yBottom: lerp(base, stackBottom, m),
      value,
    };
  }
  return out;
}

/** Прямоугольник со скруглёнными верхними углами. */
function barPath(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number): void {
  const rr = Math.max(0, Math.min(r, w / 2, h));
  ctx.moveTo(x, y + h);
  ctx.lineTo(x, y + rr);
  ctx.arcTo(x, y, x + rr, y, rr);
  ctx.lineTo(x + w - rr, y);
  ctx.arcTo(x + w, y, x + w, y + rr, rr);
  ctx.lineTo(x + w, y + h);
  ctx.closePath();
}

function visibleRange(e: VizTimeEngine): [number, number] {
  const L = e.layer;
  if (!L) return [0, -1];
  const { start, end } = e.viewport.state.current;
  return [
    Math.max(0, Math.floor((start - L.start) / L.step) - 1),
    Math.min(L.length - 1, Math.ceil((end - L.start) / L.step)),
  ];
}

/** Рост при появлении: столбики поднимаются слева направо. */
function grow(e: VizTimeEngine, x: number): number {
  if (e.reveal >= 1) return 1;
  const p = e.plot;
  const u = clamp((x - p.left) / Math.max(p.width, 1), 0, 1);
  return easeOut(clamp(e.reveal * 1.7 - u * 0.7, 0, 1));
}

const BAR_PAINTER: ChartDataPainter = {
  prepare(e) {
    const [kA, kB] = visibleRange(e);
    let max = 0;
    for (let k = kA; k <= kB; k++) {
      const col = column(e, k);
      if (!col) continue;
      for (const seg of segments(e, k, col)) if (seg && seg.top > max) max = seg.top;
    }
    return max;
  },
  under(e, dt) {
    const L = e.layer;
    const h = e.hover;
    if (!L || !h) return false;
    const col = column(e, clamp(h.k, 0, L.length - 1));
    if (!col) return false;
    const { x, busy } = glideTo(e, (col.x0 + col.x1) / 2, dt);
    const p = e.plot;
    const half = (col.x1 - col.x0) / 2;
    e.ctx.fillStyle = rgba(e.colors.text, 0.055);
    e.ctx.fillRect(x - half, p.top, half * 2, p.height);
    return busy;
  },
  data(e, _dt, now, dim) {
    const L = e.layer;
    if (!L) return false;
    const { ctx, colors: C, options: o } = e;
    const p = e.plot;
    const [kA, kB] = visibleRange(e);
    const hoverK = e.hover ? e.hover.k : -1;
    const stacked = e.stackMix > 0.5;
    const n = e.series.length;
    const base = p.top + p.height;
    // Градиент по высоте самого столбика: верх — полный цвет серии, к основанию светлее.
    const fade = 1 - 0.5 * o.gradient;
    const labels: { x: number; y: number; text: string; a: number }[] = [];
    // Крайние столбики не заезжают на подписи оси Y.
    ctx.save();
    ctx.beginPath();
    ctx.rect(p.left, 0, p.width, p.top + p.height + 1);
    ctx.clip();
    let visibleCount = 0;
    for (let k = kA; k <= kB; k++) if (column(e, k)) visibleCount++;
    for (let k = kA; k <= kB; k++) {
      const col = column(e, k);
      if (!col || col.x1 < p.left || col.x0 > p.left + p.width) continue;
      const g = grow(e, col.x0);
      const parts = boxes(e, k, col, g);
      const focusCol = hoverK < 0 || hoverK === k ? 1 : 0.72;
      let topmost = -1;
      for (let s = 0; s < n; s++)
        if (parts[s] && (topmost < 0 || parts[s]!.yTop <= parts[topmost]!.yTop)) topmost = s;
      for (let s = 0; s < n; s++) {
        const box = parts[s];
        const se = e.series[s];
        if (!box) continue;
        const alpha = dim * lerp(0.2, 1, se.e) * Math.min(1, se.w * 1.4) * focusCol * (1 + 0.15 * se.f);
        if (alpha < 0.005) continue;
        // Зазор стопки — у нижнего края части, если под ней есть другая.
        const inset = box.yBottom < base - 0.5 ? GAP * e.stackMix : 0;
        const yTop = box.yTop;
        const yBottom = box.yBottom - inset;
        const h = yBottom - yTop;
        if (h <= 0.25 || box.w <= 0.25) continue;
        const radius = lerp(RADIUS, s === topmost ? RADIUS : 0, e.stackMix);
        ctx.globalAlpha = Math.min(1, alpha);
        const fill = ctx.createLinearGradient(0, yTop, 0, yBottom);
        fill.addColorStop(0, rgba(se.color, 1));
        fill.addColorStop(1, rgba(se.color, fade));
        ctx.fillStyle = fill;
        ctx.beginPath();
        barPath(ctx, box.x, yTop, box.w, h, radius);
        ctx.fill();
        if (!stacked && o.points !== 'none' && box.w >= LABEL_MIN_WIDTH && g >= 1) {
          const show = o.points === 'always' || e.sparse || visibleCount <= 16;
          if (show) labels.push({ x: box.x + box.w / 2, y: yTop, text: valueText(e, box.value), a: alpha });
        }
      }
      ctx.globalAlpha = 1;
      if (stacked && topmost >= 0 && o.points !== 'none' && col.bw >= LABEL_MIN_WIDTH && g >= 1) {
        const show = o.points === 'always' || e.sparse || visibleCount <= 16;
        const total = parts.reduce((sum, b) => sum + (b ? b.value : 0), 0);
        // В долях сумма всегда 100% — подписывать нечего.
        if (show && total > 0 && e.pctMix < 0.5)
          labels.push({
            x: col.bx + col.bw / 2,
            y: parts[topmost]!.yTop,
            text: valueText(e, total),
            a: dim * focusCol,
          });
      }
    }
    ctx.globalAlpha = 1;
    ctx.restore();
    if (labels.length) {
      ctx.font = `600 11px ${e.font}`;
      ctx.textAlign = 'center';
      for (const l of labels) {
        ctx.fillStyle = rgba(C.text2, l.a);
        ctx.fillText(l.text, l.x, Math.max(p.top + 10, l.y - 6));
      }
      ctx.textAlign = 'left';
    }
    return paintBarLive(e, now);
  },
};

function valueText(e: VizTimeEngine, v: number): string {
  if (e.pctMix > 0.5) return `${formatNumber(v)}%`;
  return formatValue(v, e.options.unit);
}

/** Живые продажи: кольцо на верхе столбика серии и всплывающее «+1». */
function paintBarLive(e: VizTimeEngine, now: number): boolean {
  const L = e.layer;
  if (!L || (!e.pulses.length && !e.floats.length)) return false;
  const { ctx, colors: C } = e;
  const life = Math.max(e.options.slow, 0.25);
  const at = (id: string, t: number): VizPoint | null => {
    const k = Math.floor((t - L.start) / L.step);
    return anchorBar(e, k, id, false);
  };
  for (const pulse of e.pulses) {
    const si = e.series.findIndex((s) => s.id === pulse.id);
    const point = si >= 0 ? at(pulse.id, pulse.t) : null;
    if (!point) continue;
    const age = (now - pulse.born) / (1300 * life);
    ctx.beginPath();
    ctx.arc(point.x, point.y, 4 + 16 * easeOut(age), 0, Math.PI * 2);
    ctx.strokeStyle = rgba(e.series[si].color, 0.85 * (1 - age));
    ctx.lineWidth = 2;
    ctx.stroke();
    dot(ctx, C.surface, point.x, point.y, e.series[si].color, 3, 2, 1 - age * age);
  }
  ctx.font = `600 12px ${e.font}`;
  ctx.textAlign = 'center';
  for (const f of e.floats) {
    const si = e.series.findIndex((s) => s.id === f.id);
    const point = si >= 0 ? at(f.id, f.t) : null;
    if (!point) continue;
    const age = (now - f.born) / (1600 * life);
    const a = age < 0.15 ? age / 0.15 : 1 - (age - 0.15) / 0.85;
    ctx.fillStyle = rgba(C.text, a);
    ctx.fillText(f.text, point.x, point.y - 10 - 26 * easeOut(age));
  }
  ctx.textAlign = 'left';
  return true;
}

/** Серия под точкой: в группе — по колонке серии, в стопке — часть, в которую попали. */
function hitBar(e: VizTimeEngine, x: number, y: number): string | null {
  const L = e.layer;
  if (!L) return null;
  const k = Math.floor((e.tOf(x) - L.start) / L.step);
  if (k < 0 || k >= L.length) return null;
  const col = column(e, k);
  if (!col) return null;
  const parts = boxes(e, k, col, 1);
  const base = e.plot.top + e.plot.height;
  if (e.stackMix > 0.5) {
    if (x < col.bx - 3 || x > col.bx + col.bw + 3) return null;
    for (let s = 0; s < parts.length; s++) {
      const box = parts[s];
      if (!box || e.series[s].wt === 0) continue;
      if (y >= box.yTop - 2 && y <= box.yBottom + 1) return e.series[s].id;
    }
    return null;
  }
  // Группа: колонка серии от её столбика до основания и немного выше — по тонким столбикам легко попасть.
  for (let s = 0; s < parts.length; s++) {
    const box = parts[s];
    if (!box || e.series[s].wt === 0) continue;
    if (
      x >= box.x - GAP / 2 &&
      x <= box.x + box.w + GAP / 2 &&
      y <= base &&
      y >= Math.min(box.yTop, base - 8) - 24
    )
      return e.series[s].id;
  }
  return null;
}

/** Якорь подсказки столбика: верх части серии (или верх всего столбика), за полосой наведения. */
function anchorBar(e: VizTimeEngine, k: number, seriesId: string | null, follow: boolean): VizPoint | null {
  const col = column(e, k);
  if (!col) return null;
  const centre = (col.x0 + col.x1) / 2;
  const shift = follow && e.cx !== null ? e.cx - centre : 0;
  const parts = boxes(e, k, col, 1);
  const si = e.series.findIndex((s) => s.id === seriesId);
  const box = si >= 0 ? parts[si] : null;
  if (box) return { x: box.x + box.w / 2 + shift, y: box.yTop, width: box.w };
  let top = e.plot.top + e.plot.height;
  for (const b of parts) if (b && b.yTop < top) top = b.yTop;
  return { x: centre + shift, y: top, width: col.bw };
}

export const BAR_RENDERER: VizChartRenderer = {
  kind: 'bar',
  morph: 'aggregate',
  paint: (e, dt, now) => paintChart(e, dt, now, BAR_PAINTER),
  hit: hitBar,
  anchor: anchorBar,
};
