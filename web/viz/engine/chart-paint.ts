// SPDX-License-Identifier: Apache-2.0
import { formatCompact, formatDay, formatNumber, formatRange, formatTime } from '../model/format';
import { type VizTheme, type VizZoneTone, zoneToneColor } from '../model/palette';
import { isMidnight, TICK_LEVELS, type TickLevel, tickLevelFor, ticksFor } from '../model/time';
import { type Rgba, resolveColor, rgba } from './colors';
import type { VizTimeEngine, ZoneHitBox } from './time-engine';

export const clamp = (v: number, a: number, b: number) => Math.min(b, Math.max(a, v));
export const lerp = (a: number, b: number, t: number) => a + (b - a) * t;
export const easeOut = (t: number) => 1 - (1 - t) ** 3;
export const easeInOut = (t: number) => (t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2);
export const approach = (cur: number, target: number, dt: number, tau: number) =>
  tau <= 0 ? target : cur + (target - cur) * (1 - Math.exp(-dt / tau));

const toneCache = new Map<string, Rgba>();

/** Цвет тона зоны; нейтральная — цветом текста. */
export function toneRgba(tone: VizZoneTone, theme: VizTheme, text: Rgba): Rgba {
  const css = zoneToneColor(tone, theme);
  if (!css) return text;
  const key = `${theme}|${tone}`;
  let c = toneCache.get(key);
  if (!c) {
    c = resolveColor(css);
    toneCache.set(key, c);
  }
  return c;
}

export function roundRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
): void {
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + w, y, x + w, y + h, r);
  ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r);
  ctx.arcTo(x, y, x + w, y, r);
  ctx.closePath();
}

/** Волна скелетона: узнаваемый силуэт графика, пока данных нет. */
export function skeletonWave(x: number, w: number): number {
  const u = x / w;
  return (
    0.3 +
    0.2 * Math.sin(u * 7.1 + 0.6) * Math.exp(-0.4 * u) +
    0.12 * Math.sin(u * 17.3 + 1.7) +
    0.22 * Math.exp(-(((u - 0.78) * 5) ** 2))
  );
}

export function dot(
  ctx: CanvasRenderingContext2D,
  surface: Rgba,
  x: number,
  y: number,
  color: Rgba,
  r: number,
  ring: number,
  alpha: number,
): void {
  ctx.beginPath();
  ctx.arc(x, y, r + ring, 0, Math.PI * 2);
  ctx.fillStyle = rgba(surface, alpha);
  ctx.fill();
  ctx.beginPath();
  ctx.arc(x, y, r, 0, Math.PI * 2);
  ctx.fillStyle = rgba(color, alpha);
  ctx.fill();
}

/**
 * Как рисовать данные поверх общего каркаса (зоны, сетка, оси, «сейчас», пропуски, скелетон).
 * Свой у волн и у столбиков; состояние между шагами кадра — в замыкании рисовальщика.
 */
export interface ChartDataPainter {
  /** Готовит кадр и возвращает максимум видимых значений — верх оси Y. */
  prepare(e: VizTimeEngine): number;
  /** Под данными, над сеткой: полоса наведённого отрезка. */
  under?(e: VizTimeEngine, dt: number): boolean;
  /** Сами данные; `dim` — приглушение на время обновления и скелетона. */
  data(e: VizTimeEngine, dt: number, now: number, dim: number): boolean;
  /** Над данными, под подписями осей: перекрестье наведения. */
  over?(e: VizTimeEngine, dt: number): boolean;
}

/** Рисует холст за кадр: общий каркас и данные рисовальщика. Возвращает, нужен ли следующий кадр. */
export function paintChart(e: VizTimeEngine, dt: number, now: number, painter: ChartDataPainter): boolean {
  const { ctx } = e;
  if (e.W <= 0 || e.H <= 0) return false;
  ctx.setTransform(e.dpr, 0, 0, e.dpr, 0, 0);
  ctx.clearRect(0, 0, e.W, e.H);
  let busy = false;

  const skelTarget = e.status === 'loading' || e.status === 'error' ? 1 : 0;
  e.skel =
    Math.abs(e.skel - skelTarget) < 0.01
      ? skelTarget
      : approach(e.skel, skelTarget, dt, 180 * e.options.slow);
  if (e.skel !== skelTarget) busy = true;

  const L = e.layer;
  const ready = e.status === 'ready' && !!L && L.length > 0;
  busy = e.fitY(ready ? painter.prepare(e) : 0, dt) || busy;
  busy = e.fitAxis(axisLabelWidth(e), dt) || busy;

  const showData = e.status === 'ready' || e.status === 'empty';
  const zones = showData && e.options.zones;
  if (zones) paintZones(e, 'fill');
  e.zoneHits = [];
  if (showData) {
    paintFuture(e);
    if (ready) paintGaps(e);
    if (e.effectWindow) paintEffect(e);
  }
  if (e.status !== 'loading' && e.status !== 'error') paintGrid(e);
  if (showData) {
    paintMarkerLine(e);
    paintNow(e);
  }
  if (ready) {
    if (e.hover && painter.under) busy = painter.under(e, dt) || busy;
    busy = painter.data(e, dt, now, lerp(1, 0.42, e.refA) * (1 - e.skel)) || busy;
    if (e.hover && painter.over) busy = painter.over(e, dt) || busy;
  }
  if (zones) paintZones(e, 'labels');
  if (e.status !== 'loading' && e.status !== 'error') {
    paintYLabels(e);
    busy = paintXLabels(e, dt) || busy;
  }
  if (e.draft) paintDraft(e);
  if (e.skel > 0.01) {
    paintSkeleton(e, now, e.skel);
    busy = true;
  }
  return busy;
}

/** Перекрестье плавно едет к отрезку `k`: возвращает текущий X и едет ли ещё. */
export function glideTo(e: VizTimeEngine, tx: number, dt: number): { x: number; busy: boolean } {
  const h = e.hover;
  if (e.cx == null || h?.snap) {
    e.cx = tx;
    if (h) h.snap = false;
  } else e.cx = approach(e.cx, tx, dt, 55 * e.options.slow);
  const busy = Math.abs(e.cx - tx) > 0.3;
  if (!busy) e.cx = tx;
  return { x: e.cx, busy };
}

function yLabel(e: VizTimeEngine, v: number): string {
  return e.pctMix > 0.5 ? `${formatNumber(v)}%` : formatCompact(v);
}

function axisLabelWidth(e: VizTimeEngine): number {
  const ctx = e.ctx;
  ctx.font = `500 11px ${e.font}`;
  let w = 0;
  for (const set of e.ysets) {
    if (set.a <= 0.02 || e.ymax / set.step > 24) continue;
    for (let v = set.step, i = 0; v <= Math.max(e.ymax, set.step) * 1.001 && i < 12; v += set.step, i++)
      w = Math.max(w, ctx.measureText(yLabel(e, v)).width);
  }
  return w;
}

/** Заливка зоны: тон — заметнее, нейтральные — через одну, чтобы соседние периоды читались. */
export function zoneFill(toned: boolean, index: number, strength = 1): number {
  if (toned) return 0.07 * strength;
  return index % 2 ? 0.028 * strength : 0;
}

/**
 * Зоны в два слоя: подкраска — под данными, подписи — поверх (иначе столбики до верха, доли
 * и стопка их закрывают). `part` — какой слой рисовать.
 */
function paintZones(e: VizTimeEngine, part: 'fill' | 'labels'): void {
  const { ctx, colors: C } = e;
  const p = e.plot;
  const right = p.left + p.width;
  const bottom = p.top + p.height;
  const hits: ZoneHitBox[] = [];
  ctx.save();
  ctx.font = `500 12px ${e.font}`;
  e.zones.forEach((z, i) => {
    const a = e.zoneAlpha.get(z.id) ?? 1;
    let x0 = e.xOf(z.from);
    let x1 = e.xOf(z.to);
    if (x1 < p.left || x0 > right) return;
    x0 = Math.max(p.left, x0);
    x1 = Math.min(right, x1);
    if (x1 - x0 < 1) return;
    const tone = z.tone ?? 'neutral';
    const toned = tone !== 'neutral';
    const color = toneRgba(tone, e.theme, C.text);
    const hovered = e.zoneHoverId === z.id;
    const base = zoneFill(toned, i) * (e.options.zoneFocus ? 3 : 1);
    ctx.fillStyle = rgba(color, (base + (hovered ? 0.025 : 0)) * a);
    if (part === 'fill') {
      ctx.fillRect(x0, p.top, x1 - x0, bottom - p.top);
      return;
    }
    const room = x1 - x0 - 16;
    if (room <= 36) return;
    let label = z.label;
    let note = z.note ? ` · ${z.note}` : '';
    const close = z.removable ? 18 : 0;
    let wl = ctx.measureText(label).width;
    let wn = ctx.measureText(note).width;
    if (wl + wn + close > room) {
      note = '';
      wn = 0;
    }
    while (label.length > 2 && wl + close > room) {
      label = `${label.slice(0, -2)}…`;
      wl = ctx.measureText(label).width;
    }
    const ly = p.top + 17;
    ctx.fillStyle = rgba(e.options.zoneFocus ? C.text : C.text2, Math.min(1, a * (hovered ? 1.3 : 1)));
    ctx.fillText(label, x0 + 8, ly);
    if (note) {
      ctx.fillStyle = rgba(C.muted, a);
      ctx.fillText(note, x0 + 8 + wl, ly);
    }
    let hitRight = x0 + 8 + wl + wn + 6;
    let closeBox: { x0: number; x1: number } | null = null;
    if (z.removable) {
      const cx = x0 + 8 + wl + wn + 10;
      const cy = ly - 4;
      ctx.strokeStyle = rgba(C.text2, a * (hovered ? 1 : 0.55));
      ctx.lineWidth = 1.4;
      ctx.beginPath();
      ctx.moveTo(cx - 3.5, cy - 3.5);
      ctx.lineTo(cx + 3.5, cy + 3.5);
      ctx.moveTo(cx + 3.5, cy - 3.5);
      ctx.lineTo(cx - 3.5, cy + 3.5);
      ctx.stroke();
      closeBox = { x0: cx - 8, x1: cx + 8 };
      hitRight = cx + 8;
    }
    hits.push({ zone: z, x0: x0 + 2, x1: hitRight, y0: p.top, y1: p.top + 26, close: closeBox });
  });
  ctx.restore();
  e.zoneHits = hits;
}

/** Будущее после «сейчас» — едва заметно темнее. */
function paintFuture(e: VizTimeEngine): void {
  if (!e.now) return;
  const p = e.plot;
  const x0 = Math.max(p.left, e.xOf(e.now));
  const x1 = p.left + p.width;
  if (x1 - x0 < 2) return;
  e.ctx.fillStyle = rgba(e.colors.text, 0.022);
  e.ctx.fillRect(x0, p.top, x1 - x0, p.height);
}

function paintGaps(e: VizTimeEngine): void {
  const L = e.layer;
  if (!L || !L.target.length) return;
  const { ctx, colors: C } = e;
  const p = e.plot;
  const right = p.left + p.width;
  const empty = (k: number) => L.target.every((v) => v[k] !== v[k]);
  let k = 0;
  while (k < L.length) {
    if (!empty(k)) {
      k++;
      continue;
    }
    const a = k;
    while (k < L.length && empty(k)) k++;
    const x0 = Math.max(p.left, e.xOf(L.c0 + (a - 1) * L.step));
    const x1 = Math.min(right, e.xOf(L.c0 + k * L.step));
    if (x1 - x0 <= 2) continue;
    ctx.fillStyle = rgba(C.text, 0.04);
    ctx.fillRect(x0, p.top, x1 - x0, p.height);
    if (x1 - x0 > 70 && e.options.labels.gap) {
      ctx.font = `500 12px ${e.font}`;
      ctx.fillStyle = rgba(C.text2, 1);
      ctx.textAlign = 'center';
      ctx.fillText(e.options.labels.gap, (x0 + x1) / 2, p.top + p.height - 12);
      ctx.textAlign = 'left';
    }
  }
}

/** Окна «до» и «после» изменения: с чем сравнивали эффект. */
function paintEffect(e: VizTimeEngine): void {
  const fx = e.effectWindow;
  if (!fx) return;
  const { ctx, colors: C } = e;
  const p = e.plot;
  const right = p.left + p.width;
  const xm = clamp(e.xOf(fx.t), p.left, right);
  const xa = Math.max(p.left, e.xOf(fx.t - fx.window));
  const xb = Math.min(right, e.xOf(fx.afterEnd));
  ctx.font = `500 11px ${e.font}`;
  if (xm - xa > 1) {
    ctx.fillStyle = rgba(C.text, 0.045);
    ctx.fillRect(xa, p.top, xm - xa, p.height);
    if (xm - xa > 70 && fx.beforeLabel) {
      ctx.fillStyle = rgba(C.muted, 1);
      ctx.fillText(fx.beforeLabel, xa + 8, p.top + p.height - 10);
    }
  }
  if (xb - xm > 1) {
    ctx.fillStyle = rgba(fx.color ? resolveColor(fx.color) : C.text, 0.09);
    ctx.fillRect(xm, p.top, xb - xm, p.height);
    if (xb - xm > 70 && fx.afterLabel) {
      ctx.fillStyle = rgba(C.text2, 1);
      ctx.fillText(fx.afterLabel, xm + 8, p.top + p.height - 10);
    }
  }
}

function paintGrid(e: VizTimeEngine): void {
  const { ctx, colors: C } = e;
  const p = e.plot;
  if (e.options.grid) {
    for (const set of e.ysets) {
      if (set.a <= 0 || e.ymax / set.step > 24) continue;
      ctx.fillStyle = rgba(C.border, set.a);
      for (let v = set.step; v <= e.ymax * 1.001; v += set.step) {
        const y = Math.round(e.yOf(v));
        if (y < p.top - 1) break;
        ctx.fillRect(p.left, y - 0.5, p.width, 1);
      }
    }
  }
  ctx.fillStyle = rgba(C.text, 0.14);
  ctx.fillRect(p.left, Math.round(p.top + p.height) - 0.5, p.width, 1);
}

function paintYLabels(e: VizTimeEngine): void {
  const { ctx, colors: C } = e;
  const p = e.plot;
  const bottom = p.top + p.height;
  ctx.font = `500 11px ${e.font}`;
  ctx.textAlign = 'right';
  ctx.textBaseline = 'middle';
  ctx.fillStyle = rgba(C.muted, 1);
  ctx.fillText('0', p.left - 10, bottom);
  for (const set of e.ysets) {
    if (set.a <= 0.02 || e.ymax / set.step > 24) continue;
    ctx.fillStyle = rgba(C.muted, set.a);
    for (let v = set.step; v <= e.ymax * 1.001; v += set.step) {
      const y = e.yOf(v);
      if (y < p.top - 4) break;
      ctx.fillText(yLabel(e, v), p.left - 10, y);
    }
  }
  ctx.textAlign = 'left';
  ctx.textBaseline = 'alphabetic';
}

/** Подписи оси X: уровень делений выбирается по ширине, соседние уровни перетекают. */
function paintXLabels(e: VizTimeEngine, dt: number): boolean {
  const { ctx, colors: C } = e;
  const p = e.plot;
  const view = e.viewport.state.current;
  const span = view.end - view.start;
  const zone = e.options.timeZone;
  const chosen: TickLevel = tickLevelFor(span, p.width);
  let busy = false;
  for (const level of TICK_LEVELS) {
    const target = level === chosen ? 1 : 0;
    const a = e.xalpha.get(level) ?? target;
    const next = Math.abs(a - target) < 0.01 ? target : approach(a, target, dt, 150 * e.options.slow);
    if (next !== target) busy = true;
    e.xalpha.set(level, next);
  }
  const marks = new Map<number, number>();
  const range = { start: view.start - span * 0.05, end: view.end + span * 0.05 };
  for (const level of TICK_LEVELS) {
    const a = e.xalpha.get(level) ?? 0;
    if (a <= 0.01) continue;
    for (const t of ticksFor(level, range, zone)) marks.set(t, Math.max(marks.get(t) ?? 0, a));
  }
  ctx.font = `500 11px ${e.font}`;
  ctx.textAlign = 'center';
  const fine = chosen !== 'month' && chosen < 86_400_000;
  const right = p.left + p.width;
  for (const [t, a] of marks) {
    const x = e.xOf(t);
    const edge = clamp(Math.min(x - p.left + 18, right + 18 - x) / 20, 0, 1);
    if (edge <= 0) continue;
    const midnight = isMidnight(t, zone);
    ctx.fillStyle = rgba(midnight && fine ? C.text2 : C.muted, a * edge);
    const label = midnight || !fine ? formatDay(t, zone) : formatTime(t, zone);
    const half = ctx.measureText(label).width / 2;
    ctx.fillText(label, clamp(x, p.left - 8 + half, e.W - 2 - half), p.top + p.height + 17);
  }
  ctx.textAlign = 'left';
  return busy;
}

/** Вертикаль у значка в фокусе. */
function paintMarkerLine(e: VizTimeEngine): void {
  if (!e.markerFocus) return;
  const m = e.markers.find((item) => item.id === e.markerFocus);
  if (!m) return;
  const p = e.plot;
  const x = e.xOf(m.t);
  if (x < p.left - 1 || x > p.left + p.width + 1) return;
  e.ctx.fillStyle = rgba(e.colors.text, 0.35);
  e.ctx.fillRect(Math.round(x) - 0.5, p.top - 8, 1, p.height + 8);
}

function paintNow(e: VizTimeEngine): void {
  if (!e.now) return;
  const { ctx, colors: C } = e;
  const p = e.plot;
  const x = e.xOf(e.now);
  const right = p.left + p.width;
  if (x < p.left || x > right) return;
  // Над строкой подписей зон вертикаль не идёт — не перечёркивает «Последние билеты · 3 200 ₽».
  const top = e.zoneHits.length ? p.top + 24 : p.top;
  ctx.fillStyle = rgba(C.text, 0.32);
  ctx.fillRect(Math.round(x) - 0.5, top, 1, p.top + p.height - top);
  const label = e.options.labels.now;
  if (!label) return;
  ctx.font = `600 10px ${e.font}`;
  ctx.fillStyle = rgba(C.text2, 1);
  const atRight = x > right - 40;
  ctx.textAlign = atRight ? 'right' : 'left';
  ctx.fillText(label, atRight ? x - 5 : x + 5, p.top + 34);
  ctx.textAlign = 'left';
}

/** Черновик новой зоны в режиме разметки. */
function paintDraft(e: VizTimeEngine): void {
  const d = e.draft;
  if (!d) return;
  const { ctx, colors: C } = e;
  const p = e.plot;
  const right = p.left + p.width;
  const x0 = clamp(e.xOf(d.start), p.left, right);
  const x1 = clamp(e.xOf(d.end), p.left, right);
  const col = toneRgba(e.options.zoneDrawTone, e.theme, C.text);
  ctx.fillStyle = rgba(col, 0.14);
  ctx.fillRect(x0, p.top, x1 - x0, p.height);
  ctx.fillStyle = rgba(col, 0.9);
  ctx.fillRect(Math.round(x0) - 0.5, p.top, 1, p.height);
  ctx.fillRect(Math.round(x1) - 0.5, p.top, 1, p.height);
  ctx.fillRect(x0, p.top, x1 - x0, 1.5);
  if (d.end <= d.start) return;
  const text = formatRange(d.start, d.end, e.options.timeZone);
  ctx.font = `600 12px ${e.font}`;
  const w = ctx.measureText(text).width;
  const cx = clamp((x0 + x1) / 2, p.left + w / 2 + 10, right - w / 2 - 10);
  ctx.fillStyle = rgba(C.raised, 0.96);
  roundRect(ctx, cx - w / 2 - 9, p.top + 8, w + 18, 24, 7);
  ctx.fill();
  ctx.fillStyle = rgba(C.text, 1);
  ctx.textAlign = 'center';
  ctx.fillText(text, cx, p.top + 24);
  ctx.textAlign = 'left';
}

/** Полуширина блика скелетона, px. */
const SHIMMER_HALF = 160;

function paintSkeleton(e: VizTimeEngine, now: number, a: number): void {
  const { ctx, colors: C } = e;
  const p = e.plot;
  const right = p.left + p.width;
  const bottom = p.top + p.height;
  ctx.save();
  ctx.globalAlpha = a;
  for (let i = 0; i <= 4; i++) {
    const y = bottom - (p.height * i) / 4.4;
    ctx.fillStyle = rgba(C.border, 1);
    ctx.fillRect(p.left, Math.round(y) - 0.5, p.width, 1);
    ctx.fillStyle = rgba(C.input, 1);
    roundRect(ctx, p.left - 34, y - 4, 24, 8, 4);
    ctx.fill();
  }
  for (let i = 0; i < 7; i++) {
    ctx.fillStyle = rgba(C.input, 1);
    roundRect(ctx, p.left + (p.width * (i + 0.5)) / 7 - 18, bottom + 10, 36, 8, 4);
    ctx.fill();
  }
  const period = 1800 * Math.max(e.options.slow, 0.25);
  const shimmer = (now % period) / period;
  for (const [k, alpha] of [
    [0, 0.9],
    [1, 0.55],
  ] as const) {
    ctx.beginPath();
    ctx.moveTo(p.left, bottom);
    for (let x = p.left; x <= right + 4; x += 4) {
      const xx = Math.min(x, right);
      const v = k === 0 ? skeletonWave(xx - p.left, p.width) : 0.55 * skeletonWave(right - xx, p.width);
      ctx.lineTo(xx, bottom - v * p.height);
    }
    ctx.lineTo(right, bottom);
    ctx.closePath();
    ctx.fillStyle = rgba(C.input, alpha);
    ctx.fill();
    ctx.save();
    ctx.clip();
    // Блик целиком уходит за края до перезапуска и едет с плавным разгоном и торможением:
    // иначе на узком графике он прыгает обратно у всех на виду.
    const eased = 0.5 - 0.5 * Math.cos(Math.PI * shimmer);
    const sx = p.left - SHIMMER_HALF + eased * (p.width + SHIMMER_HALF * 2);
    const sg = ctx.createLinearGradient(sx - SHIMMER_HALF, 0, sx + SHIMMER_HALF, 0);
    sg.addColorStop(0, rgba(C.text, 0));
    sg.addColorStop(0.5, rgba(C.text, e.theme === 'dark' ? 0.06 : 0.08));
    sg.addColorStop(1, rgba(C.text, 0));
    ctx.fillStyle = sg;
    ctx.fillRect(p.left, p.top, p.width, p.height);
    ctx.restore();
  }
  ctx.restore();
}
