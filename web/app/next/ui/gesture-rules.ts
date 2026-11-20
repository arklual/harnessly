// What a drag decides, as plain functions: how fast the finger was going when it let go, and whether a sheet
// or a card goes or comes back. The hooks that follow a finger (sheet.tsx, notifications/motion.ts, toast.tsx) write
// the position straight into a style once a frame and ask these when the finger lifts.

/** A finger that stood still this long before it lifted carries no flick, ms. */
export const STALE_MS = 90;

/** The speed of the last stretch, softened by the ones before it: one fast sample of a jittery pointer is no flick. */
export const softSpeed = (before: number, sample: number): number => before * 0.55 + sample * 0.45;

/** The speed the drag is let go with: none when the finger had stopped before it lifted. */
export const releaseSpeed = (speed: number, lastAt: number, now: number): number =>
  now - lastAt > STALE_MS ? 0 : speed;

/** A sheet dragged down goes past this share of its height, or when flicked down faster than this (px/ms) from this far. */
export const SHEET_CLOSE_SHARE = 1 / 3;
export const SHEET_CLOSE_SPEED = 0.5;
export const SHEET_MIN_FLICK_PX = 24;

export const sheetCloses = (dy: number, height: number, speed: number): boolean =>
  dy > height * SHEET_CLOSE_SHARE || (speed > SHEET_CLOSE_SPEED && dy > SHEET_MIN_FLICK_PX);

/** A card swept sideways goes past this share of its width, or when flicked this fast (px/ms) from beyond a few slops. */
export const SWIPE_HIDE_SHARE = 0.33;
export const SWIPE_FLICK = 0.5;

export type TSwipeVerdict = 'fly' | 'back';

export const swipeVerdict = (shift: number, width: number, speed: number, slop: number): TSwipeVerdict =>
  Math.abs(shift) > width * SWIPE_HIDE_SHARE || (Math.abs(speed) > SWIPE_FLICK && Math.abs(shift) > slop * 3)
    ? 'fly'
    : 'back';
