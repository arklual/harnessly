import type { TVoiceLang } from '@/lib/voice-settings';

/** Pure helpers for the in-tab Whisper engine: audio prep before the model, text cleanup after it. */

export const WHISPER_SAMPLE_RATE = 16000;

export const WHISPER_LANGUAGE: Record<TVoiceLang, string | null> = {
  ru: 'russian',
  en: 'english',
  auto: null,
};

export const resampleLinear = (
  input: Float32Array,
  fromRate: number,
  toRate: number = WHISPER_SAMPLE_RATE,
): Float32Array => {
  if (fromRate === toRate) return input;
  const outLength = Math.floor((input.length * toRate) / fromRate);
  const out = new Float32Array(outLength);
  const ratio = fromRate / toRate;
  for (let i = 0; i < outLength; i++) {
    const pos = i * ratio;
    const left = Math.floor(pos);
    const right = Math.min(left + 1, input.length - 1);
    const frac = pos - left;
    out[i] = input[left] * (1 - frac) + input[right] * frac;
  }
  return out;
};

export const rootMeanSquare = (samples: Float32Array): number => {
  if (samples.length === 0) return 0;
  let sum = 0;
  for (const value of samples) sum += value * value;
  return Math.sqrt(sum / samples.length);
};

/** Below this RMS a segment is treated as silence and never reaches the model, which invents text on silence. */
export const SILENCE_RMS = 0.006;

export const isSilent = (samples: Float32Array): boolean => rootMeanSquare(samples) < SILENCE_RMS;

const FRAME_S = 0.03;
const EDGE_PAD_S = 0.25;

const frameRms = (samples: Float32Array, start: number, size: number): number =>
  rootMeanSquare(samples.subarray(start, Math.min(samples.length, start + size)));

/** Drops silence before the first and after the last voiced frame: Whisper invents outros on trailing silence. */
export const trimSilence = (samples: Float32Array, rate: number = WHISPER_SAMPLE_RATE): Float32Array => {
  const frame = Math.max(1, Math.round(FRAME_S * rate));
  let first = -1;
  let last = -1;
  for (let start = 0; start < samples.length; start += frame) {
    if (frameRms(samples, start, frame) < SILENCE_RMS) continue;
    if (first < 0) first = start;
    last = start + frame;
  }
  if (first < 0) return samples.subarray(0, 0);
  const pad = Math.round(EDGE_PAD_S * rate);
  return samples.subarray(Math.max(0, first - pad), Math.min(samples.length, last + pad));
};

/** Where to end a segment of at most `limit` samples: the quietest frame of its last `searchS` seconds, not mid-word. */
export const findSegmentCut = (samples: Float32Array, limit: number, rate: number, searchS = 5): number => {
  if (samples.length <= limit) return samples.length;
  const frame = Math.max(1, Math.round(FRAME_S * rate));
  let best = limit;
  let bestRms = Infinity;
  for (let start = Math.max(0, limit - Math.round(searchS * rate)); start + frame <= limit; start += frame) {
    const rms = frameRms(samples, start, frame);
    if (rms < bestRms) {
      bestRms = rms;
      best = start + Math.floor(frame / 2);
    }
  }
  return best;
};

/**
 * Phrases Whisper emits on silence or noise: caption credits and outros from its training subtitles. The Russian ones
 * are what it says on Russian speech, so they are data to match, not page text.
 */
const HALLUCINATIONS = [
  /^продолжение следует/i,
  /^субтитры (сделал|создавал|подготовил|делал)/i,
  /^редактор субтитров/i,
  /^спасибо за (просмотр|внимание)[.!]*$/i,
  /^подписывайтесь на (канал|наш канал)/i,
  /^с вами был/i,
  /^thank you for watching/i,
  /^thanks for watching/i,
  /^you[.!]*$/i,
];

const SOUND_TAG = /\s*[[(][^\])]{0,40}[\])]\s*/g;

const SENTENCE_END = /(?<=[.!?…])\s+/;

export const cleanWhisperText = (raw: string): string =>
  raw
    .replace(SOUND_TAG, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .split(SENTENCE_END)
    .filter((sentence) => sentence && !HALLUCINATIONS.some((pattern) => pattern.test(sentence)))
    .join(' ');

export const joinTranscriptParts = (parts: readonly string[]): string => parts.filter(Boolean).join(' ');
