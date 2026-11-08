// SPDX-License-Identifier: Apache-2.0
// Adapted from Taiga UI (https://github.com/taiga-family/taiga-ui), Apache-2.0.
// Source: projects/core/portals/hint/hint-position.directive.ts (12 points, priority, fallback)
// and projects/core/portals/hint/hint-anchored.directive.ts (viewport clamp, beak).
//
// Pure placement of a hint bubble: no DOM, unit-tested with plain rectangles. The
// component measures the anchor, the bubble and the viewport, calls `computeHintPlacement` and
// writes the result into CSS custom properties of the bubble.

/**
 * The 12 hint directions. `start`/`end` are logical: in RTL they mirror. The union matches
 * `VizHintDirection`.
 */
export type HintPlacementDirection =
  | 'top-start'
  | 'top'
  | 'top-end'
  | 'bottom-start'
  | 'bottom'
  | 'bottom-end'
  | 'start-top'
  | 'start'
  | 'start-bottom'
  | 'end-top'
  | 'end'
  | 'end-bottom';

/** Where a hint goes when none of the requested directions fits (Taiga TUI_HINT_DIRECTIONS). */
export const HINT_PLACEMENT_ORDER: readonly HintPlacementDirection[] = [
  'bottom-start',
  'bottom',
  'bottom-end',
  'top-start',
  'top',
  'top-end',
  'start-top',
  'start',
  'start-bottom',
  'end-top',
  'end',
  'end-bottom',
];

/** Free space the bubble keeps from every viewport edge, px (Taiga GAP). */
export const HINT_VIEWPORT_GAP = 8;

/** Distance from the bubble edge to the beak centre, px (Taiga ARROW_OFFSET). */
export const HINT_BEAK_OFFSET = 22;

export interface HintBox {
  top: number;
  left: number;
  width: number;
  height: number;
}

export interface HintPlacementInput {
  /** Anchor rectangle in viewport coordinates (a 1×1 box for a pointer-following hint). */
  anchor: HintBox;
  /** Bubble layout size (offsetWidth/offsetHeight: the enter scale must not shrink it). */
  bubble: { width: number; height: number };
  viewport: HintBox;
  /** One direction or a priority list; then HINT_PLACEMENT_ORDER. */
  direction: HintPlacementDirection | readonly HintPlacementDirection[];
  /** Gap between the anchor and the bubble, px. */
  offset: number;
  /**
   * true (default in the hint): the beak points at the anchor centre and a direction names
   * where the bubble unravels (`bottom-start` — down and towards the start). false: the
   * direction is the alignment side (`bottom-start` — start edges aligned), as in Floating UI.
   */
  centered: boolean;
  rtl: boolean;
}

export interface HintPlacement {
  /** Bubble position in viewport coordinates, px (rounded). */
  x: number;
  y: number;
  /** The direction that was used, logical (as requested in RTL too). */
  direction: HintPlacementDirection;
  /** Beak centre on the bubble, percent of its width and height (0–100). */
  beakX: number;
  beakY: number;
  /** 90 when the beak sits on a vertical edge (the bubble is beside the anchor). */
  beakRotate: 0 | 90;
}

type Point = readonly [left: number, top: number];

const MIRROR: Record<string, string> = { start: 'end', end: 'start' };

/** start ↔ end: logical directions of an RTL page in the physical (LTR) point table. */
export function mirrorHintDirection(direction: HintPlacementDirection): HintPlacementDirection {
  return direction.replace(/start|end/g, (side) => MIRROR[side] ?? side) as HintPlacementDirection;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

/** The 12 candidate positions (Taiga hint-position getPosition), physical: start = left. */
function points(anchor: HintBox, width: number, height: number, offset: number, centered: boolean) {
  const right = anchor.left + anchor.width;
  const bottom = anchor.top + anchor.height;
  const leftCenter = anchor.left + anchor.width / 2;
  const topCenter = anchor.top + anchor.height / 2;
  const narrow = anchor.width < HINT_BEAK_OFFSET * 2 || centered;
  const short = anchor.height < HINT_BEAK_OFFSET * 2 || centered;
  const start = narrow ? leftCenter - HINT_BEAK_OFFSET : anchor.left;
  const end = narrow ? leftCenter - width + HINT_BEAK_OFFSET : right - width;
  const top = short ? topCenter - HINT_BEAK_OFFSET : anchor.top;
  const bottomEdge = short ? topCenter - height + HINT_BEAK_OFFSET : bottom - height;
  // Centered: a direction names where the bubble unravels, so `-start` takes the end point.
  // Taiga 5.25 gives `start-bottom` the same point as `start-top`; here it mirrors the
  // horizontal rule (unravels down when centered, bottom edges aligned otherwise).
  const startX = centered ? end : start;
  const endX = centered ? start : end;
  const sideTop = centered ? bottomEdge : top;
  const sideBottom = centered ? top : bottomEdge;
  const above = anchor.top - height - offset;
  const below = bottom + offset;
  const before = anchor.left - width - offset;
  const after = right + offset;
  const middle = topCenter - height / 2;
  const center = leftCenter - width / 2;
  const table: Record<HintPlacementDirection, Point> = {
    'top-start': [startX, above],
    top: [center, above],
    'top-end': [endX, above],
    'bottom-start': [startX, below],
    bottom: [center, below],
    'bottom-end': [endX, below],
    'start-top': [before, sideTop],
    start: [before, middle],
    'start-bottom': [before, sideBottom],
    'end-top': [after, sideTop],
    end: [after, middle],
    'end-bottom': [after, sideBottom],
  };
  return table;
}

function fits([left, top]: Point, width: number, height: number, viewport: HintBox): boolean {
  return (
    top > viewport.top + HINT_VIEWPORT_GAP &&
    left > viewport.left + HINT_VIEWPORT_GAP &&
    top + height < viewport.top + viewport.height - HINT_VIEWPORT_GAP &&
    left + width < viewport.left + viewport.width - HINT_VIEWPORT_GAP
  );
}

/** Beak coordinate along one axis (Taiga hint-anchored update): an aligned edge snaps to 22px. */
function beakAlong(position: number, size: number, anchorStart: number, anchorSize: number): number {
  if (Math.round(position) === Math.round(anchorStart)) return HINT_BEAK_OFFSET;
  if (Math.round(position + size) === Math.round(anchorStart + anchorSize)) return size - HINT_BEAK_OFFSET;
  return anchorStart + anchorSize / 2 - position;
}

function percent(value: number, size: number): number {
  return size > 0 ? Math.round((clamp(value, 0, size) / size) * 100) : 0;
}

/**
 * Where the bubble goes: the first requested direction that fits the viewport with an 8px gap,
 * then the Taiga fallback order, then top or bottom — whichever has more room. The bubble is
 * clamped horizontally into the viewport and the beak follows the anchor centre.
 */
export function computeHintPlacement(input: HintPlacementInput): HintPlacement {
  const { anchor, bubble, viewport, offset, centered, rtl } = input;
  const { width, height } = bubble;
  const table = points(anchor, width, height, offset, centered);
  const requested = typeof input.direction === 'string' ? [input.direction] : input.direction;
  const physical = (direction: HintPlacementDirection) => (rtl ? mirrorHintDirection(direction) : direction);
  const order = [...requested, ...HINT_PLACEMENT_ORDER].map(physical);
  const viewportBottom = viewport.top + viewport.height;
  const roomBelow = viewportBottom - table.bottom[1];
  const fallback: HintPlacementDirection = table.top[1] > roomBelow ? 'top' : 'bottom';
  const chosen = order.find((direction) => fits(table[direction], width, height, viewport)) ?? fallback;
  const [left, top] = table[chosen];

  const max = Math.max(HINT_VIEWPORT_GAP, viewport.width + viewport.left - width - HINT_VIEWPORT_GAP);
  const safeLeft = clamp(Math.max(HINT_VIEWPORT_GAP, left), viewport.left + HINT_VIEWPORT_GAP, max);
  const beakX = percent(beakAlong(safeLeft, width, anchor.left, anchor.width), width);
  const beakY = percent(beakAlong(top, height, anchor.top, anchor.height), height);
  const onSide = beakX === 0 || Math.ceil(beakX) === 100;

  return {
    x: Math.round(safeLeft),
    y: Math.round(top),
    direction: physical(chosen),
    beakX,
    beakY,
    beakRotate: onSide ? 90 : 0,
  };
}
