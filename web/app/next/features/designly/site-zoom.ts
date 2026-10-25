// The canvas of a design: the viewport's box is zoomed and panned freely over its area, like a canvas in a
// design tool. The view is a scale and a shift of the box from where the layout put it (transform-origin at its corner);
// zooming keeps the point under the cursor in place. Pure: the hook in use-canvas-zoom.ts applies it.
import type { IPoint } from '@/next/features/designly/site-drag';

export interface IZoom {
  /** The scale over the fit the layout chose. */
  s: number;
  /** The shift of the box, in the area's pixels. */
  x: number;
  y: number;
}

export const ZOOM = { min: 0.25, max: 4, step: 1.25 } as const;
export const ZOOM_FIT: IZoom = { s: 1, x: 0, y: 0 };

const clamp = (low: number, high: number, value: number) => Math.max(low, Math.min(high, value));

/** The view at another scale with `at` (relative to the box's own corner as the layout put it) staying in place. */
export const zoomAt = (z: IZoom, at: IPoint, scale: number): IZoom => {
  const s = clamp(ZOOM.min, ZOOM.max, scale);
  const k = s / z.s;
  return { s, x: at.x - (at.x - z.x) * k, y: at.y - (at.y - z.y) * k };
};

/** The view shifted by the pointer's move. */
export const panBy = (z: IZoom, dx: number, dy: number): IZoom => ({ s: z.s, x: z.x + dx, y: z.y + dy });

/** A wheel's turn as a factor of scale: a trackpad's pinch gives small steps and follows the fingers, a mouse's notch
 *  is capped to one even step (about a fifth). */
export const wheelFactor = (deltaY: number): number => Math.exp(clamp(-0.2, 0.2, -deltaY * 0.01));

export const isFit = (z: IZoom): boolean => z.s === 1 && z.x === 0 && z.y === 0;

/** The scale the person reads: the layout's fit times the view's, in percent. */
export const zoomPct = (fit: number, z: IZoom): number => Math.round(fit * z.s * 100);

/**
 * A point of the section with the view at the fit, carried to where it is with the view: `origin` is the box's
 * corner in the section as the layout put it. Its inverse brings a point picked on the zoomed page back to the fit.
 */
export const shiftOf = (z: IZoom, origin: IPoint): IPoint => ({
  x: origin.x * (1 - z.s) + z.x,
  y: origin.y * (1 - z.s) + z.y,
});

export const unzoomed = (z: IZoom, shift: IPoint, at: IPoint): IPoint => ({
  x: (at.x - shift.x) / z.s,
  y: (at.y - shift.y) / z.s,
});

/** How much of the box stays in the area however far it is panned: it can never be lost off the canvas. */
export const KEEP_PX = 80;

/**
 * The view with the box kept in the area: `box` is the box's corner (in the area's pixels, as the layout put it) and
 * its size at the fit, `area` the area's size.
 */
export const keptInView = (
  z: IZoom,
  box: { x: number; y: number; w: number; h: number },
  area: { w: number; h: number },
): IZoom => {
  const keepX = Math.min(KEEP_PX, box.w * z.s);
  const keepY = Math.min(KEEP_PX, box.h * z.s);
  return {
    s: z.s,
    x: clamp(keepX - box.x - box.w * z.s, area.w - keepX - box.x, z.x),
    y: clamp(keepY - box.y - box.h * z.s, area.h - keepY - box.y, z.y),
  };
};

/** The keys of the canvas: ⌘/Ctrl with «+», «−» or «0», and Shift+1 (fit), as a design tool has them. */
export const ZOOM_KEY = { in: 'in', out: 'out', reset: 'reset' } as const;
export type TZoomKey = (typeof ZOOM_KEY)[keyof typeof ZOOM_KEY];

export const zoomKeyOf = (event: {
  key: string;
  code?: string;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}): TZoomKey | null => {
  if (event.altKey) return null;
  if (event.metaKey || event.ctrlKey) {
    if (event.key === '=' || event.key === '+') return ZOOM_KEY.in;
    if (event.key === '-' || event.key === '_') return ZOOM_KEY.out;
    if (event.key === '0') return ZOOM_KEY.reset;
    return null;
  }
  // Shift+1 is «!» on most layouts: the physical key tells it.
  if (event.shiftKey && (event.code === 'Digit1' || event.key === '1')) return ZOOM_KEY.reset;
  return null;
};

/** A forwarded pinch or ⌘-wheel from the page's frame as a wheel's delta: what `wheelFactor` turns back into it. */
export const deltaOfScale = (ratio: number): number =>
  ratio > 0 && Number.isFinite(ratio) ? -Math.log(ratio) / 0.01 : 0;

/** What Safari's gesture event carries: the scale since its start and where the fingers are. */
export const gestureOf = (event: Event): { scale: number; at: IPoint } => {
  const g = event as Event & { scale?: number; clientX?: number; clientY?: number };
  return { scale: g.scale ?? 1, at: { x: g.clientX ?? 0, y: g.clientY ?? 0 } };
};

/**
 * How the view fits the box into the area, kept as the area or the device changes until the person moves the view:
 * `auto` is the layout's own fit (never larger than the page's size), `width` and `height` fill one side of the area,
 * `whole` is the largest view that shows all of the box.
 */
export const FIT = { auto: 'auto', width: 'width', height: 'height', whole: 'whole' } as const;
export type TFit = (typeof FIT)[keyof typeof FIT];

/** The area's padding, the room the layout keeps around the box (and under it for the pill). */
export interface IInset {
  l: number;
  t: number;
  r: number;
  b: number;
}

/**
 * The view of a fit: `box` is the box's size at the layout's fit, which the layout puts in the middle of the area's
 * padded inside. Fitted by its width a page taller than the area shows its top; otherwise the box stands in the middle.
 */
export const fitView = (
  mode: TFit,
  box: { w: number; h: number },
  area: { w: number; h: number },
  pad: IInset,
): IZoom => {
  if (mode === FIT.auto || box.w <= 0 || box.h <= 0) return ZOOM_FIT;
  const iw = Math.max(1, area.w - pad.l - pad.r);
  const ih = Math.max(1, area.h - pad.t - pad.b);
  const scales = { width: iw / box.w, height: ih / box.h, whole: Math.min(iw / box.w, ih / box.h) } as const;
  const s = clamp(ZOOM.min, ZOOM.max, scales[mode]);
  const w = box.w * s;
  const h = box.h * s;
  const left = pad.l + (iw - box.w) / 2;
  const top = pad.t + (ih - box.h) / 2;
  const toTop = mode === FIT.width && h > ih;
  return { s, x: pad.l + (iw - w) / 2 - left, y: (toTop ? pad.t : pad.t + (ih - h) / 2) - top };
};

/**
 * Whether a pan told by the page (a wheel nothing on it could scroll by, a drag with the middle button) moves the
 * canvas: never while the view is locked, and a wheel leaves alone a page that fills the area at its fit — there it
 * would only push the page away.
 */
export const takesPagePan = (o: { locked: boolean; drag: boolean; fill: boolean; atFit: boolean }): boolean =>
  !o.locked && (o.drag || !(o.fill && o.atFit));
