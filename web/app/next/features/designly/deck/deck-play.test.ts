import { describe, expect, it } from 'vitest';
import { nextMove, prevMove } from './deck-play';

describe('nextMove', () => {
  it('shows the steps of a slide before the next slide', () => {
    expect(nextMove(0, 5, 0, 2)).toEqual({ to: 'step', step: 1 });
    expect(nextMove(0, 5, 2, 2)).toEqual({ to: 'slide', index: 1 });
    expect(nextMove(2, 5, 0, 0)).toEqual({ to: 'slide', index: 3 });
  });
  it('stops at the end', () => {
    expect(nextMove(4, 5, 0, 0)).toBeNull();
    expect(nextMove(4, 5, 1, 1)).toBeNull();
  });
});

describe('prevMove', () => {
  it('hides the steps before going to the previous slide', () => {
    expect(prevMove(2, 2)).toEqual({ to: 'step', step: 1 });
    expect(prevMove(2, 0)).toEqual({ to: 'slide', index: 1 });
  });
  it('stops at the start', () => {
    expect(prevMove(0, 0)).toBeNull();
  });
});
