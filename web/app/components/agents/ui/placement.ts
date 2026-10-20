/**
 * Where a floating layer (popover, menu, tooltip) goes next to its anchor.
 * Pure geometry in viewport coordinates; use-anchored-position.ts feeds it DOM rects
 * and writes the result into CSS custom properties.
 */

export const LAYER_SIDE = {
  top: 'top',
  bottom: 'bottom',
  left: 'left',
  right: 'right',
} as const;

export const LAYER_ALIGN = {
  start: 'start',
  center: 'center',
  end: 'end',
} as const;

export type TLayerSide = (typeof LAYER_SIDE)[keyof typeof LAYER_SIDE];
export type TLayerAlign = (typeof LAYER_ALIGN)[keyof typeof LAYER_ALIGN];

export interface IRect {
  top: number;
  left: number;
  width: number;
  height: number;
}

export interface ISize {
  width: number;
  height: number;
}

export interface IPlacementInput {
  anchor: IRect;
  floating: ISize;
  viewport: ISize;
  side: TLayerSide;
  align: TLayerAlign;
  /** Gap between anchor and layer. */
  offset: number;
  /** Minimal distance from the viewport edge. */
  margin: number;
}

export interface IPlacement {
  top: number;
  left: number;
  side: TLayerSide;
  maxHeight: number;
  maxWidth: number;
}

/** Below this the layer is squeezed so hard that the opposite side is always better. */
const MIN_USEFUL_HEIGHT = 120;

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), Math.max(min, max));

const alignAlong = (start: number, length: number, size: number, align: TLayerAlign): number => {
  if (align === LAYER_ALIGN.end) return start + length - size;
  if (align === LAYER_ALIGN.center) return start + (length - size) / 2;
  return start;
};

const pickSide = <T extends string>(
  preferred: T,
  opposite: T,
  need: number,
  preferredRoom: number,
  oppositeRoom: number,
): T => {
  if (need <= preferredRoom) return preferred;
  if (need <= oppositeRoom) return opposite;
  if (preferredRoom >= MIN_USEFUL_HEIGHT && preferredRoom >= oppositeRoom) return preferred;
  return oppositeRoom > preferredRoom ? opposite : preferred;
};

const placeVertical = (input: IPlacementInput): IPlacement => {
  const { anchor, floating, viewport, offset, margin, align } = input;
  const anchorBottom = anchor.top + anchor.height;
  const roomBelow = viewport.height - anchorBottom - offset - margin;
  const roomAbove = anchor.top - offset - margin;
  const side =
    input.side === LAYER_SIDE.top
      ? pickSide<TLayerSide>(LAYER_SIDE.top, LAYER_SIDE.bottom, floating.height, roomAbove, roomBelow)
      : pickSide<TLayerSide>(LAYER_SIDE.bottom, LAYER_SIDE.top, floating.height, roomBelow, roomAbove);
  const maxHeight = Math.max(0, Math.floor(side === LAYER_SIDE.top ? roomAbove : roomBelow));
  const height = Math.min(floating.height, maxHeight);
  const top = side === LAYER_SIDE.top ? anchor.top - offset - height : anchorBottom + offset;

  const maxWidth = Math.max(0, viewport.width - margin * 2);
  const width = Math.min(floating.width, maxWidth);
  const left = clamp(
    alignAlong(anchor.left, anchor.width, width, align),
    margin,
    viewport.width - margin - width,
  );
  return { top: Math.round(top), left: Math.round(left), side, maxHeight, maxWidth: Math.floor(maxWidth) };
};

const placeHorizontal = (input: IPlacementInput): IPlacement => {
  const { anchor, floating, viewport, offset, margin, align } = input;
  const anchorRight = anchor.left + anchor.width;
  const roomRight = viewport.width - anchorRight - offset - margin;
  const roomLeft = anchor.left - offset - margin;
  const side =
    input.side === LAYER_SIDE.left
      ? pickSide<TLayerSide>(LAYER_SIDE.left, LAYER_SIDE.right, floating.width, roomLeft, roomRight)
      : pickSide<TLayerSide>(LAYER_SIDE.right, LAYER_SIDE.left, floating.width, roomRight, roomLeft);
  const room = Math.max(0, side === LAYER_SIDE.left ? roomLeft : roomRight);
  const width = Math.min(floating.width, Math.max(room, 0));
  const rawLeft = side === LAYER_SIDE.left ? anchor.left - offset - width : anchorRight + offset;
  const left = clamp(rawLeft, margin, viewport.width - margin - width);

  const maxHeight = Math.max(0, Math.floor(viewport.height - margin * 2));
  const height = Math.min(floating.height, maxHeight);
  const top = clamp(
    alignAlong(anchor.top, anchor.height, height, align),
    margin,
    viewport.height - margin - height,
  );
  return {
    top: Math.round(top),
    left: Math.round(left),
    side,
    maxHeight,
    maxWidth: Math.floor(Math.max(room, width)),
  };
};

export const computePlacement = (input: IPlacementInput): IPlacement =>
  input.side === LAYER_SIDE.top || input.side === LAYER_SIDE.bottom
    ? placeVertical(input)
    : placeHorizontal(input);

/** A zero-size anchor at a pointer position (context menu). */
export const pointRect = (x: number, y: number): IRect => ({ top: y, left: x, width: 0, height: 0 });
