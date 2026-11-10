// Where a pill over the canvas stands (HRN-673): the zoom pill by default in the bottom right corner of the canvas, the
// phone's pill beside the phone (HRN-674); the person may drag one anywhere over the canvas or hide it. Its place is kept
// per browser, under the pill's own key, as its distance from the right and the bottom edges, so a window grown or
// narrowed keeps it by the same corner, and it is always put back inside the canvas.

export interface IPillPos {
  /** From the canvas's right edge to the pill's right edge, px. */
  right: number;
  /** From the canvas's bottom edge to the pill's bottom edge, px. */
  bottom: number;
}

export interface IPillPrefs {
  /** null: the corner it stands in by default. */
  pos: IPillPos | null;
  hidden: boolean;
}

export const PILL_DEFAULT: IPillPrefs = { pos: null, hidden: false };
/** The pill keeps this far from the canvas's edges. */
export const PILL_EDGE = 8;
/** A press that moved less than this is a click on the pill's buttons, not a drag. */
export const PILL_DRAG_SLOP = 4;

/** The keys the pills are kept under. */
export const PILL_KEY = { zoom: 'hx-next-zoom-pill', phone: 'hx-next-phone-pill' } as const;
export type TPillKey = (typeof PILL_KEY)[keyof typeof PILL_KEY];

/** `pos` kept inside a canvas of `area`, for a pill of `pill`. */
export const clampPill = (
  pos: IPillPos,
  area: { w: number; h: number },
  pill: { w: number; h: number },
): IPillPos => ({
  right: Math.round(
    Math.min(Math.max(PILL_EDGE, pos.right), Math.max(PILL_EDGE, area.w - pill.w - PILL_EDGE)),
  ),
  bottom: Math.round(
    Math.min(Math.max(PILL_EDGE, pos.bottom), Math.max(PILL_EDGE, area.h - pill.h - PILL_EDGE)),
  ),
});

const isNum = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value);

export const parsePill = (raw: unknown): IPillPrefs => {
  if (typeof raw !== 'object' || raw === null) return PILL_DEFAULT;
  const rec = raw as Record<string, unknown>;
  const pos = rec.pos as Record<string, unknown> | null | undefined;
  return {
    pos: pos && isNum(pos.right) && isNum(pos.bottom) ? { right: pos.right, bottom: pos.bottom } : null,
    hidden: rec.hidden === true,
  };
};

export const readPill = (key: TPillKey): IPillPrefs => {
  try {
    const text = localStorage.getItem(key);
    return text === null ? PILL_DEFAULT : parsePill(JSON.parse(text));
  } catch {
    return PILL_DEFAULT;
  }
};

export const writePill = (key: TPillKey, prefs: IPillPrefs): void => {
  try {
    localStorage.setItem(key, JSON.stringify(prefs));
  } catch {
    // Private mode or full storage: the pill keeps its place for this page.
  }
};

/** How a pill of buttons stands: up (a column) or along (a row). */
export const PILL_AXIS = { col: 'col', row: 'row' } as const;
export type TPillAxis = (typeof PILL_AXIS)[keyof typeof PILL_AXIS];
/** The gap between the phone and its pill. */
export const BESIDE_GAP = 12;

/**
 * The phone's pill by the phone (HRN-674): on its right, level with its top, standing up, where the canvas has room for
 * it there (it usually has: the canvas is wider than a phone); else over the phone along its top, by its right edge.
 * `box` is the phone's case in the canvas's own coordinates, `col` the pill's size standing up.
 */
export const besideBox = (
  area: { w: number; h: number },
  box: { left: number; top: number; right: number },
  col: { w: number; h: number },
): { axis: TPillAxis; pos: IPillPos } => {
  const left = box.right + BESIDE_GAP;
  if (left + col.w <= area.w - PILL_EDGE)
    return { axis: PILL_AXIS.col, pos: { right: area.w - left - col.w, bottom: area.h - box.top - col.h } };
  return { axis: PILL_AXIS.row, pos: { right: area.w - box.right, bottom: area.h - box.top + BESIDE_GAP } };
};
