// The smart notice under a finger, a mouse and a trackpad. Only the front plate moves: while a gesture leads, it
// follows one to one (the shift goes into --sn-dx/--sn-dy on the host and moves the shape, nothing re-renders); let
// go, it settles. Sideways it leaves (with the speed it was thrown at) or springs back, and the next plate of the stack
// rises into its place. Up and down the direction decides by the first pixels and stays: on a folded stack it pages
// (the front plate goes back, the next comes forward); on one plate, away from its edge opens it and back towards it
// folds it (an open card into the line, an announcement into the line too; it never hides). A long press (or the
// right button) opens the menu of actions. On a trackpad two fingers sideways sweep the front plate aside, a pinch out
// opens and a pinch in folds, the wheel over the stack pages it. Every gesture has its key too (smart-notice.tsx):
// ⌘J and Esc, ⌫, ↑ ↓, ⇧F10.
import { useEffect, useRef, type PointerEvent as ReactPointerEvent, type RefObject } from 'react';

export const GESTURE = {
  hide: 'hide',
  open: 'open',
  fold: 'fold',
  next: 'next',
  prev: 'prev',
  back: 'back',
} as const;
export type TGesture = (typeof GESTURE)[keyof typeof GESTURE];

export type TEdge = 'top' | 'bottom';

export interface IGestureEnd {
  /** The shift since the start, px. */
  dx: number;
  dy: number;
  /** The speed at the end, px per ms. */
  vx: number;
  vy: number;
  /** The plate's width: a sweep past a third of it hides. */
  width: number;
  /** The edge the plate stands at: away from it opens, back to it folds. */
  edge: TEdge;
  open: boolean;
  /** More than one plate: up and down page through them while the plate is folded. */
  stacked: boolean;
}

/** A move shorter than this is a press, not a gesture. */
export const SLOP_PX = 8;
const HIDE_SHARE = 0.35;
const OPEN_PX = 36;
const FOLD_PX = 40;
const PAGE_PX = 28;
const FLICK = 0.45;
const LONG_PRESS_MS = 500;
const WHEEL_IDLE_MS = 120;
const PINCH_PX = 24;
const STEP_PX = 40;
const STEP_GAP_MS = 420;
/** The longest and the shortest a thrown plate takes to leave, ms. */
const FLING_MAX_MS = 380;
const FLING_MIN_MS = 160;
/** How long the plate takes to rise from the stack into the front (hx-sn-rise in styles/smart-notice.css). */
export const RISE_MS = 620;

/** What a gesture comes to when it is let go. */
export const settleGesture = (g: IGestureEnd): TGesture => {
  const ax = Math.abs(g.dx);
  const ay = Math.abs(g.dy);
  if (ax > ay) {
    const flicked = Math.abs(g.vx) > FLICK && ax > SLOP_PX * 2;
    return ax > g.width * HIDE_SHARE || flicked ? GESTURE.hide : GESTURE.back;
  }
  // Positive: away from the edge.
  const away = g.edge === 'bottom' ? -g.dy : g.dy;
  const speed = g.edge === 'bottom' ? -g.vy : g.vy;
  if (g.stacked && !g.open) {
    if (away > PAGE_PX || (speed > FLICK && away > SLOP_PX)) return GESTURE.next;
    if (-away > PAGE_PX || (-speed > FLICK && -away > SLOP_PX)) return GESTURE.prev;
    return GESTURE.back;
  }
  if (!g.open && (away > OPEN_PX || (speed > FLICK && away > SLOP_PX * 1.5))) return GESTURE.open;
  if (-away > FOLD_PX || (-speed > FLICK && -away > SLOP_PX * 2)) return GESTURE.fold;
  return GESTURE.back;
};

/** How far the plate goes for a pull: freely at first, then harder, and only a little where it cannot go. */
export const rubber = (d: number, free: boolean): number => {
  const a = Math.abs(d);
  if (!free) return Math.sign(d) * a * 0.2;
  return Math.sign(d) * (a < 40 ? a : 40 + (a - 40) * 0.35);
};

/** How long a plate thrown at `v` px/ms takes to cover `left` px: the faster, the shorter; it never crawls. */
export const flingMs = (left: number, v: number): number =>
  Math.round(
    Math.min(FLING_MAX_MS, Math.max(FLING_MIN_MS, (Math.abs(left) / Math.max(Math.abs(v), 0.6)) * 1.4)),
  );

interface ILatest {
  edge: TEdge;
  open: boolean;
  off: boolean;
  stacked: boolean;
  onHide: () => void;
  onOpen: () => void;
  onFold: () => void;
  onMenu: () => void;
  onStep: (dir: 1 | -1) => void;
}

const showShift = (el: HTMLElement | null, now: ILatest, dx: number, dy: number, axis: 'x' | 'y' | null) => {
  if (!el) return;
  const away = now.edge === 'bottom' ? -dy : dy;
  // Sideways it goes one to one; up and down freely through a stack, otherwise only towards what it can become.
  const free = now.stacked && !now.open ? true : now.open ? away < 0 : true;
  const y = axis === 'y' ? rubber(dy, free) : 0;
  el.dataset.drag = '';
  el.style.setProperty('--sn-dx', `${axis === 'x' ? dx : 0}px`);
  el.style.setProperty('--sn-dy', `${y}px`);
};

export const clearShift = (el: HTMLElement | null) => {
  if (!el) return;
  delete el.dataset.drag;
  el.style.removeProperty('--sn-dx');
  el.style.removeProperty('--sn-dy');
};

/** The next plate rises from the stack into the front: the shape plays hx-sn-rise once. */
export const rise = (el: HTMLElement | null, dir: 1 | -1 = 1) => {
  if (!el) return;
  clearShift(el);
  delete el.dataset.fling;
  el.dataset.rise = dir === 1 ? 'next' : 'prev';
  window.setTimeout(() => {
    if (el.dataset.rise) delete el.dataset.rise;
  }, RISE_MS);
};

const finishGesture = (
  el: HTMLElement | null,
  now: ILatest,
  end: Omit<IGestureEnd, 'width' | 'edge' | 'open' | 'stacked'>,
) => {
  const width = el?.querySelector<HTMLElement>('.hx-sn__body:not([data-ghost])')?.offsetWidth ?? 320;
  const result = settleGesture({ ...end, width, edge: now.edge, open: now.open, stacked: now.stacked });
  if (result === GESTURE.hide && el) {
    // Thrown on with its own speed: as far as off the window, the faster the sooner.
    const dir = end.dx < 0 ? -1 : 1;
    const to = dir * (window.innerWidth * 0.6 + width);
    const ms = flingMs(to - end.dx, end.vx);
    delete el.dataset.drag;
    el.style.setProperty('--sn-fling-ms', `${ms}ms`);
    el.style.setProperty('--sn-dx', `${to}px`);
    el.dataset.fling = '';
    const stacked = now.stacked;
    window.setTimeout(() => {
      el.style.removeProperty('--sn-fling-ms');
      // The next plate takes the front from the stack; the last one leaves with the whole notice, still thrown.
      if (stacked) rise(el, 1);
      now.onHide();
    }, ms);
    return;
  }
  if (result === GESTURE.next || result === GESTURE.prev) {
    const dir = result === GESTURE.next ? 1 : -1;
    rise(el, dir);
    now.onStep(dir);
    return;
  }
  clearShift(el);
  if (result === GESTURE.open) now.onOpen();
  if (result === GESTURE.fold) now.onFold();
};

interface IGestureOptions extends ILatest {
  /** The element whose --sn-dx/--sn-dy and data-drag/data-fling/data-rise the gesture sets. */
  host: RefObject<HTMLElement | null>;
  /** The plate is drawn: the wheel is listened to from then on. */
  present: boolean;
}

interface IDrag {
  id: number;
  x0: number;
  y0: number;
  x: number;
  y: number;
  t: number;
  vx: number;
  vy: number;
  axis: 'x' | 'y' | null;
  long: number | null;
}

/** Where a press does not start a gesture: text to type into or select. */
const isField = (target: EventTarget | null): boolean =>
  target instanceof Element && target.closest('input, textarea, pre, .hx-otp, .hx-sn__reply') !== null;

/** The speed, smoothed: a finger lifted after a pause throws nothing. */
const SMOOTH = 0.6;

const useGestures = ({ host, present, ...options }: IGestureOptions) => {
  const drag = useRef<IDrag | null>(null);
  /** A gesture ran: the click that ends it does nothing. */
  const swallow = useRef(false);
  const latest = useRef<ILatest>(options);
  useEffect(() => {
    latest.current = options;
  });

  const stopLong = () => {
    const d = drag.current;
    if (d?.long != null) window.clearTimeout(d.long);
    if (d) d.long = null;
  };

  const onPointerDown = (event: ReactPointerEvent<HTMLElement>) => {
    if (latest.current.off || event.button !== 0 || isField(event.target)) return;
    if (host.current?.dataset.fling !== undefined) return;
    swallow.current = false;
    const long =
      event.pointerType === 'mouse'
        ? null
        : window.setTimeout(() => {
            swallow.current = true;
            drag.current = null;
            navigator.vibrate?.(10);
            latest.current.onMenu();
          }, LONG_PRESS_MS);
    drag.current = {
      id: event.pointerId,
      x0: event.clientX,
      y0: event.clientY,
      x: event.clientX,
      y: event.clientY,
      t: event.timeStamp,
      vx: 0,
      vy: 0,
      axis: null,
      long,
    };
  };

  const onPointerMove = (event: ReactPointerEvent<HTMLElement>) => {
    const d = drag.current;
    if (!d || d.id !== event.pointerId) return;
    const dt = Math.max(1, event.timeStamp - d.t);
    d.vx = d.vx * (1 - SMOOTH) + ((event.clientX - d.x) / dt) * SMOOTH;
    d.vy = d.vy * (1 - SMOOTH) + ((event.clientY - d.y) / dt) * SMOOTH;
    d.x = event.clientX;
    d.y = event.clientY;
    d.t = event.timeStamp;
    const dx = d.x - d.x0;
    const dy = d.y - d.y0;
    if (d.axis === null) {
      if (Math.hypot(dx, dy) < SLOP_PX) return;
      stopLong();
      // Decided by the first pixels, and kept to the end.
      d.axis = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y';
      swallow.current = true;
      event.currentTarget.setPointerCapture(event.pointerId);
    }
    showShift(host.current, latest.current, dx, dy, d.axis);
  };

  const onPointerUp = (event: ReactPointerEvent<HTMLElement>) => {
    const d = drag.current;
    if (!d || d.id !== event.pointerId) return;
    stopLong();
    drag.current = null;
    if (d.axis === null) return;
    // A finger that stood still before it lifted throws nothing.
    const still = event.timeStamp - d.t > 80;
    const x = d.axis === 'x';
    finishGesture(host.current, latest.current, {
      dx: x ? d.x - d.x0 : 0,
      dy: x ? 0 : d.y - d.y0,
      vx: x && !still ? d.vx : 0,
      vy: x || still ? 0 : d.vy,
    });
  };

  const onPointerCancel = () => {
    stopLong();
    drag.current = null;
    clearShift(host.current);
  };

  /** The click that ends a gesture (or a long press) does not press the button under it. */
  const onClickCapture = (event: { preventDefault: () => void; stopPropagation: () => void }) => {
    if (!swallow.current) return;
    swallow.current = false;
    event.preventDefault();
    event.stopPropagation();
  };

  const onContextMenu = (event: { preventDefault: () => void; target: EventTarget | null }) => {
    if (isField(event.target)) return;
    event.preventDefault();
    stopLong();
    latest.current.onMenu();
  };

  // The trackpad: the wheel is listened to by hand, as React's is passive and could not keep the page from zooming or
  // going back a page.
  useEffect(() => {
    const el = host.current;
    if (!el || !present) return;
    let dx = 0;
    let vx = 0;
    let lastT = 0;
    let pinch = 0;
    let step = 0;
    let lastStep = 0;
    let idle: number | null = null;
    const wheel = (event: WheelEvent) => {
      const now = latest.current;
      if (now.off || el.dataset.fling !== undefined) return;
      if (event.ctrlKey) {
        // A pinch: out opens, in folds.
        event.preventDefault();
        pinch += event.deltaY;
        if (pinch < -PINCH_PX && !now.open) {
          pinch = 0;
          now.onOpen();
        } else if (pinch > PINCH_PX && now.open) {
          pinch = 0;
          now.onFold();
        }
        return;
      }
      if (Math.abs(event.deltaX) > Math.abs(event.deltaY) || dx !== 0) {
        // Two fingers sideways: the front plate goes with them, one to one, and is thrown or springs back when they
        // stop (the trackpad's own momentum keeps sending deltas a while, so the speed is real).
        event.preventDefault();
        const dt = Math.max(1, event.timeStamp - lastT);
        lastT = event.timeStamp;
        vx = vx * 0.4 + (-event.deltaX / dt) * 0.6;
        dx -= event.deltaX;
        showShift(el, now, dx, 0, 'x');
        if (idle !== null) window.clearTimeout(idle);
        idle = window.setTimeout(() => {
          idle = null;
          const shift = dx;
          const speed = vx;
          dx = 0;
          vx = 0;
          finishGesture(el, latest.current, { dx: shift, dy: 0, vx: speed, vy: 0 });
        }, WHEEL_IDLE_MS);
        return;
      }
      if (now.open || !now.stacked) return;
      event.preventDefault();
      step += event.deltaY;
      if (Math.abs(step) < STEP_PX || event.timeStamp - lastStep < STEP_GAP_MS) return;
      lastStep = event.timeStamp;
      const dir = step > 0 ? 1 : -1;
      step = 0;
      rise(el, dir);
      now.onStep(dir);
    };
    el.addEventListener('wheel', wheel, { passive: false });
    return () => {
      el.removeEventListener('wheel', wheel);
      if (idle !== null) window.clearTimeout(idle);
    };
  }, [host, present]);

  return { onPointerDown, onPointerMove, onPointerUp, onPointerCancel, onClickCapture, onContextMenu };
};

export default useGestures;
