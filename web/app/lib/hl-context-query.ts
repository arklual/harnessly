/**
 * Where an «@»-context popover opens in composer text: the same "caret is the end of the
 * text" convention the slash and `$skill` queries already use (see parseCommandQuery), so no caret
 * position needs to be read out of the contentEditable.
 */

export interface IAtQuery {
  /** Index of `@` itself in the text. */
  start: number;
  query: string;
}

const AT_WORD_RE = /(^|\s)@([^\s@]*)$/;

export const parseAtQuery = (text: string): IAtQuery | null => {
  const match = text.match(AT_WORD_RE);
  if (!match || match.index === undefined) return null;
  return { start: match.index + match[1].length, query: match[2] };
};

/** Removes the typed `@query` (picking it turns it into an attached chip, not inline text). */
export const removeAtMention = (text: string, at: IAtQuery): string => {
  const before = text.slice(0, at.start);
  const after = text.slice(at.start + 1 + at.query.length);
  if (before.endsWith(' ') && after.startsWith(' ')) return `${before}${after.slice(1)}`;
  return `${before}${after}`;
};
