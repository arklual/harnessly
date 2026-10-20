/** Merges speech-recognition partial/final segments into composer text. Pure, engine-agnostic. */

export const VOICE_STATE = {
  idle: 'idle',
  requesting: 'requesting',
  recording: 'recording',
  transcribing: 'transcribing',
  error: 'error',
} as const;
export type TVoiceState = (typeof VOICE_STATE)[keyof typeof VOICE_STATE];

export interface IVoiceTranscript {
  /** Segments the engine already finalized, in order, already joined into one string. */
  committed: string;
  /** The engine's current in-flight guess; replaced wholesale on every update, never appended to. */
  partial: string;
}

export const EMPTY_VOICE_TRANSCRIPT: IVoiceTranscript = { committed: '', partial: '' };

const trimmed = (value: string): string => value.replace(/\s+$/u, '');

/** Joins two spoken segments with exactly one space, tolerating either side being empty. */
export const joinSpokenSegments = (a: string, b: string): string => {
  const left = trimmed(a);
  const right = b.trim();
  if (!left) return right;
  if (!right) return left;
  return `${left} ${right}`;
};

export const applyPartialTranscript = (state: IVoiceTranscript, text: string): IVoiceTranscript => ({
  ...state,
  partial: text,
});

export const applyFinalTranscript = (state: IVoiceTranscript, text: string): IVoiceTranscript => ({
  committed: joinSpokenSegments(state.committed, text),
  partial: '',
});

/** What the engine has recognized so far, spoken segments merged with the still-shifting tail. */
export const spokenText = (state: IVoiceTranscript): string =>
  joinSpokenSegments(state.committed, state.partial);

/**
 * Composes the editor value while dictation is live: text typed before recording started, plus everything
 * spoken so far. `basePriorToStart` never changes for the life of one recording — recomputing from it (rather
 * than appending in place) is what lets a later partial safely replace an earlier one instead of stacking.
 */
export const composeDictatedValue = (basePriorToStart: string, state: IVoiceTranscript): string => {
  const spoken = spokenText(state);
  if (!spoken) return basePriorToStart;
  return joinSpokenSegments(basePriorToStart, spoken);
};

/**
 * Where the final transcript goes. Untouched field: after the text typed before recording, as during the
 * recording. Text typed or edited while dictating: appended to what is in the field now — never replaced.
 */
export const mergeDictation = (base: string, current: string, state: IVoiceTranscript): string => {
  if (current === base) return composeDictatedValue(base, state);
  return joinSpokenSegments(current, spokenText(state));
};

/** Optional lightweight punctuation cleanup for engines (server ASR) that emit none of their own. */
export const applyAutoPunctuation = (text: string): string => {
  // Both alphabets: the dictation language is the person's, whatever the page language is.
  const withCapital = text.replace(
    /(^\s*|[.!?]\s+)([a-zа-яё])/gu,
    (_m, sep: string, ch: string) => sep + ch.toUpperCase(),
  );
  if (!withCapital) return withCapital;
  return /[.!?…]$/u.test(withCapital) ? withCapital : `${withCapital}.`;
};
