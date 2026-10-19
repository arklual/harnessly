export type Side = 'top' | 'bottom' | 'left' | 'right';
export type Align = 'start' | 'center' | 'end';

export interface Placement {
  side: Side;
  align: Align;
}

export interface Rect {
  left: number;
  top: number;
  width: number;
  height: number;
}

export interface Placed {
  left: number;
  top: number;
  side: Side;
  /** Height the popup may take on its side before it has to scroll. */
  maxHeight: number;
}

const opposite: Record<Side, Side> = { top: 'bottom', bottom: 'top', left: 'right', right: 'left' };

/**
 * Where a popup of the given size goes next to an anchor inside a viewport: on the wanted side when it fits there,
 * else on the opposite side when that has more room, then moved along the other axis to stay inside the viewport
 * with a margin.
 */
export function place(
  anchor: Rect,
  popup: { width: number; height: number },
  viewport: { width: number; height: number },
  want: Placement,
  gap = 6,
  margin = 8,
): Placed {
  const room: Record<Side, number> = {
    top: anchor.top - gap - margin,
    bottom: viewport.height - (anchor.top + anchor.height) - gap - margin,
    left: anchor.left - gap - margin,
    right: viewport.width - (anchor.left + anchor.width) - gap - margin,
  };
  const vertical = want.side === 'top' || want.side === 'bottom';
  const need = vertical ? popup.height : popup.width;
  let side = want.side;
  if (room[side] < need && room[opposite[side]] > room[side]) side = opposite[side];

  const alongStart = vertical ? anchor.left : anchor.top;
  const alongSize = vertical ? anchor.width : anchor.height;
  const popupAlong = vertical ? popup.width : popup.height;
  const viewAlong = vertical ? viewport.width : viewport.height;
  let along =
    want.align === 'start'
      ? alongStart
      : want.align === 'end'
        ? alongStart + alongSize - popupAlong
        : alongStart + (alongSize - popupAlong) / 2;
  along = Math.max(margin, Math.min(along, viewAlong - margin - popupAlong));

  let across: number;
  if (side === 'bottom') across = anchor.top + anchor.height + gap;
  else if (side === 'top') across = anchor.top - gap - Math.min(popup.height, Math.max(room.top, 0));
  else if (side === 'right') across = anchor.left + anchor.width + gap;
  else across = anchor.left - gap - popup.width;

  return vertical
    ? { left: along, top: Math.max(margin, across), side, maxHeight: Math.max(room[side], 80) }
    : { left: Math.max(margin, across), top: along, side, maxHeight: viewport.height - 2 * margin };
}
