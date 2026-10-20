/**
 * Where a reader of the timeline is, kept as «at the end» or as the first entry on screen and its distance
 * from the top of the scroller, never as a raw scrollTop: content above can be replaced, collapse while a socket
 * comes back, or grow while the tab is hidden, and a raw position then lands somewhere else — the timeline jumped up.
 */

export const AT_END_PX = 8;
/** Scroll events this long after the person touched the timeline are theirs; the rest come from layout changes. */
export const GESTURE_WINDOW_MS = 800;

export interface IScrollAnchor {
  atEnd: boolean;
  entryId: string | null;
  /** The entry's top minus the scroller's top, in pixels, when the anchor was taken. */
  offset: number;
}

export interface IEntryBox {
  id: string;
  top: number;
  bottom: number;
}

export const isAtEnd = (scrollHeight: number, scrollTop: number, clientHeight: number): boolean =>
  scrollHeight - scrollTop - clientHeight <= AT_END_PX;

/** The first entry whose bottom is below the scroller's top edge: the one the reader is looking at. */
export const takeAnchor = (
  atEnd: boolean,
  entries: readonly IEntryBox[],
  scrollerTop: number,
): IScrollAnchor => {
  if (atEnd) return { atEnd: true, entryId: null, offset: 0 };
  const first = entries.find((entry) => entry.bottom > scrollerTop + 1);
  return first
    ? { atEnd: false, entryId: first.id, offset: first.top - scrollerTop }
    : { atEnd: false, entryId: null, offset: 0 };
};

/** How far to scroll so the anchored entry is back where it was; `null` when there is nothing to do. */
export const anchorCorrection = (
  anchor: IScrollAnchor,
  entryTop: number | null,
  scrollerTop: number,
): number | null => {
  if (anchor.atEnd || anchor.entryId === null || entryTop === null) return null;
  const delta = entryTop - scrollerTop - anchor.offset;
  return Math.abs(delta) < 1 ? null : delta;
};

export interface IHeldHeight {
  /** The content's box now, at least the height already held. */
  box: number;
  /** How tall the rows themselves are, without the held space. */
  rows: number;
  distanceToEnd: number;
  viewport: number;
  /** How long empty space has been held, ms; 0 when none is. */
  heldFor: number;
}

/** The most empty space held: the live thought (six lines and its margins) is what folds at a turn's end. */
export const MAX_HELD_GAP = 240;
/** Held space is given back after this long, by when the answer that replaces the thought has grown. */
export const HOLD_MS = 2_000;

/**
 * The height the content keeps while its end is on screen, so the end getting shorter (the live thought
 * folding as the answer comes) leaves empty space for a moment instead of pulling everything down; the next rows fill
 * it. At most MAX_HELD_GAP of empty space, for at most HOLD_MS, and none once the end is off screen. A hold without a
 * limit kept up to a screen of blank space under the last message for as long as the reader stayed at the end, so a
 * conversation opened on an empty screen. `null`: nothing is held.
 */
export const heldHeight = (
  held: number | null,
  { box, rows, distanceToEnd, viewport, heldFor }: IHeldHeight,
): number | null => {
  if (distanceToEnd > viewport || heldFor >= HOLD_MS) return null;
  const next = Math.min(Math.max(held ?? 0, box), rows + MAX_HELD_GAP);
  return next > rows ? next : null;
};
