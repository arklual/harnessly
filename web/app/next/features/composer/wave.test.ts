import { describe, expect, it } from 'vitest';
import { createWave, FLOOR, idleLevel, SAMPLE_MS, shape, WAVE_MODE } from '@/next/features/composer/wave';

const run = (wave: ReturnType<typeof createWave>, levels: number[], frameMs = 16, from = 0) => {
  let now = from;
  const seen: number[] = [];
  for (const level of levels) {
    now += frameMs;
    wave.step(now, level, WAVE_MODE.live);
    seen.push(wave.envelope);
  }
  return { now, seen };
};

describe('the wave', () => {
  it('rises fast and falls slowly, with no jump from one frame to the next', () => {
    const wave = createWave();
    const loud = run(wave, Array(12).fill(1));
    const rise = loud.seen.at(-1) as number;
    expect(rise).toBeGreaterThan(0.6);
    const quiet = run(wave, Array(12).fill(0), 16, loud.now);
    // the same number of frames brings it down less than it came up
    expect(rise - (quiet.seen.at(-1) as number)).toBeLessThan(rise * 0.65);
    const all = [...loud.seen, ...quiet.seen];
    for (let i = 1; i < all.length; i++) expect(Math.abs(all[i] - all[i - 1])).toBeLessThan(0.45);
  });

  it('lifts quiet speech before it is drawn', () => {
    expect(shape(0.1)).toBeGreaterThan(0.2);
    expect(shape(0)).toBe(0);
    expect(shape(3)).toBe(1);
  });

  it('takes a bar at a steady pace and glides the older ones left in between', () => {
    const wave = createWave();
    let previous: number | null = null;
    const frame = 16;
    for (let t = frame; t < 1000; t += frame) {
      wave.step(t, 0.5, WAVE_MODE.live);
      const at = wave.bars(2, t, WAVE_MODE.live)[0].at;
      expect(at).toBeGreaterThanOrEqual(0);
      expect(at).toBeLessThan(1);
      if (previous !== null) {
        const moved = at - previous;
        // a frame moves the bars by its share of a step, or a bar was taken and the glide starts over
        const share = frame / SAMPLE_MS;
        expect(Math.abs(moved - share) < 1e-9 || Math.abs(moved - share + 1) < 1e-9).toBe(true);
      }
      previous = at;
    }
  });

  it('never goes below its floor and never above the whole height', () => {
    const wave = createWave();
    run(wave, [0, 0, 1, 1, 0.3, 0]);
    for (const bar of wave.bars(40, 5000, WAVE_MODE.live)) {
      expect(bar.h).toBeGreaterThanOrEqual(FLOOR);
      expect(bar.h).toBeLessThanOrEqual(1);
    }
  });

  it('breathes a little in a quiet room, and not at all under reduced motion', () => {
    const calm = createWave();
    const reduced = createWave(true);
    calm.step(0, 0, WAVE_MODE.live);
    reduced.step(0, 0, WAVE_MODE.live);
    const a = calm.bars(8, 0, WAVE_MODE.live).map((b) => b.h);
    const b = calm.bars(8, 1000, WAVE_MODE.live).map((b) => b.h);
    expect(a).not.toEqual(b);
    const x = reduced.bars(8, 0, WAVE_MODE.live).map((b) => b.h);
    const y = reduced.bars(8, 1000, WAVE_MODE.live).map((b) => b.h);
    expect(x).toEqual(y);
    expect(reduced.bars(3, 0, WAVE_MODE.live).map((b) => b.at)).toEqual([0, 1, 2]);
  });

  it('sweeps softly while the recording is turned into text', () => {
    const wave = createWave();
    wave.step(0, 1, WAVE_MODE.live);
    wave.step(50, 1, WAVE_MODE.live);
    for (let t = 100; t < 2000; t += 50) wave.step(t, 1, WAVE_MODE.busy);
    expect(wave.envelope).toBeLessThan(0.1);
    const bars = wave.bars(30, 2000, WAVE_MODE.busy).map((b) => b.h);
    expect(Math.max(...bars) - Math.min(...bars)).toBeGreaterThan(0.1);
  });

  it('does not jump when a hidden tab comes back', () => {
    const wave = createWave();
    wave.step(0, 0, WAVE_MODE.live);
    wave.step(60_000, 1, WAVE_MODE.live);
    // a minute of silence did not fill the history: only the bars of one frame stand, the rest are at rest
    const bars = wave.bars(12, 60_000, WAVE_MODE.live);
    expect(bars[8].h).toBeLessThan(FLOOR + 0.07);
  });

  it('has a calm loudness of its own for a browser without a meter', () => {
    for (let t = 0; t < 5000; t += 100) {
      expect(idleLevel(t)).toBeGreaterThan(0.1);
      expect(idleLevel(t)).toBeLessThan(0.26);
    }
  });
});
