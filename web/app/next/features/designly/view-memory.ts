// What a design remembers of how it is looked at: the device (and its browser), the canvas's view and the page. It is
// kept in the browser per design, so the Designly tab and the right pane open the same design the same way.
import { canRotate, deviceOf, DEVICES, keptSize, type ISize } from '@/next/features/designly/devices';
import { ZOOM, ZOOM_FIT, type IZoom } from '@/next/features/designly/site-zoom';

const KEY_PREFIX = 'hx-next-dl-view:';
/** 2: the view is locked unless the person unlocked it (HRN-673); a record of 1 kept `locked: false` by default. */
const VERSION = 2;
const VERSIONS: readonly unknown[] = [1, VERSION];
/** The quiet after the last change before the view is written: a pan or a drag is not written frame by frame. */
export const SAVE_MS = 400;
/** A box that changed by more than this since the view was saved is not the page it was moved over. */
export const BOX_DRIFT = 0.25;

export interface IViewMemory {
  device: string;
  size: ISize | null;
  rot: boolean;
  chrome: boolean;
  browser: string | null;
  /** The view is the standard one of its device (HRN-674); false when the person moved it, then `zoom` is the view. */
  std: boolean;
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
  std: true,
  zoom: ZOOM_FIT,
  locked: true,
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

/** A record from before HRN-674 kept a fit (by width, by height, whole…) or null for a moved view: any fit is the
 *  standard view now. */
const stdOf = (raw: Record<string, unknown>): boolean =>
  typeof raw.std === 'boolean' ? raw.std : raw.fit !== null;

/** A saved record taken apart into what is valid; whatever is not stays at its default. */
export const parseView = (raw: unknown): IViewMemory => {
  if (!isRecord(raw) || !VERSIONS.includes(raw.v)) return VIEW_DEFAULT;
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
  return {
    device,
    // A kept size under the least of «Резина» (150 × 932 once seen) is dropped: the window fills the area again.
    size:
      device === 'fill' && isRecord(raw.size)
        ? keptSize({ w: num(raw.size.w) ?? 0, h: num(raw.size.h) ?? 0 })
        : null,
    // A computer turned before HRN-674 has nothing to turn it back with now: it stands.
    rot: raw.rot === true && canRotate(deviceOf(device)),
    chrome: raw.chrome !== false,
    browser: typeof raw.browser === 'string' ? raw.browser : null,
    std: stdOf(raw),
    zoom,
    // Only an unlock the person made since the lock became the default is kept.
    locked: raw.v !== VERSION || raw.locked !== false,
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
 * size it was; over a very different one it is lost, so it is the standard view again.
 */
export const startView = (
  saved: Pick<IViewMemory, 'std' | 'zoom' | 'box'>,
  box: ISize,
): { std: boolean; zoom: IZoom } => {
  if (saved.std) return { std: true, zoom: ZOOM_FIT };
  if (saved.box === null || drifted(saved.box.w, box.w) || drifted(saved.box.h, box.h))
    return { std: true, zoom: ZOOM_FIT };
  return { std: false, zoom: saved.zoom };
};
