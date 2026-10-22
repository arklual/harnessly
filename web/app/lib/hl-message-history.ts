import { ANNOTATION_BLOCK_RE } from '@/lib/hl-annotation-format';
import { DIFF_COMMENT_BLOCK_RE } from '@/lib/hl-diff-comment';

/** One sent message as the runtime keeps it, newest first. */
export interface IHistoryEntry {
  id: string;
  message: string;
  sentAt: string;
}

/** The runtime keeps this many messages per workspace. */
export const HISTORY_LIMIT = 500;

/**
 * What the person typed, without the blocks the composer appended to it: the picked page elements and the diff
 * comments belong to that one message, they are not worth recalling. A slash command is not a message either.
 */
export const historyText = (sent: string): string => {
  const typed = sent.replace(ANNOTATION_BLOCK_RE, '').replace(DIFF_COMMENT_BLOCK_RE, '').trim();
  return typed.startsWith('/') ? '' : typed;
};

/** The entry goes first; an older one with the same text is dropped, and the list keeps its limit. */
export const withEntry = (entries: readonly IHistoryEntry[], entry: IHistoryEntry): IHistoryEntry[] =>
  [entry, ...entries.filter((e) => e.message !== entry.message)].slice(0, HISTORY_LIMIT);

export const withoutEntry = (entries: readonly IHistoryEntry[], id: string): IHistoryEntry[] =>
  entries.filter((e) => e.id !== id);

/** Walking back through the history with the arrow keys: the place in the list and what was typed before. */
export interface IRecall {
  /** -1: not walking. */
  index: number;
  /** The text shown by the last step; the walk ends as soon as the person edits it. */
  shown: string;
  draft: string;
}

export const NOT_RECALLING: IRecall = { index: -1, shown: '', draft: '' };

export interface IRecallStep {
  recall: IRecall;
  /** The new text of the field, or null when the key must stay an ordinary key. */
  value: string | null;
}

/**
 * ArrowUp / ArrowDown in the composer. The walk starts only from an empty field (or one that still holds the text of
 * the last step), so the arrows keep moving the caret through text the person is writing. ArrowDown past the newest
 * message brings back what was typed before the walk.
 */
export const stepRecall = (
  entries: readonly IHistoryEntry[],
  recall: IRecall,
  key: 'up' | 'down',
  current: string,
): IRecallStep => {
  const walking = recall.index >= 0 && current === recall.shown;
  if (!walking) {
    if (key === 'down' || current !== '' || entries.length === 0)
      return { recall: NOT_RECALLING, value: null };
    const shown = entries[0].message;
    return { recall: { index: 0, shown, draft: current }, value: shown };
  }
  if (key === 'up') {
    if (recall.index >= entries.length - 1) return { recall, value: null };
    const index = recall.index + 1;
    const shown = entries[index].message;
    return { recall: { ...recall, index, shown }, value: shown };
  }
  if (recall.index === 0) return { recall: NOT_RECALLING, value: recall.draft };
  const index = recall.index - 1;
  const shown = entries[index].message;
  return { recall: { ...recall, index, shown }, value: shown };
};
