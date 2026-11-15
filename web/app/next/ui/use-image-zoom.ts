import { useEffect, useRef, type RefObject } from 'react';

/** The fitted view; the scale is a share of it. */
export const ZOOM_FIT = 1;
/** At most this many times the picture's own pixels (and at least DOUBLE_ZOOM_MIN times fitted). */
export const ZOOM_MAX_REAL = 8;
/** One press of «+» or «−», and of the keys + and −. */
export const ZOOM_STEP = 1.5;
/** A mouse wheel notch, and a trackpad pinch (which the browser reports as a wheel with ctrl held), per delta unit. */
const WHEEL_RATE = 0.0022;
const PINCH_WHEEL_RATE = 0.01;
/** A finger that stays within this is a tap, further is a drag. */
const TAP_SLOP_PX = 8;
const DOUBLE_TAP_MS = 300;
const DOUBLE_TAP_PX = 32;
/** A double click / tap on a fitted picture goes to its real size, but at least this much. */
const DOUBLE_ZOOM_MIN = 2;
/** The drawing layer learns the settled zoom this long after the last move (it redraws sharper then). */
const SETTLE_MS = 140;
const EASE_ATTR = 'ease';
/** The viewer's own controls (the picture editor's dock, versions and drawing layer): not the picture. */
const KEEP = '.hx-imgview__keep';
/** The drawing layer: with a tool on, one finger draws there and two pinch. */
const DRAW = '.hx-ie__canvas';
/** Sent on the stage's root once the zoom settles; `data-zoom` on it holds the scale. */
export const ZOOM_SETTLED_EVENT = 'hx-zoom';

interface IView {
  scale: number;
  x: number;
  y: number;
}
interface IPoint {
  x: number;
  y: number;
}

/** What a gesture leaves for the viewer's own handlers, which run after these (they sit higher in the tree). */
export interface IZoomFlags {
  /** The gesture that just began may step to the next picture: one finger, picture fitted, nothing pinched. */
  swipeOk: boolean;
  /** The gesture panned or pinched, so the click that follows it is not a tap. */
  dragged: boolean;
}

/** The zoom as the buttons show it. */
export interface IZoomState {
  /** The picture's shown size as a share of its own pixels (1 = 100%). */
  real: number;
  fitted: boolean;
  atMin: boolean;
  atMax: boolean;
}

export interface IZoomControls {
  zoomIn: () => void;
  zoomOut: () => void;
  fit: () => void;
  /** Fitted: to the real size (100%); otherwise back to fitted. */
  toggleReal: () => void;
}

export interface IZoomOptions {
  /** A drawing tool is on: one finger or the mouse on the drawing layer draws, two fingers still pinch and pan. */
  drawing?: boolean;
  /** How small it may get, as a share of fitted. */
  min?: number;
  /** A wheel without ctrl pans a zoomed picture (two fingers on a trackpad); off: every wheel zooms. */
  wheelPans?: boolean;
  /** +, − and 0 (outside a field), while the viewer is open. */
  keys?: boolean;
  /** The zoom after each change, once per frame. */
  onChange?: (state: IZoomState) => void;
}

export interface IZoom {
  flags: RefObject<IZoomFlags>;
  controls: RefObject<IZoomControls | null>;
}

/** +, − (with or without shift) and 0: fitted. */
const KEY_FACTOR: Readonly<Record<string, number>> = {
  '+': ZOOM_STEP,
  '=': ZOOM_STEP,
  '-': 1 / ZOOM_STEP,
  _: 1 / ZOOM_STEP,
  '0': 0,
};

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

const editable = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  (target.isContentEditable || target.closest('input, textarea, select, [contenteditable]') !== null);

/**
 * Zoom for a picture: the wheel (and a trackpad pinch) zooms towards the cursor, two fingers pinch and pan, a drag pans
 * while zoomed, a double click or double tap goes to the real size and back to fit; `controls` are for buttons, and
 * +, − and 0 are keys. The view is written straight into the target's `translate` and `scale` (no render per move);
 * `data-zoomed` on the stage says it is zoomed in. The target is the picture, or the box that holds it and its marks
 * (they zoom together). `resetKey` is the picture shown: a new one starts fitted.
 */
const useImageZoom = (
  stage: RefObject<HTMLElement | null>,
  target: RefObject<HTMLElement | null>,
  active: boolean,
  resetKey: string | undefined,
  options: IZoomOptions = {},
): IZoom => {
  const flags = useRef<IZoomFlags>({ swipeOk: true, dragged: false });
  const controls = useRef<IZoomControls | null>(null);
  const opts = useRef(options);
  useEffect(() => {
    opts.current = options;
  });
  useEffect(() => {
    const root = stage.current;
    if (!active || !root) return;
    const bound = target.current;
    let view: IView = { scale: ZOOM_FIT, x: 0, y: 0 };
    const pointers = new Map<number, IPoint>();
    let pinch: { distance: number; mid: IPoint; view: IView } | null = null;
    let origin: IPoint = { x: 0, y: 0 };
    let lastTap: { at: number; x: number; y: number } | null = null;
    let lastType = '';
    // The gesture began on the drawing layer with a tool on: its finger draws, it does not pan.
    let drawGesture = false;
    let frame = 0;
    let settle = 0;
    flags.current = { swipeOk: true, dragged: false };

    const picture = (): HTMLImageElement | null => {
      const el = target.current;
      if (el instanceof HTMLImageElement) return el;
      return el?.querySelector('img') ?? null;
    };
    /** How big the fitted picture is against its own pixels. */
    const fitReal = (): number => {
      const el = target.current;
      const img = picture();
      if (!el || !img || !img.naturalWidth || !el.clientWidth) return 1;
      return el.clientWidth / img.naturalWidth;
    };
    const minScale = () => Math.min(ZOOM_FIT, opts.current.min ?? ZOOM_FIT);
    const maxScale = () => Math.max(DOUBLE_ZOOM_MIN, ZOOM_MAX_REAL / fitReal());

    const report = () => {
      frame = 0;
      const real = fitReal();
      opts.current.onChange?.({
        real: view.scale * real,
        fitted: view.scale === ZOOM_FIT,
        atMin: view.scale <= minScale() + 1e-6,
        atMax: view.scale >= maxScale() - 1e-6,
      });
    };
    const settled = () => {
      settle = 0;
      root.dataset.zoom = String(view.scale);
      root.dispatchEvent(new CustomEvent(ZOOM_SETTLED_EVENT, { detail: view.scale }));
    };
    const paint = () => {
      const el = target.current;
      if (!el) return;
      if (view.scale === ZOOM_FIT) {
        el.style.removeProperty('translate');
        el.style.removeProperty('scale');
      } else {
        el.style.translate = `${view.x}px ${view.y}px`;
        el.style.scale = String(view.scale);
      }
      if (view.scale > ZOOM_FIT) root.dataset.zoomed = 'true';
      else delete root.dataset.zoomed;
      if (!frame) frame = requestAnimationFrame(report);
      window.clearTimeout(settle);
      settle = window.setTimeout(settled, SETTLE_MS);
    };
    const ease = (on: boolean) => {
      if (on) target.current?.setAttribute(`data-${EASE_ATTR}`, '');
      else target.current?.removeAttribute(`data-${EASE_ATTR}`);
    };
    /**
     * Where the target's centre is with no pan, from its place in the layout (its own translate and scale do not move
     * it, and a box read mid-animation would).
     */
    const centre = (): IPoint => {
      const el = target.current;
      if (!el) return { x: 0, y: 0 };
      const parent = el.offsetParent;
      const base = parent ? parent.getBoundingClientRect() : { left: 0, top: 0 };
      const border =
        parent instanceof HTMLElement ? { x: parent.clientLeft, y: parent.clientTop } : { x: 0, y: 0 };
      return {
        x: base.left + border.x + el.offsetLeft + el.offsetWidth / 2,
        y: base.top + border.y + el.offsetTop + el.offsetHeight / 2,
      };
    };
    /** Zooms from `base` to `next` so the point that was under `from` ends up under `to`. */
    const zoom = (next: number, from: IPoint, to: IPoint, base: IView) => {
      const scale = clamp(next, minScale(), maxScale());
      // At fitted or smaller the picture stays in the middle.
      if (scale <= ZOOM_FIT + 1e-3) {
        view = { scale: Math.abs(scale - ZOOM_FIT) < 1e-3 ? ZOOM_FIT : scale, x: 0, y: 0 };
        paint();
        return;
      }
      const c = centre();
      const ratio = scale / base.scale;
      view = {
        scale,
        x: to.x - c.x - (from.x - c.x - base.x) * ratio,
        y: to.y - c.y - (from.y - c.y - base.y) * ratio,
      };
      paint();
    };
    const fit = () => {
      ease(true);
      view = { scale: ZOOM_FIT, x: 0, y: 0 };
      paint();
    };
    const toggle = (at: IPoint) => {
      if (view.scale !== ZOOM_FIT) {
        fit();
        return;
      }
      ease(true);
      zoom(Math.max(DOUBLE_ZOOM_MIN, 1 / fitReal()), at, at, view);
    };
    /** A button or a key: about the middle of what is seen of the picture. */
    const stepBy = (factor: number) => {
      ease(true);
      const c = centre();
      const seen = { x: c.x, y: c.y };
      zoom(view.scale * factor, seen, seen, view);
    };
    controls.current = {
      zoomIn: () => stepBy(ZOOM_STEP),
      zoomOut: () => stepBy(1 / ZOOM_STEP),
      fit,
      toggleReal: () => {
        if (view.scale !== ZOOM_FIT) {
          fit();
          return;
        }
        ease(true);
        const c = centre();
        zoom(1 / fitReal(), c, c, view);
      },
    };
    const pair = (): [IPoint, IPoint] | null => {
      const [a, b] = [...pointers.values()];
      return a && b ? [a, b] : null;
    };
    const startPinch = () => {
      const both = pair();
      if (!both) return;
      const [a, b] = both;
      pinch = {
        distance: Math.hypot(a.x - b.x, a.y - b.y) || 1,
        mid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 },
        view,
      };
    };

    const closest = (node: EventTarget | null, selector: string) =>
      node instanceof Element && node.closest(selector) !== null;
    /** The drawing layer, while a tool is on. */
    const onDrawLayer = (target: EventTarget | null) =>
      opts.current.drawing === true && closest(target, DRAW);
    /** The editor's own controls (dock, versions) are not the picture; the drawing layer is, for the wheel. */
    const onKept = (target: EventTarget | null) => closest(target, KEEP) && !closest(target, DRAW);
    const onWheel = (event: WheelEvent) => {
      if (onKept(event.target)) return;
      event.preventDefault();
      ease(false);
      const lines = event.deltaMode === WheelEvent.DOM_DELTA_LINE ? 16 : 1;
      const pinched = event.ctrlKey || event.metaKey;
      if (!pinched && opts.current.wheelPans && view.scale > ZOOM_FIT) {
        view = { ...view, x: view.x - event.deltaX * lines, y: view.y - event.deltaY * lines };
        paint();
        return;
      }
      const rate = event.ctrlKey ? PINCH_WHEEL_RATE : WHEEL_RATE;
      const at = { x: event.clientX, y: event.clientY };
      zoom(view.scale * Math.exp(-event.deltaY * lines * rate), at, at, view);
    };
    const onDown = (event: PointerEvent) => {
      if (event.button !== 0 || onKept(event.target) || closest(event.target, 'button')) return;
      const draws = onDrawLayer(event.target);
      // The mouse and the pen draw on the layer; only fingers zoom over it.
      if (draws && event.pointerType !== 'touch') return;
      ease(false);
      lastType = event.pointerType;
      flags.current.dragged = false;
      origin = { x: event.clientX, y: event.clientY };
      pointers.set(event.pointerId, origin);
      if (pointers.size === 1) drawGesture = draws;
      flags.current.swipeOk = pointers.size === 1 && view.scale === ZOOM_FIT;
      // Captured by the picture, so the click that ends a drag is still on the picture and does not close the viewer.
      // A finger on the drawing layer stays with the layer (it holds it while it draws).
      if (!draws && (view.scale > ZOOM_FIT || pointers.size > 1))
        (target.current ?? root).setPointerCapture(event.pointerId);
      // The layer drops its stroke itself when a second finger lands (use-zoom-canvas.ts).
      if (pointers.size === 2) {
        startPinch();
        drawGesture = false;
      }
    };
    const onMove = (event: PointerEvent) => {
      const previous = pointers.get(event.pointerId);
      if (!previous) return;
      pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
      const both = pair();
      if (pinch && both) {
        const [a, b] = both;
        const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
        flags.current.dragged = true;
        flags.current.swipeOk = false;
        zoom(
          pinch.view.scale * (Math.hypot(a.x - b.x, a.y - b.y) / pinch.distance),
          pinch.mid,
          mid,
          pinch.view,
        );
        return;
      }
      if (drawGesture || view.scale <= ZOOM_FIT) return;
      flags.current.swipeOk = false;
      if (Math.hypot(event.clientX - origin.x, event.clientY - origin.y) > TAP_SLOP_PX)
        flags.current.dragged = true;
      view = { ...view, x: view.x + event.clientX - previous.x, y: view.y + event.clientY - previous.y };
      paint();
    };
    const onUp = (event: PointerEvent) => {
      if (!pointers.delete(event.pointerId)) return;
      if (pointers.size < 2) pinch = null;
      if (pointers.size === 0) drawGesture = false;
      if (event.type !== 'pointerup' || event.pointerType !== 'touch' || pointers.size > 0) return;
      if (
        onDrawLayer(event.target) ||
        flags.current.dragged ||
        Math.hypot(event.clientX - origin.x, event.clientY - origin.y) > TAP_SLOP_PX
      )
        return;
      const at = { x: event.clientX, y: event.clientY };
      const before = lastTap;
      lastTap = { at: event.timeStamp, ...at };
      if (
        before &&
        event.timeStamp - before.at < DOUBLE_TAP_MS &&
        Math.hypot(at.x - before.x, at.y - before.y) < DOUBLE_TAP_PX
      ) {
        lastTap = null;
        toggle(at);
      }
    };
    // A touch screen's double tap is read in onUp; this is the mouse's.
    const onDouble = (event: MouseEvent) => {
      if (
        lastType === 'touch' ||
        onKept(event.target) ||
        onDrawLayer(event.target) ||
        closest(event.target, 'button')
      )
        return;
      toggle({ x: event.clientX, y: event.clientY });
    };
    const onKey = (event: KeyboardEvent) => {
      if (!opts.current.keys || event.altKey || editable(event.target)) return;
      const factor = KEY_FACTOR[event.key];
      if (factor === undefined) return;
      event.preventDefault();
      if (factor === 0) fit();
      else stepBy(factor);
    };
    const onEnd = (event: TransitionEvent) => {
      if (event.target === target.current) ease(false);
    };

    root.addEventListener('wheel', onWheel, { passive: false });
    root.addEventListener('pointerdown', onDown);
    root.addEventListener('pointermove', onMove);
    root.addEventListener('pointerup', onUp);
    root.addEventListener('pointercancel', onUp);
    root.addEventListener('dblclick', onDouble);
    // On the window: a press on «Вписать» disables it and the focus falls to the page, the keys still work.
    window.addEventListener('keydown', onKey);
    root.addEventListener('transitionend', onEnd);
    // The picture's own size is known once it loads: the percentage follows.
    root.addEventListener('load', report, true);
    report();
    return () => {
      root.removeEventListener('wheel', onWheel);
      root.removeEventListener('pointerdown', onDown);
      root.removeEventListener('pointermove', onMove);
      root.removeEventListener('pointerup', onUp);
      root.removeEventListener('pointercancel', onUp);
      root.removeEventListener('dblclick', onDouble);
      window.removeEventListener('keydown', onKey);
      root.removeEventListener('transitionend', onEnd);
      root.removeEventListener('load', report, true);
      cancelAnimationFrame(frame);
      window.clearTimeout(settle);
      controls.current = null;
      delete root.dataset.zoomed;
      delete root.dataset.zoom;
      bound?.style.removeProperty('translate');
      bound?.style.removeProperty('scale');
      bound?.removeAttribute(`data-${EASE_ATTR}`);
    };
    // A new picture (the key) rebinds with a fitted view. The options are read per gesture: a tool turned on or off
    // keeps the view.
  }, [stage, target, active, resetKey]);

  return { flags, controls };
};

export default useImageZoom;
