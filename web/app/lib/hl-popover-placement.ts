const GAP = 8;
const MARGIN = 8;

export interface IPopoverPlacement {
  left: number;
  top: number;
  side: 'above' | 'below';
}

interface IPlacePopoverInput {
  /** The selection in viewport coordinates: its horizontal span, the top of the first line and the bottom of the last. */
  selection: { left: number; right: number; firstTop: number; lastBottom: number };
  size: { width: number; height: number };
  viewport: { width: number; height: number };
  /** On a touch screen the system menu sits above the selection, so the panel prefers the space below it; so does a panel opened from a button. */
  preferBelow: boolean;
}

export const placePopover = ({
  selection,
  size,
  viewport,
  preferBelow,
}: IPlacePopoverInput): IPopoverPlacement => {
  const centered = (selection.left + selection.right) / 2 - size.width / 2;
  const left = Math.round(Math.max(MARGIN, Math.min(centered, viewport.width - size.width - MARGIN)));
  const aboveTop = Math.round(selection.firstTop - GAP - size.height);
  const belowTop = Math.round(selection.lastBottom + GAP);
  const fitsAbove = aboveTop >= MARGIN;
  const fitsBelow = belowTop + size.height <= viewport.height - MARGIN;
  if (preferBelow ? fitsBelow : !fitsAbove && fitsBelow) return { left, top: belowTop, side: 'below' };
  if (fitsAbove) return { left, top: aboveTop, side: 'above' };
  return { left, top: Math.max(MARGIN, viewport.height - size.height - MARGIN), side: 'below' };
};
