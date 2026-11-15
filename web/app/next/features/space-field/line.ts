// The one line of the field over a space (boards, documents): what was done, in a few words, with «Отменить» and
// «В чат». The agent's turn, the voice and a quick edit all say their result here; the chat's window opens only by
// «В чат». One line at a time: the newer one takes its place.
import { useSyncExternalStore } from 'react';

export interface ISpaceLine {
  text: string;
  /** Takes the change back; no button where nothing can be taken back. */
  undo?: () => void | Promise<void>;
  /** The agent asks it: answered «Да» or «Нет» right here. */
  question?: boolean;
  /** Shown with «В чат»: the whole of it is in the chat. */
  toChat?: boolean;
  /** When it was said: a line goes by itself a while after. */
  at: number;
}

let line: ISpaceLine | null = null;
const listeners = new Set<() => void>();
const tell = () => {
  for (const f of listeners) f();
};

/** Say a result in the space's field. */
export const showSpaceLine = (next: Omit<ISpaceLine, 'at'>): void => {
  line = { ...next, at: Date.now() };
  tell();
};

/** The line goes: dismissed, or its time is up. Only the line that was shown: a newer one stays. */
export const clearSpaceLine = (shown: ISpaceLine | null = line): void => {
  if (line === null || line !== shown) return;
  line = null;
  tell();
};

const subscribe = (f: () => void) => {
  listeners.add(f);
  return () => {
    listeners.delete(f);
  };
};

/** The line now, outside React. */
export const spaceLineNow = (): ISpaceLine | null => line;

export const useSpaceLine = (): ISpaceLine | null => useSyncExternalStore(subscribe, () => line);

/**
 * The gist of an answer for one line: its first sentence or paragraph, without markdown, cut at a word. The whole
 * answer is in the chat; a question is said whole, since it waits for «Да» or «Нет».
 */
export const gistOf = (md: string, max = 160): string => {
  const plain = md
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/^\s{0,3}(#{1,6}|[-*+]|\d+[.)]|>)\s+/gm, '')
    .replace(/[*_`~]+/g, '')
    .trim();
  const para =
    plain
      .split(/\n\s*\n/)[0]
      ?.replace(/\s+/g, ' ')
      .trim() ?? '';
  const first = para.match(/^.+?[.!?…](?=\s|$)/)?.[0] ?? para;
  const said = first.length >= 24 || first === para ? first : para;
  if (said.length <= max) return said;
  const cut = said.slice(0, max);
  const at = cut.lastIndexOf(' ');
  return `${(at > max * 0.6 ? cut.slice(0, at) : cut).replace(/[\s,;:—-]+$/, '')}…`;
};

/** The question an answer ends with, said whole; null when the answer asks nothing. */
export const questionOf = (md: string): string | null => {
  const lines = md
    .trim()
    .split('\n')
    .map((l) => l.replace(/[*_`]+/g, '').trim())
    .filter(Boolean);
  const last = lines.at(-1) ?? '';
  if (!/\?\s*$/.test(last)) return null;
  const sentences = last.match(/[^.!?…]+[.!?…]*/g) ?? [last];
  return (sentences.at(-1) ?? last).trim();
};
