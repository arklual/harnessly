import { describe, expect, it } from 'vitest';
import { CALM_FACTOR, nextStep } from '@/next/core/net/calm-poll';

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
});
