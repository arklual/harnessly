// The remarks a person writes on the lines of a chat's work in the «Ревью кода» app, before they go to the chat's
// agent. The hub keeps them per person and chat (history/review_notes.go); here is what they are, where they stand in a
// diff that changed since they were written, and the one message they make for the agent.
import { LINE_SIDE, type TLineSide } from '@/next/features/review/model';

export const NOTE_STATE = { draft: 'draft', sent: 'sent', done: 'done' } as const;
export type TNoteState = (typeof NOTE_STATE)[keyof typeof NOTE_STATE];

export interface IReviewNote {
  id: string;
  /** The worktree the file is in; a chat may have worked in several. */
  worktree?: string;
  /** The commit the lines were read at, when known. */
  base?: string;
  path: string;
  side: TLineSide;
  from: number;
  to: number;
  /** The text of the lines when the note was written: the note follows it to a new diff. */
  lines: string;
  text: string;
  state: TNoteState;
  createdAt: number;
  updatedAt: number;
  sentAt?: number;
}

/** Where a note stands in the text shown now: its lines, or none when they are gone (the note is outdated). */
export type TPlace = { from: number; to: number } | null;

const NOTE_ID_CHARS = 'abcdefghijklmnopqrstuvwxyz0123456789';

export const newNoteId = (random: () => number = Math.random): string =>
  Array.from({ length: 16 }, () => NOTE_ID_CHARS[Math.floor(random() * NOTE_ID_CHARS.length)]).join('');

/** The text of lines `from`–`to` (1-based, inclusive) of a document split into lines. */
export const linesOf = (doc: readonly string[], from: number, to: number): string =>
  doc.slice(from - 1, to).join('\n');

/**
 * Where the note's lines are in the document now: where they were when they are still there, else the nearest place
 * that has the same lines, else null.
 */
export const placeNote = (
  note: Pick<IReviewNote, 'from' | 'to' | 'lines'>,
  doc: readonly string[],
): TPlace => {
  const size = note.to - note.from + 1;
  if (note.to <= doc.length && linesOf(doc, note.from, note.to) === note.lines)
    return { from: note.from, to: note.to };
  let best: TPlace = null;
  for (let start = 1; start + size - 1 <= doc.length; start++) {
    if (linesOf(doc, start, start + size - 1) !== note.lines) continue;
    if (!best || Math.abs(start - note.from) < Math.abs(best.from - note.from))
      best = { from: start, to: start + size - 1 };
  }
  return best;
};

/** The notes that go to the agent with the next «Отправить агенту»: the drafts, by file and line. */
export const draftsOf = (notes: readonly IReviewNote[]): IReviewNote[] =>
  notes
    .filter((n) => n.state === NOTE_STATE.draft)
    .sort((a, b) => a.path.localeCompare(b.path) || a.from - b.from || a.createdAt - b.createdAt);

const FENCE = '```';
const fenceFor = (text: string): string => {
  let fence = FENCE;
  while (text.includes(fence)) fence += '`';
  return fence;
};

const rangeOf = (n: Pick<IReviewNote, 'from' | 'to'>) =>
  n.from === n.to ? `${n.from}` : `${n.from}-${n.to}`;

/** The place and the fragment of a note, as the agent reads them. */
export const quoteLines = (
  n: Pick<IReviewNote, 'path' | 'side' | 'from' | 'to' | 'lines'>,
  words: INoteWords,
) => {
  const fence = fenceFor(n.lines);
  const where = `${n.path}:${rangeOf(n)}${n.side === LINE_SIDE.before ? ` (${words.before})` : ''}`;
  return `${where}\n${fence}\n${n.lines}\n${fence}`;
};

export interface INoteWords {
  /** The opening line of the message with the remarks. */
  intro: string;
  /** The heading of what the person adds to all of them. */
  more: string;
  /** Said of lines of the old version. */
  before: string;
}

/** The one message the drafts and the person's last words make for the agent. */
export const agentMessage = (notes: readonly IReviewNote[], extra: string, words: INoteWords): string => {
  const parts = [words.intro];
  notes.forEach((n, i) => parts.push(`${i + 1}. ${quoteLines(n, words)}\n${n.text.trim()}`));
  const more = extra.trim();
  if (more) parts.push(`${words.more}\n${more}`);
  return parts.join('\n\n');
};

/** A question on lines, sent to the agent at once. */
export const questionMessage = (
  at: Pick<IReviewNote, 'path' | 'side' | 'from' | 'to' | 'lines'>,
  text: string,
  words: INoteWords & { question: string },
): string => `${words.question}\n\n${quoteLines(at, words)}\n\n${text.trim()}`;
