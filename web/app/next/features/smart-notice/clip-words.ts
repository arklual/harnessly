// A notice's text cut short at a word, with «…»: the answer's start comes from the machine cut at a count of
// characters, mid-word («…Отдал подча»), and a card shows only a line or two of it anyway.

/** The longest text a notice keeps, in characters: two lines of the card (380px, the small text) and a little less,
 *  so the cut is this one, at a word, and not the stylesheet's two-line clamp after it. */
export const NOTICE_TEXT_MAX = 90;
/** What the machine sends of an answer's start at most (internal/wire SnippetMax): a text this long was cut there. */
export const SNIPPET_MAX = 160;

const TRAIL = /[\s,.;:!?—–-]+$/u;

/**
 * The text whole when it fits in `max` and was not cut before; otherwise up to the last whole word within `max`, its
 * trailing punctuation dropped, and «…». A first word longer than `max` is cut where it stands.
 */
export const clipWords = (text: string, max = NOTICE_TEXT_MAX, cutAt = SNIPPET_MAX): string => {
  const chars = [...text.trim()];
  const wasCut = chars.length >= cutAt;
  if (chars.length <= max && !wasCut) return chars.join('');
  const head = chars.slice(0, Math.min(max, wasCut ? chars.length - 1 : max) + 1).join('');
  // A cut text ends in a broken word: the space before it is the last place to keep.
  const space = head.search(/\s\S*$/u);
  const kept = space > 0 ? head.slice(0, space) : head.slice(0, -1);
  return `${kept.replace(TRAIL, '')}…`;
};
