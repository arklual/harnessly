import { describe, expect, it } from 'vitest';
import { STALE_MS, releaseSpeed, sheetCloses, softSpeed, swipeVerdict } from '@/next/ui/gesture-rules';

describe('releaseSpeed', () => {
  it('keeps the speed of a finger that lifts at once', () => {
    expect(releaseSpeed(0.9, 1000, 1000 + STALE_MS)).toBe(0.9);
  });
  it('drops the speed of a finger that stood still before it lifted', () => {
    expect(releaseSpeed(0.9, 1000, 1000 + STALE_MS + 1)).toBe(0);
  });
});

describe('softSpeed', () => {
  it('is not taken in by one fast sample after slow ones', () => {
    let v = 0;
    for (let i = 0; i < 6; i += 1) v = softSpeed(v, 0.1);
    expect(softSpeed(v, 2)).toBeLessThan(1.2);
    expect(softSpeed(v, 2)).toBeGreaterThan(v);
  });
});

describe('sheetCloses', () => {
  it('closes past a third of its height, slowly', () => {
    expect(sheetCloses(201, 600, 0)).toBe(true);
    expect(sheetCloses(199, 600, 0)).toBe(false);
  });
  it('closes on a flick from a little way, not from a wobble', () => {
    expect(sheetCloses(60, 600, 0.8)).toBe(true);
    expect(sheetCloses(10, 600, 0.8)).toBe(false);
  });
  it('comes back when cancelled short and slow', () => {
    expect(sheetCloses(0, 600, 0)).toBe(false);
  });
});

describe('swipeVerdict', () => {
  const SLOP = 8;
  it('flies off past a third of the width either way', () => {
    expect(swipeVerdict(121, 360, 0, SLOP)).toBe('fly');
    expect(swipeVerdict(-121, 360, 0, SLOP)).toBe('fly');
    expect(swipeVerdict(100, 360, 0, SLOP)).toBe('back');
  });
  it('flies off when flicked, but not from a small shift', () => {
    expect(swipeVerdict(40, 360, 0.7, SLOP)).toBe('fly');
    expect(swipeVerdict(10, 360, 0.7, SLOP)).toBe('back');
  });
});
