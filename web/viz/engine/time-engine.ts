// SPDX-License-Identifier: Apache-2.0
import { VIZ_UNIT_COUNT, type VizUnit } from '../model/format';
import type { VizTheme, VizZoneTone } from '../model/palette';
import { niceCeil, niceStep } from '../model/scale';
import { stackAt } from '../model/stack';
import {
  floorTo,
  GRANULARITY_STEP,
  HOUR,
  MOSCOW_ZONE,
  type VizTimeRange,
  type VizTimeZone,
} from '../model/time';
import type { VizBucketFrame, VizMarker, VizPoints, VizStackMode, VizZone } from '../model/types';
import type { VizViewport } from '../model/viewport';
import {
  canvasFont,
  documentTheme,
  readCanvasColors,
  resolveColor,
  type Rgba,
  type VizCanvasColors,
  VIZ_THEME_ATTRIBUTES,
} from './colors';
import {
  buildLayer,
  easeLayer,
  layerFilled,
  layerKnot,
  layerValue,
  refreshTangents,
  type WaveLayer,
} from './layer';
import type { VizChartRenderer, VizPoint } from './renderer';

export const WAVE_STATUS = {
  loading: 'loading',
  ready: 'ready',
  empty: 'empty',
  error: 'error',
} as const;

export type WaveStatus = (typeof WAVE_STATUS)[keyof typeof WAVE_STATUS];

/** Слова на холсте. Пустая строка — подпись не рисуется. */
export interface WaveLabels {
  /** У вертикали текущего момента. */
  readonly now: string;
  /** Над пропуском в данных. */
  readonly gap: string;
  /** Перед значением максимума. */
  readonly peak: string;
  /** Перед суммой у одиночной точки, когда серий несколько. */
  readonly total: string;
  /** После даты отрезка, который ещё идёт. */
  readonly current: string;
}

export const DEFAULT_WAVE_LABELS: WaveLabels = {
  now: 'сейчас',
  gap: 'нет данных',
  peak: 'пик',
  total: 'Всего',
  current: 'идёт сейчас',
};

/** Окна сравнения «до → после» вокруг изменения. */
export interface WaveEffectWindow {
  readonly t: number;
  /** Длина окна «до», мс. */
  readonly window: number;
  readonly afterEnd: number;
  readonly color?: string;
  readonly beforeLabel?: string;
  readonly afterLabel?: string;
}

export interface WaveEngineOptions {
  readonly mode: VizStackMode;
  /** 0 — ломаная, 1 — плавная кривая. */
  readonly smooth: number;
  /** Плотность заливки под линией, 0…1. */
  readonly gradient: number;
  readonly lineWidth: number;
  readonly points: VizPoints;
  readonly grid: boolean;
  readonly zones: boolean;
  /** Подпись максимума. */
  readonly peak: boolean;
  /** Множитель длительности анимаций: 1 — как задумано, 4 — замедленно, 0 — без анимаций. */
  readonly slow: number;
  readonly unit: VizUnit;
  readonly timeZone: VizTimeZone;
  /** Режим разметки периода: протяжка выделяет, а не сдвигает. */
  readonly zoneDraw: boolean;
  readonly zoneDrawTone: VizZoneTone;
  /** Зоны ярче обычного (обучение, подсветка). */
  readonly zoneFocus: boolean;
  /** Живые продажи: точки на концах линий. */
  readonly live: boolean;
  readonly labels: WaveLabels;
}

export const DEFAULT_WAVE_OPTIONS: WaveEngineOptions = {
  mode: 'overlay',
  smooth: 1,
  gradient: 0.6,
  lineWidth: 2,
  points: 'auto',
  grid: true,
  zones: true,
  peak: false,
  slow: 1,
  unit: VIZ_UNIT_COUNT,
  timeZone: MOSCOW_ZONE,
  zoneDraw: false,
  zoneDrawTone: 'purple',
  zoneFocus: false,
  live: false,
  labels: DEFAULT_WAVE_LABELS,
};

export interface WaveEngineSeries {
  readonly id: string;
  /** Любой CSS-цвет. */
  readonly color: string;
}

export interface WaveHover {
  /** Индекс отрезка в текущем кадре. */
  readonly k: number;
  readonly start: number;
  readonly step: number;
  /** Середина отрезка по X, CSS-пиксели от левого края холста. */
  readonly x: number;
  /** Где на линии стоит якорь подсказки, CSS-пиксели от верха холста. */
  readonly y: number;
  /** Серия под курсором (на линии или внутри её заливки). */
  readonly seriesId: string | null;
}

export interface WavePress {
  readonly seriesId: string | null;
  readonly k: number;
  readonly start: number;
  readonly pointerType: string;
}

export interface WaveZoneHit {
  readonly zone: VizZone;
  readonly x: number;
  readonly y: number;
  readonly width: number;
}

export interface WaveEngineCallbacks {
  onHover?(hover: WaveHover | null): void;
  onPress?(press: WavePress): void;
  onZoneHover?(hit: WaveZoneHit | null): void;
  onZoneRemove?(id: string): void;
  /** Протяжка в режиме разметки: черновик периода; `done` — палец/кнопка отпущены. */
  onZoneDraft?(range: VizTimeRange | null, done: boolean): void;
  /** Пользователь сам взялся за окно: колесо, протяжка, щипок. */
  onInteract?(): void;
  onEscape?(): void;
  /** Каждый нарисованный кадр — оверлеи подстраивают позиции. */
  onPaint?(): void;
}

export interface PlotRect {
  readonly left: number;
  readonly top: number;
  readonly width: number;
  readonly height: number;
}

export interface WaveSeriesState {
  id: string;
  color: Rgba;
  /** Видимость: вес серии 0…1 и цель. */
  w: number;
  wt: number;
  /** Яркость: 1 — обычная, меньше — приглушена фокусом на другой. */
  e: number;
  et: number;
  /** Выделенность: 1 — серия в фокусе. */
  f: number;
  ft: number;
  /** Появление: 0 → 1. */
  rv: number;
}

export interface Pulse {
  readonly id: string;
  readonly t: number;
  readonly born: number;
}

export interface Float {
  readonly id: string;
  readonly t: number;
  readonly text: string;
  readonly born: number;
}

export interface Sample {
  readonly ts: Float64Array;
  readonly tops: Float64Array[];
  readonly bottoms: Float64Array[];
  readonly max: number;
  readonly bucketPx: number;
  readonly kA: number;
  readonly kB: number;
}

interface Down {
  x: number;
  y: number;
  start: VizTimeRange;
  moved: boolean;
  mode: 'pan' | 'zone';
  pointerType: string;
  hist: [number, number][];
  zone: { hit: ZoneHitBox | null };
}

export interface ZoneHitBox {
  readonly zone: VizZone;
  readonly x0: number;
  readonly x1: number;
  readonly y0: number;
  readonly y1: number;
  readonly close: { x0: number; x1: number } | null;
}

/** Полоса над 100% в режиме долей, px: место под подписи зон. */
const PERCENT_HEADROOM = 34;

const clamp = (v: number, a: number, b: number) => Math.min(b, Math.max(a, v));
const approach = (cur: number, target: number, dt: number, tau: number) =>
  tau <= 0 ? target : cur + (target - cur) * (1 - Math.exp(-dt / tau));
const settle = (cur: number, target: number, dt: number, tau: number, eps = 1e-3) => {
  const next = approach(cur, target, dt, tau);
  return Math.abs(next - target) < eps ? target : next;
};

/**
 * Движок графика времени на canvas: окно, слои данных, стопка, оси, зоны, значки, зум, сдвиг,
 * щипок и касания; сообщает наружу, что под курсором. Как рисовать сами данные — волнами или
 * столбиками — решает `renderer`. Ничего не знает про React, тултипы и легенду: компоненты
 * вокруг читают геометрию через `xOf`/`yOf`/`plot`.
 */
export class VizTimeEngine {
  readonly canvas: HTMLCanvasElement;
  readonly ctx: CanvasRenderingContext2D;
  /** Холст не умеет 2D: рисовать нечем. */
  readonly inert: boolean;
  options: WaveEngineOptions = DEFAULT_WAVE_OPTIONS;
  callbacks: WaveEngineCallbacks = {};

  series: WaveSeriesState[] = [];
  layer: WaveLayer | null = null;
  prev: WaveLayer | null = null;
  morph = 1;
  status: WaveStatus = WAVE_STATUS.loading;
  refreshing = false;
  refA = 0;
  stackMix = 0;
  pctMix = 0;
  ymax = 10;
  ysnap = true;
  ysets: { step: number; a: number }[] = [];
  /** Следующий кадр — другая метрика: показанные значения сразу растягиваются под новую шкалу. */
  private rescaleNext = false;
  xalpha = new Map<number | 'month', number>();
  reveal = 1;
  private revealAt = 0;
  zones: VizZone[] = [];
  zoneAlpha = new Map<string, number>();
  zoneHits: ZoneHitBox[] = [];
  zoneHoverId: string | null = null;
  markers: VizMarker[] = [];
  markerFocus: string | null = null;
  now = 0;
  eventAt: number | null = null;
  hover: { k: number; y: number; snap: boolean } | null = null;
  cx: number | null = null;
  pick: string | null = null;
  legendFocus: string | null = null;
  pinned: string | null = null;
  affected: readonly string[] | null = null;
  draft: VizTimeRange | null = null;
  effectWindow: WaveEffectWindow | null = null;
  /** Видимость скелетона 0…1. */
  skel = 1;
  pulses: Pulse[] = [];
  floats: Float[] = [];
  colors!: VizCanvasColors;
  theme: VizTheme = 'dark';
  font = 'Inter, system-ui, sans-serif';
  dpr = 1;
  W = 0;
  H = 0;
  /** Левое поле под подписи оси Y — подстраивается под самую длинную. */
  axisLeft = 48;
  private axisSnap = true;
  readonly pad = { right: 14, top: 36, bottom: 26 };

  private raf = 0;
  private last = 0;
  private readonly paintListeners = new Set<() => void>();
  private down: Down | null = null;
  private dragging = false;
  /** Ведут пальцем по значениям (долгое нажатие): жест не листает график и не считается тапом. */
  private scrubbing = false;
  private readonly pointers = new Map<number, { x: number; y: number }>();
  private pinch: { dist: number; range: VizTimeRange; anchor: number } | null = null;
  private keyboard = false;
  private readonly unsubscribe: () => void;
  private readonly resizeObserver: ResizeObserver | null;
  private readonly themeObserver: MutationObserver | null;

  constructor(
    canvas: HTMLCanvasElement,
    readonly viewport: VizViewport,
    readonly renderer: VizChartRenderer,
  ) {
    this.canvas = canvas;
    this.canvas.dataset.chart = renderer.kind;
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
    this.bind();
    this.resize();
  }

  destroy(): void {
    cancelAnimationFrame(this.raf);
    this.raf = 0;
    this.unsubscribe();
    this.resizeObserver?.disconnect();
    this.themeObserver?.disconnect();
    this.unbind();
  }

  // ───────── входные данные ─────────

  setOptions(patch: Partial<WaveEngineOptions>): void {
    this.options = { ...this.options, ...patch };
    this.canvas.dataset.zoneDraw = this.options.zoneDraw ? 'true' : 'false';
    if (!this.options.zoneDraw && this.draft) {
      this.draft = null;
      this.callbacks.onZoneDraft?.(null, true);
    }
    this.kick();
  }

  setSeries(list: readonly WaveEngineSeries[]): void {
    const old = new Map(this.series.map((s) => [s.id, s]));
    this.series = list.map((item) => {
      const prev = old.get(item.id);
      return {
        id: item.id,
        color: resolveColor(item.color),
        w: prev?.w ?? 1,
        wt: prev?.wt ?? 1,
        e: prev?.e ?? 1,
        et: prev?.et ?? 1,
        f: prev?.f ?? 0,
        ft: prev?.ft ?? 0,
        rv: prev?.rv ?? 1,
      };
    });
    if (this.frame) this.setFrame(this.frame, true);
    this.kick();
  }

  /** Скрытые серии: плавно опускаются к нулю, стопка перестраивается. */
  setHidden(hidden: ReadonlySet<string>): void {
    for (const s of this.series) s.wt = hidden.has(s.id) ? 0 : 1;
    this.kick();
  }

  private frame: VizBucketFrame | null = null;

  /**
   * Новый кадр. Та же детализация — значения доезжают до новых; другая — график перетекает
   * из старой детализации в новую.
   */
  setFrame(frame: VizBucketFrame | null, keepMotion = false): void {
    const rescale = this.rescaleNext && !!frame && frame !== this.frame;
    if (rescale) this.rescaleNext = false;
    this.frame = frame;
    const ids = this.series.map((s) => s.id);
    if (!frame) {
      this.layer = null;
      this.prev = null;
      this.kick();
      return;
    }
    const first = !this.layer;
    if (this.layer && this.layer.granularity !== frame.granularity && this.renderer.morph === 'aggregate') {
      // Столбики: новый отрезок вырастает из суммы старых, что в него попали (день → часы и обратно).
      this.layer = buildLayer(frame, ids, this.layer);
      this.prev = null;
      this.morph = 1;
    } else if (this.layer && this.layer.granularity !== frame.granularity) {
      this.prev = this.layer;
      this.layer = buildLayer(frame, ids, null);
      this.morph = 0;
    } else {
      this.layer = buildLayer(frame, ids, keepMotion || !first ? this.layer : null);
      if (rescale && !first) this.rescaleShown(this.layer);
    }
    if (first) {
      this.ysnap = true;
      this.axisSnap = true;
    }
    this.kick();
  }

  /** Метрика сменится со следующим кадром: старые кривые подгоняются под новую шкалу, а не взлетают с нуля. */
  expectRescale(): void {
    this.rescaleNext = true;
  }

  private rescaleShown(layer: WaveLayer): void {
    const peak = (arrays: readonly Float64Array[]) => {
      let best = 0;
      for (let i = 0; i < layer.length; i++) {
        let sum = 0;
        for (const a of arrays) if (a[i] === a[i]) sum += a[i];
        if (sum > best) best = sum;
      }
      return best;
    };
    const before = peak(layer.shown);
    const after = peak(layer.target);
    if (before <= 0 || after <= 0) return;
    const k = after / before;
    for (const a of layer.shown) for (let i = 0; i < a.length; i++) if (a[i] === a[i]) a[i] *= k;
    layer.settled = false;
    layer.tangentsDirty = true;
    this.ymax *= k;
    this.ysets = [];
  }

  setStatus(status: WaveStatus, refreshing = false): void {
    const was = this.status;
    this.status = status;
    this.refreshing = refreshing;
    if (status === WAVE_STATUS.ready && was !== WAVE_STATUS.ready) this.replay();
    if (status !== WAVE_STATUS.ready) {
      this.hoverLock = null;
      this.setHoverState(null);
    }
    this.kick();
  }

  /** Заново проиграть появление волн. */
  replay(): void {
    this.reveal = 0;
    this.revealAt = 0;
    this.ysnap = true;
    this.kick();
  }

  setTimeline(now: number, eventAt: number | null = null): void {
    this.now = now;
    this.eventAt = eventAt;
    this.kick();
  }

  setZones(zones: readonly VizZone[]): void {
    const known = new Set(this.zones.map((z) => z.id));
    for (const z of zones) if (!known.has(z.id) && this.zoneAlpha.size > 0) this.zoneAlpha.set(z.id, 0);
    for (const z of zones) if (!this.zoneAlpha.has(z.id)) this.zoneAlpha.set(z.id, 1);
    this.zones = [...zones];
    this.kick();
  }

  /** Значок в фокусе (наведён или закреплён) и серии, которых он касается. */
  setMarkers(markers: readonly VizMarker[], focusId: string | null): void {
    this.markers = [...markers];
    this.markerFocus = focusId;
    const marker = focusId ? markers.find((m) => m.id === focusId) : null;
    this.affected = marker?.series?.length ? marker.series : null;
    this.kick();
  }

  /** Серия под курсором в легенде и закреплённая серия. */
  setFocus(legendFocus: string | null, pinned: string | null): void {
    this.legendFocus = legendFocus;
    this.pinned = pinned;
    this.kick();
  }

  setEffectWindow(window: WaveEffectWindow | null): void {
    this.effectWindow = window;
    this.kick();
  }

  pulse(list: readonly { id: string; t: number }[]): void {
    const born = performance.now();
    for (const p of list) this.pulses.push({ ...p, born });
    this.kick();
  }

  float(list: readonly { id: string; t: number; text: string }[]): void {
    const born = performance.now();
    for (const f of list) this.floats.push({ ...f, born });
    this.kick();
  }

  /** Каждый нарисованный кадр — для соседей, выровненных по графику (полоса «весь период»). */
  subscribePaint(listener: () => void): () => void {
    this.paintListeners.add(listener);
    return () => this.paintListeners.delete(listener);
  }

  // ───────── геометрия для оверлеев ─────────

  get plot(): PlotRect {
    return {
      left: this.axisLeft,
      top: this.pad.top,
      width: Math.max(0, this.W - this.pad.right - this.axisLeft),
      height: Math.max(0, this.H - this.pad.bottom - this.pad.top),
    };
  }

  xOf(t: number): number {
    const { start, end } = this.viewport.state.current;
    const p = this.plot;
    return p.left + ((t - start) / (end - start)) * p.width;
  }

  tOf(x: number): number {
    const { start, end } = this.viewport.state.current;
    const p = this.plot;
    return start + ((x - p.left) / Math.max(p.width, 1)) * (end - start);
  }

  yOf(v: number): number {
    const p = this.plot;
    return p.top + p.height - (v / this.ymax) * p.height;
  }

  /** Значения серий в момент `t` с учётом стопки и видимости; `exact` — ровно в узле отрезка. */
  topsAt(t: number, exact = false): Float64Array {
    const n = this.series.length;
    const vals = new Float64Array(n);
    const tops = new Float64Array(n);
    const bottoms = new Float64Array(n);
    const L = this.layer;
    if (!L) return tops;
    const k = exact ? Math.round((t - L.c0) / L.step) : -1;
    for (let s = 0; s < n; s++) {
      const w = this.series[s].w;
      const v = exact && (L.length < 2 || this.morph >= 1) ? layerKnot(L, s, k) : this.valueAt(s, t);
      vals[s] = w < 1e-3 ? 0 : v * w;
    }
    stackAt(vals, this.stackMix, this.pctMix, tops, bottoms);
    return tops;
  }

  /** Середина отрезка `k` текущего кадра. */
  bucketCenter(k: number): number {
    return this.layer ? this.layer.c0 + k * this.layer.step : 0;
  }

  /** Значение серии `s` в момент `t`: во время смены детализации — смесь старой и новой. */
  valueAt(s: number, t: number): number {
    const smooth = this.options.smooth;
    const L = this.layer;
    if (!L) return Number.NaN;
    refreshTangents(L);
    const a = layerValue(L, s, t, smooth);
    const P = this.prev;
    if (!P || this.morph >= 1) return a;
    refreshTangents(P);
    const b = layerValue(P, P.ids.indexOf(this.series[s].id), t, smooth);
    if (a !== a) return b;
    if (b !== b) return a;
    const m = this.morph;
    return b + (a - b) * (m < 0.5 ? 4 * m * m * m : 1 - (-2 * m + 2) ** 3 / 2);
  }

  // ───────── цикл кадров ─────────

  kick(): void {
    if (this.raf || this.inert) return;
    this.last = performance.now();
    this.raf = requestAnimationFrame((t) => this.frameTick(t));
  }

  private frameTick(now: number): void {
    this.raf = 0;
    const dt = Math.min(50, Math.max(0, now - this.last));
    this.last = now;
    let busy = this.step(dt, now);
    busy = this.renderer.paint(this, dt, now) || busy;
    this.callbacks.onPaint?.();
    for (const listener of this.paintListeners) listener();
    if (busy || this.viewport.state.animating) this.raf = requestAnimationFrame((t) => this.frameTick(t));
  }

  private step(dt: number, now: number): boolean {
    const slow = this.options.slow;
    let busy = false;
    if (this.morph < 1) {
      this.morph = slow <= 0 ? 1 : Math.min(1, this.morph + dt / (620 * slow));
      busy = true;
      if (this.morph >= 1) this.prev = null;
    }
    if (this.layer && easeLayer(this.layer, dt, 190 * slow)) busy = true;
    if (this.prev) easeLayer(this.prev, dt, 190 * slow);
    const focus = this.legendFocus ?? this.pick ?? this.pinned;
    for (const s of this.series) {
      s.et = this.emphasis(s.id, focus);
      s.ft = focus === s.id ? 1 : 0;
      s.w = settle(s.w, s.wt, dt, 150 * slow);
      s.e = settle(s.e, s.et, dt, 110 * slow);
      s.f = settle(s.f, s.ft, dt, 110 * slow);
      if (s.w !== s.wt || s.e !== s.et || s.f !== s.ft) busy = true;
    }
    const mt = this.options.mode === 'overlay' ? 0 : 1;
    const pt = this.options.mode === 'percent' ? 1 : 0;
    this.stackMix = settle(this.stackMix, mt, dt, 170 * slow);
    this.pctMix = settle(this.pctMix, pt, dt, 170 * slow);
    if (this.stackMix !== mt || this.pctMix !== pt) busy = true;
    if (this.reveal < 1 && this.status === WAVE_STATUS.ready && this.layer) {
      if (!this.revealAt) this.revealAt = now - this.reveal * 1300 * slow;
      this.reveal = slow <= 0 ? 1 : Math.min(1, Math.max(1e-6, (now - this.revealAt) / (1300 * slow)));
      busy = true;
    }
    const rt = this.refreshing ? 1 : 0;
    this.refA = settle(this.refA, rt, dt, 140 * slow, 0.01);
    if (this.refA !== rt) busy = true;
    for (const z of this.zones) {
      const a = this.zoneAlpha.get(z.id) ?? 1;
      if (a < 1) {
        this.zoneAlpha.set(z.id, slow <= 0 ? 1 : Math.min(1, a + dt / (420 * slow)));
        busy = true;
      }
    }
    const life = Math.max(slow, 0.25);
    this.pulses = this.pulses.filter((p) => now - p.born < 1300 * life);
    this.floats = this.floats.filter((f) => now - f.born < 1600 * life);
    if (this.pulses.length || this.floats.length) busy = true;
    return busy;
  }

  /** Яркость серии: легенда приглушает сильнее всего, затем серия под курсором, затем значок изменения. */
  private emphasis(id: string, focus: string | null): number {
    if (this.legendFocus) return id === this.legendFocus ? 1 : 0.12;
    if (focus) return id === focus ? 1 : 0.2;
    if (this.affected) return this.affected.includes(id) ? 1 : 0.16;
    return 1;
  }

  /** Выборка кривых по ширине: шаг ~2,5 px плюс точные узлы, чтобы пики не срезались. */
  sample(): Sample | null {
    const L = this.layer;
    const p = this.plot;
    if (!L || L.length === 0 || p.width <= 0) return null;
    const { start, end } = this.viewport.state.current;
    const span = end - start;
    const bucketPx = (p.width * L.step) / span;
    const kA = Math.max(0, Math.floor((start - L.c0) / L.step));
    const kB = Math.min(L.length - 1, Math.ceil((end - L.c0) / L.step));
    const ts: number[] = [];
    if (bucketPx >= 4 || this.prev) {
      const N = Math.ceil(p.width / 2.5);
      let k = kA;
      for (let i = 0; i <= N; i++) {
        const t = start + (span * i) / N;
        while (k <= kB && L.c0 + k * L.step < t) {
          const c = L.c0 + k * L.step;
          if (c > start) ts.push(c);
          k++;
        }
        ts.push(t);
      }
    } else {
      ts.push(start);
      for (let k = kA; k <= kB; k++) {
        const c = L.c0 + k * L.step;
        if (c > start && c < end) ts.push(c);
      }
      ts.push(end);
    }
    const n = ts.length;
    const ns = this.series.length;
    const tops = this.series.map(() => new Float64Array(n));
    const bottoms = this.series.map(() => new Float64Array(n));
    const vals = new Float64Array(ns);
    const tp = new Float64Array(ns);
    const bt = new Float64Array(ns);
    let max = 0;
    for (let j = 0; j < n; j++) {
      for (let s = 0; s < ns; s++) {
        const w = this.series[s].w;
        vals[s] = w < 1e-3 ? 0 : this.valueAt(s, ts[j]) * w;
      }
      stackAt(vals, this.stackMix, this.pctMix, tp, bt);
      for (let s = 0; s < ns; s++) {
        tops[s][j] = tp[s];
        bottoms[s][j] = bt[s];
        if (tp[s] > max) max = tp[s];
      }
    }
    return { ts: Float64Array.from(ts), tops, bottoms, max, bucketPx, kA, kB };
  }

  /** Мало точек: подписываем значение у точки и держим запас сверху. */
  get sparse(): boolean {
    return !!this.layer && this.layer.length > 0 && layerFilled(this.layer) <= 2;
  }

  /** Верх оси Y под видимые значения: плавно доезжает, деления перетекают. */
  fitY(maxValue: number, dt: number): boolean {
    const slow = this.options.slow;
    let yT = maxValue;
    const percent = this.options.mode === 'percent' && this.pctMix > 0.98;
    // Доли: над 100% — полоса под подписи зон и этапов, иначе столбики и волны до верха их закрывают.
    if (percent)
      yT =
        (100 * this.plot.height) /
        Math.max(this.plot.height - Math.min(PERCENT_HEADROOM, this.plot.height * 0.18), 1);
    else if (yT > 0) yT = niceCeil(yT * (this.sparse ? 1.6 : 1.08));
    else yT = this.status === WAVE_STATUS.ready ? this.ymax : 10;
    let busy = false;
    if (this.ysnap && (this.layer || this.status !== WAVE_STATUS.ready)) {
      this.ymax = yT;
      this.ysnap = false;
      this.ysets = [];
    } else {
      this.ymax = approach(this.ymax, yT, dt, 210 * slow);
      if (Math.abs(this.ymax - yT) > yT * 1e-3) busy = true;
      else this.ymax = yT;
    }
    const step = percent ? 25 : niceStep(yT / 4);
    if (!this.ysets.some((s) => s.step === step)) this.ysets.push({ step, a: this.ysets.length ? 0 : 1 });
    for (const set of this.ysets) {
      const target = set.step === step ? 1 : 0;
      set.a = settle(set.a, target, dt, 160 * slow, 0.01);
      if (set.a !== target) busy = true;
    }
    this.ysets = this.ysets.filter((s) => s.a > 0 || s.step === step);
    return busy;
  }

  /** Левое поле под подписи: доезжает до ширины самой длинной подписи оси. */
  fitAxis(labelWidth: number, dt: number): boolean {
    const need = clamp(Math.ceil(labelWidth) + 20, 40, 96);
    if (Math.abs(need - this.axisLeft) < 0.5) return false;
    this.axisLeft = this.axisSnap ? need : approach(this.axisLeft, need, dt, 140 * this.options.slow);
    this.axisSnap = false;
    return true;
  }

  // ───────── попадание ─────────

  /** Серия под точкой холста — решает способ рисования. */
  hitSeries(x: number, y: number): string | null {
    const p = this.plot;
    if (y < p.top || y > p.top + p.height || !this.layer) return null;
    return this.renderer.hit(this, x, y);
  }

  /**
   * Якорь подсказки: отрезок `k` и серия под курсором (или закреплённая). `pinned` — стоять
   * ровно на отрезке, иначе следовать за плавно едущим перекрестьем.
   */
  tooltipAnchor(k: number, seriesId: string | null, pinned: boolean): VizPoint | null {
    if (!this.layer || k < 0 || k >= this.layer.length) return null;
    const point = this.renderer.anchor(this, k, seriesId, !pinned);
    const p = this.plot;
    if (!point || point.x < p.left - 2 || point.x > p.left + p.width + 2) return null;
    return point;
  }

  private hitZone(x: number, y: number): ZoneHitBox | null {
    if (!this.options.zones) return null;
    return this.zoneHits.find((h) => x >= h.x0 && x <= h.x1 && y >= h.y0 && y <= h.y1) ?? null;
  }

  private bucketAt(x: number): number {
    const L = this.layer;
    if (!L || L.length === 0) return -1;
    return clamp(Math.round((this.tOf(x) - L.c0) / L.step), 0, L.length - 1);
  }

  /** Состояние наведения; наружу — только когда сменился отрезок или серия. */
  /** Закреплённый отрезок: подсказка стоит на нём, движение мыши её не сдвигает. */
  private hoverLock: number | null = null;

  /**
   * Закрепить подсказку на отрезке `k` (клик мимо линий и столбиков) или снять закрепление (`null`).
   * Пока отрезок закреплён, наведение мыши и уход курсора с графика подсказку не трогают.
   */
  lockHover(k: number | null): void {
    if (this.hoverLock === k) return;
    this.hoverLock = k;
    const L = this.layer;
    if (k === null || !L || k < 0 || k >= L.length) {
      this.hoverLock = null;
      this.setHoverState(null);
      return;
    }
    this.setHoverState({ k, y: this.plot.top + this.plot.height / 2 });
  }

  setHoverState(next: { k: number; y: number } | null, seriesId: string | null = null): void {
    const before = this.hover;
    if (!next) {
      const had = !!before || this.pick !== null;
      this.hover = null;
      this.cx = null;
      this.pick = null;
      if (had) this.callbacks.onHover?.(null);
      this.kick();
      return;
    }
    this.hover = { k: next.k, y: next.y, snap: !before };
    const changed = !before || before.k !== next.k || this.pick !== seriesId;
    this.pick = seriesId;
    if (changed) this.emitHover();
    this.kick();
  }

  emitHover(): void {
    const L = this.layer;
    const h = this.hover;
    if (!L || !h) return;
    const point = this.renderer.anchor(this, h.k, this.pinned ?? this.pick, false);
    const x = point?.x ?? this.xOf(L.c0 + h.k * L.step);
    const y = point?.y ?? this.plot.top + this.plot.height - 8;
    this.callbacks.onHover?.({
      k: h.k,
      start: L.start + h.k * L.step,
      step: L.step,
      x,
      y,
      seriesId: this.pick,
    });
  }

  /** Палец держат и ведут: подсказка — у отрезка под пальцем, перетаскивание графика снято. */
  scrubAt(clientX: number, clientY: number): void {
    this.scrubbing = true;
    this.down = null;
    this.dragging = false;
    delete this.canvas.dataset.dragging;
    const L = this.layer;
    if (!L || this.status !== WAVE_STATUS.ready) return;
    const p = this.pos({ clientX, clientY });
    const x = clamp(p.x, this.plot.left, this.plot.left + this.plot.width);
    this.setHoverState({ k: this.bucketAt(x), y: p.y }, this.hitSeries(x, p.y));
  }

  endScrub(): void {
    if (!this.scrubbing) return;
    this.scrubbing = false;
    this.setHoverState(null);
  }

  /** Подсказку можно открыть с клавиатуры: стрелки ходят по отрезкам. */
  moveHover(delta: number): void {
    const L = this.layer;
    if (!L || L.length === 0) return;
    const p = this.plot;
    const k0 = this.hover ? this.hover.k : this.bucketAt(p.left + p.width - 10);
    const k = clamp(k0 + (this.hover ? delta : 0), 0, L.length - 1);
    const t = L.c0 + k * L.step;
    const { start, end } = this.viewport.state.target;
    if (t < start || t > end) {
      const span = end - start;
      this.viewport.setRange({ start: t - span / 2, end: t + span / 2 });
    }
    this.keyboard = true;
    this.setHoverState({ k, y: p.top + p.height * 0.35 });
  }

  // ───────── ввод ─────────

  private readonly handlers: [keyof HTMLElementEventMap, (event: never) => void, AddEventListenerOptions?][] =
    [];

  private bind(): void {
    const on = <K extends keyof HTMLElementEventMap>(
      type: K,
      fn: (event: HTMLElementEventMap[K]) => void,
      opts?: AddEventListenerOptions,
    ) => {
      this.canvas.addEventListener(type, fn as EventListener, opts);
      this.handlers.push([type, fn as (event: never) => void, opts]);
    };
    on('wheel', (e) => this.onWheel(e), { passive: false });
    on('pointerdown', (e) => this.onDown(e));
    on('pointermove', (e) => this.onMove(e));
    on('pointerup', (e) => this.onUp(e));
    on('pointercancel', (e) => this.onCancel(e));
    on('pointerleave', (e) => this.onLeave(e));
    on('keydown', (e) => this.onKey(e));
    on('blur', () => {
      if (this.keyboard) {
        this.keyboard = false;
        this.setHoverState(null);
      }
    });
  }

  private unbind(): void {
    for (const [type, fn, opts] of this.handlers)
      this.canvas.removeEventListener(type, fn as EventListener, opts);
    this.handlers.length = 0;
  }

  private pos(e: { clientX: number; clientY: number }) {
    const r = this.canvas.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  }

  private interactive(): boolean {
    return this.status === WAVE_STATUS.ready || this.status === WAVE_STATUS.empty;
  }

  private onWheel(e: WheelEvent): void {
    if (!this.interactive()) return;
    e.preventDefault();
    this.callbacks.onInteract?.();
    const { start, end } = this.viewport.state.target;
    if (Math.abs(e.deltaX) > Math.abs(e.deltaY) && !e.ctrlKey) {
      this.viewport.panBy((e.deltaX / Math.max(this.plot.width, 1)) * (end - start));
      return;
    }
    const p = this.pos(e);
    const plot = this.plot;
    const anchor =
      start +
      ((clamp(p.x, plot.left, plot.left + plot.width) - plot.left) / Math.max(plot.width, 1)) * (end - start);
    this.viewport.zoomAround(anchor, Math.exp(e.deltaY * (e.ctrlKey ? 0.012 : 0.0016)));
  }

  private onDown(e: PointerEvent): void {
    if (e.button !== 0 || !this.interactive()) return;
    const p = this.pos(e);
    this.pointers.set(e.pointerId, p);
    if (this.pointers.size === 2) {
      const [a, b] = [...this.pointers.values()];
      const range = this.viewport.state.target;
      this.pinch = { dist: Math.hypot(a.x - b.x, a.y - b.y), range, anchor: this.tOf((a.x + b.x) / 2) };
      this.down = null;
      this.callbacks.onInteract?.();
      return;
    }
    this.down = {
      x: p.x,
      y: p.y,
      start: this.viewport.state.target,
      moved: false,
      mode: this.options.zoneDraw ? 'zone' : 'pan',
      pointerType: e.pointerType,
      hist: [[performance.now(), p.x]],
      zone: { hit: this.hitZone(p.x, p.y) },
    };
    this.canvas.setPointerCapture?.(e.pointerId);
  }

  private onMove(e: PointerEvent): void {
    const p = this.pos(e);
    if (this.pointers.has(e.pointerId)) this.pointers.set(e.pointerId, p);
    if (this.pinch && this.pointers.size === 2) {
      const [a, b] = [...this.pointers.values()];
      const dist = Math.max(Math.hypot(a.x - b.x, a.y - b.y), 1);
      const f = this.pinch.dist / dist;
      const { range, anchor } = this.pinch;
      this.viewport.setRange(
        { start: anchor - (anchor - range.start) * f, end: anchor + (range.end - anchor) * f },
        { instant: true },
      );
      return;
    }
    if (this.scrubbing) return;
    const d = this.down;
    if (!d) {
      if (e.pointerType === 'mouse') this.onHover(p);
      return;
    }
    const dx = p.x - d.x;
    const dy = p.y - d.y;
    if (!d.moved) {
      if (Math.abs(dx) <= 3 && Math.abs(dy) <= 3) return;
      // Палец повёл вертикально — это прокрутка страницы, не график.
      if (d.pointerType !== 'mouse' && Math.abs(dy) > Math.abs(dx)) {
        this.down = null;
        return;
      }
      d.moved = true;
      if (d.mode === 'pan') {
        this.dragging = true;
        this.canvas.dataset.dragging = 'true';
        this.setHoverState(null);
        this.callbacks.onInteract?.();
      }
    }
    if (d.mode === 'pan') {
      const span = d.start.end - d.start.start;
      const shift = -(dx / Math.max(this.plot.width, 1)) * span;
      this.viewport.setRange({ start: d.start.start + shift, end: d.start.end + shift }, { instant: true });
      d.hist.push([performance.now(), p.x]);
      if (d.hist.length > 6) d.hist.shift();
    } else {
      this.draft = this.snapRange(this.tOf(d.x), this.tOf(p.x));
      this.callbacks.onZoneDraft?.(this.draft, false);
      this.kick();
    }
  }

  private onUp(e: PointerEvent): void {
    this.pointers.delete(e.pointerId);
    if (this.pinch) {
      if (this.pointers.size < 2) this.pinch = null;
      return;
    }
    const d = this.down;
    this.down = null;
    this.dragging = false;
    delete this.canvas.dataset.dragging;
    if (!d || this.scrubbing) return;
    const p = this.pos(e);
    if (d.moved && d.mode === 'pan') {
      const [ta, xa] = d.hist[0];
      const [tb, xb] = d.hist[d.hist.length - 1];
      if (tb - ta > 0 && performance.now() - tb < 80) {
        const v = (xb - xa) / (tb - ta);
        const span = this.viewport.state.target.end - this.viewport.state.target.start;
        this.viewport.panBy(-((v * 260) / Math.max(this.plot.width, 1)) * span);
      }
      return;
    }
    if (d.moved && d.mode === 'zone') {
      this.callbacks.onZoneDraft?.(this.draft, true);
      return;
    }
    const hit = d.zone.hit;
    if (hit?.close && p.x >= hit.close.x0 && p.x <= hit.close.x1) {
      this.callbacks.onZoneRemove?.(hit.zone.id);
      return;
    }
    if (hit || this.options.zoneDraw || this.status !== WAVE_STATUS.ready) return;
    const k = this.bucketAt(p.x);
    const seriesId = this.hitSeries(p.x, p.y);
    // Касание: сначала показать подсказку у точки, как наведение мышью.
    if (e.pointerType !== 'mouse' && k >= 0) this.setHoverState({ k, y: p.y }, seriesId);
    this.callbacks.onPress?.({
      seriesId,
      k,
      start: this.layer ? this.layer.start + k * this.layer.step : 0,
      pointerType: e.pointerType,
    });
  }

  private onCancel(e: PointerEvent): void {
    this.pointers.delete(e.pointerId);
    if (this.pointers.size < 2) this.pinch = null;
    this.down = null;
    this.dragging = false;
    delete this.canvas.dataset.dragging;
  }

  private onLeave(e: PointerEvent): void {
    if (this.down || e.pointerType !== 'mouse' || this.hoverLock !== null) return;
    this.setHoverState(null);
    if (this.zoneHoverId) {
      this.zoneHoverId = null;
      this.callbacks.onZoneHover?.(null);
    }
    this.canvas.dataset.pointer = 'false';
  }

  private onHover(p: { x: number; y: number }): void {
    if (this.hoverLock !== null) return;
    const zh = this.interactive() && !this.options.zoneDraw ? this.hitZone(p.x, p.y) : null;
    if ((zh?.zone.id ?? null) !== this.zoneHoverId) {
      this.zoneHoverId = zh?.zone.id ?? null;
      this.callbacks.onZoneHover?.(zh ? { zone: zh.zone, x: zh.x0, y: zh.y0, width: zh.x1 - zh.x0 } : null);
      this.kick();
    }
    if (zh) {
      this.canvas.dataset.pointer = zh.zone.removable ? 'true' : 'false';
      if (this.hover) this.setHoverState(null);
      return;
    }
    const plot = this.plot;
    if (
      this.status !== WAVE_STATUS.ready ||
      !this.layer ||
      p.x < plot.left - 4 ||
      p.x > plot.left + plot.width + 4 ||
      p.y > plot.top + plot.height + 6
    ) {
      if (this.hover) this.setHoverState(null);
      this.canvas.dataset.pointer = 'false';
      return;
    }
    const seriesId = this.options.zoneDraw ? null : this.hitSeries(p.x, p.y);
    this.canvas.dataset.pointer = seriesId ? 'true' : 'false';
    this.setHoverState({ k: this.bucketAt(p.x), y: p.y }, seriesId);
  }

  private onKey(e: KeyboardEvent): void {
    if (this.status !== WAVE_STATUS.ready || !this.layer) return;
    const plot = this.plot;
    const centre = this.cx ?? plot.left + plot.width / 2;
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      this.moveHover(e.key === 'ArrowLeft' ? -1 : 1);
    } else if (e.key === '+' || e.key === '=') {
      e.preventDefault();
      this.viewport.zoomAround(this.tOf(centre), 0.7);
    } else if (e.key === '-' || e.key === '_') {
      e.preventDefault();
      this.viewport.zoomAround(this.tOf(centre), 1.45);
    } else if (e.key === '0') {
      e.preventDefault();
      this.viewport.setRange(this.viewport.extent);
    } else if (e.key === 'Escape') {
      this.setHoverState(null);
      this.callbacks.onEscape?.();
    } else if (e.key === 'Enter' && this.hover) {
      e.preventDefault();
      const L = this.layer;
      this.callbacks.onPress?.({
        seriesId: this.pick,
        k: this.hover.k,
        start: L.start + this.hover.k * L.step,
        pointerType: 'keyboard',
      });
    }
  }

  /** Края черновика зоны прилипают к часам (вблизи) или дням. */
  private snapRange(a: number, b: number): VizTimeRange {
    const granularity = this.layer && this.layer.granularity === 'hour' ? 'hour' : 'day';
    const step = GRANULARITY_STEP[granularity];
    const snap = (t: number) => {
      const f = floorTo(t, granularity, this.options.timeZone);
      return t - f > step / 2 ? f + step : f;
    };
    const x = snap(a);
    const y = snap(b);
    return { start: Math.min(x, y), end: Math.max(x, y) };
  }

  // ───────── тема и размер ─────────

  readTheme(): void {
    this.colors = readCanvasColors(this.canvas);
    this.theme = documentTheme();
    this.font = canvasFont(this.canvas);
    this.kick();
  }

  resize(): void {
    const dpr = Math.min(2.5, globalThis.devicePixelRatio || 1);
    const W = this.canvas.clientWidth;
    const H = this.canvas.clientHeight;
    if (W <= 0 || H <= 0) return;
    this.dpr = dpr;
    this.W = W;
    this.H = H;
    this.canvas.width = Math.round(W * dpr);
    this.canvas.height = Math.round(H * dpr);
    this.kick();
  }

  get isDragging(): boolean {
    return this.dragging;
  }

  /** Часовой шаг по умолчанию для выборки, когда кадра ещё нет. */
  static readonly FALLBACK_STEP = HOUR;
}
