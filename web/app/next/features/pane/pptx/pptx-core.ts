// A PowerPoint file read for the pane and edited in place. The slide's XML stays the truth: an edit changes its text pieces and
// everything else goes back as it came. What is drawn comes as plain values, with what a slide takes from its layout, its master and
// the theme already worked out; nothing in the file reaches the page as markup.
import { unzipSync, type Unzipped } from 'fflate';
import { spliceText } from '@/next/features/pane/text-splice';
import {
  packParts,
  readPart,
  readRelations,
  safeHref,
  type IRelation,
} from '@/next/features/pane/office-parts';
import {
  replaceRange as replaceIn,
  type IParagraphs,
  type ITextPlace,
} from '@/next/features/pane/text-editing';

const A = 'http://schemas.openxmlformats.org/drawingml/2006/main';
const P = 'http://schemas.openxmlformats.org/presentationml/2006/main';
const R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const PRESENTATION = 'ppt/presentation.xml';
/** Of a slide's measures to CSS pixels. */
const EMU_PX = 1 / 9525;
const PT_PX = 4 / 3;
const DEFAULT_SIZE_PT = 18;
const LINE = 1.2;
const DEFAULT_INSET = { l: 91440, t: 45720, r: 91440, b: 45720 };

export type TPptxFill =
  | { kind: 'solid'; color: string }
  | { kind: 'gradient'; angle: number; stops: readonly { at: number; color: string }[] }
  | { kind: 'image'; path: string };

export interface IPptxBox {
  x: number;
  y: number;
  w: number;
  h: number;
  rotation: number;
  flipH: boolean;
  flipV: boolean;
}

export type TPptxRun =
  | {
      kind: 'text';
      text: string;
      size: number;
      bold: boolean;
      italic: boolean;
      underline: boolean;
      strike: boolean;
      color: string;
      font: string | null;
      href: string | null;
    }
  | { kind: 'break' };

export interface IPptxParagraph {
  /** Its number among the slide's paragraphs; null for what stands on the layout or the master, which is not edited here. */
  index: number | null;
  align: 'left' | 'center' | 'right' | 'justify' | null;
  left: number;
  firstLine: number;
  marker: string | null;
  before: number;
  after: number;
  lineHeight: number;
  /** The size of the words to come, for a paragraph with none yet. */
  size: number;
  runs: TPptxRun[];
}

export interface IPptxText {
  anchor: 'top' | 'middle' | 'bottom';
  insets: { l: number; t: number; r: number; b: number };
  wrap: boolean;
  paragraphs: IPptxParagraph[];
}

export interface IPptxLine {
  color: string;
  width: number;
  dashed: boolean;
}

export type TPptxShape =
  | {
      kind: 'shape';
      name: string;
      box: IPptxBox;
      geometry: string;
      radius: number;
      path: { d: string; w: number; h: number } | null;
      fill: TPptxFill | null;
      line: IPptxLine | null;
      text: IPptxText | null;
    }
  | {
      kind: 'picture';
      name: string;
      box: IPptxBox;
      path: string;
      crop: { l: number; t: number; r: number; b: number };
    }
  | { kind: 'line'; name: string; box: IPptxBox; line: IPptxLine }
  | {
      kind: 'table';
      name: string;
      box: IPptxBox;
      columns: readonly number[];
      rows: readonly {
        height: number;
        cells: readonly {
          span: number;
          rowSpan: number;
          hidden: boolean;
          fill: TPptxFill | null;
          text: IPptxText;
        }[];
      }[];
    }
  /** A chart or a diagram: this reading does not draw it, and says so in its place. */
  | { kind: 'other'; name: string; box: IPptxBox; what: 'chart' | 'diagram' | 'object' };

export interface IPptxDrawn {
  background: TPptxFill | null;
  shapes: readonly TPptxShape[];
}

interface IPart {
  path: string;
  doc: Document;
  relations: Map<string, IRelation>;
}

interface IMaster extends IPart {
  theme: ITheme;
  colorMap: Map<string, string>;
}

interface ILayout extends IPart {
  master: IMaster;
}

export interface IPptxSlide extends IPart {
  layout: ILayout | null;
  /** Its paragraphs in reading order; an edit names one by its place here. */
  paragraphs: Element[];
}

interface ITheme {
  colors: Map<string, string>;
  major: string | null;
  minor: string | null;
}

export interface IPptxBook {
  zip: Unzipped;
  width: number;
  height: number;
  slides: IPptxSlide[];
  defaults: Element | null;
  /** The parts an edit touched: the only ones written anew. */
  changed: Map<string, Document>;
}

const inA = (element: Element | null | undefined, name: string): Element | null =>
  element ? ([...element.children].find((c) => c.localName === name && c.namespaceURI === A) ?? null) : null;

const inP = (element: Element | null | undefined, name: string): Element | null =>
  element ? ([...element.children].find((c) => c.localName === name && c.namespaceURI === P) ?? null) : null;

const num = (element: Element | null, name: string): number | null => {
  const raw = element?.getAttribute(name);
  if (raw === null || raw === undefined || raw === '') return null;
  const value = Number(raw);
  return Number.isFinite(value) ? value : null;
};

const SAFE_FONT = /^[\p{L}\p{N} -]+$/u;

// Colours.
const toHsl = (hex: string): [number, number, number] => {
  const [r, g, b] = [0, 2, 4].map((at) => parseInt(hex.slice(at, at + 2), 16) / 255);
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  if (max === min) return [0, 0, l];
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  const raw = max === r ? (g - b) / d + (g < b ? 6 : 0) : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return [raw / 6, s, l];
};

const channel = (p: number, q: number, t0: number): number => {
  const t = (t0 + 1) % 1;
  if (t < 1 / 6) return p + (q - p) * 6 * t;
  if (t < 1 / 2) return q;
  if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
  return p;
};

const toHex = (h: number, s: number, l: number): string => {
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const values = s === 0 ? [l, l, l] : [channel(p, q, h + 1 / 3), channel(p, q, h), channel(p, q, h - 1 / 3)];
  return values
    .map((v) =>
      Math.round(Math.min(1, Math.max(0, v)) * 255)
        .toString(16)
        .padStart(2, '0'),
    )
    .join('');
};

interface IColorScope {
  master: IMaster | null;
  /** What `phClr` means: the colour of the style that holds it. */
  placeholder?: string | null;
}

const PRESET_COLORS: Readonly<Record<string, string>> = {
  black: '000000',
  white: 'ffffff',
  red: 'ff0000',
  green: '008000',
  blue: '0000ff',
  yellow: 'ffff00',
  gray: '808080',
};

const baseColor = (element: Element, scope: IColorScope): string | null => {
  const raw = element.getAttribute('val') ?? '';
  if (element.localName === 'srgbClr') return /^[0-9a-f]{6}$/i.test(raw) ? raw.toLowerCase() : null;
  if (element.localName === 'sysClr') {
    const last = element.getAttribute('lastClr') ?? '';
    return /^[0-9a-f]{6}$/i.test(last) ? last.toLowerCase() : null;
  }
  if (element.localName === 'prstClr') return PRESET_COLORS[raw] ?? null;
  if (element.localName === 'schemeClr') {
    if (raw === 'phClr') return scope.placeholder ?? null;
    const named = scope.master?.colorMap.get(raw) ?? raw;
    return scope.master?.theme.colors.get(named) ?? null;
  }
  return null;
};

const COLOR_TAGS: ReadonlySet<string> = new Set(['srgbClr', 'schemeClr', 'sysClr', 'prstClr']);

/** The colour held right inside this element, with its changes; as #rrggbb, or rgb() when it is see-through. */
const colorIn = (holder: Element | null, scope: IColorScope): string | null => {
  const element = holder ? ([...holder.children].find((c) => COLOR_TAGS.has(c.localName)) ?? null) : null;
  if (!element) return null;
  const base = baseColor(element, scope);
  if (!base) return null;
  const [h, startS, startL] = toHsl(base);
  let s = startS;
  let l = startL;
  let alpha = 1;
  for (const change of element.children) {
    const value = num(change, 'val');
    if (value === null) continue;
    const part = value / 100000;
    if (change.localName === 'lumMod') l *= part;
    else if (change.localName === 'lumOff') l += part;
    else if (change.localName === 'satMod') s *= part;
    else if (change.localName === 'tint') l = l * part + (1 - part);
    else if (change.localName === 'shade') l *= part;
    else if (change.localName === 'alpha') alpha = part;
  }
  const hex = toHex(h, s, l);
  if (alpha >= 1) return `#${hex}`;
  const [r, g, b] = [0, 2, 4].map((at) => parseInt(hex.slice(at, at + 2), 16));
  return `rgb(${r} ${g} ${b} / ${Math.round(alpha * 100)}%)`;
};

const hexOf = (color: string | null): string | null => (color?.startsWith('#') ? color.slice(1) : null);

const blipPath = (holder: Element | null, part: IPart): string | null => {
  const blip = inA(holder, 'blip');
  const id = blip?.getAttributeNS(R, 'embed');
  const relation = id ? part.relations.get(id) : null;
  return relation && !relation.external ? relation.target : null;
};

/** The fill right inside this element: solid, gradient, picture; null when it has none, and 'none' when it says to have none. */
const fillIn = (holder: Element | null, part: IPart, scope: IColorScope): TPptxFill | null | 'none' => {
  if (!holder) return null;
  if (inA(holder, 'noFill')) return 'none';
  const solid = inA(holder, 'solidFill');
  if (solid) {
    const color = colorIn(solid, scope);
    return color ? { kind: 'solid', color } : null;
  }
  const gradient = inA(holder, 'gradFill');
  if (gradient) {
    const stops = [...(inA(gradient, 'gsLst')?.children ?? [])]
      .map((stop) => ({ at: (num(stop, 'pos') ?? 0) / 1000, color: colorIn(stop, scope) }))
      .filter((stop): stop is { at: number; color: string } => stop.color !== null)
      .sort((a, b) => a.at - b.at);
    if (stops.length === 0) return null;
    return { kind: 'gradient', angle: (num(inA(gradient, 'lin'), 'ang') ?? 0) / 60000, stops };
  }
  const image =
    inA(holder, 'blipFill') ?? [...holder.children].find((c) => c.localName === 'blipFill') ?? null;
  if (image) {
    const path = blipPath(image, part);
    return path ? { kind: 'image', path } : null;
  }
  return null;
};

const readTheme = (zip: Unzipped, master: Pick<IPart, 'relations'>): ITheme => {
  const colors = new Map<string, string>();
  const relation = [...master.relations.values()].find((rel) => rel.type.endsWith('/theme'));
  const doc = relation ? readPart(zip, relation.target) : null;
  const scheme = doc?.getElementsByTagNameNS(A, 'clrScheme')[0];
  for (const entry of scheme?.children ?? []) {
    const color = entry.children[0];
    const value = color?.getAttribute('lastClr') ?? color?.getAttribute('val') ?? '';
    if (/^[0-9a-f]{6}$/i.test(value)) colors.set(entry.localName, value.toLowerCase());
  }
  const fontOf = (name: string): string | null => {
    const family = doc?.getElementsByTagNameNS(A, name)[0];
    const face = inA(family, 'latin')?.getAttribute('typeface') ?? '';
    return SAFE_FONT.test(face) ? face : null;
  };
  return { colors, major: fontOf('majorFont'), minor: fontOf('minorFont') };
};

const treeOf = (part: IPart): Element | null => inP(inP(part.doc.documentElement, 'cSld'), 'spTree');

const placeholderOf = (shape: Element): Element | null => {
  const holder = [...shape.children].find((c) => /^nv\w+Pr$/.test(c.localName));
  return inP(inP(holder, 'nvPr'), 'ph');
};

const nameOf = (shape: Element): string => {
  const holder = [...shape.children].find((c) => /^nv\w+Pr$/.test(c.localName));
  return inP(holder, 'cNvPr')?.getAttribute('name') ?? '';
};

const shapesIn = (part: IPart): Element[] =>
  [...(treeOf(part)?.children ?? [])].filter((c) => c.localName === 'sp');

/** The shape on a layout or a master that a placeholder takes after. */
const findPlaceholder = (part: IPart | null, ph: Element, byType: boolean): Element | null => {
  if (!part) return null;
  const idx = ph.getAttribute('idx');
  const type = ph.getAttribute('type') ?? 'obj';
  const shapes = shapesIn(part);
  if (!byType && idx !== null) {
    const same = shapes.find((shape) => placeholderOf(shape)?.getAttribute('idx') === idx);
    if (same) return same;
  }
  const kinds = (t: string) => (t === 'ctrTitle' ? 'title' : t === 'subTitle' || t === 'obj' ? 'body' : t);
  return (
    shapes.find((shape) => {
      const other = placeholderOf(shape);
      return other && kinds(other.getAttribute('type') ?? 'obj') === kinds(type);
    }) ?? null
  );
};

const boxOfXfrm = (xfrm: Element): IPptxBox | null => {
  const off = inA(xfrm, 'off');
  const ext = inA(xfrm, 'ext');
  if (!off || !ext) return null;
  return {
    x: (num(off, 'x') ?? 0) * EMU_PX,
    y: (num(off, 'y') ?? 0) * EMU_PX,
    w: (num(ext, 'cx') ?? 0) * EMU_PX,
    h: (num(ext, 'cy') ?? 0) * EMU_PX,
    rotation: (num(xfrm, 'rot') ?? 0) / 60000,
    flipH: xfrm.getAttribute('flipH') === '1',
    flipV: xfrm.getAttribute('flipV') === '1',
  };
};

/** Child coordinates of a group, into the slide's. */
type TMap = (box: IPptxBox) => IPptxBox;
const SAME: TMap = (box) => box;

const groupMap = (group: Element, outer: TMap): TMap => {
  const xfrm = inA(inP(group, 'grpSpPr'), 'xfrm');
  const off = inA(xfrm, 'off');
  const ext = inA(xfrm, 'ext');
  const chOff = inA(xfrm, 'chOff');
  const chExt = inA(xfrm, 'chExt');
  if (!off || !ext || !chOff || !chExt) return outer;
  const sx = (num(ext, 'cx') ?? 1) / (num(chExt, 'cx') || 1);
  const sy = (num(ext, 'cy') ?? 1) / (num(chExt, 'cy') || 1);
  const ox = (num(off, 'x') ?? 0) * EMU_PX;
  const oy = (num(off, 'y') ?? 0) * EMU_PX;
  const cx = (num(chOff, 'x') ?? 0) * EMU_PX;
  const cy = (num(chOff, 'y') ?? 0) * EMU_PX;
  return (box) =>
    outer({
      ...box,
      x: ox + (box.x - cx) * sx,
      y: oy + (box.y - cy) * sy,
      w: box.w * sx,
      h: box.h * sy,
    });
};

const pathOf = (custom: Element): { d: string; w: number; h: number } | null => {
  const path = inA(inA(custom, 'pathLst'), 'path');
  if (!path) return null;
  const point = (element: Element | undefined) =>
    element ? `${num(element, 'x') ?? 0} ${num(element, 'y') ?? 0}` : '0 0';
  const commands: string[] = [];
  for (const step of path.children) {
    const points = [...step.children];
    if (step.localName === 'moveTo') commands.push(`M${point(points[0])}`);
    else if (step.localName === 'lnTo') commands.push(`L${point(points[0])}`);
    else if (step.localName === 'cubicBezTo')
      commands.push(`C${point(points[0])} ${point(points[1])} ${point(points[2])}`);
    else if (step.localName === 'quadBezTo') commands.push(`Q${point(points[0])} ${point(points[1])}`);
    else if (step.localName === 'close') commands.push('Z');
    // An arc has no plain form in these terms: its end stands in, the shape keeps its outline nearly.
  }
  const w = num(path, 'w');
  const h = num(path, 'h');
  return commands.length > 0 && w && h ? { d: commands.join(' '), w, h } : null;
};

const lineOf = (
  ln: Element | null,
  part: IPart,
  scope: IColorScope,
  fallback: IPptxLine | null,
): IPptxLine | null => {
  if (!ln) return fallback;
  if (inA(ln, 'noFill')) return null;
  const fill = fillIn(ln, part, scope);
  const color = fill && fill !== 'none' && fill.kind === 'solid' ? fill.color : (fallback?.color ?? null);
  if (!color) return null;
  const width = num(ln, 'w');
  const dash = inA(ln, 'prstDash')?.getAttribute('val');
  return {
    color,
    width: width === null ? (fallback?.width ?? 1) : Math.max(0.5, width * EMU_PX),
    dashed: Boolean(dash && dash !== 'solid'),
  };
};

const WINGDINGS: Readonly<Record<string, string>> = {
  '§': '▪',
  ü: '✓',
  Ø: '➢',
  q: '❑',
  n: '■',
  l: '●',
  v: '❖',
  o: '□',
};

const romanOf = (n: number): string => {
  const marks: readonly [number, string][] = [
    [10, 'x'],
    [9, 'ix'],
    [5, 'v'],
    [4, 'iv'],
    [1, 'i'],
  ];
  let out = '';
  let left = n;
  for (const [size, mark] of marks)
    while (left >= size) {
      out += mark;
      left -= size;
    }
  return out;
};

const autoNumber = (type: string, n: number): string => {
  const letter = String.fromCharCode(97 + ((n - 1) % 26));
  const body = type.startsWith('alphaLc')
    ? letter
    : type.startsWith('alphaUc')
      ? letter.toUpperCase()
      : type.startsWith('romanLc')
        ? romanOf(n)
        : type.startsWith('romanUc')
          ? romanOf(n).toUpperCase()
          : String(n);
  if (type.endsWith('ParenBoth')) return `(${body})`;
  if (type.endsWith('ParenR')) return `${body})`;
  return `${body}.`;
};

const spacing = (holder: Element | null, size: number): number | null => {
  const points = num(inA(holder, 'spcPts'), 'val');
  if (points !== null) return (points / 100) * PT_PX;
  const percent = num(inA(holder, 'spcPct'), 'val');
  return percent !== null ? (percent / 100000) * size * LINE : null;
};

interface ITextScope {
  part: IPart;
  color: IColorScope;
  theme: ITheme | null;
  /** The level lists a shape's paragraphs take after, from the weakest. */
  lists: readonly (Element | null)[];
  fontColor: string | null;
  indexOf: ReadonlyMap<Element, number> | null;
  scale: number;
}

const levelOf = (list: Element | null, level: number): Element | null =>
  list ? inA(list, `lvl${level + 1}pPr`) : null;

const fontName = (face: string | null | undefined, theme: ITheme | null): string | null => {
  if (!face) return null;
  if (face.startsWith('+mj')) return theme?.major ?? null;
  if (face.startsWith('+mn')) return theme?.minor ?? null;
  return SAFE_FONT.test(face) ? face : null;
};

const readParagraphs = (body: Element, scope: ITextScope): IPptxParagraph[] => {
  const counters = new Map<number, number>();
  return [...body.children]
    .filter((c) => c.localName === 'p' && c.namespaceURI === A)
    .map((p) => {
      const pPr = inA(p, 'pPr');
      const level = num(pPr, 'lvl') ?? 0;
      // From the strongest: the paragraph itself, then the lists from the shape's own down to the file's defaults.
      const levels = [...scope.lists]
        .reverse()
        .map((list) => levelOf(list, level))
        .filter((item): item is Element => item !== null);
      const chain = pPr ? [pPr, ...levels] : levels;
      const firstOf = <T>(read: (element: Element) => T | null | undefined): T | null => {
        for (const element of chain) {
          const found = read(element);
          if (found !== null && found !== undefined) return found;
        }
        return null;
      };
      const runChain = levels.map((item) => inA(item, 'defRPr'));
      const runProp = <T>(
        rPr: Element | null,
        read: (element: Element) => T | null | undefined,
      ): T | null => {
        for (const element of [rPr, ...runChain]) {
          if (!element) continue;
          const found = read(element);
          if (found !== null && found !== undefined) return found;
        }
        return null;
      };
      const sizeOf = (rPr: Element | null) =>
        ((runProp(rPr, (e) => num(e, 'sz')) ?? DEFAULT_SIZE_PT * 100) / 100) * PT_PX * scope.scale;

      const runs: TPptxRun[] = [];
      for (const item of p.children) {
        if (item.localName === 'br') runs.push({ kind: 'break' });
        if (item.localName !== 'r' && item.localName !== 'fld') continue;
        const rPr = inA(item, 'rPr');
        const flag = (name: string) =>
          runProp(rPr, (e) => {
            const raw = e.getAttribute(name);
            return raw === null ? null : raw === '1' || raw === 'true';
          }) ?? false;
        const color =
          runProp(rPr, (e) => {
            const solid = inA(e, 'solidFill');
            return solid ? colorIn(solid, scope.color) : null;
          }) ??
          scope.fontColor ??
          '#000000';
        const link = inA(rPr, 'hlinkClick')?.getAttributeNS(R, 'id');
        const relation = link ? scope.part.relations.get(link) : null;
        const underlined = runProp(rPr, (e) => e.getAttribute('u'));
        const struck = runProp(rPr, (e) => e.getAttribute('strike'));
        runs.push({
          kind: 'text',
          text: inA(item, 't')?.textContent ?? '',
          size: sizeOf(rPr),
          bold: flag('b'),
          italic: flag('i'),
          underline: Boolean(underlined && underlined !== 'none'),
          strike: Boolean(struck && struck !== 'noStrike'),
          color,
          font: fontName(
            runProp(rPr, (e) => inA(e, 'latin')?.getAttribute('typeface')),
            scope.theme,
          ),
          href: relation?.external ? safeHref(relation.target) : null,
        });
      }
      const firstRun = runs.find((run) => run.kind === 'text');
      const size = firstRun?.kind === 'text' ? firstRun.size : sizeOf(inA(p, 'endParaRPr'));

      const char = firstOf((e) => {
        if (inA(e, 'buNone')) return '';
        return inA(e, 'buChar')?.getAttribute('char');
      });
      const face = firstOf((e) => inA(e, 'buFont')?.getAttribute('typeface'));
      const auto = firstOf((e) => {
        if (inA(e, 'buNone') || inA(e, 'buChar')) return '';
        return inA(e, 'buAutoNum')?.getAttribute('type');
      });
      let marker: string | null = null;
      if (auto) {
        const start = firstOf((e) => num(inA(e, 'buAutoNum'), 'startAt')) ?? 1;
        const n = counters.get(level) ?? start;
        counters.set(level, n + 1);
        marker = autoNumber(auto, n);
      } else {
        counters.delete(level);
        if (char) marker = (face && /wingdings/i.test(face) ? WINGDINGS[char] : char) ?? '•';
      }
      const hasWords = runs.some((run) => run.kind === 'text' && run.text !== '');
      const lineSpace = firstOf((e) => inA(e, 'lnSpc'));
      const percent = num(inA(lineSpace, 'spcPct'), 'val');
      const points = num(inA(lineSpace, 'spcPts'), 'val');
      const algn = firstOf((e) => e.getAttribute('algn'));
      const aligns: Readonly<Record<string, IPptxParagraph['align']>> = {
        l: 'left',
        ctr: 'center',
        r: 'right',
        just: 'justify',
      };
      return {
        index: scope.indexOf?.get(p) ?? null,
        align: aligns[algn ?? ''] ?? null,
        left: (firstOf((e) => num(e, 'marL')) ?? 0) * EMU_PX,
        firstLine: (firstOf((e) => num(e, 'indent')) ?? 0) * EMU_PX,
        // An empty paragraph shows no bullet, in the file's own program too.
        marker: hasWords ? marker : null,
        before:
          spacing(
            firstOf((e) => inA(e, 'spcBef')),
            size,
          ) ?? 0,
        after:
          spacing(
            firstOf((e) => inA(e, 'spcAft')),
            size,
          ) ?? 0,
        lineHeight:
          points !== null ? (points / 100) * PT_PX : (percent !== null ? percent / 100000 : 1) * LINE * size,
        size,
        runs,
      };
    });
};

const readBodyProps = (shape: Element, chain: readonly (Element | null)[]) => {
  const bodies = [shape, ...chain]
    .filter((item): item is Element => item !== null)
    .map((item) => inA(inP(item, 'txBody'), 'bodyPr'));
  const first = <T>(read: (element: Element) => T | null): T | null => {
    for (const body of bodies) {
      if (!body) continue;
      const found = read(body);
      if (found !== null) return found;
    }
    return null;
  };
  const anchors: Readonly<Record<string, IPptxText['anchor']>> = { t: 'top', ctr: 'middle', b: 'bottom' };
  const inset = (name: 'lIns' | 'tIns' | 'rIns' | 'bIns', fallback: number) =>
    (first((e) => num(e, name)) ?? fallback) * EMU_PX;
  return {
    anchor: anchors[first((e) => e.getAttribute('anchor')) ?? ''] ?? 'top',
    insets: {
      l: inset('lIns', DEFAULT_INSET.l),
      t: inset('tIns', DEFAULT_INSET.t),
      r: inset('rIns', DEFAULT_INSET.r),
      b: inset('bIns', DEFAULT_INSET.b),
    },
    wrap: first((e) => e.getAttribute('wrap')) !== 'none',
    scale: (first((e) => num(inA(e, 'normAutofit'), 'fontScale')) ?? 100000) / 100000,
  };
};

/** A slide, or a layout or a master behind it, as values for the page. */
const readShapes = (
  book: IPptxBook,
  part: IPart,
  layout: ILayout | null,
  master: IMaster | null,
  indexOf: ReadonlyMap<Element, number> | null,
): TPptxShape[] => {
  const out: TPptxShape[] = [];
  const colorScope: IColorScope = { master };
  const txStyles = inP(master?.doc.documentElement, 'txStyles');

  const walk = (parent: Element, map: TMap) => {
    for (const item of parent.children) {
      if (item.localName === 'AlternateContent') {
        // The form every reader takes: what a program unlike the file's own is given.
        const fallback = [...item.children].find((c) => c.localName === 'Fallback');
        const chosen = fallback ?? [...item.children].find((c) => c.localName === 'Choice');
        if (chosen) walk(chosen, map);
      } else if (item.localName === 'grpSp') walk(item, groupMap(item, map));
      else if (item.localName === 'sp' || item.localName === 'cxnSp') readShape(item, map);
      else if (item.localName === 'pic') readPicture(item, map);
      else if (item.localName === 'graphicFrame') readFrame(item, map);
    }
  };

  const textScope = (
    shape: Element,
    ph: Element | null,
    chain: readonly (Element | null)[],
    scale: number,
    fontColor: string | null,
  ): ITextScope => {
    const type = ph?.getAttribute('type') ?? (ph ? 'obj' : null);
    const styleName =
      type === 'title' || type === 'ctrTitle'
        ? 'titleStyle'
        : type === 'body' || type === 'subTitle' || type === 'obj' || (ph && type === null)
          ? 'bodyStyle'
          : 'otherStyle';
    const inherited = [...chain].reverse().map((item) => inA(inP(item, 'txBody'), 'lstStyle'));
    return {
      part,
      color: colorScope,
      theme: master?.theme ?? null,
      lists: [book.defaults, inP(txStyles, styleName), ...inherited, inA(inP(shape, 'txBody'), 'lstStyle')],
      fontColor,
      indexOf,
      scale,
    };
  };

  const readShape = (shape: Element, map: TMap) => {
    const ph = placeholderOf(shape);
    // What stands on a layout or a master as a place to fill is not on the slide.
    if (ph && indexOf === null) return;
    const fromLayout = ph ? findPlaceholder(layout, ph, false) : null;
    const fromMaster = ph
      ? findPlaceholder(master, fromLayout ? (placeholderOf(fromLayout) ?? ph) : ph, true)
      : null;
    const chain = [fromLayout, fromMaster];
    const spPr = inP(shape, 'spPr');
    const xfrm =
      inA(spPr, 'xfrm') ?? inA(inP(fromLayout, 'spPr'), 'xfrm') ?? inA(inP(fromMaster, 'spPr'), 'xfrm');
    const box = xfrm ? boxOfXfrm(xfrm) : null;
    if (!box) return;
    const placed = map(box);
    const style = inP(shape, 'style');
    const styleColor = (name: string): string | null => {
      const ref = inA(style, name);
      // A font is named by a word, a fill or a line by a number, and number 0 means none.
      return ref?.getAttribute('idx') === '0' ? null : colorIn(ref, colorScope);
    };
    const name = nameOf(shape);
    const preset = inA(spPr, 'prstGeom');
    const styleLine = styleColor('lnRef');
    const line = lineOf(
      inA(spPr, 'ln'),
      part,
      { ...colorScope, placeholder: hexOf(styleLine) },
      styleLine ? { color: styleLine, width: 1, dashed: false } : null,
    );
    if (shape.localName === 'cxnSp' || preset?.getAttribute('prst') === 'line') {
      if (line) out.push({ kind: 'line', name, box: placed, line });
      return;
    }
    let fill = fillIn(spPr, part, colorScope);
    if (fill === null) {
      const inherited = [fromLayout, fromMaster]
        .map((item) => fillIn(inP(item, 'spPr'), part, colorScope))
        .find((found) => found !== null);
      fill = inherited ?? null;
    }
    if (fill === null) {
      const color = styleColor('fillRef');
      if (color) fill = { kind: 'solid', color };
    }
    const body = inP(shape, 'txBody');
    const props = readBodyProps(shape, chain);
    const custom = inA(spPr, 'custGeom');
    const adjust = inA(inA(preset, 'avLst'), 'gd')?.getAttribute('fmla');
    const adjusted = adjust?.startsWith('val ') ? Number(adjust.slice(4)) : 16667;
    const text: IPptxText | null = body
      ? {
          anchor: props.anchor,
          insets: props.insets,
          wrap: props.wrap,
          paragraphs: readParagraphs(body, textScope(shape, ph, chain, props.scale, styleColor('fontRef'))),
        }
      : null;
    out.push({
      kind: 'shape',
      name,
      box: placed,
      geometry: preset?.getAttribute('prst') ?? 'rect',
      radius: (Math.min(placed.w, placed.h) * adjusted) / 100000,
      path: custom ? pathOf(custom) : null,
      fill: fill === 'none' ? null : fill,
      line,
      text,
    });
  };

  const readPicture = (picture: Element, map: TMap) => {
    const fill = inP(picture, 'blipFill');
    const path = blipPath(fill, part);
    const ph = placeholderOf(picture);
    const fromLayout = ph ? findPlaceholder(layout, ph, false) : null;
    const xfrm = inA(inP(picture, 'spPr'), 'xfrm') ?? inA(inP(fromLayout, 'spPr'), 'xfrm');
    const box = xfrm ? boxOfXfrm(xfrm) : null;
    if (!path || !box) return;
    const rect = inA(fill, 'srcRect');
    const side = (name: string) => (num(rect, name) ?? 0) / 100000;
    out.push({
      kind: 'picture',
      name: nameOf(picture),
      box: map(box),
      path,
      crop: { l: side('l'), t: side('t'), r: side('r'), b: side('b') },
    });
  };

  const readFrame = (frame: Element, map: TMap) => {
    const xfrm = inP(frame, 'xfrm');
    const box = xfrm ? boxOfXfrm(xfrm) : null;
    if (!box) return;
    const placed = map(box);
    const name = nameOf(frame);
    const data = inA(inA(frame, 'graphic'), 'graphicData');
    const table = inA(data, 'tbl');
    if (!table) {
      const uri = data?.getAttribute('uri') ?? '';
      const what = uri.includes('/chart') ? 'chart' : uri.includes('/diagram') ? 'diagram' : 'object';
      out.push({ kind: 'other', name, box: placed, what });
      return;
    }
    const scope = textScope(frame, null, [], 1, null);
    out.push({
      kind: 'table',
      name,
      box: placed,
      columns: [...(inA(table, 'tblGrid')?.children ?? [])].map((column) => (num(column, 'w') ?? 0) * EMU_PX),
      rows: [...table.children]
        .filter((row) => row.localName === 'tr')
        .map((row) => ({
          height: (num(row, 'h') ?? 0) * EMU_PX,
          cells: [...row.children]
            .filter((cell) => cell.localName === 'tc')
            .map((cell) => {
              const tcPr = inA(cell, 'tcPr');
              const body = inA(cell, 'txBody');
              const fill = fillIn(tcPr, part, colorScope);
              const anchors: Readonly<Record<string, IPptxText['anchor']>> = {
                t: 'top',
                ctr: 'middle',
                b: 'bottom',
              };
              return {
                span: num(cell, 'gridSpan') ?? 1,
                rowSpan: num(cell, 'rowSpan') ?? 1,
                hidden: cell.getAttribute('hMerge') === '1' || cell.getAttribute('vMerge') === '1',
                fill: fill === 'none' ? null : fill,
                text: {
                  anchor: anchors[tcPr?.getAttribute('anchor') ?? ''] ?? 'top',
                  insets: { l: 7, t: 4, r: 7, b: 4 },
                  wrap: true,
                  paragraphs: body ? readParagraphs(body, scope) : [],
                },
              };
            }),
        })),
    });
  };

  const tree = treeOf(part);
  if (tree) walk(tree, SAME);
  return out;
};

const backgroundOf = (part: IPart, master: IMaster | null): TPptxFill | null | undefined => {
  const bg = inP(inP(part.doc.documentElement, 'cSld'), 'bg');
  if (!bg) return undefined;
  const scope: IColorScope = { master };
  const props = inP(bg, 'bgPr');
  if (props) {
    const fill = fillIn(props, part, scope);
    return fill === 'none' ? null : fill;
  }
  const color = colorIn(inP(bg, 'bgRef'), scope);
  return color ? { kind: 'solid', color } : undefined;
};

/** A slide as the page draws it: what stands behind it on its master and its layout, then its own. */
export const drawnSlide = (book: IPptxBook, at: number): IPptxDrawn => {
  const slide = book.slides[at];
  const layout = slide.layout;
  const master = layout?.master ?? null;
  const shows = (part: IPart | null) => part?.doc.documentElement.getAttribute('showMasterSp') !== '0';
  const indexOf = new Map(slide.paragraphs.map((p, index) => [p, index]));
  const shapes: TPptxShape[] = [];
  if (master && shows(slide) && shows(layout)) shapes.push(...readShapes(book, master, null, master, null));
  if (layout && shows(slide)) shapes.push(...readShapes(book, layout, null, master, null));
  shapes.push(...readShapes(book, slide, layout, master, indexOf));
  const background = [slide, layout, master]
    .filter((part): part is IPptxSlide | ILayout | IMaster => part !== null)
    .map((part) => backgroundOf(part, master))
    .find((found) => found !== undefined);
  return { background: background ?? null, shapes };
};

const paragraphsOf = (doc: Document): Element[] => [...doc.getElementsByTagNameNS(A, 'p')];

export const readPptx = (bytes: Uint8Array): IPptxBook => {
  const zip = unzipSync(bytes);
  const presentation = readPart(zip, PRESENTATION);
  if (!presentation) throw new Error('no presentation');
  const root = presentation.documentElement;
  const size = inP(root, 'sldSz');
  const relations = readRelations(zip, PRESENTATION);
  const masters = new Map<string, IMaster>();
  const layouts = new Map<string, ILayout>();

  const masterAt = (path: string): IMaster | null => {
    const known = masters.get(path);
    if (known) return known;
    const doc = readPart(zip, path);
    if (!doc) return null;
    const rels = readRelations(zip, path);
    const map = inP(doc.documentElement, 'clrMap');
    const master: IMaster = {
      path,
      doc,
      relations: rels,
      theme: readTheme(zip, { relations: rels }),
      colorMap: new Map([...(map?.attributes ?? [])].map((attr) => [attr.name, attr.value])),
    };
    masters.set(path, master);
    return master;
  };

  const layoutAt = (path: string): ILayout | null => {
    const known = layouts.get(path);
    if (known) return known;
    const doc = readPart(zip, path);
    if (!doc) return null;
    const rels = readRelations(zip, path);
    const towards = [...rels.values()].find((rel) => rel.type.endsWith('/slideMaster'));
    const master = towards ? masterAt(towards.target) : null;
    if (!master) return null;
    const layout: ILayout = { path, doc, relations: rels, master };
    layouts.set(path, layout);
    return layout;
  };

  const slides: IPptxSlide[] = [];
  for (const entry of inP(root, 'sldIdLst')?.children ?? []) {
    const relation = relations.get(entry.getAttributeNS(R, 'id') ?? '');
    if (!relation) continue;
    const doc = readPart(zip, relation.target);
    if (!doc) continue;
    const rels = readRelations(zip, relation.target);
    const towards = [...rels.values()].find((rel) => rel.type.endsWith('/slideLayout'));
    slides.push({
      path: relation.target,
      doc,
      relations: rels,
      layout: towards ? layoutAt(towards.target) : null,
      paragraphs: paragraphsOf(doc),
    });
  }
  return {
    zip,
    width: (num(size, 'cx') ?? 12192000) * EMU_PX,
    height: (num(size, 'cy') ?? 6858000) * EMU_PX,
    slides,
    defaults: inP(root, 'defaultTextStyle'),
    changed: new Map(),
  };
};

export const slideTitle = (slide: IPptxSlide): string | null => {
  for (const shape of shapesIn(slide)) {
    const type = placeholderOf(shape)?.getAttribute('type');
    if (type !== 'title' && type !== 'ctrTitle') continue;
    const text = [...shape.getElementsByTagNameNS(A, 't')].map((t) => t.textContent ?? '').join('');
    return text.trim() || null;
  }
  return null;
};

// Editing.
const piecesOf = (p: Element): Element[] => {
  const out: Element[] = [];
  for (const item of p.children) {
    if (item.localName !== 'r' && item.localName !== 'fld') continue;
    const piece = inA(item, 't');
    if (piece) out.push(piece);
  }
  return out;
};

const textOf = (p: Element): string =>
  piecesOf(p)
    .map((piece) => piece.textContent ?? '')
    .join('');

/** A run for words to come, in the given look; a paragraph's closing mark keeps the same fields under another name. */
const newRun = (doc: Document, look: Element | null): Element => {
  const run = doc.createElementNS(A, 'a:r');
  if (look) {
    const rPr = doc.createElementNS(A, 'a:rPr');
    for (const attr of look.attributes) rPr.setAttribute(attr.name, attr.value);
    for (const item of look.children) rPr.append(item.cloneNode(true));
    run.append(rPr);
  }
  run.append(doc.createElementNS(A, 'a:t'));
  return run;
};

/** Words go before the paragraph's closing mark: the file's order requires it. */
const addToEnd = (p: Element, item: Element) => {
  const mark = inA(p, 'endParaRPr');
  if (mark) mark.before(item);
  else p.append(item);
};

const markChanged = (book: IPptxBook, slide: IPptxSlide) => book.changed.set(slide.path, slide.doc);

export const pptxParagraphs = (book: IPptxBook, at: number): IParagraphs => {
  const slide = book.slides[at];
  const setText = (index: number, text: string): boolean => {
    const p = slide.paragraphs[index];
    if (!p || textOf(p) === text) return false;
    const pieces = piecesOf(p);
    if (pieces.length === 0) {
      const run = newRun(slide.doc, inA(p, 'endParaRPr'));
      addToEnd(p, run);
      const piece = inA(run, 't');
      if (piece) pieces.push(piece);
    }
    spliceText(pieces, text);
    markChanged(book, slide);
    return true;
  };
  return {
    text: (index) => textOf(slide.paragraphs[index]),
    setText,
    split: (index, offset) => {
      const p = slide.paragraphs[index];
      const next = p.cloneNode(false) as Element;
      const properties = inA(p, 'pPr');
      if (properties) next.append(properties.cloneNode(true));
      let left = offset;
      const lastLook = [...p.children].filter((c) => c.localName === 'r').map((run) => inA(run, 'rPr'));
      for (const item of [...p.children]) {
        if (item.localName === 'pPr') continue;
        if (item.localName === 'endParaRPr') {
          next.append(item.cloneNode(true));
          continue;
        }
        const size = inA(item, 't')?.textContent?.length ?? 0;
        if (left > 0 && left >= size) {
          left -= size;
          continue;
        }
        if (left === 0) {
          const mark = inA(next, 'endParaRPr');
          if (mark) mark.before(item);
          else next.append(item);
          continue;
        }
        const piece = inA(item, 't');
        const rest = item.cloneNode(true) as Element;
        const restPiece = inA(rest, 't');
        const words = piece?.textContent ?? '';
        if (piece) piece.textContent = words.slice(0, left);
        if (restPiece) restPiece.textContent = words.slice(left);
        const mark = inA(next, 'endParaRPr');
        if (mark) mark.before(rest);
        else next.append(rest);
        left = 0;
      }
      p.after(next);
      slide.paragraphs.splice(index + 1, 0, next);
      // The half with no words keeps the look of the words beside it.
      const look = lastLook.at(-1) ?? null;
      if (piecesOf(next).length === 0 && look) addToEnd(next, newRun(slide.doc, look));
      if (piecesOf(p).length === 0 && look) addToEnd(p, newRun(slide.doc, look));
      markChanged(book, slide);
    },
    join: (index) => {
      const p = slide.paragraphs[index];
      const previous = slide.paragraphs[index - 1];
      if (!p || !previous || p.previousElementSibling !== previous) return null;
      const where = textOf(previous).length;
      for (const item of [...p.children])
        if (item.localName !== 'pPr' && item.localName !== 'endParaRPr') addToEnd(previous, item);
      p.remove();
      slide.paragraphs.splice(index, 1);
      markChanged(book, slide);
      return where;
    },
    joinable: (index) => {
      const here = slide.paragraphs[index];
      const next = slide.paragraphs[index + 1];
      return Boolean(here && (!next || next.previousElementSibling === here));
    },
  };
};

export const replacePptxRange = (
  book: IPptxBook,
  at: number,
  from: ITextPlace,
  to: ITextPlace,
  text: string,
): ITextPlace | null => replaceIn(pptxParagraphs(book, at), from, to, text);

/** After a state of the slide came back: its paragraphs are read again. */
export const rereadSlide = (book: IPptxBook, at: number): void => {
  const slide = book.slides[at];
  slide.paragraphs = paragraphsOf(slide.doc);
  markChanged(book, slide);
};

/** The words of each slide's paragraphs: what a later file is compared with. */
export const pptxSnapshot = (book: IPptxBook): readonly (readonly string[])[] =>
  book.slides.map((slide) => slide.paragraphs.map(textOf));

/**
 * The person's edits, on top of a file that changed under them: paragraph by paragraph. False when one has nowhere to go: slides
 * or paragraphs were added or taken away, or both changed the same one.
 */
export const mergePptx = (
  base: readonly (readonly string[])[],
  mine: IPptxBook,
  theirs: IPptxBook,
): boolean => {
  if (mine.slides.length !== base.length || theirs.slides.length !== base.length) return false;
  const edits: [number, number, string][] = [];
  for (let at = 0; at < base.length; at += 1) {
    const was = base[at];
    if (
      mine.slides[at].paragraphs.length !== was.length ||
      theirs.slides[at].paragraphs.length !== was.length
    )
      return false;
    for (let index = 0; index < was.length; index += 1) {
      const text = textOf(mine.slides[at].paragraphs[index]);
      if (text === was[index]) continue;
      const other = textOf(theirs.slides[at].paragraphs[index]);
      if (other !== was[index] && other !== text) return false;
      edits.push([at, index, text]);
    }
  }
  for (const [at, index, text] of edits) pptxParagraphs(theirs, at).setText(index, text);
  return true;
};

/** The file again: only the slides edited are written anew. */
export const writePptx = (book: IPptxBook): Uint8Array => packParts(book.zip, book.changed);
