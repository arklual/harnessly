// The loop of a scene on one canvas: the worker's OffscreenCanvas (scene-worker.ts) or the page's own canvas when the
// browser has no OffscreenCanvas. It draws at the screen's rate (requestAnimationFrame; a timer of ~60 Hz where a
// worker has none), stops while the page is hidden or the person asked for less motion (one still frame then), and
// starts its scene anew on a resize or a change of the look.
//
// The dim and the grain of a look are drawn here, over the scene, so nothing above the canvas is blended anew each
// frame. And the loop watches itself: when a frame takes too long to draw, or frames come late, it lowers the scene's
// detail (fewer particles, a smaller canvas) one step at a time, so a weak device keeps the rate, not the detail.
import { SCENE_SCALE, makeScene, type IScene } from '@/next/features/looks/scenes';
import type { ISceneColors } from '@/next/features/looks/tokens';
import type { TScene } from '@/next/features/looks/types';

export interface ISceneSpec {
  scene: TScene;
  colors: ISceneColors;
  /** 0–1. */
  speed: number;
  /** The stage's colour over the scene, 0–1. */
  dim: number;
  /** The grain's strength, 0–1. */
  grain: number;
}

export interface ISceneSize {
  /** CSS pixels. */
  w: number;
  h: number;
  dpr: number;
}

type TCanvas = OffscreenCanvas | HTMLCanvasElement;
type TCtx = OffscreenCanvasRenderingContext2D | CanvasRenderingContext2D;

type TRaf = (fn: (now: number) => void) => number;

const rafOf = (): { raf: TRaf; cancel: (id: number) => void } => {
  const g = globalThis as unknown as {
    requestAnimationFrame?: TRaf;
    cancelAnimationFrame?: (id: number) => void;
  };
  if (typeof g.requestAnimationFrame === 'function' && typeof g.cancelAnimationFrame === 'function')
    return { raf: g.requestAnimationFrame.bind(globalThis), cancel: g.cancelAnimationFrame.bind(globalThis) };
  return {
    raf: (fn) => setTimeout(() => fn(performance.now()), 16) as unknown as number,
    cancel: (id) => clearTimeout(id),
  };
};

/** The steps of detail a scene goes down when it runs late; it never goes below the last. */
export const DETAILS = [1, 0.7, 0.5, 0.35] as const;
/** Frames in a window the loop judges itself over (about 2 seconds). */
const WINDOW = 120;
/** A window is late when drawing took more than this on average (ms)… */
const DRAW_BUDGET = { worker: 6, page: 3 } as const;
/** …or when more than this share of its frames came after a missed one. */
const LATE_SHARE = 0.2;

/** A tile of grey noise for the grain, made once per canvas. */
const noiseTile = (): TCanvas | null => {
  const N = 128;
  const canvas =
    typeof OffscreenCanvas !== 'undefined'
      ? new OffscreenCanvas(N, N)
      : typeof document !== 'undefined'
        ? Object.assign(document.createElement('canvas'), { width: N, height: N })
        : null;
  const ctx = canvas?.getContext('2d') as TCtx | null | undefined;
  if (!canvas || !ctx) return null;
  const img = ctx.createImageData(N, N);
  let seed = 9;
  for (let i = 0; i < N * N; i++) {
    seed = (seed * 1664525 + 1013904223) >>> 0;
    const v = seed >>> 24;
    img.data[i * 4] = v;
    img.data[i * 4 + 1] = v;
    img.data[i * 4 + 2] = v;
    img.data[i * 4 + 3] = 255;
  }
  ctx.putImageData(img, 0, 0);
  return canvas;
};

export class SceneRunner {
  private ctx: TCtx | null;
  private scene: IScene | null = null;
  private spec: ISceneSpec | null = null;
  private size: ISceneSize = { w: 0, h: 0, dpr: 1 };
  private frameId: number | null = null;
  private start = 0;
  private last = 0;
  private running = false;
  private still = false;
  private readonly loop = rafOf();
  private detailStep = 0;
  private grain: CanvasPattern | null = null;
  // The window the loop judges itself over.
  private frames = 0;
  private drawMs = 0;
  private late = 0;
  private prevNow = 0;

  constructor(
    private readonly canvas: TCanvas,
    /** Where it runs: a worker's budget is larger, its thread does nothing else. */
    private readonly where: 'worker' | 'page' = 'page',
    /** Told when the detail goes down (the page marks it for the person's diagnostics). */
    private readonly onDetail?: (detail: number) => void,
  ) {
    this.ctx = canvas.getContext('2d', { alpha: false }) as TCtx | null;
  }

  get detail(): number {
    return DETAILS[this.detailStep];
  }

  set(spec: ISceneSpec | null, size: ISceneSize): void {
    // A new look starts at full detail again; a resize keeps what this device showed it can draw.
    if (spec?.scene !== this.spec?.scene) this.detailStep = 0;
    this.spec = spec;
    this.size = size;
    this.rebuild();
  }

  /** Runs (true) or holds on its last frame (false): the page is hidden. */
  play(on: boolean, still: boolean): void {
    this.still = still;
    if (on && !still) this.resume();
    else this.pause();
    if (still) this.drawOnce();
  }

  private rebuild(): void {
    const { ctx, spec } = this;
    if (!ctx || !spec || this.size.w < 1 || this.size.h < 1) {
      this.scene = null;
      return;
    }
    // Below half the detail the canvas itself gets smaller: fewer pixels to fill and to hand to the screen.
    const shrink = this.detail < 0.5 ? 0.75 : 1;
    const px = Math.min(1.5, this.size.dpr) * SCENE_SCALE[spec.scene] * shrink;
    const w = Math.max(1, Math.round(this.size.w * px));
    const h = Math.max(1, Math.round(this.size.h * px));
    if (this.canvas.width !== w) this.canvas.width = w;
    if (this.canvas.height !== h) this.canvas.height = h;
    ctx.setTransform(px, 0, 0, px, 0, 0);
    this.scene = makeScene(spec.scene, this.size.w, this.size.h, spec.colors, spec.speed, this.detail, px);
    if (spec.grain > 0 && !this.grain) {
      const tile = noiseTile();
      this.grain = tile ? ctx.createPattern(tile, 'repeat') : null;
    }
    this.resetWindow();
    this.drawOnce();
  }

  private paint(t: number, dt: number): void {
    const { ctx, scene, spec } = this;
    if (!ctx || !scene || !spec) return;
    scene.frame(ctx, t, dt);
    if (spec.dim > 0) {
      ctx.globalAlpha = spec.dim;
      ctx.fillStyle = spec.colors.stage;
      ctx.fillRect(0, 0, this.size.w, this.size.h);
    }
    if (spec.grain > 0 && this.grain) {
      ctx.globalAlpha = spec.grain * (spec.colors.mode === 'dark' ? 0.18 : 0.12);
      ctx.globalCompositeOperation = 'overlay';
      ctx.fillStyle = this.grain;
      ctx.fillRect(0, 0, this.size.w, this.size.h);
      ctx.globalCompositeOperation = 'source-over';
    }
    ctx.globalAlpha = 1;
  }

  private drawOnce(): void {
    this.paint(this.last / 1000, 0);
  }

  private resetWindow(): void {
    this.frames = 0;
    this.drawMs = 0;
    this.late = 0;
    this.prevNow = 0;
  }

  /** One frame's account: how long it drew and whether it came after a missed one. */
  private judge(now: number, drew: number): void {
    if (this.prevNow > 0 && now - this.prevNow > 25) this.late++;
    this.prevNow = now;
    this.drawMs += drew;
    this.frames++;
    if (this.frames < WINDOW) return;
    const slow = this.drawMs / this.frames > DRAW_BUDGET[this.where] || this.late / this.frames > LATE_SHARE;
    this.resetWindow();
    if (!slow || this.detailStep >= DETAILS.length - 1) return;
    this.detailStep++;
    this.onDetail?.(this.detail);
    this.rebuild();
  }

  private resume(): void {
    if (this.running || !this.scene || this.scene.still) return;
    this.running = true;
    this.prevNow = 0;
    const tick = (now: number) => {
      if (!this.running) return;
      if (this.start === 0) this.start = now - this.last;
      const t = now - this.start;
      const dt = Math.min(0.05, Math.max(0, (t - this.last) / 1000));
      this.last = t;
      const before = performance.now();
      this.paint(t / 1000, dt);
      this.judge(now, performance.now() - before);
      this.frameId = this.loop.raf(tick);
    };
    this.frameId = this.loop.raf(tick);
  }

  private pause(): void {
    this.running = false;
    this.start = 0;
    if (this.frameId !== null) this.loop.cancel(this.frameId);
    this.frameId = null;
  }

  stop(): void {
    this.pause();
    this.scene = null;
  }
}
