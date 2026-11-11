import { describe, expect, it } from 'vitest';
import {
  isWheelNotch,
  pageSize,
  pageTarget,
  springDone,
  springStep,
  stackLook,
  withinOne,
  type ISpring,
} from '@/next/features/smart-notice/pager';

describe('stackLook', () => {
  it('draws the stack at rest as the stylesheet does', () => {
    const front = stackLook(0, 40, 64);
    expect(front).toMatchObject({ toEdge: 0, scale: 1, opacity: 1, tint: 0, content: 1 });
    const first = stackLook(1, 40, 64);
    expect(first.toEdge).toBeCloseTo(-(40 * 0.07 + 9));
    expect(first.scale).toBeCloseTo(0.93);
    expect(first.tint).toBeCloseTo(0.172);
    expect(stackLook(2, 40, 64).toEdge).toBeCloseTo(-(40 * 0.14 + 17));
    expect(stackLook(3, 40, 64).opacity).toBe(0);
  });

  it('carries the front plate with the finger, one to one, and fades it', () => {
    const half = stackLook(-0.5, 40, 64);
    expect(half.toEdge).toBe(32);
    expect(half.opacity).toBeCloseTo(0.5);
    expect(stackLook(-1, 40, 64).opacity).toBe(0);
  });

  it('changes continuously between the places, never by a jump', () => {
    for (let d = -1; d < 3; d += 0.01) {
      const a = stackLook(d, 40, 64);
      const b = stackLook(d + 0.01, 40, 64);
      expect(Math.abs(a.toEdge - b.toEdge)).toBeLessThan(1);
      expect(Math.abs(a.scale - b.scale)).toBeLessThan(0.01);
      expect(Math.abs(a.opacity - b.opacity)).toBeLessThan(0.02);
      expect(Math.abs(a.tint - b.tint)).toBeLessThan(0.01);
    }
  });
});

describe('pageTarget', () => {
  it('goes to the nearest plate when let go slowly', () => {
    expect(pageTarget(0.3, 0)).toBe(0);
    expect(pageTarget(0.6, 0)).toBe(1);
    expect(pageTarget(-0.6, 0)).toBe(-1);
  });

  it('is carried on by a flick, at most one plate', () => {
    expect(pageTarget(0.2, 4)).toBe(1);
    expect(pageTarget(-0.2, -4)).toBe(-1);
    expect(pageTarget(0.2, 40)).toBe(1);
  });
});

describe('springStep', () => {
  it('comes to rest at the target, from any speed', () => {
    let s: ISpring = { x: 0.4, v: 6 };
    for (let i = 0; i < 120 && !springDone(s, 1); i++) s = springStep(s, 1, 1 / 60);
    expect(springDone(s, 1)).toBe(true);
  });

  it('keeps the speed it was let go with at first', () => {
    const s = springStep({ x: 0, v: 5 }, 0, 1 / 60);
    expect(s.x).toBeGreaterThan(0);
  });
});

describe('pageSize and isWheelNotch', () => {
  it('is about the plate height, within bounds', () => {
    expect(pageSize(40)).toBe(64);
    expect(pageSize(10)).toBe(56);
    expect(pageSize(400)).toBe(96);
  });

  it('tells a mouse wheel click from a trackpad', () => {
    expect(isWheelNotch({ deltaMode: 0, deltaY: 100 })).toBe(true);
    expect(isWheelNotch({ deltaMode: 1, deltaY: 3 })).toBe(true);
    expect(isWheelNotch({ deltaMode: 0, deltaY: 4 })).toBe(false);
    expect(isWheelNotch({ deltaMode: 0, deltaY: 12.5 })).toBe(false);
  });
});

describe('withinOne', () => {
  it('lets one gesture move the stack by one plate at most, either way', () => {
    expect(withinOne(0.4, 0)).toBe(0.4);
    expect(withinOne(3.2, 0)).toBe(1);
    expect(withinOne(-5, 0)).toBe(-1);
    // After the stack paged (its position moved back by one), the same gesture cannot go on to the next plate.
    expect(withinOne(0.6, -1)).toBe(0);
  });
});
