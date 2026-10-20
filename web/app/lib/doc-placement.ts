import type { IDocRect } from '@/types/doc-threads';

/** Where floating pieces of the viewer (comment button, composer, thread popover) sit over the document frame. */
export interface IBox {
  width: number;
  height: number;
}

export interface IPoint {
  top: number;
  left: number;
}

const EDGE = 8;
const clamp = (value: number, min: number, max: number): number =>
  Math.max(min, Math.min(value, Math.max(min, max)));

export const COMMENT_BUTTON_SIZE = 32;
export const POPOVER_WIDTH = 320;
const POPOVER_RAISE = 34;
const MIN_POPOVER_ROOM = 120;

/**
 * The «Comment» button: by the end of the selection, just under its last line, so it never covers
 * the selected words; above the first line when there is no room below. Clamped inside the frame.
 */
export const placeCommentButton = (
  selection: { rect: IDocRect; end: IDocRect },
  box: IBox,
  size = COMMENT_BUTTON_SIZE,
  gap = 6,
): IPoint => {
  const left = clamp(selection.end.left + selection.end.width + gap, EDGE, box.width - size - EDGE);
  const below = selection.end.top + selection.end.height + gap;
  const top = below + size + EDGE <= box.height ? below : selection.rect.top - size - gap;
  return { top: clamp(top, EDGE, box.height - size - EDGE), left };
};

export const GUTTER_MIN = 340;

export type TThreadPlacement = { mode: 'gutter'; top: number; left: number } | { mode: 'card' };

/**
 * Where a thread's popover goes. In the free gutter right of the page's text column when it is at least
 * 340px wide, level with the pin, so it never covers the quoted text or the line the pin is on; otherwise the
 * same bottom card the composer uses, which is tied to the window edge and not to the line being read.
 */
export const placeThread = (
  rect: IDocRect,
  box: IBox,
  columnRight: number,
  width = POPOVER_WIDTH,
): TThreadPlacement => {
  const gutter = box.width - columnRight;
  if (gutter < GUTTER_MIN) return { mode: 'card' };
  const left = Math.max(columnRight + EDGE, columnRight + (gutter - width) / 2);
  return { mode: 'gutter', top: clamp(rect.top - POPOVER_RAISE, EDGE, box.height - MIN_POPOVER_ROOM), left };
};

/** A pin is drawn only while its place is inside the visible part of the document. */
export const isInView = (rect: IDocRect | null | undefined, box: IBox): rect is IDocRect =>
  Boolean(rect) &&
  rect!.top + rect!.height > 0 &&
  rect!.top < box.height &&
  rect!.left < box.width &&
  rect!.left + rect!.width > 0;
