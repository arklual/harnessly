import { describe, expect, it } from 'vitest';
import { GESTURE, rubber, settleGesture, type IGestureEnd } from '@/next/features/smart-notice/use-gestures';

const end = (patch: Partial<IGestureEnd>): IGestureEnd => ({
  dx: 0,
  dy: 0,
  vx: 0,
  vy: 0,
  width: 300,
  edge: 'bottom',
  open: false,
  ...patch,
});

describe('settleGesture', () => {
  it('hides a plate swept past a third of its width, or flicked aside', () => {
    expect(settleGesture(end({ dx: 120 }))).toBe(GESTURE.hide);
    expect(settleGesture(end({ dx: -40, vx: -1 }))).toBe(GESTURE.hide);
    expect(settleGesture(end({ dx: 60 }))).toBe(GESTURE.back);
  });

  it('opens a plate pulled away from its edge, and folds it pushed back', () => {
    expect(settleGesture(end({ dy: -50 }))).toBe(GESTURE.open);
    expect(settleGesture(end({ edge: 'top', dy: 50 }))).toBe(GESTURE.open);
    expect(settleGesture(end({ dy: 50 }))).toBe(GESTURE.back);
    expect(settleGesture(end({ open: true, dy: 80 }))).toBe(GESTURE.fold);
    expect(settleGesture(end({ open: true, edge: 'top', dy: -80 }))).toBe(GESTURE.fold);
    expect(settleGesture(end({ open: true, dy: -80 }))).toBe(GESTURE.back);
  });

  it('takes a quick short flick for the whole gesture', () => {
    expect(settleGesture(end({ dy: -14, vy: -0.8 }))).toBe(GESTURE.open);
    expect(settleGesture(end({ open: true, dy: 20, vy: 0.9 }))).toBe(GESTURE.fold);
  });
});

describe('rubber', () => {
  it('goes freely at first, harder later, and hardly where the plate cannot go', () => {
    expect(rubber(30, true)).toBe(30);
    expect(rubber(140, true)).toBeCloseTo(75);
    expect(rubber(-140, true)).toBeCloseTo(-75);
    expect(rubber(50, false)).toBeCloseTo(10);
  });
});
