import { describe, expect, it } from 'vitest';
import { BEHIND_GRACE_SEC, PACE, Stopwatch, formatClock, formatMs, pace, slideBudgets } from './clock';

describe('formatClock', () => {
  it('writes m:ss', () => {
    expect(formatClock(0)).toBe('0:00');
    expect(formatClock(9)).toBe('0:09');
    expect(formatClock(75)).toBe('1:15');
    expect(formatClock(600)).toBe('10:00');
    expect(formatClock(3723)).toBe('1:02:03');
  });
  it('is safe on nonsense', () => {
    expect(formatClock(-5)).toBe('0:00');
    expect(formatClock(Number.NaN)).toBe('0:00');
    expect(formatClock(59.9)).toBe('0:59');
  });
  it('writes milliseconds', () => {
    expect(formatMs(72_000)).toBe('1:12');
    expect(formatMs(1499)).toBe('0:01');
  });
});

describe('slideBudgets', () => {
  it('shares the minutes in proportion to the notes', () => {
    const b = slideBudgets([30, 90, 60], 6);
    expect(b.map(Math.round)).toEqual([60, 180, 120]);
    expect(b.reduce((a, c) => a + c, 0)).toBeCloseTo(360);
  });
  it('shares equally when nothing is written', () => {
    expect(slideBudgets([0, 0, 0, 0], 8)).toEqual([120, 120, 120, 120]);
  });
  it('has nothing for no slides', () => {
    expect(slideBudgets([], 10)).toEqual([]);
    expect(slideBudgets([30, 90], 0)).toEqual([30, 90]);
  });
});

describe('pace', () => {
  const budgets = [60, 120, 120];
  it('is on time inside the plan of the slides so far', () => {
    expect(pace(30, 0, budgets)).toBe(PACE.onTime);
    expect(pace(60 + BEHIND_GRACE_SEC, 0, budgets)).toBe(PACE.onTime);
    expect(pace(100, 1, budgets)).toBe(PACE.onTime);
  });
  it('is behind once the current slide has run past its end and the grace', () => {
    expect(pace(60 + BEHIND_GRACE_SEC + 1, 0, budgets)).toBe(PACE.behind);
    expect(pace(400, 2, budgets)).toBe(PACE.behind);
  });
  it('being ahead of the plan is still on time', () => {
    expect(pace(5, 2, budgets)).toBe(PACE.onTime);
  });
});

describe('Stopwatch', () => {
  it('counts from the start', () => {
    const w = new Stopwatch();
    expect(w.elapsed(500)).toBe(0);
    w.start(1000);
    expect(w.elapsed(4500)).toBe(3500);
  });
  it('stands still while paused and goes on after', () => {
    const w = new Stopwatch();
    w.start(0);
    w.pause(2000);
    expect(w.paused).toBe(true);
    expect(w.elapsed(9000)).toBe(2000);
    w.resume(10_000);
    expect(w.elapsed(11_000)).toBe(3000);
    w.pause(12_000);
    w.pause(13_000);
    w.resume(20_000);
    w.resume(21_000);
    expect(w.elapsed(21_000)).toBe(5000);
  });
  it('ignores a pause before the start', () => {
    const w = new Stopwatch();
    w.pause(10);
    w.start(100);
    expect(w.elapsed(300)).toBe(200);
  });
});
