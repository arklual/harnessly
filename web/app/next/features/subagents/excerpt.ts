// What a subagent returned, shortened for the feed: its first lines, the rest on a click.

/** The lines of the feed's excerpt. */
export const EXCERPT_LINES = 3;

export interface IExcerpt {
  text: string;
  /** The agent said more than the excerpt shows. */
  more: boolean;
}

/** The first non-empty lines of `text`, as markdown (a fenced block cut open is closed, so it does not eat the page). */
export const excerptOf = (text: string): IExcerpt => {
  const lines = text.split('\n').filter((line) => line.trim() !== '');
  const head = lines.slice(0, EXCERPT_LINES);
  const open = head.filter((line) => line.trimStart().startsWith('```')).length % 2 === 1;
  return { text: (open ? [...head, '```'] : head).join('\n'), more: lines.length > EXCERPT_LINES };
};

/** The most the card's summary keeps: the CSS clamps it to two lines, this keeps the DOM short. */
export const SUMMARY_CHARS = 280;

/** What a subagent returned as one plain line for the collapsed card: code blocks dropped, markdown marks stripped to
 *  their text (a code span is just its text, a link is its label), cut at the end of a sentence when one fits. */
export const summaryOf = (text: string): string => {
  const plain = text
    .slice(0, 20_000)
    .replace(/```[\s\S]*?(```|$)/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/<[^>]+>/g, ' ')
    .split('\n')
    .map((line) =>
      line
        .replace(/^\s*(#{1,6}|>|[-*+]|\d+[.)])\s+/, '')
        .replace(/^[\s:|-]+$/, '')
        .replace(/\|/g, ' '),
    )
    .join(' ')
    .replace(/(\*\*|__|~~|`)/g, '')
    .replace(/(^|\s)[*_]+(\S)/g, '$1$2')
    .replace(/(\S)[*_]+(?=\s|$)/g, '$1')
    .replace(/\s+/g, ' ')
    .trim();
  if (plain.length <= SUMMARY_CHARS) return plain;
  const head = plain.slice(0, SUMMARY_CHARS);
  const stop = Math.max(head.lastIndexOf('. '), head.lastIndexOf('! '), head.lastIndexOf('? '));
  return stop > SUMMARY_CHARS / 2 ? head.slice(0, stop + 1) : `${head.trimEnd()}…`;
};
