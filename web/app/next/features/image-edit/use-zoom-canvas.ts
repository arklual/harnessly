// What a drawing layer over a zoomable picture needs (ui/use-image-zoom.ts zooms the stage with the layer in it): its
// pixels follow the settled zoom, so the marks stay sharp at 800%, and a second finger is a pinch, not a stroke.
import { useEffect, useRef, type RefObject } from 'react';
import { ZOOM_SETTLED_EVENT } from '@/next/ui/use-image-zoom';

/** A canvas larger than this (pixels, and pixels on a side) is refused or slow on a phone: the zoom stops adding detail. */
const MAX_AREA = 16_000_000;
const MAX_SIDE = 16_384;

/** The viewer that zooms; `data-zoom` on it is the settled scale. */
const ROOT = '.hx-imgview';

export const zoomOf = (el: Element | null): number => {
  const root = el?.closest<HTMLElement>(ROOT);
  const value = Number(root?.dataset.zoom);
  return Number.isFinite(value) && value > 0 ? value : 1;
};

/** The canvas's pixels for its box at this zoom. */
export const canvasSize = (
  cssW: number,
  cssH: number,
  dpr: number,
  zoom: number,
): { w: number; h: number } => {
  const want = dpr * Math.max(1, zoom);
  const w = Math.max(1, cssW * want);
  const h = Math.max(1, cssH * want);
  const cut = Math.min(1, Math.sqrt(MAX_AREA / (w * h)), MAX_SIDE / Math.max(w, h));
  return { w: Math.max(1, Math.floor(w * cut)), h: Math.max(1, Math.floor(h * cut)) };
};

/** Sizes the canvas's pixels to its box at the settled zoom; true when they changed. */
const resize = (el: HTMLCanvasElement): boolean => {
  const size = canvasSize(el.clientWidth, el.clientHeight, window.devicePixelRatio || 1, zoomOf(el));
  if (el.width === size.w && el.height === size.h) return false;
  el.width = size.w;
  el.height = size.h;
  return true;
};

/**
 * Keeps `canvas` sized to its box at the settled zoom and repaints it (`paint` is read when it runs). Returns the
 * pointers down on it: more than one is a pinch, which the viewer zooms with, and the stroke under way is dropped.
 */
export const useZoomCanvas = (
  canvas: RefObject<HTMLCanvasElement | null>,
  paint: RefObject<() => void>,
  key: unknown = null,
): RefObject<Set<number>> => {
  const pointers = useRef(new Set<number>());
  useEffect(() => {
    const el = canvas.current;
    if (!el) return;
    const fit = () => {
      resize(el);
      paint.current();
    };
    fit();
    const observer = new ResizeObserver(fit);
    observer.observe(el);
    const root = el.closest(ROOT);
    root?.addEventListener(ZOOM_SETTLED_EVENT, fit);
    return () => {
      observer.disconnect();
      root?.removeEventListener(ZOOM_SETTLED_EVENT, fit);
    };
  }, [canvas, paint, key]);
  return pointers;
};
