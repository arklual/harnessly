// What a design remembers of how it is looked at: the device (and its browser), the canvas's view and the page. It is
// kept in the browser per design, so the Designly tab and the right pane open the same design the same way.
import { DEVICES, type ISize } from '@/next/features/designly/devices';
import { FIT, ZOOM, ZOOM_FIT, type IZoom, type TFit } from '@/next/features/designly/site-zoom';

const KEY_PREFIX = 'hx-next-dl-view:';
const VERSION = 1;
/** The quiet after the last change before the view is written: a pan or a drag is not written frame by frame. */
export const SAVE_MS = 400;
/** The sizes «Резина» takes. */
const SIZE = { min: 1, max: 3840 } as const;
/** A box that changed by more than this since the view was saved is not the page it was moved over. */
export const BOX_DRIFT = 0.25;

export interface IViewMemory {
  device: string;
  size: ISize | null;
  rot: boolean;
  chrome: boolean;
  browser: string | null;
  /** The fit the view keeps; null when the person moved it, then `zoom` is the view. */
  fit: TFit | null;
  zoom: IZoom;
  locked: boolean;
  /** The box's size at the fit when the view was saved: a view is only carried over a box of about that size. */
  box: ISize | null;
  page: string | null;
}

export const VIEW_DEFAULT: IViewMemory = {
  device: 'fill',
  size: null,
  rot: false,
  chrome: true,
  browser: null,
  fit: FIT.auto,
  zoom: ZOOM_FIT,
  locked: false,
  box: null,
  page: null,
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

const num = (value: unknown): number | null =>
  typeof value === 'number' && Number.isFinite(value) ? value : null;

const sizeOf = (value: unknown, min: number, max: number): ISize | null => {
  if (!isRecord(value)) return null;
  const w = num(value.w);
  const h = num(value.h);
  if (w === null || h === null || w < min || h < min) return null;
  return { w: Math.min(max, Math.round(w)), h: Math.min(max, Math.round(h)) };
};

const fitOf = (value: unknown): TFit | null | undefined => {
  if (value === null) return null;
  return Object.values(FIT).find((mode) => mode === value);
};

/** A saved record taken apart into what is valid; whatever is not stays at its default. */
export const parseView = (raw: unknown): IViewMemory => {
  if (!isRecord(raw) || raw.v !== VERSION) return VIEW_DEFAULT;
  const device =
    typeof raw.device === 'string' && DEVICES.some((d) => d.id === raw.device) ? raw.device : null;
  if (device === null) return { ...VIEW_DEFAULT, page: typeof raw.page === 'string' ? raw.page : null };
  const zoomRaw = isRecord(raw.zoom) ? raw.zoom : {};
  const s = num(zoomRaw.s);
  const x = num(zoomRaw.x);
  const y = num(zoomRaw.y);
  const zoom: IZoom =
    s === null || x === null || y === null
      ? ZOOM_FIT
      : { s: Math.max(ZOOM.min, Math.min(ZOOM.max, s)), x, y };
  const fit = fitOf(raw.fit);
  return {
    device,
    size: device === 'fill' ? sizeOf(raw.size, SIZE.min, SIZE.max) : null,
    rot: raw.rot === true,
    chrome: raw.chrome !== false,
    browser: typeof raw.browser === 'string' ? raw.browser : null,
    fit: fit === undefined ? FIT.auto : fit,
    zoom,
    locked: raw.locked === true,
    box: sizeOf(raw.box, 1, Number.MAX_SAFE_INTEGER),
    page: typeof raw.page === 'string' ? raw.page : null,
  };
};

export const readView = (design: string): IViewMemory => {
  try {
    const text = localStorage.getItem(KEY_PREFIX + design);
    return text === null ? VIEW_DEFAULT : parseView(JSON.parse(text));
  } catch {
    return VIEW_DEFAULT;
  }
};

export const writeView = (design: string, view: IViewMemory): void => {
  try {
    localStorage.setItem(KEY_PREFIX + design, JSON.stringify({ v: VERSION, ...view }));
  } catch {
    // Private mode or full storage: the view lives for this page only.
  }
};

const drifted = (was: number, now: number) => was <= 0 || Math.abs(now - was) / was > BOX_DRIFT;

/**
 * The view to start with over a box of this size: a free view (a pan, a zoom) is kept only where the page is about the
 * size it was; over a very different one it is lost, so it is the fit again.
 */
export const startView = (
  saved: Pick<IViewMemory, 'fit' | 'zoom' | 'box'>,
  box: ISize,
): { fit: TFit | null; zoom: IZoom } => {
  if (saved.fit !== null) return { fit: saved.fit, zoom: ZOOM_FIT };
  if (saved.box === null || drifted(saved.box.w, box.w) || drifted(saved.box.h, box.h))
    return { fit: FIT.auto, zoom: ZOOM_FIT };
  return { fit: null, zoom: saved.zoom };
};
