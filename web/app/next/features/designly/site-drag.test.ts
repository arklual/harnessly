import { describe, expect, it } from 'vitest';
import {
  boxShift,
  chatWidthAt,
  DEV_H,
  devHeightAt,
  floatDropAt,
  floatGrown,
  floatMoved,
  pillShift,
  tweaksMovedBy,
  viewportDragged,
  zoneAt,
  type IGesture,
} from '@/next/features/designly/site-drag';

const RECT = { left: 100, top: 50, width: 1000, height: 700 };
const gesture = (kind: IGesture['kind'], dock: IGesture['dock'] = 'left'): IGesture => ({
  kind,
  dock,
  rect: RECT,
  from: { x: 600, y: 600 },
});

describe('zoneAt', () => {
  it('a press that has barely moved is no zone', () => {
    expect(zoneAt(gesture('island'), { x: 610, y: 620 })).toBeNull();
  });

  it('the field dragged to an edge docks the chat there', () => {
    expect(zoneAt(gesture('island'), { x: 150, y: 600 })).toBe('left');
    expect(zoneAt(gesture('island'), { x: 1080, y: 600 })).toBe('right');
  });

  it('the field pulled up opens a window, pulled down folds', () => {
    expect(zoneAt(gesture('island'), { x: 600, y: 500 })).toBe('float');
    expect(zoneAt(gesture('island'), { x: 600, y: 660 })).toBe('min');
    expect(zoneAt(gesture('island'), { x: 600, y: 580 })).toBeNull();
  });

  it('a docked chat does not offer its own side, but offers a window and the field', () => {
    expect(zoneAt(gesture('dock', 'left'), { x: 120, y: 300 })).toBeNull();
    expect(zoneAt(gesture('dock', 'left'), { x: 1090, y: 300 })).toBe('right');
    expect(zoneAt(gesture('dock', 'left'), { x: 600, y: 300 })).toBe('float');
    expect(zoneAt(gesture('dock', 'right'), { x: 600, y: 700 })).toBe('island');
  });

  it('a window dropped at the bottom becomes the field, at an edge docks', () => {
    expect(zoneAt(gesture('float'), { x: 600, y: 730 })).toBe('island');
    expect(zoneAt(gesture('float'), { x: 130, y: 300 })).toBe('left');
    expect(zoneAt(gesture('float'), { x: 600, y: 300 })).toBeNull();
  });
});

describe('the window', () => {
  const f = { x: 100, y: 100, w: 360, h: 400 };

  it('a drop opens the window with its head under the pointer, inside the section', () => {
    expect(floatDropAt(RECT, { x: 600, y: 300 })).toEqual({ x: 320, y: 128, w: 360, h: 560 });
    expect(floatDropAt(RECT, { x: 100, y: 50 })).toMatchObject({ x: 12, y: 12 });
    expect(floatDropAt(RECT, { x: 1100, y: 750 })).toMatchObject({ x: 628, y: 128 });
  });

  it('moves with the pointer but not out of the section on any axis', () => {
    expect(floatMoved(f, RECT, 50, 20)).toEqual({ ...f, x: 150, y: 120 });
    expect(floatMoved(f, RECT, -500, -500)).toEqual({ ...f, x: 0, y: 0 });
    expect(floatMoved(f, RECT, 5000, 5000)).toEqual({ ...f, x: 640, y: 620 });
  });

  it('grows by its corner between the minimum, the cap and the section', () => {
    expect(floatGrown(f, RECT, -500, -500)).toEqual({ ...f, w: 300, h: 280 });
    expect(floatGrown(f, RECT, 900, 100)).toEqual({ ...f, w: 680, h: 500 });
    expect(floatGrown(f, RECT, 100, 900)).toEqual({ ...f, w: 460, h: 588 });
    expect(floatGrown({ ...f, x: 800 }, RECT, 900, 0).w).toBe(300);
  });
});

describe('the chat width', () => {
  it('follows the border to the right for a chat on the left, to the left for the right one', () => {
    expect(chatWidthAt(372, 'left', 50)).toBe(422);
    expect(chatWidthAt(372, 'right', 50)).toBe(322);
  });

  it('stays between 160 and 620', () => {
    expect(chatWidthAt(372, 'left', -900)).toBe(160);
    expect(chatWidthAt(372, 'left', 900)).toBe(620);
  });
});

describe('the field and the pill', () => {
  const box = { left: 300, top: 600, width: 600, height: 60 };

  it('the field keeps 8 px from the section edges', () => {
    expect(boxShift(box, RECT, -900, 0)).toEqual({ x: -292, y: 0 });
    expect(boxShift(box, RECT, 900, 900)).toEqual({ x: 92, y: 32 });
  });

  it('the pill pulls up at half, and down ever heavier', () => {
    expect(pillShift(-40)).toBe(-20);
    expect(pillShift(20)).toBeCloseTo(20 / 2.5, 5);
    expect(pillShift(200)).toBeLessThan(200 / 2.5);
  });
});

describe('viewportDragged', () => {
  const start = { w: 800, h: 600 };

  it('a handle moves a side by twice the pointer, at the scale of the drawing', () => {
    expect(viewportDragged('e', start, 1, 50, 99)).toEqual({ w: 900, h: 600 });
    expect(viewportDragged('s', start, 0.5, 99, 50)).toEqual({ w: 800, h: 800 });
    expect(viewportDragged('se', start, 1, 10, 10)).toEqual({ w: 820, h: 620 });
  });

  it('stays between 240 and 3840', () => {
    expect(viewportDragged('se', start, 1, -2000, 9000)).toEqual({ w: 240, h: 3840 });
  });
});

describe('devHeightAt', () => {
  it('pulled up it grows, kept between the bounds', () => {
    expect(devHeightAt(220, -80)).toBe(300);
    expect(devHeightAt(220, 100)).toBe(120);
    expect(devHeightAt(220, 9000)).toBe(DEV_H.min);
    expect(devHeightAt(220, -9000)).toBe(DEV_H.max);
  });
});

describe('tweaksMovedBy', () => {
  const box = { x: 700, y: 100, w: 288, h: 400, bw: 1000, bh: 800 };

  it('follows the pointer', () => {
    expect(tweaksMovedBy(box, -100, 50)).toEqual({ x: 600, y: 150 });
  });

  it('keeps 8 px of the browser free on every side', () => {
    expect(tweaksMovedBy(box, 500, 900)).toEqual({ x: 1000 - 288 - 8, y: 800 - 400 - 8 });
    expect(tweaksMovedBy(box, -900, -900)).toEqual({ x: 8, y: 8 });
  });

  it('a panel taller than the browser sits at the top margin', () => {
    expect(tweaksMovedBy({ ...box, h: 900 }, 0, 200)).toEqual({ x: 700, y: 8 });
  });
});
