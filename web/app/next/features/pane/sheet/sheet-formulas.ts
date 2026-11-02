// Formulas counted again after an edit, inside the worker. A formula this engine cannot count keeps its last value and the
// file asks the next program to count everything (sheet-core writeXlsx), so a wrong number never gets written as a right one.
import FormulaParser, { DepParser } from 'fast-formula-parser';
import {
  addressOf,
  CELL_KIND,
  loadSheet,
  type IBook,
  type ICell,
  type ISheet,
} from '@/next/features/pane/sheet/sheet-core';

/** Past this the file is left for its own program: counting would hold up every edit. */
const MAX_FORMULAS = 20000;

interface IRef {
  sheet?: string;
  row?: number;
  col?: number;
  from?: { row: number; col: number };
  to?: { row: number; col: number };
}

interface ICellPlace {
  row: number;
  column: number;
}

interface IFormulaPlace {
  sheet: number;
  row: number;
  column: number;
}

const valueOf = (cell: ICell | undefined): number | string | boolean | null => {
  if (!cell || (cell.value === '' && !cell.formula)) return null;
  if (cell.kind === CELL_KIND.number) return Number(cell.value);
  if (cell.kind === CELL_KIND.bool) return cell.value === '1';
  return cell.value;
};

const sheetIndex = (book: IBook, name: string | undefined, fallback: number): number => {
  if (!name) return fallback;
  const index = book.sheets.findIndex((sheet) => sheet.name === name);
  return index < 0 ? fallback : index;
};

const formulasOf = (book: IBook): IFormulaPlace[] | null => {
  const places: IFormulaPlace[] = [];
  for (let sheet = 0; sheet < book.sheets.length; sheet += 1) {
    const { rows }: ISheet = loadSheet(book, sheet);
    for (let row = 0; row < rows.length; row += 1) {
      const cells = rows[row];
      if (!cells) continue;
      for (let column = 0; column < cells.length; column += 1) {
        if (!cells[column]?.formula) continue;
        places.push({ sheet, row, column });
        if (places.length > MAX_FORMULAS) return null;
      }
    }
  }
  return places;
};

const keyOf = (sheet: number, row: number, column: number) => `${sheet}!${row}:${column}`;

export interface IChangedCell {
  sheet: number;
  row: number;
  column: number;
}

const parserOf = (book: IBook) =>
  new FormulaParser({
    onCell: (ref: IRef) =>
      valueOf(book.sheets[sheetIndex(book, ref.sheet, 0)]?.rows[(ref.row ?? 1) - 1]?.[(ref.col ?? 1) - 1]),
    onRange: (ref: IRef) => {
      const rows = book.sheets[sheetIndex(book, ref.sheet, 0)]?.rows ?? [];
      const from = ref.from ?? { row: 1, col: 1 };
      const to = ref.to ?? from;
      const lastRow = Math.min(to.row, rows.length);
      const out: (number | string | boolean | null)[][] = [];
      for (let at = from.row; at <= lastRow; at += 1) {
        const line: (number | string | boolean | null)[] = [];
        for (let col = from.col; col <= to.col; col += 1) line.push(valueOf(rows[at - 1]?.[col - 1]));
        out.push(line);
      }
      return out;
    },
  });

/** Counts one formula into its cell. False when the engine cannot count it: the cell keeps what it held. */
const countCell = (parser: FormulaParser, book: IBook, place: IFormulaPlace): boolean => {
  const cell = book.sheets[place.sheet].rows[place.row]?.[place.column];
  if (!cell?.formula) return false;
  try {
    const result: unknown = parser.parse(cell.formula, {
      sheet: book.sheets[place.sheet].name,
      row: place.row + 1,
      col: place.column + 1,
    });
    if (typeof result === 'number') {
      cell.kind = CELL_KIND.number;
      cell.value = String(result);
    } else if (typeof result === 'boolean') {
      cell.kind = CELL_KIND.bool;
      cell.value = result ? '1' : '0';
    } else if (typeof result === 'string') {
      cell.kind = CELL_KIND.text;
      cell.value = result;
    } else if (result && typeof result === 'object' && '_error' in result) {
      cell.kind = CELL_KIND.error;
      cell.value = String((result as { _error: unknown })._error);
    } else return false;
    return true;
  } catch {
    return false;
  }
};

/** Who reads a cell: formulas found by the cell they name, and those reading a range looked through. */
const readersOf = (book: IBook, places: readonly IFormulaPlace[]) => {
  const dependency = new DepParser({ onVariable: () => null });
  const singles = new Map<string, number[]>();
  const ranges: { at: number; sheet: number; top: number; left: number; bottom: number; right: number }[] =
    [];
  places.forEach((place, at) => {
    const cell = book.sheets[place.sheet].rows[place.row]?.[place.column];
    let refs: IRef[] = [];
    try {
      refs = dependency.parse(cell?.formula ?? '', {
        sheet: book.sheets[place.sheet].name,
        row: place.row + 1,
        col: place.column + 1,
      }) as IRef[];
    } catch {
      return;
    }
    for (const ref of refs) {
      const home = sheetIndex(book, ref.sheet, place.sheet);
      if (ref.from && ref.to)
        ranges.push({
          at,
          sheet: home,
          top: ref.from.row - 1,
          left: ref.from.col - 1,
          bottom: ref.to.row - 1,
          right: ref.to.col - 1,
        });
      else if (ref.row !== undefined && ref.col !== undefined) {
        const key = keyOf(home, ref.row - 1, ref.col - 1);
        const list = singles.get(key);
        if (list) list.push(at);
        else singles.set(key, [at]);
      }
    }
  });
  return (target: IChangedCell): number[] => {
    const found = [...(singles.get(keyOf(target.sheet, target.row, target.column)) ?? [])];
    for (const range of ranges)
      if (
        range.sheet === target.sheet &&
        target.row >= range.top &&
        target.row <= range.bottom &&
        target.column >= range.left &&
        target.column <= range.right
      )
        found.push(range.at);
    return found;
  };
};

/**
 * Counts again the edited cells' formulas and everything downstream, each after what it reads. A cycle is walked once and
 * left as it comes out. Walked with a list and not by calling itself: a long column of =A1+1 runs deeper than the call stack.
 * Returns the cells it changed, which the save writes.
 */
export const recount = (book: IBook, sheet: number, edited: readonly ICellPlace[]): IChangedCell[] => {
  const places = formulasOf(book);
  if (places === null) return [];
  const readers = readersOf(book, places);
  const indexOf = new Map(places.map((place, at) => [keyOf(place.sheet, place.row, place.column), at]));
  const seen = new Set<number>();
  const finished: number[] = [];
  const walk = (start: IChangedCell) => {
    const stack = [
      { at: indexOf.get(keyOf(start.sheet, start.row, start.column)) ?? -1, next: readers(start), read: 0 },
    ];
    if (stack[0].at >= 0) {
      if (seen.has(stack[0].at)) return;
      seen.add(stack[0].at);
    }
    while (stack.length > 0) {
      const top = stack[stack.length - 1];
      if (top.read < top.next.length) {
        const at = top.next[top.read];
        top.read += 1;
        if (seen.has(at)) continue;
        seen.add(at);
        stack.push({ at, next: readers(places[at]), read: 0 });
      } else {
        stack.pop();
        if (top.at >= 0) finished.push(top.at);
      }
    }
  };
  for (const { row, column } of edited) walk({ sheet, row, column });

  const parser = parserOf(book);
  const changed: IChangedCell[] = [];
  for (const at of finished.reverse()) {
    const place = places[at];
    if (!countCell(parser, book, place)) continue;
    const path = book.kind === 'csv' ? 'csv' : book.sheets[place.sheet].path;
    let list = book.edits.get(path);
    if (!list) {
      list = new Map();
      book.edits.set(path, list);
    }
    const cell = book.sheets[place.sheet].rows[place.row]?.[place.column];
    if (cell) list.set(addressOf(place.row, place.column), cell);
    changed.push({ sheet: place.sheet, row: place.row, column: place.column });
  }
  return changed;
};

/** A file saved by a program that does not count (one an agent made, often) holds formulas with no value. */
export const hasUncounted = ({ rows }: ISheet): boolean =>
  rows.some((cells) => cells?.some((cell) => Boolean(cell?.formula) && cell?.value === ''));

/**
 * Counts the formulas that came with no value, only to show them: the file is not changed by being opened. A formula reading
 * another of them may come first, so it goes round until nothing moves, and no longer than a chain could need.
 */
export const countUncounted = (book: IBook): void => {
  const places = formulasOf(book);
  if (places === null) return;
  const parser = parserOf(book);
  const open = places.filter(
    (place) => book.sheets[place.sheet].rows[place.row]?.[place.column]?.value === '',
  );
  for (let round = 0; round < 32; round += 1) {
    let moved = false;
    for (const place of open) {
      const cell = book.sheets[place.sheet].rows[place.row]?.[place.column];
      const before = cell?.value;
      if (countCell(parser, book, place) && cell?.value !== before) moved = true;
    }
    if (!moved) return;
  }
};
