// The dictation's own words (Settings → «Голосовой ввод»): the form's text turned into an entry, the checks the runtime
// makes too (internal/wire/me.go), and the list's changes. All pure; the list is saved by the page.
import { VOICE_LIMITS, type VoiceWord } from '@/next/stores/me';

/** Why an entry is not taken: said in the form's words. */
export const WORD_PROBLEM = {
  empty: 'empty',
  long: 'long',
  twice: 'twice',
  full: 'full',
  heardLong: 'heardLong',
  heardMany: 'heardMany',
} as const;
export type WordProblem = (typeof WORD_PROBLEM)[keyof typeof WORD_PROBLEM];

/** «как слышится» as typed: variants apart by commas or lines, without blanks and repeats. */
export const parseHeard = (text: string): string[] => {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of text.split(/[,;\n]/)) {
    const variant = part.trim().replace(/\s+/g, ' ');
    const key = variant.toLowerCase();
    if (variant === '' || seen.has(key)) continue;
    seen.add(key);
    out.push(variant);
  }
  return out;
};

export const heardText = (heard: readonly string[]): string => heard.join(', ');

const same = (a: string, b: string): boolean => a.toLowerCase() === b.toLowerCase();

/**
 * Checks an entry against the list. `editing` is the word being changed (its own place is not a repeat); null for a new
 * one. Null when the entry is fine.
 */
export const wordProblem = (
  words: readonly VoiceWord[],
  entry: VoiceWord,
  editing: string | null,
): WordProblem | null => {
  if (entry.word === '') return WORD_PROBLEM.empty;
  if ([...entry.word].length > VOICE_LIMITS.wordLen) return WORD_PROBLEM.long;
  if (entry.heard.length > VOICE_LIMITS.heard) return WORD_PROBLEM.heardMany;
  if (entry.heard.some((h) => [...h].length > VOICE_LIMITS.wordLen)) return WORD_PROBLEM.heardLong;
  const others = words.filter((w) => editing === null || !same(w.word, editing));
  if (others.some((w) => same(w.word, entry.word))) return WORD_PROBLEM.twice;
  if (editing === null && words.length >= VOICE_LIMITS.words) return WORD_PROBLEM.full;
  return null;
};

/** The list with the entry put in: in the place of the word being changed, else at the end. */
export const withWord = (
  words: readonly VoiceWord[],
  entry: VoiceWord,
  editing: string | null,
): VoiceWord[] => {
  if (editing === null) return [...words, entry];
  return words.map((w) => (same(w.word, editing) ? entry : w));
};

export const withoutWord = (words: readonly VoiceWord[], word: string): VoiceWord[] =>
  words.filter((w) => !same(w.word, word));
