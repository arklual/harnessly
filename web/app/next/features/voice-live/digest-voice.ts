// The day's digest read out by voice, the logic without React: what the page gives, how the person's words
// steer the reading, which phrase is said and how much of it. The hook is use-digest-voice.ts, the controls
// digest-listen.tsx and digest-line.tsx.
import { DIGEST_VOICE, type TDigestVoice } from '@/next/features/voice-live/digest-listen';
import { FLOOR, type TFloor } from '@/next/features/voice-live/use-voice-call';

/** One phrase of the digest: what is said, and the item of the page it belongs to (its anchor, the id of the item). */
export interface IDigestPhrase {
  text: string;
  anchor: string;
}

/** Where the reading is. */
export const DIGEST_STEP = {
  idle: 'idle',
  /** The voice reads the phrase `at`. */
  reading: 'reading',
  /** The person asked something: the voice answers about the item of the phrase `at`. */
  answering: 'answering',
  /** The voice answered and asked «Продолжить?». */
  asking: 'asking',
  paused: 'paused',
} as const;
export type TDigestStep = (typeof DIGEST_STEP)[keyof typeof DIGEST_STEP];

/** The note that makes the voice say a phrase word for word (realtime promptDigest). */
export const readNote = (p: IDigestPhrase): string => `READ: ${p.text}`;

/** The whole digest for the voice's first items, items marked by their anchors: it answers questions from it. */
export const digestScreen = (phrases: readonly IDigestPhrase[]): string => {
  const out = ["The person's day digest, read out phrase by phrase:"];
  for (const p of phrases) out.push(`[${p.anchor}] ${p.text}`);
  return out.join('\n');
};

/** «Да», «продолжай», «дальше», «давай»: go on reading. JS's \b knows no Cyrillic, hence the look-aheads. */
const ON_RE = /^(да(?![а-яё])|ага|угу|давай|продолж|дальше|читай|поехали|ok|окей|хорошо|yes|go on|continue)/i;
/** «Хватит», «стоп», «закончим», «всё»: end. */
const STOP_RE =
  /^(нет(?![а-яё])|не надо|хватит|стоп|закончи|заканчива|всё(?![а-яё])|достаточно|пока(?![а-яё])|stop|enough|no\b)/i;

export const DIGEST_SAID = { on: 'on', stop: 'stop', ask: 'ask' } as const;
export type TDigestSaid = (typeof DIGEST_SAID)[keyof typeof DIGEST_SAID];

/** What the person's line means for the reading: go on, end, or a question about the item. */
export const digestSaid = (text: string): TDigestSaid | null => {
  const t = text.trim().replace(/^[\s,.!?…-]+/, '');
  if (!t) return null;
  if (STOP_RE.test(t)) return DIGEST_SAID.stop;
  if (ON_RE.test(t) && t.split(/\s+/).length <= 3) return DIGEST_SAID.on;
  return DIGEST_SAID.ask;
};

/** A phrase split by what the voice said of it so far, by words: the said part lit, the rest grey. */
export const splitSaid = (phrase: string, heard: string): { said: string; rest: string } => {
  const words = phrase.split(/\s+/).filter(Boolean);
  const n = Math.min(words.length, heard.split(/\s+/).filter(Boolean).length);
  return { said: words.slice(0, n).join(' '), rest: words.slice(n).join(' ') };
};

/** Whether the voice said a phrase out (most of its words), not cut short; transcripts drop and merge a word or two. */
export const saidOut = (phrase: string, heard: string): boolean => {
  const want = phrase.split(/\s+/).filter(Boolean).length;
  const got = heard.split(/\s+/).filter(Boolean).length;
  return want === 0 || got >= Math.ceil(want * 0.7);
};

/** The controls' state from where the reading is and who has the floor. */
export const digestState = (
  live: boolean,
  step: TDigestStep,
  floor: TFloor,
  interrupted: boolean,
): TDigestVoice => {
  if (!live || step === DIGEST_STEP.idle) return DIGEST_VOICE.off;
  if (step === DIGEST_STEP.paused) return DIGEST_VOICE.paused;
  if (interrupted) return DIGEST_VOICE.barged;
  if (floor === FLOOR.thinking) return DIGEST_VOICE.thinking;
  if (step === DIGEST_STEP.reading) return DIGEST_VOICE.reading;
  return floor === FLOOR.speaking ? DIGEST_VOICE.reading : DIGEST_VOICE.listening;
};
