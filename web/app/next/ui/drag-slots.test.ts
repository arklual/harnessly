import { describe, expect, it } from 'vitest';
import { clampTo, shiftsOf, slotOf } from '@/next/ui/drag-slots';

// Three rows of 30 with a gap of 2, and the same with a tall group (50) first.
const EVEN = [
  { start: 0, end: 30 },
  { start: 32, end: 62 },
  { start: 64, end: 94 },
];
const UNEVEN = [
  { start: 0, end: 50 },
  { start: 52, end: 82 },
  { start: 84, end: 114 },
];

describe('slotOf: the place a carried row takes', () => {
  it('stays put until its middle reaches a neighbour’s middle', () => {
    expect(slotOf(EVEN, 0, 15)).toBe(0);
    expect(slotOf(EVEN, 0, 46)).toBe(0);
    expect(slotOf(EVEN, 0, 47)).toBe(1);
    expect(slotOf(EVEN, 0, 79)).toBe(2);
    expect(slotOf(EVEN, 0, 500)).toBe(2);
  });
  it('goes up the same way', () => {
    expect(slotOf(EVEN, 2, 48)).toBe(2);
    expect(slotOf(EVEN, 2, 47)).toBe(1);
    expect(slotOf(EVEN, 2, 15)).toBe(0);
    expect(slotOf(EVEN, 2, -100)).toBe(0);
  });
});

describe('shiftsOf: who steps aside and where the carried row settles', () => {
  it('moves the rows between by the carried row’s room, and settles it in the last one’s place', () => {
    expect(shiftsOf(EVEN, 0, 2)).toEqual([64, -32, -32]);
    expect(shiftsOf(EVEN, 2, 0)).toEqual([32, 32, -64]);
    expect(shiftsOf(EVEN, 1, 1)).toEqual([0, 0, 0]);
  });
  it('keeps the gaps when the rows are of different heights', () => {
    // The tall group goes last: the others close up by its 50 and the gap; it ends where the last one ended.
    expect(shiftsOf(UNEVEN, 0, 2)).toEqual([64, -52, -52]);
    // The last row goes first: the others step down by its 30 and the gap; it starts where the first one started.
    expect(shiftsOf(UNEVEN, 2, 0)).toEqual([32, 32, -84]);
  });
});

describe('clampTo', () => {
  it('keeps a value inside the range, and leaves it when the range cannot hold the row', () => {
    expect(clampTo(-5, 0, 100)).toBe(0);
    expect(clampTo(150, 0, 100)).toBe(100);
    expect(clampTo(50, 0, 100)).toBe(50);
    expect(clampTo(50, 0, -10)).toBe(50);
  });
});
