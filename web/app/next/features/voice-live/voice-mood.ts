// The mascot's mood: a face the voice agent can make on top of what it does (listen, think, speak). Whoever
// has a reason to — the agent's tool, a screen — calls setVoiceMood('joy'); it fades back to neutral by itself, so a mood
// never gets stuck. The mascot (voice-head.tsx) reads it, the CSS draws it (styles/voice-mood.css).
import { useSyncExternalStore } from 'react';

export const VOICE_MOOD = {
  neutral: 'neutral',
  joy: 'joy',
  sad: 'sad',
  thinking: 'thinking',
  surprised: 'surprised',
} as const;
export type VoiceMood = (typeof VOICE_MOOD)[keyof typeof VOICE_MOOD];

/** How long a mood stays when the caller does not say. */
export const MOOD_MS = 4000;

let current: VoiceMood = VOICE_MOOD.neutral;
let timer: ReturnType<typeof setTimeout> | null = null;
const listeners = new Set<() => void>();

const set = (mood: VoiceMood) => {
  if (mood === current) return;
  current = mood;
  for (const listen of listeners) listen();
};

/** Makes the mood, then back to neutral after `ms` (0 or less keeps it until the next call). */
export const setVoiceMood = (mood: VoiceMood, ms: number = MOOD_MS) => {
  if (timer) clearTimeout(timer);
  timer = null;
  set(mood);
  if (mood === VOICE_MOOD.neutral || !(ms > 0)) return;
  timer = setTimeout(() => {
    timer = null;
    set(VOICE_MOOD.neutral);
  }, ms);
};

export const voiceMood = (): VoiceMood => current;

const subscribe = (listen: () => void) => {
  listeners.add(listen);
  return () => listeners.delete(listen);
};

export const useVoiceMood = (): VoiceMood =>
  useSyncExternalStore(subscribe, voiceMood, () => VOICE_MOOD.neutral);
