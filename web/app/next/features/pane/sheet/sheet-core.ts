// A spreadsheet file as data: read, changed and written back. Pure and without a DOM, for the worker and the tests.
// An .xlsx is a zip of XML, and a save changes only the cells that were edited, in place: everything else in the file
// (charts, pictures, pivot tables, macros, styles) goes back byte for byte, so nothing is lost for the program that made it.
import { strFromU8, strToU8, unzipSync, zipSync, type Unzipped } from 'fflate';
import { addressOf, columnName } from '@/next/features/pane/sheet/sheet-names';
import { formatCodeOf, formatValue, plainNumber } from '@/next/features/pane/sheet/sheet-format';

export const CELL_KIND = { number: 'n', text: 's', bool: 'b', error: 'e' } as const;
export type TCellKind = (typeof CELL_KIND)[keyof typeof CELL_KIND];

export interface ICell {
  kind: TCellKind;
  /** The stored value: a number's digits, the text itself, 1/0 of a boolean. */
  value: string;
  /** The formula without its «=». */
  formula?: string;
  /** The index of the cell's style in the file. */
  style: number;
}

export interface ICellStyle {
  bold: boolean;
  italic: boolean;
  /** rrggbb, only ever hex digits. */
  color: string | null;
  fill: string | null;
  align: 'left' | 'center' | 'right' | null;
  format: string;
}

export interface ISheet {
  name: string;
  /** Inside the zip; empty for a CSV. */
  path: string;
  rows: (ICell | undefined)[][];
  columns: number;
  /** The file's widths in characters; a column it does not name has the default. */
  widths: Map<number, number>;
  frozenRows: number;
  frozenColumns: number;
  loaded: boolean;
}

export interface IBook {
  kind: 'xlsx' | 'csv';
  sheets: ISheet[];
  styles: ICellStyle[];
  shared: string[];
  date1904: boolean;
  zip: Unzipped | null;
  edits: Map<string, Map<string, ICell | null>>;
  /** Of a CSV: how it was written, so a save writes the same. */
  delimiter: string;
  bom: boolean;
  encoding: string;
  lineEnd: string;
  endsWithLine: boolean;
}

const unescapeXml = (text: string): string =>
  text.replace(/&(?:#(\d+)|#x([0-9a-fA-F]+)|(amp|lt|gt|quot|apos));/g, (_, dec, hex, name) => {
    if (dec) return String.fromCodePoint(Number(dec));
    if (hex) return String.fromCodePoint(parseInt(hex, 16));
    return { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'" }[name as 'amp'];
  });

const escapeXml = (text: string): string =>
  text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

const attr = (attributes: string, name: string): string | null => {
  const found = new RegExp(`(?:^|\\s)${name}="([^"]*)"`).exec(attributes);
  return found ? unescapeXml(found[1]) : null;
};

export const columnIndex = (letters: string): number => {
  let index = 0;
  for (const letter of letters) index = index * 26 + (letter.charCodeAt(0) - 64);
  return index - 1;
};

export { addressOf, columnName };

const parseAddress = (address: string): { row: number; column: number } | null => {
  const found = /^([A-Z]+)(\d+)$/.exec(address);
  return found ? { column: columnIndex(found[1]), row: Number(found[2]) - 1 } : null;
};

const normalizePath = (target: string): string =>
  target.startsWith('/') ? target.slice(1) : `xl/${target.replace(/^\.\//, '')}`;

const hexOf = (rgb: string | null): string | null => {
  const hex = rgb?.length === 8 ? rgb.slice(2) : rgb;
  return hex && /^[0-9a-fA-F]{6}$/.test(hex) ? hex.toLowerCase() : null;
};

const parseStyles = (xml: string | null): ICellStyle[] => {
  if (xml === null) return [];
  const custom = new Map<number, string>();
  for (const found of xml.matchAll(/<numFmt\b([^>]*)\/?>/g)) {
    const id = attr(found[1], 'numFmtId');
    const code = attr(found[1], 'formatCode');
    if (id !== null && code !== null) custom.set(Number(id), code);
  }
  const block = (name: string) => new RegExp(`<${name}\\b[^>]*>(.*?)</${name}>`, 's').exec(xml)?.[1] ?? '';
  const fonts = [...block('fonts').matchAll(/<font\b[^>]*?(?:\/>|>(.*?)<\/font>)/gs)].map((found) => {
    const body = found[1] ?? '';
    return {
      bold: /<b(?:\s[^>]*)?\/>/.test(body) && !/<b\s+val="0"/.test(body),
      italic: /<i(?:\s[^>]*)?\/>/.test(body) && !/<i\s+val="0"/.test(body),
      color: hexOf(attr(/<color\b([^>]*)\/?>/.exec(body)?.[1] ?? '', 'rgb')),
    };
  });
  const fills = [...block('fills').matchAll(/<fill\b[^>]*>(.*?)<\/fill>/gs)].map((found) =>
    /patternType="none"/.test(found[1])
      ? null
      : hexOf(attr(/<fgColor\b([^>]*)\/?>/.exec(found[1])?.[1] ?? '', 'rgb')),
  );
  return [...block('cellXfs').matchAll(/<xf\b([^>]*?)(?:\/>|>(.*?)<\/xf>)/gs)].map((found) => {
    const font = fonts[Number(attr(found[1], 'fontId') ?? 0)];
    const align = attr(/<alignment\b([^>]*)\/?>/.exec(found[2] ?? '')?.[1] ?? '', 'horizontal');
    return {
      bold: font?.bold ?? false,
      italic: font?.italic ?? false,
      color: font?.color ?? null,
      fill: fills[Number(attr(found[1], 'fillId') ?? 0)] ?? null,
      align: align === 'left' || align === 'center' || align === 'right' ? align : null,
      format: formatCodeOf(Number(attr(found[1], 'numFmtId') ?? 0), custom),
    };
  });
};

const parseShared = (xml: string | null): string[] => {
  if (xml === null) return [];
  return [...xml.matchAll(/<si\b[^>]*?(?:\/>|>(.*?)<\/si>)/gs)].map((found) => {
    // The phonetic hints of a rich text are not its text.
    const body = (found[1] ?? '').replace(/<rPh\b.*?<\/rPh>/gs, '');
    return [...body.matchAll(/<t\b[^>]*?(?:\/>|>(.*?)<\/t>)/gs)].map((t) => unescapeXml(t[1] ?? '')).join('');
  });
};

const readZipText = (zip: Unzipped, path: string): string | null => (zip[path] ? strFromU8(zip[path]) : null);

export const readXlsx = (bytes: Uint8Array): IBook => {
  const zip = unzipSync(bytes);
  const workbook = readZipText(zip, 'xl/workbook.xml');
  if (workbook === null) throw new Error('not a workbook');
  const relations = new Map<string, string>();
  for (const found of (readZipText(zip, 'xl/_rels/workbook.xml.rels') ?? '').matchAll(
    /<Relationship\b([^>]*)\/?>/g,
  )) {
    const id = attr(found[1], 'Id');
    const target = attr(found[1], 'Target');
    if (id !== null && target !== null) relations.set(id, normalizePath(target));
  }
  const sheets: ISheet[] = [];
  for (const found of workbook.matchAll(/<sheet\b([^>]*)\/?>/g)) {
    if (attr(found[1], 'state') === 'hidden' || attr(found[1], 'state') === 'veryHidden') continue;
    const path = relations.get(attr(found[1], 'r:id') ?? '');
    if (path === undefined || !zip[path]) continue;
    sheets.push({
      name: attr(found[1], 'name') ?? path,
      path,
      rows: [],
      columns: 0,
      widths: new Map(),
      frozenRows: 0,
      frozenColumns: 0,
      loaded: false,
    });
  }
  const book: IBook = {
    kind: 'xlsx',
    sheets,
    styles: parseStyles(readZipText(zip, 'xl/styles.xml')),
    shared: parseShared(readZipText(zip, 'xl/sharedStrings.xml')),
    date1904: /<workbookPr\b[^>]*date1904="(?:1|true)"/.test(workbook),
    zip,
    edits: new Map(),
    delimiter: ',',
    bom: false,
    encoding: 'utf-8',
    lineEnd: '\n',
    endsWithLine: true,
  };
  if (sheets[0]) loadSheet(book, 0);
  return book;
};

/** A sheet's cells are read when it is first opened: a file of many sheets opens at the speed of one. */
export const loadSheet = (book: IBook, index: number): ISheet => {
  const sheet = book.sheets[index];
  if (sheet.loaded || book.zip === null) return sheet;
  sheet.loaded = true;
  const xml = readZipText(book.zip, sheet.path) ?? '';
  const pane = /<pane\b([^>]*)\/?>/.exec(xml)?.[1];
  if (pane && attr(pane, 'state') !== 'split') {
    sheet.frozenRows = Number(attr(pane, 'ySplit') ?? 0);
    sheet.frozenColumns = Number(attr(pane, 'xSplit') ?? 0);
  }
  for (const found of xml.matchAll(/<col\b([^>]*)\/?>/g)) {
    const width = Number(attr(found[1], 'width'));
    const hidden = attr(found[1], 'hidden') === '1';
    const min = Number(attr(found[1], 'min') ?? 0);
    const max = Math.min(Number(attr(found[1], 'max') ?? 0), min + 256);
    for (let at = min; at <= max && at > 0; at += 1) sheet.widths.set(at - 1, hidden ? 0 : width);
  }
  const data = xml.indexOf('<sheetData');
  const cell = /<c\b([^>]*?)(?:\/>|>(.*?)<\/c>)/gs;
  cell.lastIndex = Math.max(0, data);
  for (let found = cell.exec(xml); found; found = cell.exec(xml)) {
    const place = parseAddress(attr(found[1], 'r') ?? '');
    if (!place) continue;
    const parsed = parseCell(book, found[1], found[2] ?? '');
    if (!parsed) continue;
    (sheet.rows[place.row] ??= [])[place.column] = parsed;
    sheet.columns = Math.max(sheet.columns, place.column + 1);
  }
  return sheet;
};

const parseCell = (book: IBook, attributes: string, body: string): ICell | null => {
  const type = attr(attributes, 't');
  const style = Number(attr(attributes, 's') ?? 0);
  const formula = /<f\b[^>]*?(?:\/>|>(.*?)<\/f>)/s.exec(body);
  const value = /<v\b[^>]*>(.*?)<\/v>/s.exec(body)?.[1];
  const shape: Pick<ICell, 'style' | 'formula'> = {
    style,
    ...(formula?.[1] ? { formula: unescapeXml(formula[1]) } : {}),
  };
  if (type === 'inlineStr') {
    const text = [...body.matchAll(/<t\b[^>]*?(?:\/>|>(.*?)<\/t>)/gs)]
      .map((t) => unescapeXml(t[1] ?? ''))
      .join('');
    return { kind: CELL_KIND.text, value: text, ...shape };
  }
  if (value === undefined)
    return shape.formula || style > 0 ? { kind: CELL_KIND.text, value: '', ...shape } : null;
  if (type === 's') return { kind: CELL_KIND.text, value: book.shared[Number(value)] ?? '', ...shape };
  if (type === 'str') return { kind: CELL_KIND.text, value: unescapeXml(value), ...shape };
  if (type === 'b') return { kind: CELL_KIND.bool, value, ...shape };
  if (type === 'e') return { kind: CELL_KIND.error, value: unescapeXml(value), ...shape };
  return { kind: CELL_KIND.number, value, ...shape };
};

/** What the cell shows. */
export const displayOf = (book: IBook, cell: ICell | undefined): string => {
  if (!cell) return '';
  if (cell.kind === CELL_KIND.bool) return cell.value === '1' ? 'ИСТИНА' : 'ЛОЖЬ';
  if (cell.kind !== CELL_KIND.number) return cell.value;
  const number = Number(cell.value);
  if (!Number.isFinite(number)) return cell.value;
  return formatValue(number, book.styles[cell.style]?.format ?? 'General', book.date1904);
};

/** What the person edits: the formula with its «=», or the value as they would type it. */
export const inputOf = (cell: ICell | undefined): string => {
  if (!cell) return '';
  if (cell.formula) return `=${cell.formula}`;
  if (cell.kind === CELL_KIND.number) return plainNumber(Number(cell.value));
  return cell.value;
};

/** What was typed, as a cell: «=» makes a formula, a number (with a comma or a point) a number, the rest text. */
export const cellOfInput = (input: string, style: number): ICell | null => {
  if (input === '') return style > 0 ? { kind: CELL_KIND.text, value: '', style } : null;
  if (input.startsWith('=') && input.length > 1)
    return { kind: CELL_KIND.text, value: '', formula: input.slice(1), style };
  const number = input.trim().replace(/\s/g, '').replace(',', '.');
  if (/^-?(\d+\.?\d*|\.\d+)(e[+-]?\d+)?$/i.test(number))
    return { kind: CELL_KIND.number, value: String(Number(number)), style };
  return { kind: CELL_KIND.text, value: input, style };
};

export const setCell = (book: IBook, sheetIndex: number, row: number, column: number, cell: ICell | null) => {
  const sheet = book.sheets[sheetIndex];
  (sheet.rows[row] ??= [])[column] = cell ?? undefined;
  sheet.columns = Math.max(sheet.columns, column + 1);
  const key = book.kind === 'csv' ? 'csv' : sheet.path;
  let edits = book.edits.get(key);
  if (!edits) {
    edits = new Map();
    book.edits.set(key, edits);
  }
  edits.set(addressOf(row, column), cell);
};

const cellXml = (address: string, cell: ICell | null): string => {
  if (cell === null) return '';
  const style = cell.style > 0 ? ` s="${cell.style}"` : '';
  const formula = cell.formula ? `<f>${escapeXml(cell.formula)}</f>` : '';
  if (cell.kind === CELL_KIND.number)
    return `<c r="${address}"${style}>${formula}<v>${escapeXml(cell.value)}</v></c>`;
  if (cell.kind === CELL_KIND.bool)
    return `<c r="${address}"${style} t="b">${formula}<v>${cell.value}</v></c>`;
  if (cell.kind === CELL_KIND.error)
    return `<c r="${address}"${style} t="e">${formula}<v>${escapeXml(cell.value)}</v></c>`;
  if (cell.formula) return `<c r="${address}"${style} t="str">${formula}<v>${escapeXml(cell.value)}</v></c>`;
  if (cell.value === '') return `<c r="${address}"${style}/>`;
  return `<c r="${address}"${style} t="inlineStr"><is><t xml:space="preserve">${escapeXml(cell.value)}</t></is></c>`;
};

const rowRe = /<row\b([^>]*?)(?:\/>|>(.*?)<\/row>)/gs;

/** The sheet's XML with these cells replaced; all the rest of it stays character for character. */
export const patchSheetXml = (xml: string, edits: ReadonlyMap<string, ICell | null>): string => {
  const byRow = new Map<number, Map<number, string>>();
  for (const [address, cell] of edits) {
    const place = parseAddress(address);
    if (!place) continue;
    let row = byRow.get(place.row);
    if (!row) {
      row = new Map();
      byRow.set(place.row, row);
    }
    row.set(place.column, cellXml(address, cell));
  }
  const rebuilt = (row: number, attributes: string, body: string): string => {
    const changes = byRow.get(row);
    if (!changes) return '';
    const cells = new Map<number, string>();
    for (const found of body.matchAll(/<c\b[^>]*?(?:\/>|>.*?<\/c>)/gs)) {
      const place = parseAddress(attr(found[0].slice(2), 'r') ?? '');
      if (place) cells.set(place.column, found[0]);
    }
    for (const [column, next] of changes) cells.set(column, next);
    const inner = [...cells.entries()]
      .sort((a, b) => a[0] - b[0])
      .map((entry) => entry[1])
      .join('');
    // «spans» is only a hint and the new cells may fall outside it.
    return `<row${attributes.replace(/\s+spans="[^"]*"/, '')}>${inner}</row>`;
  };

  const start = xml.indexOf('<sheetData');
  if (start < 0) return xml;
  const close = xml.indexOf('</sheetData>', start);
  const open = xml.indexOf('>', start);
  // An empty sheet is a self-closed <sheetData/>.
  const selfClosed = xml[open - 1] === '/';
  const before = xml.slice(0, selfClosed ? open - 1 : open + 1);
  const data = selfClosed ? '' : xml.slice(open + 1, close);
  const after = selfClosed ? xml.slice(open + 1) : xml.slice(close + '</sheetData>'.length);

  let out = '';
  let last = 0;
  rowRe.lastIndex = 0;
  const remaining = new Set(byRow.keys());
  const insertUpTo = (limit: number) => {
    for (const row of [...remaining].sort((a, b) => a - b)) {
      if (row >= limit) break;
      out += rebuilt(row, ` r="${row + 1}"`, '');
      remaining.delete(row);
    }
  };
  for (let found = rowRe.exec(data); found; found = rowRe.exec(data)) {
    const row = Number(attr(found[1], 'r')) - 1;
    out += data.slice(last, found.index);
    insertUpTo(row);
    last = found.index + found[0].length;
    if (byRow.has(row)) {
      out += rebuilt(row, ` ${found[1].trim()}`, found[2] ?? '');
      remaining.delete(row);
    } else out += found[0];
  }
  out += data.slice(last);
  insertUpTo(Number.POSITIVE_INFINITY);
  const head = selfClosed ? `${before}>` : before;
  // The stored dimension may no longer be true; it is optional.
  return `${head.replace(/<dimension\b[^>]*\/>/, '')}${out}</sheetData>${after}`;
};

/** The file with the edits in it. Only the edited sheets are rewritten. */
const LARGE_BOOK_BYTES = 4 * 1024 * 1024;

export const writeXlsx = (book: IBook): Uint8Array => {
  const zip = { ...book.zip } as Unzipped;
  for (const [path, edits] of book.edits) {
    const xml = readZipText(zip, path);
    if (xml !== null) zip[path] = strToU8(patchSheetXml(xml, edits));
  }
  // A cell's formula may have gone: a calc chain naming it makes Excel «repair» the file. It builds a new one by itself.
  if (zip['xl/calcChain.xml']) {
    delete zip['xl/calcChain.xml'];
    const types = readZipText(zip, '[Content_Types].xml');
    if (types !== null)
      zip['[Content_Types].xml'] = strToU8(types.replace(/<Override\b[^>]*calcChain[^>]*\/>/, ''));
    const relations = readZipText(zip, 'xl/_rels/workbook.xml.rels');
    if (relations !== null)
      zip['xl/_rels/workbook.xml.rels'] = strToU8(
        relations.replace(/<Relationship\b[^>]*calcChain[^>]*\/>/, ''),
      );
  }
  // The program that opens it next counts every formula again, whatever we could count here.
  const workbook = readZipText(zip, 'xl/workbook.xml');
  if (workbook !== null) {
    const next = /<calcPr\b/.test(workbook)
      ? workbook.replace(
          /<calcPr\b([^>]*?)\/?>/,
          (_, rest: string) => `<calcPr${rest.replace(/\s+fullCalcOnLoad="[^"]*"/, '')} fullCalcOnLoad="1"/>`,
        )
      : workbook.replace('</workbook>', '<calcPr fullCalcOnLoad="1"/></workbook>');
    zip['xl/workbook.xml'] = strToU8(next);
  }
  // A large file is written after each pause in editing: quick matters more than a few percent of size.
  const whole = Object.values(zip).reduce((all, entry) => all + entry.length, 0);
  return zipSync(zip, { level: whole > LARGE_BOOK_BYTES ? 1 : 6 });
};

const decodeText = (bytes: Uint8Array): { text: string; bom: boolean; encoding: string } => {
  const bom = bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf;
  try {
    return {
      text: new TextDecoder('utf-8', { fatal: true }).decode(bytes.subarray(bom ? 3 : 0)),
      bom,
      encoding: 'utf-8',
    };
  } catch {
    // Not UTF-8: Excel's own CSV in Russian is in this one.
    return { text: new TextDecoder('windows-1251').decode(bytes), bom: false, encoding: 'windows-1251' };
  }
};

const guessDelimiter = (text: string): string => {
  const line = text.slice(0, 4096).split('\n')[0] ?? '';
  let best = ',';
  let most = 0;
  for (const mark of [',', ';', '\t', '|']) {
    const count = line.split(mark).length - 1;
    if (count > most) {
      best = mark;
      most = count;
    }
  }
  return best;
};

export const parseDelimited = (text: string, delimiter: string): string[][] => {
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
    else if (char === delimiter) {
      row.push(field);
      field = '';
    } else if (char === '\n' || char === '\r') {
      if (char === '\r' && text[at + 1] === '\n') at += 1;
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else field += char;
  }
  if (field !== '' || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows;
};

export const readDelimited = (bytes: Uint8Array, name: string): IBook => {
  const { text, bom, encoding } = decodeText(bytes);
  const delimiter = name.toLowerCase().endsWith('.tsv') ? '\t' : guessDelimiter(text);
  const parsed = parseDelimited(text, delimiter);
  const sheet: ISheet = {
    name,
    path: '',
    rows: parsed.map((row) =>
      row.map((field) => (field === '' ? undefined : (cellOfCsv(field) satisfies ICell))),
    ),
    columns: parsed.reduce((most, row) => Math.max(most, row.length), 0),
    widths: new Map(),
    frozenRows: 1,
    frozenColumns: 0,
    loaded: true,
  };
  return {
    kind: 'csv',
    sheets: [sheet],
    styles: [],
    shared: [],
    date1904: false,
    zip: null,
    edits: new Map(),
    delimiter,
    bom,
    encoding,
    lineEnd: text.includes('\r\n') ? '\r\n' : '\n',
    endsWithLine: text.endsWith('\n'),
  };
};

/** A CSV's field keeps its own spelling on a save, so a number here stays what was written («1.50», «007»). */
const cellOfCsv = (field: string): ICell => {
  const plain = /^-?(0|[1-9]\d*)(\.\d+)?$/.test(field) && String(Number(field)) === field;
  return plain
    ? { kind: CELL_KIND.number, value: field, style: 0 }
    : { kind: CELL_KIND.text, value: field, style: 0 };
};

const CP1251_TAIL = 'ЂЃ‚ѓ„…†‡€‰Љ‹ЊЌЋЏђ‘’“”•–—˜™љ›њќћџ\u00a0ЎўЈ¤Ґ¦§Ё©Є«¬\u00ad®Ї°±Ііґµ¶·ё№є»јЅѕї';

/** Back into the encoding the file came in. A character it does not have becomes «?», as Excel's own save does. */
const encodeCp1251 = (text: string): Uint8Array => {
  const out = new Uint8Array(text.length);
  for (let at = 0; at < text.length; at += 1) {
    const code = text.charCodeAt(at);
    if (code < 0x80) out[at] = code;
    else if (code >= 0x410 && code <= 0x44f) out[at] = code - 0x410 + 0xc0;
    else {
      const place = CP1251_TAIL.indexOf(text[at]);
      out[at] = place >= 0 ? 0x80 + place : 0x3f;
    }
  }
  return out;
};

export const writeDelimited = (book: IBook): Uint8Array => {
  const { delimiter } = book;
  const needsQuotes = (field: string) =>
    field.includes(delimiter) || field.includes('"') || field.includes('\n') || field.includes('\r');
  const sheet = book.sheets[0];
  const lines: string[] = [];
  for (let row = 0; row < sheet.rows.length; row += 1) {
    const cells: string[] = [];
    const cellsOfRow = sheet.rows[row] ?? [];
    for (let column = 0; column < cellsOfRow.length; column += 1) {
      const cell = cellsOfRow[column];
      const field = cell ? (cell.formula ? `=${cell.formula}` : cell.value) : '';
      cells.push(needsQuotes(field) ? `"${field.replace(/"/g, '""')}"` : field);
    }
    lines.push(cells.join(delimiter));
  }
  const text = lines.join(book.lineEnd) + (book.endsWithLine ? book.lineEnd : '');
  const body = book.encoding === 'windows-1251' ? encodeCp1251(text) : strToU8(text);
  if (!book.bom) return body;
  const out = new Uint8Array(body.length + 3);
  out.set([0xef, 0xbb, 0xbf]);
  out.set(body, 3);
  return out;
};
