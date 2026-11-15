import { describe, expect, it } from 'vitest';
import { BUSY_STEP, busyStep, CALM_FACTOR, nextStep } from '@/next/core/net/calm-poll';

describe('calmPoll steps (HRN-1402)', () => {
  const o = { base: 2000, max: 15_000 };
  it('stays at the base while the answer changes, and grows to the max while it does not', () => {
    expect(nextStep(2000, o, true)).toBe(2000);
    expect(nextStep(2000, o, false)).toBe(Math.round(2000 * CALM_FACTOR));
    let step = 2000;
    for (let i = 0; i < 20; i++) step = nextStep(step, o, false);
    expect(step).toBe(15_000);
    expect(nextStep(step, o, true)).toBe(2000);
  });

  it('a move of the conversations brings a quiet poll to the busy step, not to its base (HRN-1471)', () => {
    expect(busyStep(15_000, o)).toBe(BUSY_STEP);
    expect(busyStep(2000, o)).toBe(2000);
    expect(busyStep(15_000, { base: 9000, max: 30_000 })).toBe(9000);
    expect(BUSY_STEP).toBeGreaterThanOrEqual(6000);
  });
});
