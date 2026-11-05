import { describe, expect, it } from 'vitest';
import {
  besideBox,
  BESIDE_GAP,
  clampPill,
  parsePill,
  PILL_AXIS,
  PILL_DEFAULT,
  PILL_EDGE,
} from '@/next/features/designly/zoom-pill-pos';

describe('the zoom pill’s place', () => {
  const area = { w: 1000, h: 600 };
  const pill = { w: 200, h: 36 };

  it('stays where it was put inside the canvas', () => {
    expect(clampPill({ right: 300, bottom: 200 }, area, pill)).toEqual({ right: 300, bottom: 200 });
  });

  it('is put back inside a canvas it left (a narrower window)', () => {
    expect(clampPill({ right: 900, bottom: 700 }, area, pill)).toEqual({
      right: area.w - pill.w - PILL_EDGE,
      bottom: area.h - pill.h - PILL_EDGE,
    });
    expect(clampPill({ right: -20, bottom: 0 }, area, pill)).toEqual({ right: PILL_EDGE, bottom: PILL_EDGE });
  });

  it('keeps by its corner in a canvas narrower than itself', () => {
    expect(clampPill({ right: 50, bottom: 50 }, { w: 100, h: 20 }, pill)).toEqual({
      right: PILL_EDGE,
      bottom: PILL_EDGE,
    });
  });

  it('reads what was kept, and nothing broken', () => {
    expect(parsePill({ pos: { right: 10, bottom: 20 }, hidden: true })).toEqual({
      pos: { right: 10, bottom: 20 },
      hidden: true,
    });
    expect(parsePill({ pos: { right: 'x' } })).toEqual(PILL_DEFAULT);
    expect(parsePill(null)).toEqual(PILL_DEFAULT);
  });
});

describe('the phone’s pill by the phone', () => {
  const area = { w: 1200, h: 800 };
  const col = { w: 40, h: 180 };

  it('stands on the phone’s right, level with its top', () => {
    const at = besideBox(area, { left: 400, top: 60, right: 800 }, col);
    expect(at.axis).toBe(PILL_AXIS.col);
    // Its left edge is the gap past the phone, its top the phone's top.
    expect(area.w - at.pos.right - col.w).toBe(800 + BESIDE_GAP);
    expect(area.h - at.pos.bottom - col.h).toBe(60);
  });

  it('lies over the phone along its top when the right has no room', () => {
    const at = besideBox(area, { left: 20, top: 100, right: 1180 }, col);
    expect(at.axis).toBe(PILL_AXIS.row);
    expect(at.pos).toEqual({ right: 20, bottom: area.h - 100 + BESIDE_GAP });
  });
});
