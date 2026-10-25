// Where the annotation pill and its card stand: over the picked text (under it when there is no room above or on a
// touch screen), the text itself stays uncovered, and nothing leaves the screen. Pure, so it is tested without a page.

export const ANNOTATE_GAP = 8;
export const ANNOTATE_MARGIN = 8;
/** What the card needs over the text before it is put there: the card at its usual height with a line or two of remark. */
export const ANNOTATE_CARD_ROOM = 190;

export interface IAnnotateSpot {
  /** The middle of the picked text, in the window's pixels. */
  x: number;
  top: number;
  bottom: number;
}

export interface IAnnotatePlaceInput extends IAnnotateSpot {
  viewportW: number;
  viewportH: number;
  pillW: number;
  cardW: number;
  /** The card as tall as it is now; the pill is as tall as `pillH`. */
  cardH: number;
  pillH: number;
  expanded: boolean;
  /** A touch screen has its own menu over the text: the pill goes under it. */
  preferBelow: boolean;
  /** The side the pill has taken; the card keeps it so nothing jumps. */
  side: 'above' | 'below' | null;
}

export interface IAnnotatePlaced {
  side: 'above' | 'below';
  pillLeft: number;
  cardLeft: number;
  /** The edge next to the text: the box's bottom edge when it is above, its top edge when under. */
  y: number;
}

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), Math.max(min, max));

const leftOf = (x: number, width: number, viewportW: number) =>
  clamp(Math.round(x - width / 2), ANNOTATE_MARGIN, viewportW - width - ANNOTATE_MARGIN);

export const chooseSide = (
  { top, bottom }: Pick<IAnnotateSpot, 'top' | 'bottom'>,
  viewportH: number,
  preferBelow: boolean,
): 'above' | 'below' => {
  const room = ANNOTATE_CARD_ROOM + ANNOTATE_GAP + ANNOTATE_MARGIN;
  const over = top;
  const under = viewportH - bottom;
  if (preferBelow && under >= room) return 'below';
  if (over >= room) return 'above';
  return under > over ? 'below' : 'above';
};

export const placeAnnotate = (input: IAnnotatePlaceInput): IAnnotatePlaced => {
  const side = input.side ?? chooseSide(input, input.viewportH, input.preferBelow);
  const height = input.expanded ? input.cardH : input.pillH;
  const edge =
    side === 'above'
      ? Math.max(input.top - ANNOTATE_GAP, height + ANNOTATE_MARGIN)
      : Math.min(input.bottom + ANNOTATE_GAP, input.viewportH - height - ANNOTATE_MARGIN);
  return {
    side,
    pillLeft: leftOf(input.x, input.pillW, input.viewportW),
    cardLeft: leftOf(input.x, input.cardW, input.viewportW),
    y: Math.round(edge),
  };
};
