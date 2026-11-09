import { describe, expect, it } from 'vitest';
import {
  clampFloat,
  firstFloat,
  FLOAT_MARGIN,
  FLOAT_MIN,
  parseLayout,
  PLACEMENT,
  resizeFloat,
  withPlacement,
} from '@/next/features/app-main/placement';

const SECTION = { width: 1200, height: 800 };

describe('parseLayout', () => {
  it('is the column when nothing was kept or what was kept is not a layout', () => {
    const column = { placement: PLACEMENT.left, float: null };
    expect(parseLayout(null)).toEqual(column);
    expect(parseLayout('not json')).toEqual(column);
    expect(parseLayout('{"placement":"right"}')).toEqual(column);
    expect(parseLayout('[]')).toEqual(column);
  });

  it('reads a kept placement and window, and drops a window that is not one', () => {
    expect(parseLayout('{"placement":"island","float":null}')).toEqual({ placement: 'island', float: null });
    expect(parseLayout('{"placement":"float","float":{"x":1,"y":2,"w":300,"h":400}}')).toEqual({
      placement: 'float',
      float: { x: 1, y: 2, w: 300, h: 400 },
    });
    expect(parseLayout('{"placement":"float","float":{"x":"1"}}')).toEqual({
      placement: 'float',
      float: null,
    });
  });
});

describe('clampFloat', () => {
  it('keeps a window that fits as it is', () => {
    const f = { x: 100, y: 100, w: 400, h: 400 };
    expect(clampFloat(f, SECTION)).toEqual(f);
  });

  it('pulls a window back into the section and keeps the minimum size', () => {
    const out = clampFloat({ x: 5000, y: -40, w: 10, h: 10 }, SECTION);
    expect(out.w).toBe(FLOAT_MIN.w);
    expect(out.h).toBe(FLOAT_MIN.h);
    expect(out.x).toBe(SECTION.width - FLOAT_MIN.w - FLOAT_MARGIN);
    expect(out.y).toBe(FLOAT_MARGIN);
  });

  it('never grows past the section', () => {
    const out = clampFloat({ x: 0, y: 0, w: 5000, h: 5000 }, SECTION);
    expect(out.w).toBe(SECTION.width - 2 * FLOAT_MARGIN);
    expect(out.h).toBe(SECTION.height - 2 * FLOAT_MARGIN);
  });
});

describe('withPlacement', () => {
  it('gives the window its first place once and keeps the one it had afterwards', () => {
    const first = withPlacement({ placement: PLACEMENT.left, float: null }, PLACEMENT.float, SECTION);
    expect(first.float).toEqual(clampFloat(firstFloat(SECTION), SECTION));
    const moved = { ...first, float: { x: 20, y: 30, w: 500, h: 500 } };
    const toIsland = withPlacement(moved, PLACEMENT.island, SECTION);
    expect(toIsland).toEqual({ placement: PLACEMENT.island, float: moved.float });
    expect(withPlacement(toIsland, PLACEMENT.float, SECTION).float).toEqual(moved.float);
  });

  it('does not make a window for the column', () => {
    expect(
      withPlacement({ placement: PLACEMENT.float, float: null }, PLACEMENT.left, SECTION).float,
    ).toBeNull();
  });
});

describe('resizeFloat', () => {
  const section = { width: 1200, height: 800 };
  const f = { x: 400, y: 100, w: 400, h: 400 };
  it('grows by the right and the foot, keeping the other edges', () => {
    expect(resizeFloat(f, 'se', 50, 30, section)).toEqual({ x: 400, y: 100, w: 450, h: 430 });
  });
  it('grows by the left and the top, the right and the foot staying where they were', () => {
    expect(resizeFloat(f, 'nw', -50, -40, section)).toEqual({ x: 350, y: 60, w: 450, h: 440 });
  });
  it('stops at the minimum size and at the section', () => {
    expect(resizeFloat(f, 'w', 300, 0, section)).toEqual({ x: 500, y: 100, w: 300, h: 400 });
    expect(resizeFloat(f, 'n', 0, -500, section)).toEqual({ x: 400, y: 12, w: 400, h: 488 });
    expect(resizeFloat(f, 'e', 2000, 0, section)).toEqual({ x: 400, y: 100, w: 788, h: 400 });
  });
});
