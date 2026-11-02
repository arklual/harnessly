// A Word file read for the pane and edited in place: the file's own XML stays the truth, an edit changes its text pieces
// and everything else in the file goes back as it came. What is drawn comes as plain values, so nothing in the file reaches
// the page as markup.
import { unzipSync, type Unzipped } from 'fflate';
import { spliceText } from '@/next/features/pane/text-splice';
import {
  replaceRange as replaceIn,
  type IParagraphs,
  type ITextPlace,
} from '@/next/features/pane/text-editing';
import { packParts, readPart, readRelations, safeHref } from '@/next/features/pane/office-parts';

const W = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main';
const R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const XML = 'http://www.w3.org/XML/1998/namespace';
const DOCUMENT = 'word/document.xml';
/** Twentieths of a point to CSS pixels; half-points of a font to points. */
const TWIP_PX = 1 / 15;
const EMU_PX = 1 / 9525;
/** A4, for a file that does not say. */
const DEFAULT_PAGE = 11906;

export interface IDocxLook {
  bold: boolean;
  italic: boolean;
  underline: boolean;
  strike: boolean;
  color: string | null;
  fill: string | null;
  /** In points; null: the page's own. */
  size: number | null;
  /** A family name of letters, digits, spaces and dashes; null: the page's own. */
  font: string | null;
}

export type TDocxRun =
  | { kind: 'text'; text: string; look: number; href: string | null }
  | { kind: 'tab' }
  | { kind: 'break' }
  | { kind: 'image'; path: string; width: number; height: number };

export interface IDocxParagraph {
  type: 'p';
  /** Its place among the file's paragraphs, in reading order. */
  index: number;
  /** 0: plain, 1..6: a heading of that level, -1: the title. */
  heading: number;
  align: 'left' | 'center' | 'right' | 'justify' | null;
  indent: number;
  before: number;
  after: number;
  marker: string | null;
  /** Holds a picture or a drawing: it is shown and not edited, a drawing has text of its own that this reading does not see. */
  frozen: boolean;
  runs: TDocxRun[];
}

export interface IDocxCell {
  span: number;
  fill: string | null;
  blocks: TDocxBlock[];
}

export interface IDocxTable {
  type: 'table';
  rows: IDocxCell[][];
}

export type TDocxBlock = IDocxParagraph | IDocxTable;

export interface IDocxBook {
  zip: Unzipped;
  doc: Document;
  /** The file's paragraphs in reading order; an edit names one by its place here. */
  paragraphs: Element[];
  blocks: TDocxBlock[];
  looks: IDocxLook[];
  /** The page's width in pixels, as the file sets it. */
  pageWidth: number;
  /** Reads `doc` again into `paragraphs` and `blocks`, after a change in the paragraphs' order. */
  walk: () => void;
}

const childrenOf = (element: Element, name?: string): Element[] =>
  [...element.children].filter((child) => child.namespaceURI === W && (!name || child.localName === name));

const child = (element: Element | null | undefined, name: string): Element | null =>
  element ? (childrenOf(element, name)[0] ?? null) : null;

const value = (element: Element | null, name = 'val'): string | null =>
  element ? element.getAttributeNS(W, name) : null;

const isOn = (element: Element | null): boolean | undefined => {
  if (!element) return undefined;
  const raw = value(element);
  return raw !== 'false' && raw !== '0' && raw !== 'none';
};

const hex = (raw: string | null): string | null =>
  raw && /^[0-9a-f]{6}$/i.test(raw) ? raw.toLowerCase() : null;

const HIGHLIGHTS: Readonly<Record<string, string>> = {
  yellow: 'ffff00',
  green: '00ff00',
  cyan: '00ffff',
  magenta: 'ff00ff',
  blue: '0000ff',
  red: 'ff0000',
  darkBlue: '000080',
  darkCyan: '008080',
  darkGreen: '008000',
  darkMagenta: '800080',
  darkRed: '800000',
  darkYellow: '808000',
  darkGray: '808080',
  lightGray: 'c0c0c0',
  black: '000000',
};

type TRunProps = Partial<IDocxLook>;

const readRunProps = (rPr: Element | null): TRunProps => {
  if (!rPr) return {};
  const out: TRunProps = {};
  const bold = isOn(child(rPr, 'b'));
  const italic = isOn(child(rPr, 'i'));
  const strike = isOn(child(rPr, 'strike'));
  const underline = isOn(child(rPr, 'u'));
  if (bold !== undefined) out.bold = bold;
  if (italic !== undefined) out.italic = italic;
  if (strike !== undefined) out.strike = strike;
  if (underline !== undefined) out.underline = underline;
  const color = hex(value(child(rPr, 'color')));
  if (color) out.color = color;
  const size = Number(value(child(rPr, 'sz')));
  if (size > 0) out.size = size / 2;
  const highlight = HIGHLIGHTS[value(child(rPr, 'highlight')) ?? ''];
  const shade = hex(value(child(rPr, 'shd'), 'fill'));
  if (highlight || shade) out.fill = highlight ?? shade;
  const fonts = child(rPr, 'rFonts');
  const font = value(fonts, 'hAnsi') ?? value(fonts, 'ascii') ?? value(fonts, 'cs');
  if (font && /^[\p{L}\p{N} -]+$/u.test(font)) out.font = font;
  return out;
};

interface IStyle {
  name: string;
  basedOn: string | null;
  rPr: Element | null;
  pPr: Element | null;
}

const readStyles = (doc: Document | null) => {
  const styles = new Map<string, IStyle>();
  let defaults: TRunProps = {};
  let spacing: Element | null = null;
  let plain: string | null = null;
  if (!doc) return { styles, defaults, spacing, plain };
  const root = doc.documentElement;
  const docDefaults = child(root, 'docDefaults');
  defaults = readRunProps(child(child(docDefaults, 'rPrDefault'), 'rPr'));
  spacing = child(child(child(docDefaults, 'pPrDefault'), 'pPr'), 'spacing');
  for (const style of childrenOf(root, 'style')) {
    const id = value(style, 'styleId');
    if (!id) continue;
    if (value(style, 'default') === '1' && value(style, 'type') === 'paragraph') plain = id;
    styles.set(id, {
      name: value(child(style, 'name')) ?? id,
      basedOn: value(child(style, 'basedOn')),
      rPr: child(style, 'rPr'),
      pPr: child(style, 'pPr'),
    });
  }
  return { styles, defaults, spacing, plain };
};

const chainOf = (styles: Map<string, IStyle>, id: string | null): IStyle[] => {
  const chain: IStyle[] = [];
  const seen = new Set<string>();
  for (let at = id; at && !seen.has(at);) {
    seen.add(at);
    const style = styles.get(at);
    if (!style) break;
    chain.unshift(style);
    at = style.basedOn;
  }
  return chain;
};

const ROMAN: readonly [number, string][] = [
  [1000, 'm'],
  [900, 'cm'],
  [500, 'd'],
  [400, 'cd'],
  [100, 'c'],
  [90, 'xc'],
  [50, 'l'],
  [40, 'xl'],
  [10, 'x'],
  [9, 'ix'],
  [5, 'v'],
  [4, 'iv'],
  [1, 'i'],
];

const roman = (n: number): string => {
  let out = '';
  let left = n;
  for (const [size, mark] of ROMAN)
    while (left >= size) {
      out += mark;
      left -= size;
    }
  return out;
};

const letter = (n: number): string => String.fromCharCode(97 + ((n - 1) % 26));

const counted = (n: number, format: string | null): string => {
  if (format === 'lowerLetter') return letter(n);
  if (format === 'upperLetter') return letter(n).toUpperCase();
  if (format === 'lowerRoman') return roman(n);
  if (format === 'upperRoman') return roman(n).toUpperCase();
  return String(n);
};

interface INumberLevel {
  format: string | null;
  text: string;
  left: number;
}

const readNumbering = (doc: Document | null) => {
  const abstract = new Map<string, Map<number, INumberLevel>>();
  const lists = new Map<string, Map<number, INumberLevel>>();
  if (!doc) return lists;
  for (const item of childrenOf(doc.documentElement, 'abstractNum')) {
    const levels = new Map<number, INumberLevel>();
    for (const level of childrenOf(item, 'lvl'))
      levels.set(Number(value(level, 'ilvl')), {
        format: value(child(level, 'numFmt')),
        text: value(child(level, 'lvlText')) ?? '',
        left: Number(value(child(child(level, 'pPr'), 'ind'), 'left') ?? 0) * TWIP_PX,
      });
    abstract.set(value(item, 'abstractNumId') ?? '', levels);
  }
  for (const item of childrenOf(doc.documentElement, 'num')) {
    const levels = abstract.get(value(child(item, 'abstractNumId')) ?? '');
    if (levels) lists.set(value(item, 'numId') ?? '', levels);
  }
  return lists;
};

/** What holds a paragraph's runs inside it: a run met through these is read, and edited. */
const WRAPPERS: ReadonlySet<string> = new Set([
  'ins',
  'smartTag',
  'sdt',
  'sdtContent',
  'fldSimple',
  'customXml',
]);
const HOLDERS: ReadonlySet<string> = new Set([...WRAPPERS, 'hyperlink']);

const isFrozen = (p: Element): boolean =>
  ['drawing', 'pict', 'object', 'AlternateContent'].some(
    (name) => p.getElementsByTagNameNS('*', name).length > 0,
  );

export const readDocx = (bytes: Uint8Array): IDocxBook => {
  const zip = unzipSync(bytes);
  const doc = readPart(zip, DOCUMENT);
  if (!doc) throw new Error('no document');
  const { styles, defaults, spacing, plain } = readStyles(readPart(zip, 'word/styles.xml'));
  const lists = readNumbering(readPart(zip, 'word/numbering.xml'));
  const relations = readRelations(zip, DOCUMENT);
  const looks: IDocxLook[] = [];
  const lookIds = new Map<string, number>();
  const paragraphs: Element[] = [];
  const counters = new Map<string, number>();

  const lookOf = (props: TRunProps): number => {
    const look: IDocxLook = {
      bold: props.bold ?? false,
      italic: props.italic ?? false,
      underline: props.underline ?? false,
      strike: props.strike ?? false,
      color: props.color ?? null,
      fill: props.fill ?? null,
      size: props.size ?? null,
      font: props.font ?? null,
    };
    const key = JSON.stringify(look);
    const found = lookIds.get(key);
    if (found !== undefined) return found;
    looks.push(look);
    lookIds.set(key, looks.length - 1);
    return looks.length - 1;
  };

  const readRun = (run: Element, base: TRunProps, href: string | null, out: TDocxRun[]) => {
    const rPr = child(run, 'rPr');
    const character = chainOf(styles, value(child(rPr, 'rStyle'))).map((style) => readRunProps(style.rPr));
    const look = lookOf(Object.assign({}, base, ...character, readRunProps(rPr)));
    for (const part of run.children) {
      if (part.localName === 't') out.push({ kind: 'text', text: part.textContent ?? '', look, href });
      else if (part.localName === 'tab') out.push({ kind: 'tab' });
      else if (part.localName === 'br') out.push({ kind: 'break' });
      else if (part.localName === 'drawing') {
        const blip = part.getElementsByTagNameNS('*', 'blip')[0];
        const extent = part.getElementsByTagNameNS('*', 'extent')[0];
        const target = relations.get(blip?.getAttributeNS(R, 'embed') ?? '')?.target;
        if (target)
          out.push({
            kind: 'image',
            path: target,
            width: Number(extent?.getAttribute('cx') ?? 0) * EMU_PX,
            height: Number(extent?.getAttribute('cy') ?? 0) * EMU_PX,
          });
      }
    }
  };

  const readInline = (parent: Element, base: TRunProps, href: string | null, out: TDocxRun[]) => {
    for (const item of childrenOf(parent)) {
      if (item.localName === 'r') readRun(item, base, href, out);
      else if (item.localName === 'hyperlink') {
        const target = relations.get(item.getAttributeNS(R, 'id') ?? '');
        readInline(item, base, target?.external ? safeHref(target.target) : null, out);
      } else if (WRAPPERS.has(item.localName)) readInline(item, base, href, out);
    }
  };

  const markerOf = (pPr: Element | null, chain: IStyle[]): { marker: string | null; left: number } => {
    const own = child(pPr, 'numPr');
    const inherited = [...chain].reverse().map((style) => child(style.pPr, 'numPr'));
    const numPr = own ?? inherited.find(Boolean) ?? null;
    const id = value(child(numPr, 'numId'));
    const level = Number(value(child(numPr, 'ilvl')) ?? 0);
    const levels = id ? lists.get(id) : undefined;
    const format = levels?.get(level);
    if (!id || !format) return { marker: null, left: 0 };
    if (format.format === 'bullet') return { marker: '•', left: format.left };
    const key = `${id}:${level}`;
    const now = (counters.get(key) ?? 0) + 1;
    counters.set(key, now);
    for (const known of counters.keys()) {
      const [list, deeper] = known.split(':');
      if (list === id && Number(deeper) > level) counters.delete(known);
    }
    const marker = format.text.replace(/%(\d)/g, (_, digit: string) => {
      const at = Number(digit) - 1;
      return counted(counters.get(`${id}:${at}`) ?? 1, levels?.get(at)?.format ?? null);
    });
    return { marker, left: format.left };
  };

  const readParagraph = (p: Element): IDocxParagraph => {
    const index = paragraphs.length;
    paragraphs.push(p);
    const pPr = child(p, 'pPr');
    const styleId = value(child(pPr, 'pStyle')) ?? plain;
    const chain = chainOf(styles, styleId);
    const base: TRunProps = Object.assign({}, defaults, ...chain.map((style) => readRunProps(style.rPr)));
    const name = chain.at(-1)?.name ?? '';
    const found = /^heading (\d)$/i.exec(name)?.[1];
    const heading = /^title$/i.test(name) ? -1 : Number(found ?? 0);
    const property = (key: string): Element | null =>
      child(pPr, key) ??
      [...chain]
        .reverse()
        .map((style) => child(style.pPr, key))
        .find(Boolean) ??
      null;
    const jc = value(property('jc'));
    const align = {
      left: 'left',
      start: 'left',
      center: 'center',
      right: 'right',
      end: 'right',
      both: 'justify',
    }[jc ?? ''];
    const ind = property('ind');
    const gap = property('spacing') ?? spacing;
    const { marker, left } = markerOf(pPr, chain);
    const runs: TDocxRun[] = [];
    readInline(p, base, null, runs);
    return {
      type: 'p',
      index,
      heading,
      align: (align as IDocxParagraph['align']) ?? null,
      indent: ind ? Number(value(ind, 'left') ?? 0) * TWIP_PX : left,
      before: Number(value(gap, 'before') ?? 0) * TWIP_PX,
      after: Number(value(gap, 'after') ?? 0) * TWIP_PX,
      marker,
      frozen: isFrozen(p),
      runs,
    };
  };

  const readBlocks = (parent: Element): TDocxBlock[] => {
    const blocks: TDocxBlock[] = [];
    for (const item of childrenOf(parent)) {
      if (item.localName === 'p') blocks.push(readParagraph(item));
      else if (item.localName === 'tbl')
        blocks.push({
          type: 'table',
          rows: childrenOf(item, 'tr').map((row) =>
            childrenOf(row, 'tc').map((cell) => {
              const tcPr = child(cell, 'tcPr');
              return {
                span: Number(value(child(tcPr, 'gridSpan')) ?? 1),
                fill: hex(value(child(tcPr, 'shd'), 'fill')),
                blocks: readBlocks(cell),
              };
            }),
          ),
        });
      else if (item.localName === 'sdt') {
        const content = child(item, 'sdtContent');
        if (content) blocks.push(...readBlocks(content));
      } else if (item.localName === 'customXml') blocks.push(...readBlocks(item));
    }
    return blocks;
  };

  const walk = () => {
    const body = child(doc.documentElement, 'body');
    if (!body) throw new Error('no body');
    paragraphs.length = 0;
    counters.clear();
    book.blocks = readBlocks(body);
    book.pageWidth = Number(value(child(child(body, 'sectPr'), 'pgSz'), 'w') ?? DEFAULT_PAGE) * TWIP_PX;
  };
  const book: IDocxBook = { zip, doc, paragraphs, blocks: [], looks, pageWidth: 0, walk };
  walk();
  return book;
};

/** A paragraph's text pieces, in reading order: what the person reads and edits. */
const piecesOf = (parent: Element): Element[] => {
  const out: Element[] = [];
  for (const item of childrenOf(parent)) {
    if (item.localName === 't') out.push(item);
    else if (item.localName === 'r' || HOLDERS.has(item.localName)) out.push(...piecesOf(item));
  }
  return out;
};

const textOf = (element: Element): string =>
  piecesOf(element)
    .map((piece) => piece.textContent ?? '')
    .join('');

/** A new run with an empty piece, in the given look. */
const newPiece = (doc: Document, look: Element | null): { run: Element; piece: Element } => {
  const run = doc.createElementNS(W, 'w:r');
  if (look) run.append(look.cloneNode(true));
  const piece = doc.createElementNS(W, 'w:t');
  run.append(piece);
  return { run, piece };
};

const keepSpaces = (pieces: readonly Element[]) => {
  for (const piece of pieces) piece.setAttributeNS(XML, 'xml:space', 'preserve');
};

export const paragraphText = (book: IDocxBook, index: number): string => textOf(book.paragraphs[index]);

export const setParagraphText = (book: IDocxBook, index: number, text: string): boolean => {
  const p = book.paragraphs[index];
  if (!p || textOf(p) === text) return false;
  const pieces = piecesOf(p);
  if (pieces.length === 0) {
    // In the look the paragraph's mark keeps for words to come.
    const { run, piece } = newPiece(book.doc, child(child(p, 'pPr'), 'rPr'));
    p.append(run);
    pieces.push(piece);
  }
  spliceText(pieces, text);
  keepSpaces(pieces);
  return true;
};

/** What stays with an element when it is cut in two: the properties, which both halves need. */
const PROPERTIES: ReadonlySet<string> = new Set([
  'pPr',
  'rPr',
  'sdtPr',
  'sdtEndPr',
  'smartTagPr',
  'customXmlPr',
]);

/** `element` keeps its first `offset` characters; the answer is a copy of it holding the rest. Tabs and marks stay on their side. */
const cutAfter = (element: Element, offset: number): Element => {
  const tail = element.cloneNode(false) as Element;
  let left = offset;
  for (const item of [...element.children]) {
    if (PROPERTIES.has(item.localName)) {
      tail.append(item.cloneNode(true));
      continue;
    }
    const size = item.localName === 't' ? (item.textContent ?? '').length : textOf(item).length;
    if (left > 0 && left >= size) {
      left -= size;
      continue;
    }
    if (left === 0) {
      tail.append(item);
      continue;
    }
    if (item.localName === 't') {
      const text = item.textContent ?? '';
      const rest = item.cloneNode(false) as Element;
      item.textContent = text.slice(0, left);
      rest.textContent = text.slice(left);
      keepSpaces([item, rest]);
      tail.append(rest);
    } else tail.append(cutAfter(item, left));
    left = 0;
  }
  return tail;
};

const lookOfRun = (run: Element | undefined): Element | null => (run ? child(run, 'rPr') : null);

/** Enter: what stands after `offset` becomes the next paragraph; an empty half keeps the look of the words beside it. */
export const splitParagraph = (book: IDocxBook, index: number, offset: number): void => {
  const p = book.paragraphs[index];
  const next = cutAfter(p, offset);
  p.after(next);
  book.paragraphs.splice(index + 1, 0, next);
  const lastHere = piecesOf(p).at(-1)?.parentElement ?? undefined;
  const firstThere = piecesOf(next)[0]?.parentElement ?? undefined;
  if (!firstThere && lastHere) next.append(newPiece(book.doc, lookOfRun(lastHere)).run);
  if (!lastHere && firstThere) p.append(newPiece(book.doc, lookOfRun(firstThere)).run);
};

/** Backspace at the start: the paragraph joins the one right before it, if one stands there. Answers where the join is. */
export const joinToPrevious = (book: IDocxBook, index: number): number | null => {
  const p = book.paragraphs[index];
  const previous = book.paragraphs[index - 1];
  if (!p || !previous || p.previousElementSibling !== previous) return null;
  if (isFrozen(p) || isFrozen(previous)) return null;
  const at = textOf(previous).length;
  for (const item of [...p.children]) if (item.localName !== 'pPr') previous.append(item);
  p.remove();
  book.paragraphs.splice(index, 1);
  return at;
};

/** The file's paragraphs as an edit takes them. */
export const docxParagraphs = (book: IDocxBook): IParagraphs => ({
  text: (index) => paragraphText(book, index),
  setText: (index, text) => setParagraphText(book, index, text),
  split: (index, offset) => splitParagraph(book, index, offset),
  join: (index) => joinToPrevious(book, index),
  joinable: (index) => {
    const here = book.paragraphs[index];
    const next = book.paragraphs[index + 1];
    return Boolean(
      here && !isFrozen(here) && (next ? next.previousElementSibling === here && !isFrozen(next) : true),
    );
  },
});

export const replaceRange = (
  book: IDocxBook,
  from: ITextPlace,
  to: ITextPlace,
  text: string,
): ITextPlace | null => replaceIn(docxParagraphs(book), from, to, text);

/** The paragraphs' words: what a later file is compared with. */
export const docxSnapshot = (book: IDocxBook): readonly string[] => book.paragraphs.map(textOf);

/**
 * The person's edits, on top of a file that changed under them (`base` is what both started from): paragraph by paragraph.
 * False when one has nowhere to go: the paragraphs were added or taken away, or both changed the same one.
 */
export const mergeDocx = (base: readonly string[], mine: IDocxBook, theirs: IDocxBook): boolean => {
  if (mine.paragraphs.length !== base.length || theirs.paragraphs.length !== base.length) return false;
  const edits: [number, string][] = [];
  for (let at = 0; at < base.length; at += 1) {
    const text = textOf(mine.paragraphs[at]);
    if (text === base[at]) continue;
    const other = textOf(theirs.paragraphs[at]);
    if (other !== base[at] && other !== text) return false;
    edits.push([at, text]);
  }
  for (const [at, text] of edits) setParagraphText(theirs, at, text);
  return true;
};

/** The file again: only the document's own part changed. */
export const writeDocx = (book: IDocxBook): Uint8Array =>
  packParts(book.zip, new Map([[DOCUMENT, book.doc]]));
