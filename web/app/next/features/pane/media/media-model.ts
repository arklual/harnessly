// The media player's arithmetic and keys. Pure: the player and its tests read it.

export const MEDIA_KIND = { video: 'video', audio: 'audio' } as const;
export type TMediaKind = (typeof MEDIA_KIND)[keyof typeof MEDIA_KIND];

export const MEDIA_ACTION = {
  mark: 'mark',
  toggle: 'toggle',
  back: 'back',
  forward: 'forward',
  frameBack: 'frameBack',
  frameForward: 'frameForward',
  louder: 'louder',
  quieter: 'quieter',
  mute: 'mute',
  full: 'full',
  start: 'start',
  end: 'end',
} as const;
export type TMediaAction = (typeof MEDIA_ACTION)[keyof typeof MEDIA_ACTION];

// By the key of the keyboard, not its letter: the Russian layout has the same keys.
const KEYS: Readonly<Record<string, TMediaAction>> = {
  Space: MEDIA_ACTION.toggle,
  KeyK: MEDIA_ACTION.toggle,
  ArrowLeft: MEDIA_ACTION.back,
  ArrowRight: MEDIA_ACTION.forward,
  Comma: MEDIA_ACTION.frameBack,
  Period: MEDIA_ACTION.frameForward,
  ArrowUp: MEDIA_ACTION.louder,
  ArrowDown: MEDIA_ACTION.quieter,
  KeyM: MEDIA_ACTION.mute,
  KeyF: MEDIA_ACTION.full,
  KeyS: MEDIA_ACTION.mark,
  Home: MEDIA_ACTION.start,
  End: MEDIA_ACTION.end,
};

export const mediaActionOf = (code: string): TMediaAction | null => KEYS[code] ?? null;

/** The tenth of the file a digit key goes to; null for any other key. */
export const tenthOf = (code: string): number | null => {
  const found = /^(?:Digit|Numpad)(\d)$/.exec(code);
  return found ? Number(found[1]) / 10 : null;
};

export const JUMP_SECONDS = 5;
export const VOLUME_STEP = 0.05;
/** A browser does not say a file's frame rate, so a frame step is one frame of the commonest. */
export const FRAME_SECONDS = 1 / 30;

export const RATES = [1, 1.25, 1.5, 2, 0.5] as const;
export type TRate = (typeof RATES)[number];

export const nextRate = (rate: TRate): TRate => RATES[(RATES.indexOf(rate) + 1) % RATES.length];

export const clamp01 = (value: number): number => Math.min(1, Math.max(0, value));

/** 0:07, 12:34, 1:02:03. An unknown length draws as a dash. */
export const formatTime = (seconds: number | null): string => {
  if (seconds === null || !Number.isFinite(seconds) || seconds < 0) return '–:––';
  const whole = Math.floor(seconds);
  const hours = Math.floor(whole / 3600);
  const minutes = Math.floor((whole % 3600) / 60);
  const rest = String(whole % 60).padStart(2, '0');
  if (hours > 0) return `${hours}:${String(minutes).padStart(2, '0')}:${rest}`;
  return `${minutes}:${rest}`;
};

/** The sound's height in `count` bars from 0 to 1, the loudest being 1. */
export const peaksOf = (samples: Float32Array, count: number): number[] => {
  const size = Math.max(1, Math.floor(samples.length / count));
  const peaks = Array.from({ length: count }, (_, bar) => {
    let top = 0;
    const from = bar * size;
    const to = Math.min(samples.length, from + size);
    for (let at = from; at < to; at += 1) top = Math.max(top, Math.abs(samples[at]));
    return top;
  });
  const loudest = Math.max(...peaks);
  return loudest > 0 ? peaks.map((peak) => peak / loudest) : peaks;
};

/** What the person marked on the timeline: a moment, or a stretch once it has an end. */
export interface IMediaMark {
  from: number;
  to: number | null;
}

/** The mark's next state, pressed at this time: a moment, a stretch from it to here, then nothing. */
export const nextMark = (mark: IMediaMark | null, time: number): IMediaMark | null => {
  if (mark === null) return { from: time, to: null };
  if (mark.to === null) return { from: Math.min(mark.from, time), to: Math.max(mark.from, time) };
  return null;
};
