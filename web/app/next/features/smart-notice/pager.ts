// Paging the stack of smart notices under a finger, a trackpad and a wheel. The stack has one continuous
// position: 0 is the front plate at rest, +1 the next one in front, −1 the one before. While a gesture leads, the
// position is the finger's travel towards the plate's edge over one page, so the plate it holds goes with it one to one
// and the plates behind come forward by the same share (scale, offset, shade, all continuous). Let go, a spring carries
// it on with the gesture's own speed to the nearest plate, and a new touch takes it wherever it is. Every frame writes
// translate, scale and opacity on four elements, and nothing re-renders; React hears only when a plate has become the
// front one (onStep), and the position is moved by one page in the same layout pass, so nothing jumps.

/** The plates drawn behind the front one at rest; one more waits invisible, to come forward while paging. */
export const STACK_DEPTH = 2;

/** Shade of a plate at depth 0, 1, 2, 3: white over black (#000, #2c2c2e, #48484a, and a step on). */
const TINT = [0, 0.172, 0.282, 0.36] as const;
/** Scale lost per step back. */
const SCALE_STEP = 0.07;

export interface IStackLook {
  /** Towards the edge, px (positive goes to the edge the notice stands at). */
  toEdge: number;
  scale: number;
  opacity: number;
  /** White over the plate: deeper plates are lighter. */
  tint: number;
  /** What the plate shows (the front one's text) fades as it goes back. */
  content: number;
}

/** How far a plate behind peeks out at depth `d` (1, 2, …), px away from the edge: as in styles/smart-notice.css. */
const peek = (d: number, height: number): number => {
  const one = height * 0.07 + 9;
  const two = height * 0.14 + 17;
  if (d <= 1) return one * d;
  return one + (two - one) * (d - 1);
};

const lerpTint = (d: number): number => {
  const at = Math.min(TINT.length - 1, Math.max(0, d));
  const i = Math.min(TINT.length - 2, Math.floor(at));
  return TINT[i] + (TINT[i + 1] - TINT[i]) * (at - i);
};

/**
 * The look of a plate at depth `d` from the front: 0 the front, 1 and 2 behind, 3 hidden behind them; below 0 it is in
 * front of the stack, gone towards the edge by `page` px per step and fading (the one the finger carries away, or the
 * one it brings in).
 */
export const stackLook = (d: number, height: number, page: number): IStackLook => {
  if (d <= 0) {
    const a = Math.min(1, -d);
    return { toEdge: 0 - page * d, scale: 1, opacity: 1 - a, tint: 0, content: 1 };
  }
  return {
    toEdge: -peek(d, height),
    scale: 1 - SCALE_STEP * Math.min(d, 3),
    opacity: d <= STACK_DEPTH ? 1 : Math.max(0, 1 - (d - STACK_DEPTH)),
    tint: lerpTint(d),
    content: Math.max(0, 1 - d * 1.6),
  };
};

/** One page of finger travel, px: about the plate's own height, so the next one comes in as this one leaves. */
export const pageSize = (height: number): number => Math.min(96, Math.max(56, height + 24));

/** Where a released position goes: the gesture's speed carries it on a while, then the nearest plate, at most one. */
export const pageTarget = (pos: number, velocity: number): number => {
  // velocity: positions per second; ~0.18 s of glide, as a flicked list does.
  const projected = pos + velocity * 0.18;
  return Math.max(-1, Math.min(1, Math.round(projected)));
};

/** One gesture moves the stack by one plate at most: its position stays within a plate of where it began. */
export const withinOne = (pos: number, base: number): number => Math.max(base - 1, Math.min(base + 1, pos));

export interface ISpring {
  x: number;
  v: number;
}

/** A stiff, nearly critically damped spring: no long wobble, but it carries the speed it was let go with. */
const STIFFNESS = 320;
const DAMPING = 2 * 0.86 * Math.sqrt(STIFFNESS);

/** One step of the spring towards `to`, `dt` seconds (semi-implicit Euler, cut into small steps). */
export const springStep = (s: ISpring, to: number, dt: number): ISpring => {
  let { x, v } = s;
  const n = Math.max(1, Math.ceil(dt / 0.004));
  const h = dt / n;
  for (let i = 0; i < n; i++) {
    v += (-STIFFNESS * (x - to) - DAMPING * v) * h;
    x += v * h;
  }
  return { x, v };
};

export const springDone = (s: ISpring, to: number): boolean =>
  Math.abs(s.x - to) < 0.002 && Math.abs(s.v) < 0.02;

/** A wheel's turn: a mouse wheel clicks by whole lines, a trackpad sends small smooth deltas. */
export const isWheelNotch = (event: Pick<WheelEvent, 'deltaMode' | 'deltaY'>): boolean =>
  event.deltaMode !== 0 || (Math.abs(event.deltaY) >= 50 && Number.isInteger(event.deltaY));

const reduced = (): boolean => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;

interface IPagerOptions {
  host: () => HTMLElement | null;
  /** The front plate has become `dir` (1 the next one, −1 the one before): React makes it the front. */
  onStep: (dir: 1 | -1) => void;
  /** The stack came to rest: how smooth its motion was, for the metrics (frames longer than LONG_FRAME_MS). */
  onFrames?: (frames: IFrames) => void;
}

/** A frame longer than this missed the screen's beat (60 Hz: 16.7 ms; one missed is 33). */
export const LONG_FRAME_MS = 25;

export interface IFrames {
  ms: number;
  frames: number;
  long: number;
}

/** Counts the frames while the stack moves (a frame loop of its own, so a gesture that paints rarely counts too). */
class FrameMeter {
  private raf = 0;
  private t0 = 0;
  private last = 0;
  private frames = 0;
  private long = 0;

  start() {
    if (this.raf) return;
    this.t0 = performance.now();
    this.last = this.t0;
    this.frames = 0;
    this.long = 0;
    const tick = (now: number) => {
      if (now - this.last > LONG_FRAME_MS) this.long += 1;
      this.frames += 1;
      this.last = now;
      this.raf = requestAnimationFrame(tick);
    };
    this.raf = requestAnimationFrame(tick);
  }

  stop(): IFrames | null {
    if (!this.raf) return null;
    cancelAnimationFrame(this.raf);
    this.raf = 0;
    return { ms: Math.round(performance.now() - this.t0), frames: this.frames, long: this.long };
  }
}

/**
 * The stack's position and its spring. `grab` starts a gesture where the stack is now (even mid-spring), `drag` moves it
 * by the finger's travel, `release` lets the spring carry it on, `nudge` moves it by a wheel's delta, `step` pages by one
 * as a key or a wheel's click does. `rebase` is called by React right after it made the stepped plate the front one.
 */
export class StackPager {
  private pos = 0;
  private vel = 0;
  /** Where the gesture took the stack, in positions; the finger's travel is added to it. */
  private base = 0;
  private target: number | null = null;
  private frame = 0;
  private last = 0;
  /** A step told to React that it has not drawn yet: the position is moved by it once it has. */
  private pending: 0 | 1 | -1 = 0;
  private pendingAt = 0;
  private height = 40;
  private page = 64;
  /** +1 when the notice stands at the bottom: down the screen is towards its edge; −1 at the top. */
  private sign = 1;
  private live = false;
  /** A finger or a trackpad leads (no spring). */
  private dragging = false;
  /** The plates drawn by the last frame: one React has just added comes in out of nothing. */
  private seen = new WeakSet<Element>();
  private meter = new FrameMeter();

  constructor(private readonly options: IPagerOptions) {}

  /** The stack is moving: a gesture leads, or the spring after it. */
  get active(): boolean {
    return this.live;
  }

  /** Begins (or takes over mid-spring) a gesture from where the stack is now. */
  grab() {
    if (!this.live) this.start();
    this.stopSpring();
    this.base = this.pos;
  }

  /** The finger has gone `dy` px down the screen since `grab`: one to one, up to one plate. */
  drag(dy: number) {
    if (!this.live) return;
    this.pos = withinOne(this.base + (dy * this.sign) / this.page, this.base);
    this.cross();
    this.paint();
  }

  /** A trackpad's gesture begins: what it moves from here on is one plate at most. */
  begin() {
    if (!this.live) this.start();
    this.stopSpring();
    this.base = this.pos;
  }

  /** A trackpad moved what it scrolls `dy` px down the screen (after `begin`): one to one, up to one plate. */
  nudge(dy: number) {
    if (!this.live) this.begin();
    this.stopSpring();
    this.pos = withinOne(this.pos + (dy * this.sign) / this.page, this.base);
    this.cross();
    this.paint();
  }

  /** Lets go at `vy` px per ms down the screen: on to the nearest plate, carried by the speed. */
  release(vy: number) {
    if (!this.live) return;
    this.vel = (vy * this.sign * 1000) / this.page;
    // The speed carries it to the next plate, never past the one the gesture may reach.
    this.springTo(withinOne(pageTarget(this.pos, this.vel), this.base));
  }

  /** A wheel's click `dy` down the screen: one page that way. */
  click(dy: number) {
    if (!this.live) this.start();
    if (dy !== 0) this.step(dy * this.sign > 0 ? 1 : -1);
  }

  /** One page on, animated: a key, a click of the wheel, a plate pressed. */
  step(dir: 1 | -1) {
    if (!this.live) this.start();
    const from = this.target ?? Math.round(this.pos);
    this.base = from;
    this.springTo(Math.max(-2, Math.min(2, from + dir)));
  }

  /** React has drawn the new front plate: the stack's position moves back by one page with it, so nothing jumps. */
  rebase() {
    if (this.pending === 0) return;
    const dir = this.pending;
    this.pending = 0;
    this.pos -= dir;
    this.base -= dir;
    if (this.target !== null) this.target -= dir;
    this.paint(true);
    if (!this.dragging && this.target !== null && !this.frame) this.springTo(this.target);
  }

  /** Stops everything and leaves the stack to the stylesheet. */
  reset() {
    if (this.frame) cancelAnimationFrame(this.frame);
    this.frame = 0;
    this.pos = 0;
    this.vel = 0;
    this.base = 0;
    this.target = null;
    this.pending = 0;
    if (this.live) this.finish();
  }

  private start() {
    const host = this.options.host();
    if (!host) return;
    const box = host.querySelector<HTMLElement>('.hx-sn__body:not([data-ghost])');
    this.height = box?.offsetHeight || 40;
    this.page = pageSize(this.height);
    // The edge the notice stands at: --sn-away is −1 at the bottom (away is up), 1 at the top.
    const away = Number.parseFloat(getComputedStyle(host).getPropertyValue('--sn-away')) || -1;
    this.sign = away < 0 ? 1 : -1;
    host.dataset.paging = '';
    delete host.dataset.rise;
    this.meter.start();
    this.live = true;
    this.dragging = true;
  }

  /** The position has passed a whole plate: that plate is the front one now. */
  private cross() {
    if (this.pending !== 0) {
      // React never drew the step (the list changed under it): let it go.
      if (performance.now() - this.pendingAt > 400) this.pending = 0;
      return;
    }
    if (this.pos >= 1 || this.pos <= -1) {
      const dir = this.pos > 0 ? 1 : -1;
      this.pending = dir;
      this.pendingAt = performance.now();
      this.options.onStep(dir);
    }
  }

  private springTo(to: number) {
    this.dragging = false;
    this.target = to;
    if (reduced()) {
      this.pos = to;
      this.cross();
      if (this.pending === 0) this.settle();
      return;
    }
    if (!this.frame) {
      this.last = performance.now();
      this.frame = requestAnimationFrame(this.tick);
    }
  }

  private tick = (now: number) => {
    this.frame = 0;
    if (this.target === null) return;
    const dt = Math.min(0.032, Math.max(0.001, (now - this.last) / 1000));
    this.last = now;
    const s = springStep({ x: this.pos, v: this.vel }, this.target, dt);
    this.pos = s.x;
    this.vel = s.v;
    this.cross();
    this.paint();
    if (springDone(s, this.target) && this.pending === 0) {
      this.pos = this.target;
      this.settle();
      return;
    }
    this.frame = requestAnimationFrame(this.tick);
  };

  private stopSpring() {
    if (this.frame) cancelAnimationFrame(this.frame);
    this.frame = 0;
    this.target = null;
    this.dragging = true;
  }

  /** At rest on a plate: the inline looks go and the stylesheet draws the same stack. */
  private settle() {
    this.target = null;
    this.vel = 0;
    if (Math.abs(this.pos) > 0.001) {
      // React never drew the step: back to the plate it still shows.
      this.pos = Math.sign(this.pos) * 0.99;
      this.springTo(0);
      return;
    }
    this.pos = 0;
    this.finish();
  }

  private finish() {
    this.live = false;
    this.dragging = false;
    const frames = this.meter.stop();
    if (frames && frames.frames > 2) this.options.onFrames?.(frames);
    const host = this.options.host();
    if (!host) return;
    for (const el of host.querySelectorAll<HTMLElement>(
      '.hx-sn__shape, .hx-sn__plate, .hx-sn__tint, .hx-sn__body, .hx-sn__peek',
    )) {
      el.style.removeProperty('translate');
      el.style.removeProperty('scale');
      el.style.removeProperty('opacity');
    }
    // A frame later, so the stylesheet's transitions do not play the last step again.
    requestAnimationFrame(() => {
      if (!this.live) delete host.dataset.paging;
    });
  }

  private paint(fresh = false) {
    const host = this.options.host();
    if (!host) return;
    const seen = this.seen;
    const put = (el: HTMLElement | null, d: number, content: HTMLElement | null) => {
      if (!el) return;
      const look = stackLook(d, this.height, this.page);
      el.style.translate = `0 ${(look.toEdge * this.sign).toFixed(2)}px`;
      el.style.scale = look.scale.toFixed(4);
      el.style.opacity = look.opacity.toFixed(3);
      const tint = el.querySelector<HTMLElement>(':scope > .hx-sn__tint');
      if (tint) tint.style.opacity = look.tint.toFixed(3);
      if (content) content.style.opacity = look.content.toFixed(3);
    };
    const shape = host.querySelector<HTMLElement>('.hx-sn__shape');
    put(shape, -this.pos, shape?.querySelector<HTMLElement>('.hx-sn__body:not([data-ghost])') ?? null);
    for (const plate of host.querySelectorAll<HTMLElement>('.hx-sn__plate[data-depth]')) {
      const depth = Number(plate.dataset.depth);
      put(plate, depth - this.pos, plate.querySelector<HTMLElement>(':scope > .hx-sn__peek'));
      // The plate that went round to the back of a short stack was not there a frame ago: it fades in.
      if (fresh && !seen.has(plate) && depth > 0)
        plate.animate([{ opacity: 0 }, {}], { duration: 220, easing: 'ease-out' });
      seen.add(plate);
    }
  }
}
