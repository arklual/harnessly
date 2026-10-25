import { describe, expect, it } from 'vitest';
import {
  ANNOTATE_MARGIN,
  chooseSide,
  placeAnnotate,
  type IAnnotatePlaceInput,
} from '@/next/ui/annotate/place';

const base: IAnnotatePlaceInput = {
  x: 500,
  top: 400,
  bottom: 420,
  viewportW: 1000,
  viewportH: 800,
  pillW: 130,
  cardW: 340,
  cardH: 150,
  pillH: 36,
  expanded: false,
  preferBelow: false,
  side: null,
};

describe('chooseSide', () => {
  it('is above the text when there is room for the card there', () => {
    expect(chooseSide(base, 800, false)).toBe('above');
  });

  it('is under the text when the top of the screen is too near', () => {
    expect(chooseSide({ top: 60, bottom: 80 }, 800, false)).toBe('below');
  });

  it('is under the text on a touch screen when there is room', () => {
    expect(chooseSide(base, 800, true)).toBe('below');
  });

  it('takes the roomier side when neither fits', () => {
    expect(chooseSide({ top: 120, bottom: 200 }, 300, false)).toBe('above');
    expect(chooseSide({ top: 30, bottom: 60 }, 300, false)).toBe('below');
  });
});

describe('placeAnnotate', () => {
  it('stands over the text, centred on it', () => {
    const placed = placeAnnotate(base);
    expect(placed.side).toBe('above');
    expect(placed.y).toBe(392);
    expect(placed.pillLeft).toBe(435);
    expect(placed.cardLeft).toBe(330);
  });

  it('puts the box under the text, with a gap', () => {
    expect(placeAnnotate({ ...base, side: 'below' }).y).toBe(428);
  });

  it('does not leave the screen sideways', () => {
    expect(placeAnnotate({ ...base, x: 4 }).cardLeft).toBe(ANNOTATE_MARGIN);
    expect(placeAnnotate({ ...base, x: 998 }).cardLeft).toBe(1000 - 340 - ANNOTATE_MARGIN);
  });

  it('keeps the card inside the screen when the text is near the edge it opens to', () => {
    const above = placeAnnotate({ ...base, top: 90, expanded: true, side: 'above' });
    expect(above.y).toBe(150 + ANNOTATE_MARGIN);
    const below = placeAnnotate({ ...base, top: 700, bottom: 790, expanded: true, side: 'below' });
    expect(below.y).toBe(800 - 150 - ANNOTATE_MARGIN);
  });

  it('keeps the side it was given', () => {
    expect(placeAnnotate({ ...base, top: 40, bottom: 60, side: 'above' }).side).toBe('above');
  });
});
