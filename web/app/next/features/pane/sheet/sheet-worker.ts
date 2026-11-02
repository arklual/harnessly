// The spreadsheet lives here, away from the page: a file of a hundred thousand rows is read, counted and written without
// holding up the interface. The page asks for the rows it shows.
import {
  cellOfInput,
  displayOf,
  inputOf,
  loadSheet,
  readDelimited,
  readXlsx,
  setCell,
  writeDelimited,
  writeXlsx,
  type IBook,
  type ICell,
} from '@/next/features/pane/sheet/sheet-core';
import { countUncounted, hasUncounted, recount } from '@/next/features/pane/sheet/sheet-formulas';
import { statsOf } from '@/next/features/pane/sheet/sheet-model';
import {
  SHEET_FILE,
  type ISheetAnswer,
  type ISheetAsk,
  type TSheetFile,
  type IShownCell,
} from '@/next/features/pane/sheet/sheet-api';

let book: IBook | null = null;

interface IUnsaved {
  sheet: string;
  row: number;
  column: number;
  input: string;
}

// The person's edits the disk does not hold yet: what goes on top when the file turned out to have changed under them.
let unsaved: IUnsaved[] = [];
let inTheSave = 0;

const readBook = (kind: TSheetFile, bytes: Uint8Array, name: string): IBook =>
  kind === SHEET_FILE.csv ? readDelimited(bytes, name) : readXlsx(bytes);

const shown = (cell: ICell | undefined): IShownCell | null =>
  book && cell
    ? {
        text: displayOf(book, cell),
        input: inputOf(cell),
        style: cell.style,
        number: cell.kind === 'n',
        value: cell.kind === 'n' ? Number(cell.value) : null,
      }
    : null;

const answer = (ask: ISheetAsk): ISheetAnswer => {
  if (ask.op === 'open') {
    book = readBook(ask.kind, ask.bytes, ask.name);
    unsaved = [];
    inTheSave = 0;
    return {
      op: 'open',
      sheets: book.sheets.map((sheet) => sheet.name),
      styles: book.styles,
    };
  }
  if (!book) throw new Error('no file');
  if (ask.op === 'merge') {
    // Each edit goes onto the disk's file by the sheet's name: an edit to a sheet that is gone is what cannot be merged, and
    // the book stays as it was for the person to choose.
    const next = readBook(ask.kind, ask.bytes, ask.name);
    const names = next.sheets.map((sheet) => sheet.name);
    if (!unsaved.every((edit) => names.includes(edit.sheet)))
      return { op: 'merge', merged: false, sheets: names, styles: next.styles };
    for (const { sheet, row, column, input } of unsaved) {
      const at = names.indexOf(sheet);
      const before = loadSheet(next, at).rows[row]?.[column];
      setCell(next, at, row, column, cellOfInput(input, before?.style ?? 0));
    }
    for (const [at, name] of names.entries()) {
      const edited = unsaved.filter((edit) => edit.sheet === name);
      if (edited.length > 0) recount(next, at, edited);
    }
    book = next;
    inTheSave = 0;
    if (hasUncounted(loadSheet(book, 0))) countUncounted(book);
    return { op: 'merge', merged: true, sheets: names, styles: next.styles };
  }
  if (ask.op === 'saved') {
    unsaved = unsaved.slice(inTheSave);
    inTheSave = 0;
    return { op: 'saved' };
  }
  if (ask.op === 'sheet') {
    const sheet = loadSheet(book, ask.sheet);
    if (hasUncounted(sheet)) countUncounted(book);
    return {
      op: 'sheet',
      rows: sheet.rows.length,
      columns: sheet.columns,
      widths: [...sheet.widths.entries()],
      frozenRows: sheet.frozenRows,
      frozenColumns: sheet.frozenColumns,
    };
  }
  if (ask.op === 'rows') {
    const { rows } = loadSheet(book, ask.sheet);
    const out: (IShownCell | null)[][] = [];
    for (let row = ask.from; row < ask.to; row += 1) {
      const cells = rows[row] ?? [];
      out.push(Array.from({ length: cells.length }, (_, column) => shown(cells[column])));
    }
    return { op: 'rows', from: ask.from, rows: out };
  }
  if (ask.op === 'stats') {
    const { rows } = loadSheet(book, ask.sheet);
    const values: number[] = [];
    const last = Math.min(ask.bottom, rows.length - 1);
    for (let row = ask.top; row <= last; row += 1) {
      const cells = rows[row];
      if (!cells) continue;
      for (let column = ask.left; column <= ask.right; column += 1)
        if (cells[column]?.kind === 'n') values.push(Number(cells[column]?.value));
    }
    return { op: 'stats', stats: statsOf(values) };
  }
  if (ask.op === 'edit') {
    const sheet = book.sheets[ask.sheet];
    const changed: { sheet: number; row: number; column: number; cell: IShownCell | null }[] = [];
    for (const edit of ask.edits) {
      const before = sheet.rows[edit.row]?.[edit.column];
      setCell(book, ask.sheet, edit.row, edit.column, cellOfInput(edit.input, before?.style ?? 0));
    }
    for (const { row, column, input } of ask.edits) unsaved.push({ sheet: sheet.name, row, column, input });
    const counted = recount(book, ask.sheet, ask.edits);
    for (const { row, column } of ask.edits)
      changed.push({ sheet: ask.sheet, row, column, cell: shown(sheet.rows[row]?.[column]) });
    for (const place of counted)
      changed.push({ ...place, cell: shown(book.sheets[place.sheet].rows[place.row]?.[place.column]) });
    return { op: 'edit', changed, rows: sheet.rows.length, columns: sheet.columns };
  }
  inTheSave = unsaved.length;
  const bytes = book.kind === 'csv' ? writeDelimited(book) : writeXlsx(book);
  return { op: 'save', bytes };
};

self.onmessage = (event: MessageEvent<{ id: number; ask: ISheetAsk }>) => {
  const { id, ask } = event.data;
  try {
    const reply = answer(ask);
    const transfer = reply.op === 'save' ? [reply.bytes.buffer as ArrayBuffer] : [];
    self.postMessage({ id, reply }, { transfer });
  } catch (failure) {
    self.postMessage({ id, failure: failure instanceof Error ? failure.message : String(failure) });
  }
};
