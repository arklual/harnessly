import { describe, expect, it } from 'vitest';
import { placeCard, SIDE } from '@/next/features/tour/model';

const VIEW = { w: 1440, h: 900 };
const CARD = { w: 300, h: 160 };

const covers = (p: { x: number; y: number }, r: { x: number; y: number; w: number; h: number }) =>
  p.x < r.x + r.w && p.x + CARD.w > r.x && p.y < r.y + r.h && p.y + CARD.h > r.y;

describe('placeCard', () => {
  it('stands in the middle without a target', () => {
    expect(placeCard(null, CARD, VIEW)).toEqual({ x: 570, y: 370, side: SIDE.center });
  });

  it('stands right of the target when there is room', () => {
    const target = { x: 20, y: 140, w: 200, h: 32 };
    const p = placeCard(target, CARD, VIEW);
    expect(p.side).toBe(SIDE.right);
    expect(covers(p, target)).toBe(false);
  });

  it('goes left of a target at the right edge', () => {
    const target = { x: 1300, y: 60, w: 120, h: 32 };
    const p = placeCard(target, CARD, VIEW);
    expect(covers(p, target)).toBe(false);
  });

  it('keeps off the menu the step opened', () => {
    const target = { x: 600, y: 100, w: 40, h: 32 };
    const menu = { x: 640, y: 100, w: 320, h: 400 };
    const p = placeCard(target, CARD, VIEW, [menu]);
    expect(covers(p, target)).toBe(false);
    expect(covers(p, menu)).toBe(false);
  });

  it('stays inside the view', () => {
    const target = { x: 0, y: 860, w: 1440, h: 40 };
    const p = placeCard(target, CARD, VIEW);
    expect(p.y + CARD.h).toBeLessThanOrEqual(VIEW.h);
    expect(p.x).toBeGreaterThanOrEqual(0);
  });
});
