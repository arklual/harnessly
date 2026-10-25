// The recording's wave as numbers, apart from the canvas that draws it: the microphone's loudness is smoothed
// like an envelope (it rises fast and falls slowly, so the bars breathe with the voice instead of jumping), a new bar
// is taken at a steady pace and the older ones glide left between those moments, and a quiet room still breathes a
// little. Nothing here reads the clock or the page: `step` is told the time, so the same input gives the same bars.

/** One bar is taken this often; the older ones move a whole step left in that time. */
export const SAMPLE_MS = 55;
/** How fast the envelope follows a louder voice and a quieter one. */
export const ATTACK_MS = 40;
export const RELEASE_MS = 240;
/** The least a bar stands. */
export const FLOOR = 0.1;
/** How far a quiet bar lifts and falls on its own. */
export const BREATH = 0.06;
const BREATH_MS = 820;
/** The longest wait between two frames that counts: a tab that was hidden does not make the wave jump. */
const MAX_DT_MS = 100;
const HISTORY = 400;

/** Reduced motion: bars are taken less often, do not glide and do not breathe. */
const CALM_SAMPLE_MS = 140;

export const WAVE_MODE = {
  /** The microphone's loudness. */
  live: 'live',
  /** No meter in the browser: a calm motion of its own. */
  idle: 'idle',
  /** The recording is over and being turned into text: a soft sweep. */
  busy: 'busy',
} as const;
export type TWaveMode = (typeof WAVE_MODE)[keyof typeof WAVE_MODE];

export interface IBar {
  /** 0..1 of the wave's height. */
  h: number;
  /** In steps from the right edge (0 is the edge), fractional while gliding. */
  at: number;
}

export interface IWave {
  /** Moves to `now` (ms) with the loudness read just now (0..1). */
  step: (now: number, level: number, mode: TWaveMode) => void;
  /** The newest `count` bars, newest first. */
  bars: (count: number, now: number, mode: TWaveMode) => IBar[];
  /** The smoothed loudness, 0..1. */
  readonly envelope: number;
}

/** Quiet speech should still move the bars: the loudness is lifted before it is drawn. */
export const shape = (level: number): number => Math.min(1, Math.max(0, level) ** 0.65);

/** A calm loudness for a browser that has no meter. */
export const idleLevel = (now: number): number =>
  0.12 + 0.12 * (0.5 + 0.5 * Math.sin(now / 430)) * (0.5 + 0.5 * Math.sin(now / 1170 + 1));

export const createWave = (reduced = false): IWave => {
  const every = reduced ? CALM_SAMPLE_MS : SAMPLE_MS;
  let env = 0;
  let last: number | null = null;
  let acc = 0;
  const history: number[] = [];

  return {
    step(now, level, mode) {
      const dt = last === null ? 0 : Math.min(Math.max(now - last, 0), MAX_DT_MS);
      last = now;
      const target = mode === WAVE_MODE.busy ? 0 : shape(level);
      const tau = target > env ? ATTACK_MS : RELEASE_MS;
      env += (target - env) * (1 - Math.exp(-dt / tau));
      acc += dt;
      while (acc >= every) {
        acc -= every;
        history.unshift(env);
        if (history.length > HISTORY) history.pop();
      }
    },
    bars(count, now, mode) {
      const phase = reduced ? 0 : acc / every;
      return Array.from({ length: count }, (_, k) => {
        const at = k + phase;
        const own = k === 0 ? env : (history[k - 1] ?? 0);
        const breath = reduced ? 0 : BREATH * (0.5 + 0.5 * Math.sin(now / BREATH_MS + at * 0.5));
        const sweep =
          !reduced && mode === WAVE_MODE.busy ? 0.3 * (0.5 + 0.5 * Math.sin(at * 0.32 - now / 260)) : 0;
        const rest = FLOOR + breath + sweep;
        return { at, h: Math.min(1, rest + own * (1 - rest)) };
      });
    },
    get envelope() {
      return env;
    },
  };
};
