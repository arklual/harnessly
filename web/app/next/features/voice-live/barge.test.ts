import { describe, expect, it } from 'vitest';
import { BARGE_START, bargeStep, type IBarge } from '@/next/features/voice-live/barge';

/** Runs frames of 16 ms; returns the state and the time of the barge-in, if any. */
const run = (s: IBarge, from: number, ms: number, me: number, them: number, speaking = true) => {
  let st = s;
  let at: number | null = null;
  for (let t = from; t < from + ms; t += 16) {
    const [next, cut] = bargeStep(st, me, them, t, speaking);
    st = next;
    if (cut && at === null) at = t;
  }
  return { st, at };
};

describe('barge-in', () => {
  it('silences the voice within 200 ms of the person speaking over it', () => {
    const { st, at } = run(BARGE_START, 0, 400, 0.4, 0.2);
    expect(at).not.toBeNull();
    expect(at!).toBeLessThanOrEqual(200);
    expect(st.ducked).not.toBeNull();
  });
  it('does not take the voice’s own echo or a click for speech', () => {
    expect(run(BARGE_START, 0, 1000, 0.15, 0.3).at).toBeNull();
    expect(run(BARGE_START, 0, 1000, 0.3, 0.5).at).toBeNull();
    expect(run(BARGE_START, 0, 80, 0.5, 0.2).at).toBeNull();
    expect(run(BARGE_START, 0, 1000, 0.5, 0, false).at).toBeNull();
  });
  it('lets the voice be heard again once its cut answer is over', () => {
    const cut = run(BARGE_START, 0, 300, 0.4, 0.2).st;
    const still = run(cut, 300, 300, 0, 0.3).st;
    expect(still.ducked).not.toBeNull();
    expect(run(still, 600, 700, 0, 0).st.ducked).toBeNull();
  });
});
