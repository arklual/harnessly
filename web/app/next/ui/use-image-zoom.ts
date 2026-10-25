import { useEffect, useRef, type RefObject } from 'react';

export const ZOOM_MIN = 1;
export const ZOOM_MAX = 12;
/** A mouse wheel notch, and a trackpad pinch (which the browser reports as a wheel with ctrl held), per delta unit. */
const WHEEL_RATE = 0.0022;
const PINCH_WHEEL_RATE = 0.01;
/** A finger that stays within this is a tap, further is a drag. */
const TAP_SLOP_PX = 8;
const DOUBLE_TAP_MS = 300;
const DOUBLE_TAP_PX = 32;
/** A double click / tap on a fitted picture goes to its real size, but at least this much. */
const DOUBLE_ZOOM_MIN = 2;
const EASE_ATTR = 'ease';

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

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

/**
 * Zoom for the picture in a viewer: the wheel (and a trackpad pinch) zooms towards the cursor, two fingers pinch, a
 * drag pans while zoomed, a double click or double tap goes to the real size and back to fit. The view is written
 * straight into the picture's `translate` and `scale` (no render per move); `data-zoomed` on the stage says it is
 * zoomed. `resetKey` is the picture shown: a new one starts fitted.
 */
const useImageZoom = (
  stage: RefObject<HTMLElement | null>,
  image: RefObject<HTMLImageElement | null>,
  active: boolean,
  resetKey: string | undefined,
): RefObject<IZoomFlags> => {
  const flags = useRef<IZoomFlags>({ swipeOk: true, dragged: false });

  useEffect(() => {
    const root = stage.current;
    if (!active || !root) return;
    let view: IView = { scale: ZOOM_MIN, x: 0, y: 0 };
    const pointers = new Map<number, IPoint>();
    let pinch: { distance: number; mid: IPoint; view: IView } | null = null;
    let origin: IPoint = { x: 0, y: 0 };
    let lastTap: { at: number; x: number; y: number } | null = null;
    let lastType = '';
    flags.current = { swipeOk: true, dragged: false };

    const paint = () => {
      const img = image.current;
      if (!img) return;
      if (view.scale === ZOOM_MIN) {
        img.style.removeProperty('translate');
        img.style.removeProperty('scale');
        delete root.dataset.zoomed;
        return;
      }
      img.style.translate = `${view.x}px ${view.y}px`;
      img.style.scale = String(view.scale);
      root.dataset.zoomed = 'true';
    };
    const ease = (on: boolean) => {
      if (on) image.current?.setAttribute(`data-${EASE_ATTR}`, '');
      else image.current?.removeAttribute(`data-${EASE_ATTR}`);
    };
    /** Where the picture's centre is with no pan: its box now, minus the pan (the scale is about the centre). */
    const centre = (): IPoint => {
      const rect = image.current?.getBoundingClientRect();
      if (!rect) return { x: 0, y: 0 };
      return { x: rect.left + rect.width / 2 - view.x, y: rect.top + rect.height / 2 - view.y };
    };
    /** Zooms from `base` to `next` so the point that was under `from` ends up under `to`. */
    const zoom = (next: number, from: IPoint, to: IPoint, base: IView) => {
      const scale = clamp(next, ZOOM_MIN, ZOOM_MAX);
      if (scale === ZOOM_MIN) {
        view = { scale: ZOOM_MIN, x: 0, y: 0 };
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
    const toggle = (at: IPoint) => {
      ease(true);
      if (view.scale > ZOOM_MIN) {
        view = { scale: ZOOM_MIN, x: 0, y: 0 };
        paint();
        return;
      }
      const img = image.current;
      const natural = img && img.clientWidth ? img.naturalWidth / img.clientWidth : DOUBLE_ZOOM_MIN;
      zoom(Math.max(DOUBLE_ZOOM_MIN, natural), at, at, view);
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

    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      ease(false);
      const lines = event.deltaMode === WheelEvent.DOM_DELTA_LINE ? 16 : 1;
      const rate = event.ctrlKey ? PINCH_WHEEL_RATE : WHEEL_RATE;
      const at = { x: event.clientX, y: event.clientY };
      zoom(view.scale * Math.exp(-event.deltaY * lines * rate), at, at, view);
    };
    const onDown = (event: PointerEvent) => {
      if (event.button !== 0 || (event.target instanceof Element && event.target.closest('button'))) return;
      ease(false);
      lastType = event.pointerType;
      flags.current.dragged = false;
      origin = { x: event.clientX, y: event.clientY };
      pointers.set(event.pointerId, origin);
      flags.current.swipeOk = pointers.size === 1 && view.scale === ZOOM_MIN;
      // Captured by the picture, so the click that ends a drag is still on the picture and does not close the viewer.
      if (view.scale > ZOOM_MIN || pointers.size > 1)
        (image.current ?? root).setPointerCapture(event.pointerId);
      if (pointers.size === 2) startPinch();
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
      if (view.scale === ZOOM_MIN) return;
      flags.current.swipeOk = false;
      if (Math.hypot(event.clientX - origin.x, event.clientY - origin.y) > TAP_SLOP_PX)
        flags.current.dragged = true;
      view = { ...view, x: view.x + event.clientX - previous.x, y: view.y + event.clientY - previous.y };
      paint();
    };
    const onUp = (event: PointerEvent) => {
      if (!pointers.delete(event.pointerId)) return;
      if (pointers.size < 2) pinch = null;
      if (event.type !== 'pointerup' || event.pointerType !== 'touch' || pointers.size > 0) return;
      if (
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
      if (lastType === 'touch' || (event.target instanceof Element && event.target.closest('button'))) return;
      toggle({ x: event.clientX, y: event.clientY });
    };
    const onEnd = () => ease(false);

    root.addEventListener('wheel', onWheel, { passive: false });
    root.addEventListener('pointerdown', onDown);
    root.addEventListener('pointermove', onMove);
    root.addEventListener('pointerup', onUp);
    root.addEventListener('pointercancel', onUp);
    root.addEventListener('dblclick', onDouble);
    root.addEventListener('transitionend', onEnd);
    return () => {
      root.removeEventListener('wheel', onWheel);
      root.removeEventListener('pointerdown', onDown);
      root.removeEventListener('pointermove', onMove);
      root.removeEventListener('pointerup', onUp);
      root.removeEventListener('pointercancel', onUp);
      root.removeEventListener('dblclick', onDouble);
      root.removeEventListener('transitionend', onEnd);
      delete root.dataset.zoomed;
    };
    // A new picture (the key) rebinds with a fitted view.
  }, [stage, image, active, resetKey]);

  return flags;
};

export default useImageZoom;
