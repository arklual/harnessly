// The deck's geometry, pure: a board drawn w×h is shown scaled into the stage's slot, so every point the host and the
// board talk about is converted between the two (board px ↔ slot px ↔ percent of the slide). A block dragged by the
// host's grip snaps as the board's own drag does: to the slide's centre lines and to the layout's left margin.
// No React here.
import type { IDeckGuide, TDeckRect } from '@/next/features/designly/deck/bridge';

/** A board's size before the deck or the board said it: the common 16:9 design size. */
export const DECK_BOARD_W = 1920;
export const DECK_BOARD_H = 1080;
/** The layout's left margin, a share of the slide's width (the board's 7%). */
export const DECK_MARGIN = 0.07;
/** How close a dragged block's line comes to a guide before it sticks, in screen px. */
export const DECK_SNAP_PX = 6;
/** The block frame stands this far outside the block, screen px. */
export const DECK_FRAME_PAD = 3;

/** Screen px per board px for a board `boardW` wide shown `slotW` wide; 1 while either is unknown. */
export const deckScale = (slotW: number, boardW: number): number =>
  slotW > 0 && boardW > 0 ? slotW / boardW : 1;

/** A point on the slide as percent of its width and height, clamped to the slide. */
export const deckPercent = (x: number, y: number, w: number, h: number): [number, number] => {
  const clamp = (v: number) => Math.max(0, Math.min(100, v));
  return [clamp(w > 0 ? (x / w) * 100 : 0), clamp(h > 0 ? (y / h) * 100 : 0)];
};

export interface IDeckSnap {
  /** The move after snapping, board px. */
  dx: number;
  dy: number;
  guides: IDeckGuide[];
}

/**
 * A block at `rect` (board px, where it was when the drag began) moved by dx, dy (board px) on a board w×h: its centre
 * sticks to the vertical centre line or its left edge to the margin, and its middle to the horizontal centre line,
 * when it comes within `snap` board px of them.
 */
export const deckSnap = (
  rect: TDeckRect,
  dx: number,
  dy: number,
  w: number,
  h: number,
  snap: number,
): IDeckSnap => {
  const [x, y, bw, bh] = rect;
  const guides: IDeckGuide[] = [];
  let sx = dx;
  let sy = dy;
  const cx = x + bw / 2 + dx;
  const left = x + dx;
  const margin = w * DECK_MARGIN;
  if (Math.abs(cx - w / 2) < snap) {
    sx += w / 2 - cx;
    guides.push({ v: 1, at: w / 2 });
  } else if (Math.abs(left - margin) < snap) {
    sx += margin - left;
    guides.push({ v: 1, at: margin });
  }
  const cy = y + bh / 2 + dy;
  if (Math.abs(cy - h / 2) < snap) {
    sy += h / 2 - cy;
    guides.push({ v: 0, at: h / 2 });
  }
  return { dx: sx, dy: sy, guides };
};

/** A rect moved by dx, dy. */
export const deckShift = (rect: TDeckRect, dx: number, dy: number): TDeckRect => [
  rect[0] + dx,
  rect[1] + dy,
  rect[2],
  rect[3],
];

/** Board px rounded to a tenth: what a move writes into the board's source. */
export const deckRound = (v: number): number => Math.round(v * 10) / 10;

/** The slide's index for a board path among the deck's boards; 0 when it is not one of them. */
export const deckIndexOf = (boards: readonly { path: string }[], path: string | null): number => {
  const at = path === null ? -1 : boards.findIndex((b) => b.path === path);
  return at < 0 ? 0 : at;
};

/** One step from `index` among `count` slides, kept inside; null when it would leave the deck. */
export const deckStep = (index: number, step: number, count: number): number | null => {
  const next = Math.max(0, Math.min(count - 1, index + step));
  return next === index ? null : next;
};
