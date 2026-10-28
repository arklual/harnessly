import { describe, expect, it } from 'vitest';
import {
  deckIndexOf,
  deckPercent,
  deckRound,
  deckScale,
  deckShift,
  deckSnap,
  deckStep,
} from '@/next/features/designly/deck/layout';

describe('deck layout', () => {
  it('scales a board into the slot', () => {
    expect(deckScale(960, 1920)).toBe(0.5);
    expect(deckScale(0, 1920)).toBe(1);
    expect(deckScale(960, 0)).toBe(1);
  });

  it('turns a point into percent of the slide, clamped', () => {
    expect(deckPercent(480, 270, 1920, 1080)).toEqual([25, 25]);
    expect(deckPercent(-10, 2000, 1920, 1080)).toEqual([0, 100]);
    expect(deckPercent(10, 10, 0, 0)).toEqual([0, 0]);
  });

  it('snaps a block to the vertical centre line', () => {
    // A 200 px block at x 850: its centre at 950, 10 px short of the centre line of a 1920 board.
    const snap = deckSnap([850, 100, 200, 100], 6, 0, 1920, 1080, 12);
    expect(snap.dx).toBe(10);
    expect(snap.guides).toEqual([{ v: 1, at: 960 }]);
  });

  it('snaps the left edge to the margin when the centre is far', () => {
    // The margin of a 1920 board is 134.4; the block's left edge at 130 after the move.
    const snap = deckSnap([100, 100, 200, 100], 30, 0, 1920, 1080, 12);
    expect(snap.dx).toBeCloseTo(34.4);
    expect(snap.guides).toEqual([{ v: 1, at: 1920 * 0.07 }]);
  });

  it('snaps the middle to the horizontal centre line', () => {
    const snap = deckSnap([300, 480, 200, 100], 0, 5, 1920, 1080, 12);
    expect(snap.dy).toBe(10);
    expect(snap.guides).toEqual([{ v: 0, at: 540 }]);
  });

  it('leaves a move far from every guide alone', () => {
    expect(deckSnap([300, 100, 200, 100], 40, 40, 1920, 1080, 12)).toEqual({ dx: 40, dy: 40, guides: [] });
  });

  it('shifts and rounds', () => {
    expect(deckShift([1, 2, 3, 4], 10, 20)).toEqual([11, 22, 3, 4]);
    expect(deckRound(12.345)).toBe(12.3);
    expect(deckRound(-0.06)).toBe(-0.1);
  });

  it('finds the slide of a path and steps inside the deck', () => {
    const boards = [{ path: 'a.dc.html' }, { path: 'b.dc.html' }, { path: 'c.dc.html' }];
    expect(deckIndexOf(boards, 'b.dc.html')).toBe(1);
    expect(deckIndexOf(boards, 'x.dc.html')).toBe(0);
    expect(deckIndexOf(boards, null)).toBe(0);
    expect(deckStep(1, 1, 3)).toBe(2);
    expect(deckStep(2, 1, 3)).toBeNull();
    expect(deckStep(0, -1, 3)).toBeNull();
  });
});
