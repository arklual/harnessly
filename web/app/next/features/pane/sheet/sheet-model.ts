// The grid's arithmetic. Pure: the view and its tests read it.

export const ROW_HEIGHT = 28;
export const HEAD_WIDTH = 52;
const DEFAULT_COLUMN = 104;
const MIN_COLUMN = 32;
const MAX_COLUMN = 520;
/** Rows drawn beyond the view, so a quick scroll does not show them arriving. */
export const OVERSCAN = 8;
export const MIN_ROWS = 40;
export const MIN_COLUMNS = 16;
export const CHUNK_ROWS = 200;

/** A column's width on screen from the file's width in characters; a hidden one has none. */
export const columnPixels = (characters: number | undefined): number => {
  if (characters === undefined) return DEFAULT_COLUMN;
  if (characters === 0) return 0;
  return Math.min(MAX_COLUMN, Math.max(MIN_COLUMN, Math.round(characters * 7 + 5)));
};

/** Where each column starts, and the whole width as one more value at the end. */
export const columnOffsets = (widths: readonly number[]): number[] => {
  const offsets = [0];
  for (const width of widths) offsets.push(offsets[offsets.length - 1] + width);
  return offsets;
};

/** The first column that reaches past this position. */
export const columnAt = (offsets: readonly number[], position: number): number => {
  let low = 0;
  let high = Math.max(0, offsets.length - 2);
  while (low < high) {
    const middle = Math.ceil((low + high) / 2);
    if (offsets[middle] <= position) low = middle;
    else high = middle - 1;
  }
  return low;
};

export interface ICellPlace {
  row: number;
  column: number;
}

export interface ISelection {
  anchor: ICellPlace;
  focus: ICellPlace;
}

export interface IRange {
  top: number;
  left: number;
  bottom: number;
  right: number;
}

export const rangeOf = ({ anchor, focus }: ISelection): IRange => ({
  top: Math.min(anchor.row, focus.row),
  left: Math.min(anchor.column, focus.column),
  bottom: Math.max(anchor.row, focus.row),
  right: Math.max(anchor.column, focus.column),
});

export const inRange = (range: IRange, row: number, column: number): boolean =>
  row >= range.top && row <= range.bottom && column >= range.left && column <= range.right;

export const clampPlace = (place: ICellPlace, rows: number, columns: number): ICellPlace => ({
  row: Math.min(Math.max(0, place.row), Math.max(0, rows - 1)),
  column: Math.min(Math.max(0, place.column), Math.max(0, columns - 1)),
});

export interface IRangeStats {
  sum: number;
  average: number;
  count: number;
}

/** What a selection adds up to; null when it holds no numbers. */
export const statsOf = (numbers: readonly number[]): IRangeStats | null => {
  if (numbers.length === 0) return null;
  let sum = 0;
  for (const value of numbers) sum += value;
  return { sum, average: sum / numbers.length, count: numbers.length };
};

/** Cells as the clipboard carries them: tabs and new lines, quoted when a cell holds one. */
export const toClipboard = (rows: readonly (readonly string[])[]): string =>
  rows
    .map((row) =>
      row.map((text) => (/[\t\n"]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text)).join('\t'),
    )
    .join('\n');

/** Text from the clipboard as rows of cells. */
export const fromClipboard = (text: string): string[][] => {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let quoted = false;
  for (let at = 0; at < text.length; at += 1) {
    const char = text[at];
    if (quoted) {
      if (char === '"' && text[at + 1] === '"') {
        field += '"';
        at += 1;
      } else if (char === '"') quoted = false;
      else field += char;
    } else if (char === '"' && field === '') quoted = true;
    else if (char === '\t') {
      row.push(field);
      field = '';
    } else if (char === '\n') {
      row.push(field.replace(/\r$/, ''));
      rows.push(row);
      row = [];
      field = '';
    } else field += char;
  }
  if (field !== '' || row.length > 0) {
    row.push(field.replace(/\r$/, ''));
    rows.push(row);
  }
  return rows;
};

interface IStyleLook {
  bold: boolean;
  italic: boolean;
  color: string | null;
  fill: string | null;
  align: 'left' | 'center' | 'right' | null;
}

/** A near-black with no colour of its own: on a dark window it cannot be read, and it means only "the usual text". */
const isPlainDark = (hex: string): boolean => {
  const value = parseInt(hex, 16);
  const channels = [(value >> 16) & 255, (value >> 8) & 255, value & 255];
  return Math.max(...channels) < 90 && Math.max(...channels) - Math.min(...channels) < 40;
};

/**
 * The file's styles as CSS rules, one per style. Colours come in as hex digits only (sheet-core). A cell with a fill keeps
 * the file's text colour, dark by default as on white paper; text with no fill follows the theme unless it is coloured,
 * since black text on a dark window cannot be read.
 */
export const stylesToCss = (styles: readonly IStyleLook[]): string =>
  styles
    .map((style, index) => {
      const rules: string[] = [];
      if (style.bold) rules.push('font-weight:600');
      if (style.italic) rules.push('font-style:italic');
      if (style.align) rules.push(`text-align:${style.align}`);
      if (style.fill) {
        rules.push(`background:#${style.fill}`);
        rules.push(`color:#${style.color ?? '111111'}`);
      } else if (style.color && !isPlainDark(style.color)) rules.push(`color:#${style.color}`);
      return rules.length > 0 ? `.hx-cells__cell[data-s="${index}"]{${rules.join(';')}}` : '';
    })
    .filter((rule) => rule !== '')
    .join('\n');

const CELL_PLACE = /^(?:(?:'([^']+)'|([^!]+))!)?\$?([A-Za-z]+)\$?(\d+)(?::\$?([A-Za-z]+)\$?(\d+))?$/;

const columnOf = (letters: string): number => {
  let index = 0;
  for (const letter of letters.toUpperCase()) index = index * 26 + letter.charCodeAt(0) - 64;
  return index - 1;
};

/** `D2`, `D2:F9`, `Продажи!D2:F9`: which sheet (null: the one open) and range; null for what names neither. */
export const parseCellPlace = (
  place: string,
  sheets: readonly string[],
): { sheet: number | null; range: IRange } | null => {
  const found = place.trim().match(CELL_PLACE);
  if (!found) return null;
  const name = found[1] ?? found[2];
  const sheet = name === undefined ? null : sheets.indexOf(name);
  if (sheet === -1) return null;
  const a = { row: Number(found[4]) - 1, column: columnOf(found[3]) };
  const b = found[5] ? { row: Number(found[6]) - 1, column: columnOf(found[5]) } : a;
  if (a.row < 0 || b.row < 0) return null;
  return { sheet, range: rangeOf({ anchor: a, focus: b }) };
};
