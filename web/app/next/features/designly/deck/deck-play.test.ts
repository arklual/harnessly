import { describe, expect, it } from 'vitest';
import { nextMove, playKey, prevMove } from './deck-play';

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

describe('playKey', () => {
  it('maps the keys of the board', () => {
    expect(playKey({ key: 'ArrowRight' })).toBe('next');
    expect(playKey({ key: ' ' })).toBe('next');
    expect(playKey({ key: 'PageDown' })).toBe('next');
    expect(playKey({ key: 'ArrowUp' })).toBe('prev');
    expect(playKey({ key: 'Escape' })).toBe('exit');
    expect(playKey({ key: 'к' })).toBe('record');
    expect(playKey({ key: 'x' })).toBeNull();
  });
  it('leaves chords to the browser', () => {
    expect(playKey({ key: 'ArrowRight', metaKey: true })).toBeNull();
    expect(playKey({ key: 'r', ctrlKey: true })).toBeNull();
  });
});
