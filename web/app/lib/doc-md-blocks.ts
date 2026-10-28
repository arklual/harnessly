// The blocks of a Markdown document with their place in the source, so a block the person double-clicked on the
// rendered page can be edited as Markdown. The page tells the block's kind, its place among the blocks of that kind and its
// visible text; the source's block of the same kind and place is taken when it reads the same (or when the page and the
// source have as many tables, code blocks or headings of that level), else the one block of that kind with exactly that
// text. Nothing is guessed: a block not found so is not offered, the whole text is. Only the block's own lines are replaced.

export const MD_KIND = {
  h1: 'h1',
  h2: 'h2',
  h3: 'h3',
  h4: 'h4',
  h5: 'h5',
  h6: 'h6',
  para: 'para',
  item: 'item',
  table: 'table',
  code: 'code',
  quote: 'quote',
  rule: 'rule',
} as const;
export type TMdKind = (typeof MD_KIND)[keyof typeof MD_KIND];

export interface IMdBlock {
  /** Offsets in the source: `source.slice(start, end)` is the block, without its trailing line break. */
  start: number;
  end: number;
  kind: TMdKind;
  /** What the block reads as on the page: no marks, whitespace collapsed, lower case. Empty for a rule. */
  text: string;
}

const FENCE = /^\s{0,3}(`{3,}|~{3,})/;
const HEADING = /^\s{0,3}#{1,6}(\s|$)/;
const ITEM = /^\s*(?:[-*+]|\d{1,9}[.)])\s+/;
const RULE = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/;
const TABLE = /^\s*\|/;
const BLANK = /^\s*$/;
const QUOTE = /^\s{0,3}>/;
const SETEXT = /^\s{0,3}(=+|-+)\s*$/;
const HEADING_KINDS: readonly TMdKind[] = [
  MD_KIND.h1,
  MD_KIND.h2,
  MD_KIND.h3,
  MD_KIND.h4,
  MD_KIND.h5,
  MD_KIND.h6,
];

/** The visible text of Markdown, as the browser's textContent of its HTML reads, normalized for comparison. */
export const plainText = (markdown: string): string =>
  markdown
    .split('\n')
    .filter((line) => !FENCE.test(line) && !/^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$/.test(line))
    .map((line) =>
      line
        .replace(/^\s{0,3}#{1,6}\s+/, '')
        .replace(/\s+#+\s*$/, '')
        .replace(/^\s*(?:>\s?)+/, '')
        .replace(ITEM, '')
        .replace(/^\[[ xX]\]\s+/, ''),
    )
    .join(' ')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]*)\]\[[^\]]*\]/g, '$1')
    .replace(/<[^>]+>/g, ' ')
    .replace(/\|/g, ' ')
    .replace(/(\*\*|__|~~|`+)/g, '')
    .replace(/(^|[\s(])[*_](?=\S)|(?<=\S)[*_](?=$|[\s.,;:!?)])/g, '$1')
    .replace(/\\([\\`*_{}[\]()#+\-.!|>~])/g, '$1')
    .replace(/&nbsp;/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .toLowerCase();

/** The page's text of a block, normalized the same way. */
export const normalizeShown = (text: string): string => text.replace(/\s+/g, ' ').trim().toLowerCase();

interface ILine {
  text: string;
  start: number;
  end: number;
}

const linesOf = (source: string): ILine[] => {
  const lines: ILine[] = [];
  let at = 0;
  for (const text of source.split('\n')) {
    lines.push({ text, start: at, end: at + text.length });
    at += text.length + 1;
  }
  return lines;
};

/** Paragraphs, headings, list items (each its own block), tables, quotes and fenced code, in order. */
export const mdBlocks = (source: string): IMdBlock[] => {
  const lines = linesOf(source);
  const blocks: IMdBlock[] = [];
  let open: { first: number; last: number; kind: 'para' | 'item' | 'table' } | null = null;

  const close = () => {
    if (!open) return;
    const start = lines[open.first].start;
    const end = lines[open.last].end;
    const first = lines[open.first].text;
    const last = lines[open.last].text;
    let kind: TMdKind = MD_KIND.para;
    let text = plainText(source.slice(start, end));
    if (open.kind === 'item') kind = MD_KIND.item;
    else if (open.kind === 'table') kind = MD_KIND.table;
    else if (QUOTE.test(first)) kind = MD_KIND.quote;
    else if (open.last > open.first && SETEXT.test(last)) {
      kind = last.trim().startsWith('=') ? MD_KIND.h1 : MD_KIND.h2;
      text = plainText(source.slice(start, lines[open.last - 1].end));
    }
    blocks.push({ start, end, kind, text });
    open = null;
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].text;
    const fence = FENCE.exec(line);
    if (fence) {
      close();
      const marker = fence[1];
      let last = i;
      for (let j = i + 1; j < lines.length; j++) {
        last = j;
        if (lines[j].text.trim().startsWith(marker)) break;
      }
      const start = lines[i].start;
      const end = lines[last].end;
      const code = lines
        .slice(i + 1, last)
        .map((l) => l.text)
        .join(' ');
      blocks.push({ start, end, kind: MD_KIND.code, text: normalizeShown(code) });
      i = last;
      continue;
    }
    if (BLANK.test(line)) {
      close();
      continue;
    }
    if (RULE.test(line) && !(open && open.kind === 'para' && /^\s*-+\s*$/.test(line))) {
      close();
      blocks.push({ start: lines[i].start, end: lines[i].end, kind: MD_KIND.rule, text: '' });
      continue;
    }
    if (HEADING.test(line)) {
      close();
      const level = /#{1,6}/.exec(line)?.[0].length ?? 1;
      blocks.push({
        start: lines[i].start,
        end: lines[i].end,
        kind: HEADING_KINDS[level - 1],
        text: plainText(line),
      });
      continue;
    }
    if (TABLE.test(line)) {
      if (open?.kind !== 'table') close();
      open = open ?? { first: i, last: i, kind: 'table' };
      open.last = i;
      continue;
    }
    if (ITEM.test(line)) {
      close();
      open = { first: i, last: i, kind: 'item' };
      continue;
    }
    if (open?.kind === 'table') close();
    if (open) open.last = i;
    else open = { first: i, last: i, kind: 'para' };
  }
  close();
  return blocks;
};

/** The kind of Markdown block a tag of the page is made from. */
const TAG_KIND: Record<string, TMdKind> = {
  table: MD_KIND.table,
  pre: MD_KIND.code,
  h1: MD_KIND.h1,
  h2: MD_KIND.h2,
  h3: MD_KIND.h3,
  h4: MD_KIND.h4,
  h5: MD_KIND.h5,
  h6: MD_KIND.h6,
  li: MD_KIND.item,
  blockquote: MD_KIND.quote,
  p: MD_KIND.para,
  dt: MD_KIND.para,
  dd: MD_KIND.para,
};

/** Kinds every renderer makes one element of, one for one: their count on the page is their count in the source. */
const STRUCTURAL = new Set<TMdKind>([MD_KIND.table, MD_KIND.code, ...HEADING_KINDS]);

/** A block of the page, as the bridge tells it: its tag, its text, its place among the page's blocks of its kind. */
export interface IShownBlock {
  tag: string;
  text: string;
  ordinal: number;
  total: number;
}

/** The source's block the page's block is made from; null when it cannot be told for sure. */
export const findBlock = (blocks: readonly IMdBlock[], shown: IShownBlock): IMdBlock | null => {
  const kind = TAG_KIND[shown.tag];
  const target = normalizeShown(shown.text);
  if (!kind || !target) return null;
  const same = blocks.filter((block) => block.kind === kind);
  const placed = same[shown.ordinal];
  if (placed && placed.text === target) return placed;
  if (placed && STRUCTURAL.has(kind) && same.length === shown.total) return placed;
  const exact = same.filter((block) => block.text === target);
  return exact.length === 1 ? exact[0] : null;
};

/** The source with one block replaced by new Markdown. */
export const replaceBlock = (source: string, block: Pick<IMdBlock, 'start' | 'end'>, next: string): string =>
  `${source.slice(0, block.start)}${next}${source.slice(block.end)}`;
