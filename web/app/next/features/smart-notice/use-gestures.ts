// The smart notice under a finger, a mouse and a trackpad. While a gesture leads, the plate follows it with no delay
// (the shift goes into --sn-dx/--sn-dy on the host, nothing re-renders); let go, it settles: swept aside it leaves
// (hidden), pulled away from its edge it opens, pushed back to it it folds, short of that it springs back. A long press
// (or the right button) opens the menu of actions. On a trackpad two fingers sideways sweep it aside, a pinch out opens
// and a pinch in folds, the wheel over the folded stack brings the next plate forward. Every gesture has its key too
// (smart-notice.tsx): ⌘J and Esc, ⌫, ↑ ↓, ⇧F10.
import { useEffect, useRef, type PointerEvent as ReactPointerEvent, type RefObject } from 'react';

export const GESTURE = { hide: 'hide', open: 'open', fold: 'fold', back: 'back' } as const;
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
}

/** A move shorter than this is a press, not a gesture. */
export const SLOP_PX = 8;
const HIDE_SHARE = 0.35;
const OPEN_PX = 36;
const FOLD_PX = 56;
const FLICK = 0.5;
const LONG_PRESS_MS = 500;
const WHEEL_IDLE_MS = 140;
const PINCH_PX = 24;
const STEP_PX = 40;
const STEP_GAP_MS = 280;
/** The time a swept plate takes to leave (styles/smart-notice.css has the same). */
export const FLING_MS = 220;

/** What a gesture comes to when it is let go. */
export const settleGesture = (g: IGestureEnd): TGesture => {
  const ax = Math.abs(g.dx);
  const ay = Math.abs(g.dy);
  if (ax > ay) {
    const flicked = Math.abs(g.vx) > FLICK && ax > SLOP_PX * 3;
    return ax > g.width * HIDE_SHARE || flicked ? GESTURE.hide : GESTURE.back;
  }
  // Positive: away from the edge.
  const away = g.edge === 'bottom' ? -g.dy : g.dy;
  const speed = g.edge === 'bottom' ? -g.vy : g.vy;
  if (!g.open && (away > OPEN_PX || (speed > FLICK && away > SLOP_PX * 1.5))) return GESTURE.open;
  if (g.open && (-away > FOLD_PX || (-speed > FLICK && -away > SLOP_PX * 2))) return GESTURE.fold;
  return GESTURE.back;
};

/** How far the plate goes for a pull: freely at first, then harder, and only a little where it cannot go. */
export const rubber = (d: number, free: boolean): number => {
  const a = Math.abs(d);
  if (!free) return Math.sign(d) * a * 0.2;
  return Math.sign(d) * (a < 40 ? a : 40 + (a - 40) * 0.35);
};

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
  // Sideways it goes freely; up and down only towards what it can become.
  const y = axis === 'y' ? rubber(dy, now.open ? away < 0 : away > 0) : 0;
  el.dataset.drag = '';
  el.style.setProperty('--sn-dx', `${axis === 'x' ? dx : 0}px`);
  el.style.setProperty('--sn-dy', `${y}px`);
};

const clearShift = (el: HTMLElement | null) => {
  if (!el) return;
  delete el.dataset.drag;
  el.style.removeProperty('--sn-dx');
  el.style.removeProperty('--sn-dy');
};

const finishGesture = (
  el: HTMLElement | null,
  now: ILatest,
  end: Omit<IGestureEnd, 'width' | 'edge' | 'open'>,
) => {
  const width = el?.querySelector<HTMLElement>('.hx-sn__shape')?.offsetWidth ?? 320;
  const result = settleGesture({ ...end, width, edge: now.edge, open: now.open });
  if (result === GESTURE.hide && el) {
    delete el.dataset.drag;
    el.dataset.fling = end.dx < 0 ? 'left' : 'right';
    window.setTimeout(() => {
      delete el.dataset.fling;
      clearShift(el);
      now.onHide();
    }, FLING_MS);
    return;
  }
  clearShift(el);
  if (result === GESTURE.open) now.onOpen();
  if (result === GESTURE.fold) now.onFold();
};

interface IGestureOptions extends ILatest {
  /** The element whose --sn-dx/--sn-dy and data-drag/data-fling the gesture sets. */
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
  target instanceof Element && target.closest('input, textarea, pre, .hx-otp') !== null;

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
    d.vx = (event.clientX - d.x) / dt;
    d.vy = (event.clientY - d.y) / dt;
    d.x = event.clientX;
    d.y = event.clientY;
    d.t = event.timeStamp;
    const dx = d.x - d.x0;
    const dy = d.y - d.y0;
    if (d.axis === null) {
      if (Math.hypot(dx, dy) < SLOP_PX) return;
      stopLong();
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
    const x = d.axis === 'x';
    finishGesture(host.current, latest.current, {
      dx: x ? d.x - d.x0 : 0,
      dy: x ? 0 : d.y - d.y0,
      vx: x ? d.vx : 0,
      vy: x ? 0 : d.vy,
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
    let pinch = 0;
    let step = 0;
    let lastStep = 0;
    let idle: number | null = null;
    const wheel = (event: WheelEvent) => {
      const now = latest.current;
      if (now.off) return;
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
      if (Math.abs(event.deltaX) > Math.abs(event.deltaY)) {
        event.preventDefault();
        dx -= event.deltaX;
        showShift(el, now, dx, 0, 'x');
        if (idle !== null) window.clearTimeout(idle);
        idle = window.setTimeout(() => {
          idle = null;
          const shift = dx;
          dx = 0;
          finishGesture(el, latest.current, { dx: shift, dy: 0, vx: 0, vy: 0 });
        }, WHEEL_IDLE_MS);
        return;
      }
      if (now.open || !now.stacked) return;
      event.preventDefault();
      step += event.deltaY;
      if (Math.abs(step) < STEP_PX || event.timeStamp - lastStep < STEP_GAP_MS) return;
      lastStep = event.timeStamp;
      now.onStep(step > 0 ? 1 : -1);
      step = 0;
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
