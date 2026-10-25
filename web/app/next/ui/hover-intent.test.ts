import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  convexHull,
  createHoverIntent,
  createWarmth,
  distanceToRect,
  fitInViewport,
  gapRect,
  HOVER,
  HOVER_PART,
  HOVER_STATE,
  inZone,
  movingToward,
  placeFloating,
  pointInPolygon,
  safeZone,
  SIDE,
  type IHoverRects,
  type IRect,
} from '@/next/ui/hover-intent';

const rect = (left: number, top: number, right: number, bottom: number): IRect => ({
  left,
  top,
  right,
  bottom,
});

// A chip at the bottom, its card above it with a 10 px gap (the composer's limits).
const TRIGGER = rect(100, 500, 180, 532);
const CARD = rect(90, 200, 390, 490);

describe('geometry', () => {
  it('tells a point inside a convex polygon of either winding, edges included', () => {
    const square = [
      { x: 0, y: 0 },
      { x: 10, y: 0 },
      { x: 10, y: 10 },
      { x: 0, y: 10 },
    ];
    expect(pointInPolygon({ x: 5, y: 5 }, square)).toBe(true);
    expect(pointInPolygon({ x: 5, y: 5 }, [...square].reverse())).toBe(true);
    expect(pointInPolygon({ x: 10, y: 5 }, square)).toBe(true);
    expect(pointInPolygon({ x: 11, y: 5 }, square)).toBe(false);
    expect(pointInPolygon({ x: 11, y: 5 }, square, 1.5)).toBe(true);
    expect(pointInPolygon({ x: 1, y: 1 }, square.slice(0, 2))).toBe(false);
  });

  it('builds the hull of the pointer and the far rect', () => {
    const hull = convexHull([
      { x: 50, y: 100 },
      { x: 0, y: 0 },
      { x: 100, y: 0 },
      { x: 100, y: 20 },
      { x: 0, y: 20 },
    ]);
    expect(hull).toHaveLength(5);
    // An inner point is dropped.
    expect(
      convexHull([
        { x: 0, y: 0 },
        { x: 10, y: 0 },
        { x: 5, y: 5 },
        { x: 5, y: 1 },
      ]),
    ).toHaveLength(3);
  });

  it('finds the gap between two rects apart on one axis, and none for overlapping ones', () => {
    expect(gapRect(TRIGGER, CARD)).toEqual(rect(100, 490, 180, 500));
    expect(gapRect(CARD, TRIGGER)).toEqual(rect(100, 490, 180, 500));
    expect(gapRect(rect(0, 0, 10, 10), rect(20, 2, 40, 8))).toEqual(rect(10, 2, 20, 8));
    expect(gapRect(rect(0, 0, 10, 10), rect(5, 5, 15, 15))).toBeNull();
  });

  it('lays the safe triangle from where the pointer left to the card', () => {
    const zones = safeZone({ x: 170, y: 500 }, TRIGGER, CARD);
    // Up toward the card's near edge, and diagonally toward its far corner: safe.
    expect(inZone({ x: 170, y: 495 }, zones)).toBe(true);
    expect(inZone({ x: 300, y: 470 }, zones)).toBe(true);
    // Beside the chip, away from the card: out.
    expect(inZone({ x: 400, y: 498 }, zones)).toBe(false);
    expect(inZone({ x: 60, y: 520 }, zones)).toBe(false);
  });

  it('measures heading: closer (or about as close) counts, farther does not', () => {
    expect(distanceToRect({ x: 100, y: 495 }, CARD)).toBe(5);
    expect(distanceToRect({ x: 200, y: 300 }, CARD)).toBe(0);
    expect(movingToward({ x: 170, y: 500 }, { x: 175, y: 495 }, CARD)).toBe(true);
    expect(movingToward({ x: 170, y: 495 }, { x: 171, y: 495.5 }, CARD)).toBe(true);
    expect(movingToward({ x: 170, y: 495 }, { x: 170, y: 505 }, CARD)).toBe(false);
  });
});

describe('placeFloating', () => {
  const viewport = { width: 1000, height: 800 };
  const size = { width: 120, height: 28 };

  it('prefers the top, centred on the anchor', () => {
    const at = placeFloating({ anchor: rect(400, 300, 440, 330), size, viewport });
    expect(at).toEqual({ x: 360, y: 262, side: SIDE.top });
  });

  it('flips below when the top has no room, and never covers the anchor', () => {
    const at = placeFloating({ anchor: rect(400, 10, 440, 40), size, viewport });
    expect(at.side).toBe(SIDE.bottom);
    expect(at.y).toBeGreaterThanOrEqual(40);
  });

  it('keeps the preferred bottom when it fits, and slides inside the window sideways', () => {
    const at = placeFloating({ anchor: rect(960, 300, 990, 330), size, viewport, side: SIDE.bottom });
    expect(at.side).toBe(SIDE.bottom);
    expect(at.x).toBe(1000 - 120 - 8);
  });

  it('tells how far a card must move and shrink to stay inside the window', () => {
    expect(fitInViewport(rect(-20, 100, 268, 300), { width: 1000, height: 800 })).toEqual({
      dx: 28,
      maxHeight: null,
    });
    expect(fitInViewport(rect(800, -40, 1100, 300), { width: 1000, height: 800 })).toEqual({
      dx: -108,
      maxHeight: 292,
    });
  });
});

describe('createHoverIntent', () => {
  let changes: boolean[];
  let rects: IHoverRects;
  const make = (over: Partial<Parameters<typeof createHoverIntent>[0]> = {}) =>
    createHoverIntent({ onChange: (open) => changes.push(open), rects: () => rects, ...over });

  beforeEach(() => {
    vi.useFakeTimers();
    changes = [];
    rects = { trigger: TRIGGER, floating: CARD };
  });
  afterEach(() => vi.useRealTimers());

  it('opens after the delay on hover, and not if the pointer passes by', () => {
    const intent = make();
    intent.enter(HOVER_PART.trigger);
    expect(intent.state).toBe(HOVER_STATE.opening);
    vi.advanceTimersByTime(HOVER.openDelay - 1);
    intent.leave(HOVER_PART.trigger, { x: 90, y: 520 });
    vi.advanceTimersByTime(1000);
    expect(changes).toEqual([]);

    intent.enter(HOVER_PART.trigger);
    vi.advanceTimersByTime(HOVER.openDelay);
    expect(changes).toEqual([true]);
  });

  it('stays while the pointer heads for the card, and the card keeps it', () => {
    const intent = make({ openDelay: 0 });
    intent.enter(HOVER_PART.trigger);
    intent.leave(HOVER_PART.trigger, { x: 170, y: 500 });
    expect(intent.state).toBe(HOVER_STATE.leaving);
    intent.move({ x: 180, y: 496 });
    vi.advanceTimersByTime(HOVER.restMs - 10);
    intent.move({ x: 190, y: 492 });
    vi.advanceTimersByTime(HOVER.restMs - 10);
    intent.enter(HOVER_PART.floating);
    vi.advanceTimersByTime(5000);
    expect(intent.open).toBe(true);
    expect(changes).toEqual([true]);
  });

  it('closes when the pointer turns away or leaves the zone', () => {
    const intent = make({ openDelay: 0 });
    intent.enter(HOVER_PART.trigger);
    intent.leave(HOVER_PART.trigger, { x: 170, y: 500 });
    intent.move({ x: 172, y: 497 });
    intent.move({ x: 172, y: 512 });
    expect(changes).toEqual([true, false]);

    intent.enter(HOVER_PART.trigger);
    intent.leave(HOVER_PART.trigger, { x: 180, y: 520 });
    intent.move({ x: 230, y: 522 });
    expect(changes).toEqual([true, false, true, false]);
  });

  it('closes when the pointer rests outside both, and as a fallback with no move at all', () => {
    const intent = make({ openDelay: 0 });
    intent.enter(HOVER_PART.trigger);
    intent.leave(HOVER_PART.trigger, { x: 170, y: 500 });
    intent.move({ x: 172, y: 497 });
    vi.advanceTimersByTime(HOVER.restMs);
    expect(changes).toEqual([true, false]);

    intent.enter(HOVER_PART.trigger);
    intent.leave(HOVER_PART.trigger, { x: 170, y: 500 });
    vi.advanceTimersByTime(HOVER.closeDelay);
    expect(changes).toEqual([true, false, true, false]);
  });

  it('heads for a kept element (a menu, a modal) as well as the card', () => {
    rects = { trigger: TRIGGER, floating: CARD, kept: [rect(600, 500, 800, 700)] };
    const intent = make({ openDelay: 0 });
    intent.enter(HOVER_PART.trigger);
    intent.leave(HOVER_PART.trigger, { x: 180, y: 520 });
    intent.move({ x: 230, y: 522 });
    expect(intent.open).toBe(true);
    intent.move({ x: 260, y: 524 }, true);
    vi.advanceTimersByTime(HOVER.restMs - 1);
    expect(intent.open).toBe(true);
  });

  it('closes a hint as soon as the pointer leaves', () => {
    const intent = make({ interactive: false, openDelay: HOVER.tipDelay });
    intent.enter(HOVER_PART.trigger);
    vi.advanceTimersByTime(HOVER.tipDelay);
    intent.leave(HOVER_PART.trigger, { x: 170, y: 500 });
    expect(changes).toEqual([true, false]);
  });

  it('pins on click: the pointer leaving keeps it, a second click or Esc lets it go', () => {
    const intent = make();
    intent.toggle();
    expect(intent.pinned).toBe(true);
    intent.leave(HOVER_PART.trigger, { x: 900, y: 900 });
    intent.move({ x: 950, y: 950 });
    vi.advanceTimersByTime(5000);
    expect(intent.open).toBe(true);
    intent.toggle();
    expect(changes).toEqual([true, false]);

    intent.toggle();
    intent.close();
    expect(changes).toEqual([true, false, true, false]);
  });

  it('opens at once on focus, keeps while focused, and goes on blur', () => {
    const intent = make();
    intent.focus();
    expect(changes).toEqual([true]);
    intent.leave(HOVER_PART.trigger, { x: 900, y: 900 });
    vi.advanceTimersByTime(5000);
    expect(intent.open).toBe(true);
    intent.blur();
    expect(changes).toEqual([true, false]);
  });

  it('stays while a nested card holds it, and goes after the grace once released', () => {
    const intent = make({ openDelay: 0 });
    intent.enter(HOVER_PART.trigger);
    intent.hold();
    intent.leave(HOVER_PART.trigger, { x: 900, y: 900 });
    intent.move({ x: 950, y: 950 });
    vi.advanceTimersByTime(5000);
    expect(intent.open).toBe(true);
    intent.release();
    vi.advanceTimersByTime(HOVER.closeDelay - 1);
    expect(intent.open).toBe(true);
    vi.advanceTimersByTime(1);
    expect(changes).toEqual([true, false]);
  });

  it('skips the delay while its group is warm', () => {
    const warmth = createWarmth(HOVER.warmMs);
    const first = make({ interactive: false, openDelay: HOVER.tipDelay, warmth });
    const second = make({ interactive: false, openDelay: HOVER.tipDelay, warmth });
    first.enter(HOVER_PART.trigger);
    vi.advanceTimersByTime(HOVER.tipDelay);
    first.leave(HOVER_PART.trigger, { x: 0, y: 0 });
    vi.advanceTimersByTime(HOVER.warmMs - 50);
    second.enter(HOVER_PART.trigger);
    expect(second.open).toBe(true);
    second.leave(HOVER_PART.trigger, { x: 0, y: 0 });
    vi.advanceTimersByTime(HOVER.warmMs + 1);
    first.enter(HOVER_PART.trigger);
    expect(first.state).toBe(HOVER_STATE.opening);
  });
});
