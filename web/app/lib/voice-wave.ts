/** One bar is 2.5 px wide with a 2 px gap. */
export const WAVE_BAR_STEP = 4.5;
const WAVE_MIN_HEIGHT = 3;
const WAVE_MAX_HEIGHT = 22;

export const waveBarCount = (width: number): number => Math.max(0, Math.floor(width / WAVE_BAR_STEP));

/** The level history as a ring of `count` bars, newest on the right; a resized track pads or trims on the left. */
export const pushWaveLevel = (levels: readonly number[], level: number, count: number): number[] => {
  const next = [...levels, level].slice(-count);
  return next.length < count ? [...Array<number>(count - next.length).fill(0), ...next] : next;
};

export const waveBarHeight = (level: number): number =>
  Math.round(WAVE_MIN_HEIGHT + Math.min(1, level) ** 0.7 * (WAVE_MAX_HEIGHT - WAVE_MIN_HEIGHT));
