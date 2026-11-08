// SPDX-License-Identifier: Apache-2.0
import type { VizTheme } from '../model/palette';
import type { VizTimeRange } from '../model/time';
import { HOUR } from '../model/time';
import type { VizBucketFrame, VizZone } from '../model/types';
import type { VizViewport } from '../model/viewport';
import {
  canvasFont,
  documentTheme,
  readCanvasColors,
  resolveColor,
  type Rgba,
  rgba,
  type VizCanvasColors,
  VIZ_THEME_ATTRIBUTES,
} from './colors';
import { buildLayer, easeLayer, type WaveLayer } from './layer';
import { clamp, roundRect, skeletonWave, toneRgba, zoneFill } from './chart-paint';

export interface NavigatorSeries {
  readonly id: string;
  readonly color: string;
}

/** Поля полосы: по горизонтали — как у графика над ней, чтобы окно стояло ровно под ним. */
export interface NavigatorInsets {
  readonly left: number;
  readonly right: number;
}

type Grip = 'left' | 'right' | 'window';

interface Drag {
  readonly grip: Grip;
  readonly x: number;
  readonly range: VizTimeRange;
  /** Нажали мимо окна: окно прыгнуло сюда; пока палец не повёл — анимация, потом — вплотную. */
  jump: boolean;
}

const MIN_SPAN = 3 * HOUR;

/** Окно после протяжки: за середину — сдвиг, за ручку — край, не уже `MIN_SPAN`. */
function dragRange(drag: Drag, shift: number): VizTimeRange {
  const r = drag.range;
  if (drag.grip === 'left') return { start: Math.min(r.start + shift, r.end - MIN_SPAN), end: r.end };
  if (drag.grip === 'right') return { start: r.start, end: Math.max(r.end + shift, r.start + MIN_SPAN) };
  return { start: r.start + shift, end: r.end + shift };
}
const GRIP_ZONE = 9;

/**
 * Полоса «весь период» под графиком: вся история стопкой, окно графика, ручки по краям.
 * Протяжка двигает окно, ручки меняют масштаб, клик мимо — переносит окно туда, колесо — зум.
 */
export class NavigatorEngine {
  readonly canvas: HTMLCanvasElement;
  private readonly ctx: CanvasRenderingContext2D;
  private readonly inert: boolean;
  private colors!: VizCanvasColors;
  private theme: VizTheme = 'dark';
  private font = 'Inter, system-ui, sans-serif';
  private dpr = 1;
  W = 0;
  H = 0;
  insets: NavigatorInsets = { left: 48, right: 14 };
  /** Отступ полосы сверху — место под значки изменений. */
  top = 2;
  private series: { id: string; color: Rgba; w: number; wt: number }[] = [];
  private layer: WaveLayer | null = null;
  private ready = false;
  private dim = 0;
  private zones: readonly VizZone[] = [];
  private zoneTones = true;
  now = 0;
  eventAt: number | null = null;
  eventLabel = '';
  slow = 1;
  private hoverGrip: Grip | null = null;
  private drag: Drag | null = null;
  private raf = 0;
  private last = 0;
  onPaint: (() => void) | null = null;
  onInteract: (() => void) | null = null;
  private readonly unsubscribe: () => void;
  private readonly resizeObserver: ResizeObserver | null;
  private readonly themeObserver: MutationObserver | null;
  private readonly detach: () => void;

  constructor(
    canvas: HTMLCanvasElement,
    readonly viewport: VizViewport,
  ) {
    this.canvas = canvas;
    const ctx = canvas.getContext('2d');
    // Без 2D-контекста (jsdom, старый WebView) движок не рисует, но принимает данные и ввод.
    this.inert = !ctx;
    this.ctx = ctx as CanvasRenderingContext2D;
    this.readTheme();
    this.unsubscribe = viewport.subscribe(() => this.kick());
    this.resizeObserver =
      typeof ResizeObserver === 'function' ? new ResizeObserver(() => this.resize()) : null;
    this.resizeObserver?.observe(canvas);
    this.themeObserver =
      typeof MutationObserver === 'function' ? new MutationObserver(() => this.readTheme()) : null;
    this.themeObserver?.observe(document.documentElement, {
      attributes: true,
      attributeFilter: [...VIZ_THEME_ATTRIBUTES],
    });
    this.detach = this.bind();
    this.resize();
  }

  destroy(): void {
    cancelAnimationFrame(this.raf);
    this.raf = 0;
    this.unsubscribe();
    this.resizeObserver?.disconnect();
    this.themeObserver?.disconnect();
    this.detach();
  }

  setSeries(list: readonly NavigatorSeries[], hidden: ReadonlySet<string>): void {
    const old = new Map(this.series.map((s) => [s.id, s]));
    this.series = list.map((s) => {
      const prev = old.get(s.id);
      const wt = hidden.has(s.id) ? 0 : 1;
      return { id: s.id, color: resolveColor(s.color), w: prev?.w ?? wt, wt };
    });
    if (this.frame) this.setFrame(this.frame, this.ready);
    this.kick();
  }

  private frame: VizBucketFrame | null = null;

  /** Кадр всей истории; `ready` — данные есть (иначе — скелетон). */
  setFrame(frame: VizBucketFrame | null, ready: boolean): void {
    this.frame = frame;
    this.ready = ready && !!frame;
    this.layer = frame
      ? buildLayer(
          frame,
          this.series.map((s) => s.id),
          this.layer,
        )
      : null;
    this.kick();
  }

  setRefreshing(refreshing: boolean): void {
    this.dim = refreshing ? 1 : 0;
    this.kick();
  }

  setZones(zones: readonly VizZone[], visible: boolean): void {
    this.zones = zones;
    this.zoneTones = visible;
    this.kick();
  }

  setInsets(insets: NavigatorInsets): void {
    if (Math.abs(insets.left - this.insets.left) < 0.5 && Math.abs(insets.right - this.insets.right) < 0.5)
      return;
    this.insets = insets;
    this.kick();
  }

  get strip() {
    return { left: this.insets.left, right: this.W - this.insets.right, top: this.top, bottom: this.H - 4 };
  }

  xOf(t: number): number {
    const s = this.strip;
    const e = this.viewport.extent;
    return s.left + ((t - e.start) / Math.max(e.end - e.start, 1)) * (s.right - s.left);
  }

  tOf(x: number): number {
    const s = this.strip;
    const e = this.viewport.extent;
    return e.start + ((x - s.left) / Math.max(s.right - s.left, 1)) * (e.end - e.start);
  }

  kick(): void {
    if (this.raf || this.inert) return;
    this.last = performance.now();
    this.raf = requestAnimationFrame((now) => this.tick(now));
  }

  private tick(now: number): void {
    this.raf = 0;
    const dt = Math.min(50, Math.max(0, now - this.last));
    this.last = now;
    let busy = false;
    if (this.layer && easeLayer(this.layer, dt, 190 * this.slow)) busy = true;
    for (const s of this.series) {
      if (s.w === s.wt) continue;
      const k = this.slow <= 0 ? 1 : 1 - Math.exp(-dt / (150 * this.slow));
      s.w = Math.abs(s.wt - s.w) < 1e-3 ? s.wt : s.w + (s.wt - s.w) * k;
      busy = true;
    }
    this.paint();
    this.onPaint?.();
    if (busy) this.raf = requestAnimationFrame((t) => this.tick(t));
  }

  private paint(): void {
    const { ctx, colors: C } = this;
    if (this.W <= 0) return;
    ctx.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
    ctx.clearRect(0, 0, this.W, this.H);
    const s = this.strip;
    const w = s.right - s.left;
    const h = s.bottom - s.top;
    if (w <= 0 || h <= 0) return;
    ctx.fillStyle = rgba(C.input, this.theme === 'dark' ? 0.55 : 0.7);
    roundRect(ctx, s.left, s.top, w, h, 8);
    ctx.fill();
    ctx.save();
    roundRect(ctx, s.left, s.top, w, h, 8);
    ctx.clip();
    const L = this.layer;
    if (!this.ready || !L) {
      ctx.beginPath();
      ctx.moveTo(s.left, s.bottom);
      for (let x = s.left; x <= s.right; x += 4)
        ctx.lineTo(x, s.bottom - skeletonWave(x - s.left, w) * h * 0.8);
      ctx.lineTo(s.right, s.bottom);
      ctx.closePath();
      ctx.fillStyle = rgba(C.text, 0.05);
      ctx.fill();
      ctx.restore();
      return;
    }
    if (this.zoneTones) {
      this.zones.forEach((z, i) => {
        const tone = z.tone ?? 'neutral';
        ctx.fillStyle = rgba(toneRgba(tone, this.theme, C.text), zoneFill(tone !== 'neutral', i, 2));
        ctx.fillRect(this.xOf(z.from), s.top, this.xOf(z.to) - this.xOf(z.from), h);
      });
    }
    const xn = this.now ? this.xOf(this.now) : s.right;
    if (xn < s.right) {
      ctx.fillStyle = rgba(C.text, 0.03);
      ctx.fillRect(xn, s.top, s.right - xn, h);
    }
    this.paintStack(L);
    if (this.now && xn > s.left && xn < s.right) {
      ctx.fillStyle = rgba(C.text, 0.45);
      ctx.fillRect(Math.round(xn) - 0.5, s.top, 1, h);
    }
    if (this.eventAt && this.eventLabel && this.xOf(this.eventAt) - xn > 90) {
      ctx.font = `600 10px ${this.font}`;
      ctx.fillStyle = rgba(C.text2, 1);
      ctx.textAlign = 'right';
      ctx.fillText(this.eventLabel, Math.min(this.xOf(this.eventAt), s.right) - 6, s.top + 14);
      ctx.textAlign = 'left';
    }
    const view = this.viewport.state.current;
    const x0 = this.xOf(view.start);
    const x1 = this.xOf(view.end);
    ctx.fillStyle = rgba(C.surface, 0.62);
    ctx.fillRect(s.left, s.top, Math.max(0, x0 - s.left), h);
    ctx.fillRect(x1, s.top, Math.max(0, s.right - x1), h);
    ctx.restore();
    const active = this.hoverGrip === 'window' || this.drag?.grip === 'window';
    ctx.strokeStyle = rgba(C.text, active ? 0.55 : 0.3);
    ctx.lineWidth = 1;
    roundRect(ctx, x0 + 0.5, s.top + 0.5, Math.max(4, x1 - x0 - 1), h - 1, 7);
    ctx.stroke();
    for (const [x, grip] of [
      [x0, 'left'],
      [x1, 'right'],
    ] as const) {
      const on = this.hoverGrip === grip || this.drag?.grip === grip;
      ctx.fillStyle = rgba(on ? C.text : C.text2, 1);
      roundRect(ctx, x - 2.5, s.top + h / 2 - 10, 5, 20, 2.5);
      ctx.fill();
    }
  }

  /** Вся история стопкой: видно форму продаж целиком, контур — сумма. */
  private paintStack(L: WaveLayer): void {
    const { ctx, colors: C } = this;
    const s = this.strip;
    const h = s.bottom - s.top;
    const n = L.length;
    if (n === 0) return;
    const cum = new Float64Array(n);
    let max = 0;
    for (let k = 0; k < n; k++) {
      let sum = 0;
      for (let i = 0; i < this.series.length; i++) {
        const v = L.shown[L.ids.indexOf(this.series[i].id)]?.[k];
        if (v !== undefined && v === v) sum += v * this.series[i].w;
      }
      cum[k] = sum;
      if (sum > max) max = sum;
    }
    if (max <= 0) return;
    const yOf = (v: number) => s.bottom - 2 - (v / max) * (h - 10);
    const xs = Array.from({ length: n }, (_, k) => this.xOf(L.c0 + k * L.step));
    const base = new Float64Array(n);
    for (const se of this.series) {
      if (se.w < 0.01) continue;
      const d = L.shown[L.ids.indexOf(se.id)];
      if (!d) continue;
      ctx.beginPath();
      for (let k = 0; k < n; k++) {
        const v = d[k] === d[k] ? d[k] * se.w : 0;
        const y = yOf(base[k] + v);
        if (k === 0) ctx.moveTo(xs[k], y);
        else ctx.lineTo(xs[k], y);
      }
      for (let k = n - 1; k >= 0; k--) ctx.lineTo(xs[k], yOf(base[k]));
      ctx.closePath();
      ctx.fillStyle = rgba(se.color, 0.5 * (this.dim ? 0.5 : 1));
      ctx.fill();
      for (let k = 0; k < n; k++) base[k] += d[k] === d[k] ? d[k] * se.w : 0;
    }
    ctx.beginPath();
    for (let k = 0; k < n; k++) {
      const y = yOf(cum[k]);
      if (k === 0) ctx.moveTo(xs[k], y);
      else ctx.lineTo(xs[k], y);
    }
    ctx.strokeStyle = rgba(C.text, 0.35);
    ctx.lineWidth = 1;
    ctx.stroke();
  }

  private gripAt(x: number): Grip | null {
    const view = this.viewport.state.target;
    const x0 = this.xOf(view.start);
    const x1 = this.xOf(view.end);
    if (Math.abs(x - x0) < GRIP_ZONE && x < (x0 + x1) / 2 + 1) return 'left';
    if (Math.abs(x - x1) < GRIP_ZONE) return 'right';
    if (x > x0 && x < x1) return 'window';
    return null;
  }

  private bind(): () => void {
    const c = this.canvas;
    const pos = (e: { clientX: number }) => e.clientX - c.getBoundingClientRect().left;
    const down = (e: PointerEvent) => {
      if (!this.ready || e.button !== 0) return;
      const x = pos(e);
      const grip = this.gripAt(x);
      c.setPointerCapture?.(e.pointerId);
      this.onInteract?.();
      if (!grip) {
        const target = this.viewport.state.target;
        const span = target.end - target.start;
        const centre = this.tOf(x);
        this.viewport.setRange({ start: centre - span / 2, end: centre + span / 2 });
        this.drag = { grip: 'window', x, range: this.viewport.state.target, jump: true };
      } else this.drag = { grip, x, range: this.viewport.state.target, jump: false };
      c.dataset.grip = this.drag.grip;
      c.dataset.dragging = 'true';
      this.kick();
    };
    const move = (e: PointerEvent) => {
      const x = pos(e);
      const d = this.drag;
      if (!d) {
        const grip = this.ready ? this.gripAt(x) : null;
        if (grip !== this.hoverGrip) {
          this.hoverGrip = grip;
          c.dataset.grip = grip ?? 'none';
          this.kick();
        }
        return;
      }
      const e0 = this.viewport.extent;
      const shift = ((x - d.x) / Math.max(this.strip.right - this.strip.left, 1)) * (e0.end - e0.start);
      if (d.jump && Math.abs(x - d.x) > 3) d.jump = false;
      this.viewport.setRange(dragRange(d, shift), { instant: !d.jump });
    };
    const up = () => {
      this.drag = null;
      delete c.dataset.dragging;
      this.kick();
    };
    const leave = () => {
      if (this.drag || !this.hoverGrip) return;
      this.hoverGrip = null;
      this.kick();
    };
    const wheel = (e: WheelEvent) => {
      if (!this.ready) return;
      e.preventDefault();
      this.onInteract?.();
      const target = this.viewport.state.target;
      const anchor = clamp(this.tOf(pos(e)), target.start, target.end);
      this.viewport.zoomAround(anchor, Math.exp(e.deltaY * 0.0016));
    };
    const key = (e: KeyboardEvent) => {
      if (!this.ready) return;
      const target = this.viewport.state.target;
      const span = target.end - target.start;
      const centre = (target.start + target.end) / 2;
      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        e.preventDefault();
        this.viewport.panBy((e.key === 'ArrowLeft' ? -1 : 1) * span * 0.2);
      } else if (e.key === '+' || e.key === '=') {
        e.preventDefault();
        this.viewport.zoomAround(centre, 0.7);
      } else if (e.key === '-' || e.key === '_') {
        e.preventDefault();
        this.viewport.zoomAround(centre, 1.45);
      } else if (e.key === 'Home' || e.key === '0') {
        e.preventDefault();
        this.viewport.setRange(this.viewport.extent);
      }
    };
    c.addEventListener('pointerdown', down);
    c.addEventListener('pointermove', move);
    c.addEventListener('pointerup', up);
    c.addEventListener('pointercancel', up);
    c.addEventListener('pointerleave', leave);
    c.addEventListener('wheel', wheel, { passive: false });
    c.addEventListener('keydown', key);
    return () => {
      c.removeEventListener('pointerdown', down);
      c.removeEventListener('pointermove', move);
      c.removeEventListener('pointerup', up);
      c.removeEventListener('pointercancel', up);
      c.removeEventListener('pointerleave', leave);
      c.removeEventListener('wheel', wheel);
      c.removeEventListener('keydown', key);
    };
  }

  readTheme(): void {
    this.colors = readCanvasColors(this.canvas);
    this.theme = documentTheme();
    this.font = canvasFont(this.canvas);
    this.kick();
  }

  resize(): void {
    const W = this.canvas.clientWidth;
    const H = this.canvas.clientHeight;
    if (W <= 0 || H <= 0) return;
    this.dpr = Math.min(2.5, globalThis.devicePixelRatio || 1);
    this.W = W;
    this.H = H;
    this.canvas.width = Math.round(W * this.dpr);
    this.canvas.height = Math.round(H * this.dpr);
    this.kick();
  }
}
