import { describe, expect, it } from 'vitest';
import {
  GESTURE,
  flingMs,
  rubber,
  settleGesture,
  type IGestureEnd,
} from '@/next/features/smart-notice/use-gestures';

const end = (patch: Partial<IGestureEnd>): IGestureEnd => ({
  dx: 0,
  dy: 0,
  vx: 0,
  vy: 0,
  width: 300,
  edge: 'bottom',
  open: false,
  stacked: false,
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
    expect(settleGesture(end({ dy: 20 }))).toBe(GESTURE.back);
    expect(settleGesture(end({ open: true, dy: 80 }))).toBe(GESTURE.fold);
    expect(settleGesture(end({ open: true, edge: 'top', dy: -80 }))).toBe(GESTURE.fold);
    expect(settleGesture(end({ open: true, dy: -80 }))).toBe(GESTURE.back);
  });

  it('takes a quick short flick for the whole gesture', () => {
    expect(settleGesture(end({ dy: -14, vy: -0.8 }))).toBe(GESTURE.open);
    expect(settleGesture(end({ open: true, dy: 20, vy: 0.9 }))).toBe(GESTURE.fold);
  });
});

describe('settleGesture on a stack', () => {
  it('pages a folded stack up and down instead of opening it', () => {
    expect(settleGesture(end({ stacked: true, dy: -40 }))).toBe(GESTURE.next);
    expect(settleGesture(end({ stacked: true, dy: 40 }))).toBe(GESTURE.prev);
    expect(settleGesture(end({ stacked: true, edge: 'top', dy: 40 }))).toBe(GESTURE.next);
    expect(settleGesture(end({ stacked: true, dy: -12, vy: -0.8 }))).toBe(GESTURE.next);
    expect(settleGesture(end({ stacked: true, dy: -10 }))).toBe(GESTURE.back);
  });

  it('folds an open card even with a stack behind it, and sweeps only sideways', () => {
    expect(settleGesture(end({ stacked: true, open: true, dy: 80 }))).toBe(GESTURE.fold);
    expect(settleGesture(end({ stacked: true, dx: 140 }))).toBe(GESTURE.hide);
  });

  it('folds one announcement into the line with a pull to its edge, never hides it', () => {
    expect(settleGesture(end({ dy: 60 }))).toBe(GESTURE.fold);
    expect(settleGesture(end({ edge: 'top', dy: -60 }))).toBe(GESTURE.fold);
  });
});

describe('flingMs', () => {
  it('throws faster for a faster finger, within bounds', () => {
    expect(flingMs(600, 3)).toBeLessThan(flingMs(600, 1));
    expect(flingMs(600, 50)).toBe(160);
    expect(flingMs(2000, 0)).toBe(380);
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
