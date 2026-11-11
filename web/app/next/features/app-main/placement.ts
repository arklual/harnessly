// Where the chat stands beside an app opened as the main thing (/apps/<id>?chat=<conv>): a column at the left, a window
// over the app, or the field at its foot (the board «Приложение — главное»). The choice is the viewer's: one for every
// app, kept in this browser, and the window's place with it.

export const PLACEMENT = { left: 'left', float: 'float', island: 'island' } as const;
export type TPlacement = (typeof PLACEMENT)[keyof typeof PLACEMENT];
export const PLACEMENTS = [PLACEMENT.left, PLACEMENT.float, PLACEMENT.island] as const;

export interface IFloat {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface IAppLayout {
  placement: TPlacement;
  float: IFloat | null;
}

export const FLOAT_MIN = { w: 300, h: 240 } as const;
/** Kept from the edge of the section so a window never goes where it cannot be grabbed. */
export const FLOAT_MARGIN = 12;
const KEY = 'hx-app-main-layout';
const FIRST_W = 360;

const isNumber = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);

const isFloat = (v: unknown): v is IFloat => {
  if (typeof v !== 'object' || v === null) return false;
  const f = v as Record<string, unknown>;
  return isNumber(f.x) && isNumber(f.y) && isNumber(f.w) && isNumber(f.h);
};

/** The kept layout, or `first` (the column) when nothing usable was kept (an older shape, a hand edit). */
export const parseLayout = (raw: string | null, first: TPlacement = PLACEMENT.left): IAppLayout => {
  const fallback: IAppLayout = { placement: first, float: null };
  if (raw === null) return fallback;
  try {
    const v: unknown = JSON.parse(raw);
    if (typeof v !== 'object' || v === null) return fallback;
    const l = v as Record<string, unknown>;
    const placement = PLACEMENTS.find((p) => p === l.placement);
    if (!placement) return fallback;
    return { placement, float: isFloat(l.float) ? l.float : null };
  } catch {
    return fallback;
  }
};

/** The layout kept under `key` (the apps' own by default); `first` where none is kept. */
export const readLayout = (key: string = KEY, first: TPlacement = PLACEMENT.left): IAppLayout => {
  try {
    return parseLayout(localStorage.getItem(key), first);
  } catch {
    return parseLayout(null, first);
  }
};

export const keepLayout = (layout: IAppLayout, key: string = KEY): void => {
  try {
    localStorage.setItem(key, JSON.stringify(layout));
  } catch {
    // Blocked or full storage: the choice holds while the page is open.
  }
};

export interface ISection {
  width: number;
  height: number;
}

/** The first window: on the right at the top, nearly as tall as the page. */
export const firstFloat = (section: ISection): IFloat => ({
  x: Math.max(FLOAT_MARGIN, section.width - FIRST_W - 40),
  y: 64,
  w: FIRST_W,
  h: Math.max(FLOAT_MIN.h, Math.min(560, section.height - 140)),
});

/** The window put inside the section: at least the minimum size, at most the section, never out of the grabbing reach. */
export const clampFloat = (f: IFloat, section: ISection): IFloat => {
  const w = Math.min(Math.max(f.w, FLOAT_MIN.w), Math.max(FLOAT_MIN.w, section.width - 2 * FLOAT_MARGIN));
  const h = Math.min(Math.max(f.h, FLOAT_MIN.h), Math.max(FLOAT_MIN.h, section.height - 2 * FLOAT_MARGIN));
  const x = Math.min(Math.max(f.x, FLOAT_MARGIN), Math.max(FLOAT_MARGIN, section.width - w - FLOAT_MARGIN));
  const y = Math.min(Math.max(f.y, FLOAT_MARGIN), Math.max(FLOAT_MARGIN, section.height - h - FLOAT_MARGIN));
  return { x, y, w, h };
};

/** The layout after the person picked a placement: a window gets its first place once, and keeps it after. */
export const withPlacement = (layout: IAppLayout, placement: TPlacement, section: ISection): IAppLayout => ({
  placement,
  float:
    placement === PLACEMENT.float ? clampFloat(layout.float ?? firstFloat(section), section) : layout.float,
});
