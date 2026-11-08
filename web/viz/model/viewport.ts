// SPDX-License-Identifier: Apache-2.0
import { HOUR, type VizTimeRange } from './time';

export interface VizViewportOptions {
  /** Вся история: окно не выходит за её границы. */
  readonly extent: VizTimeRange;
  /** Окно на старте. По умолчанию — вся история. */
  readonly initial?: VizTimeRange;
  /** Самое узкое окно, мс. По умолчанию 3 часа. */
  readonly minSpan?: number;
  /** Постоянная времени сглаживания, мс: больше — медленнее доезжает. */
  readonly tau?: number;
}

export interface VizViewportState {
  /** Окно, нарисованное сейчас (в движении). */
  readonly current: VizTimeRange;
  /** Окно, к которому оно едет. */
  readonly target: VizTimeRange;
  readonly animating: boolean;
}

export type VizViewportListener = (state: VizViewportState) => void;

export interface SetRangeOptions {
  /** Без анимации: перетаскивание пальцем, ручка навигатора. */
  readonly instant?: boolean;
}

const approach = (cur: number, target: number, dt: number, tau: number) =>
  cur + (target - cur) * (1 - Math.exp(-dt / Math.max(tau, 1)));

/**
 * Окно времени, общее для графика, полосы «весь период», значков и кнопок зума.
 * Не знает про React и canvas: держит цель и плавно подводит к ней текущее окно
 * (центр и логарифм ширины — зум ощущается равномерным на любом масштабе).
 * Подписчики получают каждый кадр движения.
 */
export class VizViewport {
  private extentRange: VizTimeRange;
  private targetRange: VizTimeRange;
  private currentRange: VizTimeRange;
  private readonly listeners = new Set<VizViewportListener>();
  private readonly minSpan: number;
  private tau: number;
  private frame = 0;
  private last = 0;

  constructor(options: VizViewportOptions) {
    this.extentRange = options.extent;
    this.minSpan = options.minSpan ?? 3 * HOUR;
    this.tau = options.tau ?? 120;
    const initial = this.clamp(options.initial ?? options.extent);
    this.targetRange = initial;
    this.currentRange = initial;
  }

  get extent(): VizTimeRange {
    return this.extentRange;
  }

  get state(): VizViewportState {
    return { current: this.currentRange, target: this.targetRange, animating: this.frame !== 0 };
  }

  /** Множитель длительности анимаций (замедленная съёмка в сторис, 0 — без анимации). */
  setTau(tau: number): void {
    this.tau = tau;
  }

  subscribe(listener: VizViewportListener): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  setExtent(extent: VizTimeRange): void {
    this.extentRange = extent;
    this.targetRange = this.clamp(this.targetRange);
    this.currentRange = this.clamp(this.currentRange);
    this.kick();
  }

  setRange(range: VizTimeRange, options: SetRangeOptions = {}): void {
    this.targetRange = this.clamp(range);
    if (options.instant || this.tau <= 0) {
      this.currentRange = this.targetRange;
      this.emit();
      return;
    }
    this.kick();
  }

  /** Зум вокруг момента `anchor`: `factor` < 1 — приблизить, > 1 — отдалить. */
  zoomAround(anchor: number, factor: number, options: SetRangeOptions = {}): void {
    const { start, end } = this.targetRange;
    this.setRange(
      { start: anchor - (anchor - start) * factor, end: anchor + (end - anchor) * factor },
      options,
    );
  }

  /** Сдвиг окна на `ms` (положительный — вправо, в будущее). */
  panBy(ms: number, options: SetRangeOptions = {}): void {
    this.setRange({ start: this.targetRange.start + ms, end: this.targetRange.end + ms }, options);
  }

  /** Один шаг анимации на `dt` мс. Возвращает, едет ли окно дальше. Нужен тестам и своему циклу. */
  step(dt: number): boolean {
    const c = this.currentRange;
    const t = this.targetRange;
    const cc = (c.start + c.end) / 2;
    const cs = Math.log(c.end - c.start);
    const tc = (t.start + t.end) / 2;
    const ts = Math.log(t.end - t.start);
    let nc = approach(cc, tc, dt, this.tau);
    let ns = approach(cs, ts, dt, this.tau);
    const done = Math.abs(nc - tc) < (t.end - t.start) * 2e-4 && Math.abs(ns - ts) < 2e-4;
    if (done) {
      nc = tc;
      ns = ts;
    }
    const half = Math.exp(ns) / 2;
    this.currentRange = done ? t : { start: nc - half, end: nc + half };
    return !done;
  }

  dispose(): void {
    if (this.frame && typeof cancelAnimationFrame === 'function') cancelAnimationFrame(this.frame);
    this.frame = 0;
    this.listeners.clear();
  }

  private clamp(range: VizTimeRange): VizTimeRange {
    const e = this.extentRange;
    const full = Math.max(e.end - e.start, 1);
    const span = Math.min(Math.max(range.end - range.start, Math.min(this.minSpan, full)), full);
    const center = (range.start + range.end) / 2;
    let start = center - span / 2;
    let end = center + span / 2;
    if (start < e.start) {
      end += e.start - start;
      start = e.start;
    }
    if (end > e.end) {
      start -= end - e.end;
      end = e.end;
    }
    return { start: Math.max(e.start, start), end: Math.min(e.end, end) };
  }

  private kick(): void {
    if (this.frame || typeof requestAnimationFrame !== 'function') {
      if (typeof requestAnimationFrame !== 'function') {
        this.currentRange = this.targetRange;
        this.emit();
      }
      return;
    }
    this.last = performance.now();
    const tick = (now: number) => {
      const dt = Math.min(50, Math.max(0, now - this.last));
      this.last = now;
      const moving = this.step(dt);
      this.frame = moving ? requestAnimationFrame(tick) : 0;
      this.emit();
    };
    this.frame = requestAnimationFrame(tick);
  }

  private emit(): void {
    const state = this.state;
    for (const listener of this.listeners) listener(state);
  }
}
