// Where words picked in the chat's feed stand, kept so they can be found again: the feed draws only the turns near the
// screen (feed-window.ts) and redraws a turn as it grows, so a Range of the page goes stale. An anchor is the turn's row
// (its data-key) and the place of the words in the row's text; the words themselves tell a row whose text has changed
// (then they are looked for anywhere in the row).

export interface IFeedAnchor {
  /** The feed row the words are in: its data-key. */
  key: string;
  /** Where the words start in the row's text. */
  start: number;
  /** The words as the page had them (the row's text from start, so many characters). */
  raw: string;
}

const ROW = '.hx-turnwrap[data-key]';

const elementOf = (node: Node): Element | null =>
  node.nodeType === 1 ? (node as Element) : node.parentElement;

/** The row of the feed with this key, if it is drawn. */
const feedRow = (key: string, root: ParentNode = document): HTMLElement | null => {
  for (const row of root.querySelectorAll<HTMLElement>(ROW)) if (row.dataset.key === key) return row;
  return null;
};

/** Where the picked words stand; the words past the row they start in are cut off. Null outside a row. */
export const anchorOf = (range: Range): IFeedAnchor | null => {
  const row = elementOf(range.startContainer)?.closest<HTMLElement>(ROW);
  const key = row?.dataset.key;
  if (!row || !key) return null;
  const doc = row.ownerDocument;
  const words = range.cloneRange();
  if (!row.contains(range.endContainer)) words.setEnd(row, row.childNodes.length);
  const before = doc.createRange();
  before.setStart(row, 0);
  before.setEnd(words.startContainer, words.startOffset);
  const raw = words.toString();
  if (raw.trim() === '') return null;
  return { key, start: before.toString().length, raw };
};

/** The node and the offset in it of a character of the row's text. */
const pointAt = (row: HTMLElement, at: number, end: boolean): [Text, number] | null => {
  const walker = row.ownerDocument.createTreeWalker(row, NodeFilter.SHOW_TEXT);
  let passed = 0;
  for (let node = walker.nextNode() as Text | null; node; node = walker.nextNode() as Text | null) {
    const size = node.data.length;
    // The end of the words is the end of the node they finish in, not the start of the next one.
    if (at < passed + size || (end && at === passed + size)) return [node, at - passed];
    passed += size;
  }
  return null;
};

/** The words of an anchor in the page now: null when their row is not drawn or no longer has them. */
export const rangeOf = (anchor: IFeedAnchor, root: ParentNode = document): Range | null => {
  const row = feedRow(anchor.key, root);
  if (!row) return null;
  const text = row.textContent ?? '';
  const length = anchor.raw.length;
  let start = anchor.start;
  if (text.slice(start, start + length) !== anchor.raw) {
    start = text.indexOf(anchor.raw);
    if (start < 0) return null;
  }
  const from = pointAt(row, start, false);
  const to = pointAt(row, start + length, true);
  if (!from || !to) return null;
  const range = row.ownerDocument.createRange();
  range.setStart(from[0], from[1]);
  range.setEnd(to[0], to[1]);
  return range;
};

/** A range kept from an earlier look still holds its words. */
export const stillHolds = (range: Range, anchor: IFeedAnchor): boolean =>
  range.startContainer.isConnected && range.toString() === anchor.raw;
