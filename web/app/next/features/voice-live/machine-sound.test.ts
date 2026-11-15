// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { framer, nextStart, preferMachine, prefersMachine } from '@/next/features/voice-live/machine-sound';

describe('machine sound (HRN-1486)', () => {
  it('cuts the microphone into whole 20 ms frames, the rest waits', () => {
    const got: number[][] = [];
    const push = framer(4, (f) => got.push([...f]));
    push(new Float32Array([1, 2, 3]));
    expect(got).toEqual([]);
    push(new Float32Array([4, 5, 6, 7, 8, 9]));
    expect(got).toEqual([
      [1, 2, 3, 4],
      [5, 6, 7, 8],
    ]);
    push(new Float32Array([10, 11, 12]));
    expect(got[2]).toEqual([9, 10, 11, 12]);
  });

  it('plays the voice right after its last piece, a little ahead after a gap, and drops what would lag', () => {
    expect(nextStart(0, 10)).toBeCloseTo(10.08);
    expect(nextStart(10.2, 10)).toBe(10.2);
    expect(nextStart(10.7, 10)).toBeNull();
  });

  it('remembers for a few days that the device needs the machine', () => {
    const now = 1_000_000_000_000;
    preferMachine(true, now);
    expect(prefersMachine(now + 3600_000)).toBe(true);
    expect(prefersMachine(now + 4 * 24 * 3600_000)).toBe(false);
    preferMachine(false);
    expect(prefersMachine(now)).toBe(false);
  });
});
