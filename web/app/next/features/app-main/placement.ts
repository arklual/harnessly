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

/** The kept layout, or the column when nothing usable was kept (an older shape, a hand edit). */
export const parseLayout = (raw: string | null): IAppLayout => {
  const fallback: IAppLayout = { placement: PLACEMENT.left, float: null };
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

export const readLayout = (): IAppLayout => {
  try {
    return parseLayout(localStorage.getItem(KEY));
  } catch {
    return parseLayout(null);
  }
};

export const keepLayout = (layout: IAppLayout): void => {
  try {
    localStorage.setItem(KEY, JSON.stringify(layout));
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

/** An edge or a corner of the window the pointer grows it by. */
export const FLOAT_EDGES = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'] as const;
export type TFloatEdge = (typeof FLOAT_EDGES)[number];

/**
 * The window grown by an edge or a corner: the pointer moved (dx, dy) since it took hold. The opposite edge stays where
 * it was; the window keeps the minimum size and stays inside the section.
 */
export const resizeFloat = (
  f: IFloat,
  edge: TFloatEdge,
  dx: number,
  dy: number,
  section: ISection,
): IFloat => {
  const right = f.x + f.w;
  const bottom = f.y + f.h;
  let { x, y, w, h } = f;
  if (edge.includes('e')) w = Math.min(Math.max(FLOAT_MIN.w, f.w + dx), section.width - FLOAT_MARGIN - f.x);
  if (edge.includes('s')) h = Math.min(Math.max(FLOAT_MIN.h, f.h + dy), section.height - FLOAT_MARGIN - f.y);
  if (edge.includes('w')) {
    x = Math.min(Math.max(FLOAT_MARGIN, f.x + dx), right - FLOAT_MIN.w);
    w = right - x;
  }
  if (edge.includes('n')) {
    y = Math.min(Math.max(FLOAT_MARGIN, f.y + dy), bottom - FLOAT_MIN.h);
    h = bottom - y;
  }
  return clampFloat({ x, y, w, h }, section);
};
