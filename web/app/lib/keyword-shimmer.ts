/**
 * The word «ultracode» typed in a prompt opts that turn into Claude Code's dynamic workflows; like Claude Code
 * itself, the field shows it in shifting rainbow letters. The letters are coloured with the CSS Custom Highlight API —
 * nothing is inserted into the editable text, so the caret, undo and IME stay the browser's.
 */

export const SHIMMER_KEYWORD_RE = /\bultracode\b/gi;
/** One highlight per colour of the rainbow: `::highlight(tw-ultra-<n>)` in the composer styles. */
export const SHIMMER_COLORS = 7;
export const SHIMMER_STEP_MS = 110;

const highlightName = (index: number): string => `tw-ultra-${index}`;

/** Offsets of every letter of every keyword in the text, in order. */
export const keywordLetterOffsets = (text: string): number[] => {
  const offsets: number[] = [];
  for (const match of text.matchAll(SHIMMER_KEYWORD_RE)) {
    for (let i = 0; i < match[0].length; i += 1) offsets.push((match.index ?? 0) + i);
  }
  return offsets;
};

/** The colour of a letter at a step: neighbours differ by one, and the band moves one letter per step. */
export const shimmerColorOf = (letter: number, step: number): number =>
  (((letter - step) % SHIMMER_COLORS) + SHIMMER_COLORS) % SHIMMER_COLORS;

interface ITextPosition {
  node: Text;
  offset: number;
}

/** Text nodes of the element with their start in its text, in document order. */
const textNodesOf = (root: HTMLElement): { node: Text; start: number }[] => {
  const nodes: { node: Text; start: number }[] = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let start = 0;
  for (let current = walker.nextNode(); current; current = walker.nextNode()) {
    const node = current as Text;
    nodes.push({ node, start });
    start += node.data.length;
  }
  return nodes;
};

const positionOf = (nodes: { node: Text; start: number }[], offset: number): ITextPosition | null => {
  for (const { node, start } of nodes) {
    if (offset < start + node.data.length) return { node, offset: offset - start };
  }
  return null;
};

const highlightsSupported = (): boolean =>
  typeof CSS !== 'undefined' && 'highlights' in CSS && typeof Highlight !== 'undefined';

/** The colour highlights are shared by every field on the page (a `::highlight` name is global); each field adds and takes back its own ranges. */
let shared: { highlights: Highlight[]; users: number } | null = null;

const acquire = (): Highlight[] => {
  if (!shared) {
    const highlights = Array.from({ length: SHIMMER_COLORS }, () => new Highlight());
    highlights.forEach((highlight, index) => CSS.highlights.set(highlightName(index), highlight));
    shared = { highlights, users: 0 };
  }
  shared.users += 1;
  return shared.highlights;
};

const release = () => {
  if (!shared) return;
  shared.users -= 1;
  if (shared.users > 0) return;
  shared.highlights.forEach((_, index) => CSS.highlights.delete(highlightName(index)));
  shared = null;
};

/**
 * Colours the keyword letters inside `root` and keeps them moving while any are there. Returns a stop function.
 * Without the Highlight API (older browsers) it does nothing; with reduced motion the colours stay still.
 */
export const startKeywordShimmer = (root: HTMLElement): (() => void) => {
  if (!highlightsSupported()) return () => undefined;
  const highlights = acquire();
  const still =
    typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
  let step = 0;
  let timer: ReturnType<typeof setInterval> | null = null;
  let own: { range: Range; color: number }[] = [];

  const clearOwn = () => {
    own.forEach(({ range, color }) => highlights[color].delete(range));
    own = [];
  };

  const paint = () => {
    clearOwn();
    const nodes = textNodesOf(root);
    const text = nodes.map(({ node }) => node.data).join('');
    const offsets = keywordLetterOffsets(text);
    offsets.forEach((offset, letter) => {
      const position = positionOf(nodes, offset);
      if (!position) return;
      const range = document.createRange();
      range.setStart(position.node, position.offset);
      range.setEnd(position.node, position.offset + 1);
      const color = shimmerColorOf(letter, step);
      highlights[color].add(range);
      own.push({ range, color });
    });
    return offsets.length > 0;
  };

  const tick = () => {
    step += 1;
    if (!paint() && timer) {
      clearInterval(timer);
      timer = null;
    }
  };

  const refresh = () => {
    const any = paint();
    if (any && !still && !timer) timer = setInterval(tick, SHIMMER_STEP_MS);
  };

  const observer = new MutationObserver(refresh);
  observer.observe(root, { characterData: true, childList: true, subtree: true });
  refresh();

  return () => {
    observer.disconnect();
    if (timer) clearInterval(timer);
    clearOwn();
    release();
  };
};
