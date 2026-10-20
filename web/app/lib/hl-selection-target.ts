import { buildQuote, type ITextQuote, type ITextSource } from '@/lib/hl-text-annotation';

/** Marks the text of a message that can be quoted; the value says whose message it is. */
export const ANNOTATABLE_ATTRIBUTE = 'data-annotatable';
const ANNOTATABLE = `[${ANNOTATABLE_ATTRIBUTE}]`;
export const ANNOTATE_MESSAGE_EVENT = 'hl:annotate-message';

const SKIPPED = 'pre, input, textarea, select, button, [contenteditable]:not([contenteditable="false"])';
const BLOCK = 'p, li, h1, h2, h3, h4, h5, h6, blockquote, td, th';

export interface ISelectionRect {
  left: number;
  right: number;
  firstTop: number;
  lastBottom: number;
}

export interface ISelectionTarget {
  quote: ITextQuote;
  source: ITextSource;
  rect: ISelectionRect;
  /** The fragment, kept to paint it while the browser selection is gone. */
  range: Range;
  /** The panel was asked for by a button, not by a selection. */
  fromButton: boolean;
}

const elementOf = (node: Node): Element | null => (node instanceof Element ? node : node.parentElement);

const sourceOf = (root: Element): ITextSource | null => {
  const entryId = root.closest<HTMLElement>('[data-entry-id]')?.dataset.entryId;
  const role = root.getAttribute(ANNOTATABLE_ATTRIBUTE);
  return entryId && (role === 'agent' || role === 'user') ? { entryId, role } : null;
};

const blockWithin = (node: Node, root: Element): Element => {
  const block = elementOf(node)?.closest(BLOCK);
  return block && root.contains(block) ? block : root;
};

const rectOf = (range: Range): ISelectionRect => {
  const rects = Array.from(range.getClientRects()).filter((rect) => rect.width > 0 || rect.height > 0);
  const first = rects[0] ?? range.getBoundingClientRect();
  const last = rects.at(-1) ?? first;
  const bounds = range.getBoundingClientRect();
  return { left: bounds.left, right: bounds.right, firstTop: first.top, lastBottom: last.bottom };
};

const textAround = (range: Range, root: Element): { before: string; after: string } => {
  const startBlock = blockWithin(range.startContainer, root);
  const endBlock = blockWithin(range.endContainer, root);
  const before = document.createRange();
  before.setStart(startBlock, 0);
  before.setEnd(range.startContainer, range.startOffset);
  const after = document.createRange();
  after.setStart(range.endContainer, range.endOffset);
  after.setEnd(endBlock, endBlock.childNodes.length);
  return { before: before.toString(), after: after.toString() };
};

/**
 * The fragment the person selected, when it lies inside one quotable message of `scope` and not in code or a field;
 * null otherwise, so a selection anywhere else is left alone.
 */
export const readSelectionTarget = (scope: Element, selection: Selection | null): ISelectionTarget | null => {
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null;
  const range = selection.getRangeAt(0);
  const start = elementOf(range.startContainer);
  const end = elementOf(range.endContainer);
  const root = start?.closest(ANNOTATABLE);
  if (!start || !end || !root || root !== end.closest(ANNOTATABLE) || !scope.contains(root)) return null;
  if (start.closest(SKIPPED) || end.closest(SKIPPED)) return null;
  const source = sourceOf(root);
  const quote = source && buildQuote({ selected: selection.toString(), ...textAround(range, root) });
  if (!source || !quote) return null;
  return { quote, source, rect: rectOf(range), range: range.cloneRange(), fromButton: false };
};

/** The whole message, for the button in its actions: whoever cannot select with a long tap still has a way in. */
export const readMessageTarget = (root: Element): ISelectionTarget | null => {
  const source = sourceOf(root);
  const quote =
    source &&
    buildQuote({
      selected: root instanceof HTMLElement ? root.innerText : (root.textContent ?? ''),
      before: '',
      after: '',
    });
  if (!source || !quote) return null;
  const range = document.createRange();
  range.selectNodeContents(root);
  const bounds = root.getBoundingClientRect();
  return {
    quote,
    source,
    range,
    fromButton: true,
    rect: { left: bounds.left, right: bounds.right, firstTop: bounds.bottom, lastBottom: bounds.bottom },
  };
};

export const messageRootOf = (node: EventTarget | null): Element | null =>
  node instanceof Element ? (node.closest('.agent-message')?.querySelector(ANNOTATABLE) ?? null) : null;

export const targetKey = ({ source, quote }: ISelectionTarget): string =>
  `${source.entryId}\n${quote.before}\n${quote.quote}`;
