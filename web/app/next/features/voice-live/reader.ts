// A long reading out loud («читать вслух документ полностью… больше десяти минут подряд»): the realtime
// voice says a few hundred words at most in one answer, so a long text is cut into parts at sentence ends and given to
// the voice one by one, each after the last one was said. The person speaking pauses it; «продолжи» (or the card's
// button) goes on from the part that was cut. Pure parts and a small store; the call's owner drives it.
import { useSyncExternalStore } from 'react';

/** About a minute of speech per part: long enough to flow, short enough for the voice to say word for word. */
export const PART_CHARS = 900;

/** The text cut into parts at sentence ends (a paragraph's end first), none longer than max unless one sentence is. */
export const readingParts = (text: string, max = PART_CHARS): string[] => {
  const sentences = text
    .replace(/\r/g, '')
    .split(/(?<=[.!?…])\s+|\n{2,}/u)
    .map((s) => s.replace(/\s+/g, ' ').trim())
    .filter(Boolean);
  const parts: string[] = [];
  let cur = '';
  for (const s of sentences) {
    if (cur && cur.length + 1 + s.length > max) {
      parts.push(cur);
      cur = s;
    } else cur = cur ? `${cur} ${s}` : s;
  }
  if (cur) parts.push(cur);
  return parts;
};

export interface IReading {
  title: string;
  parts: string[];
  /** The part being said, or the next one when paused. */
  at: number;
  paused: boolean;
}

let current: IReading | null = null;
const listeners = new Set<() => void>();
const set = (next: IReading | null) => {
  current = next;
  for (const l of listeners) l();
};

export const reading = (): IReading | null => current;
export const startReading = (title: string, text: string): IReading | null => {
  const parts = readingParts(text);
  set(parts.length ? { title, parts, at: 0, paused: false } : null);
  return current;
};
export const pauseReading = () => {
  if (current && !current.paused) set({ ...current, paused: true });
};
export const resumeReading = () => {
  if (current?.paused) set({ ...current, paused: false });
};
/** The part after the one said; null when the text is over (the reading ends). */
export const nextPart = (): string | null => {
  if (!current) return null;
  const at = current.at + 1;
  if (at >= current.parts.length) {
    set(null);
    return null;
  }
  set({ ...current, at });
  return current.parts[at] ?? null;
};
export const stopReading = () => set(null);

/** What the voice is told for a part: the words as they are, nothing of its own. */
export const readInstruction = (part: string, i: number, n: number): string =>
  `Read this aloud exactly as written, word for word, in its language, with no comment before or after` +
  `${i === 0 ? '' : ' (it continues the text you are reading)'}; part ${i + 1} of ${n}: «${part}»`;

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};
export const useReading = (): IReading | null => useSyncExternalStore(subscribe, reading, () => null);
