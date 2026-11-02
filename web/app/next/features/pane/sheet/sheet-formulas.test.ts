import { strToU8, zipSync } from 'fflate';
import { describe, expect, it } from 'vitest';
import { cellOfInput, loadSheet, readXlsx, setCell, type IBook } from '@/next/features/pane/sheet/sheet-core';
import { countUncounted, hasUncounted, recount } from '@/next/features/pane/sheet/sheet-formulas';

const sheetXml = (cells: string) => `<worksheet><sheetData>${cells}</sheetData></worksheet>`;

const bookOf = (first: string, second: string): IBook =>
  readXlsx(
    zipSync({
      'xl/workbook.xml': strToU8(
        '<workbook><sheets><sheet name="Один" sheetId="1" r:id="rId1"/><sheet name="Два" sheetId="2" r:id="rId2"/></sheets></workbook>',
      ),
      'xl/_rels/workbook.xml.rels': strToU8(
        '<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/></Relationships>',
      ),
      'xl/worksheets/sheet1.xml': strToU8(sheetXml(first)),
      'xl/worksheets/sheet2.xml': strToU8(sheetXml(second)),
    }),
  );

const valueAt = (book: IBook, sheet: number, row: number, column: number) =>
  loadSheet(book, sheet).rows[row]?.[column]?.value;

const FIRST =
  '<row r="1"><c r="A1"><v>2</v></c><c r="B1"><f>A1*3</f><v>6</v></c></row>' +
  '<row r="2"><c r="A2"><v>5</v></c><c r="B2"><f>SUM(A1:A2)</f><v>7</v></c></row>';
const SECOND = '<row r="1"><c r="A1"><f>Один!B1+1</f><v>7</v></c><c r="B1"><f>A1*2</f><v>14</v></c></row>';

describe('counting formulas again', () => {
  it('counts what reads an edited cell, across sheets and through ranges, and only that', () => {
    const book = bookOf(FIRST, SECOND);
    loadSheet(book, 0);
    setCell(book, 0, 0, 0, cellOfInput('10', 0));
    const changed = recount(book, 0, [{ row: 0, column: 0 }]);
    expect(valueAt(book, 0, 0, 1)).toBe('30');
    expect(valueAt(book, 0, 1, 1)).toBe('15');
    expect(valueAt(book, 1, 0, 0)).toBe('31');
    expect(valueAt(book, 1, 0, 1)).toBe('62');
    expect(changed).toHaveLength(4);
  });

  it('leaves alone what does not read the edit', () => {
    const book = bookOf(FIRST, SECOND);
    loadSheet(book, 0);
    setCell(book, 0, 1, 0, cellOfInput('6', 0));
    const changed = recount(book, 0, [{ row: 1, column: 0 }]);
    expect(valueAt(book, 0, 1, 1)).toBe('8');
    expect(changed).toEqual([{ sheet: 0, row: 1, column: 1 }]);
  });

  it('gets through a column deeper than the call stack, each after what it reads', () => {
    const rows = ['<row r="1"><c r="A1"><v>1</v></c></row>'];
    for (let row = 2; row <= 15000; row += 1)
      rows.push(`<row r="${row}"><c r="A${row}"><f>A${row - 1}+1</f><v>${row}</v></c></row>`);
    const book = bookOf(rows.join(''), '');
    loadSheet(book, 0);
    setCell(book, 0, 0, 0, cellOfInput('101', 0));
    recount(book, 0, [{ row: 0, column: 0 }]);
    expect(valueAt(book, 0, 14999, 0)).toBe('15100');
  });

  it('survives a cycle', () => {
    const book = bookOf(
      '<row r="1"><c r="A1"><f>B1+1</f><v>1</v></c><c r="B1"><f>A1+1</f><v>2</v></c></row>',
      '',
    );
    loadSheet(book, 0);
    setCell(book, 0, 0, 0, cellOfInput('=B1+2', 0));
    expect(() => recount(book, 0, [{ row: 0, column: 0 }])).not.toThrow();
  });
});

describe('a file saved with no values', () => {
  const FIRST_BARE =
    '<row r="1"><c r="A1"><v>2</v></c><c r="B1"><f>C1*3</f></c><c r="C1"><f>A1+1</f></c></row>';

  it('counts its formulas to show them, whatever order they come in, and changes nothing in the file', () => {
    const book = bookOf(FIRST_BARE, '<row r="1"><c r="A1"><f>Один!B1+1</f></c></row>');
    expect(hasUncounted(loadSheet(book, 0))).toBe(true);
    countUncounted(book);
    expect(valueAt(book, 0, 0, 2)).toBe('3');
    expect(valueAt(book, 0, 0, 1)).toBe('9');
    expect(valueAt(book, 1, 0, 0)).toBe('10');
    expect(book.edits.size).toBe(0);
  });

  it('is not asked for when the values are there', () => {
    expect(hasUncounted(loadSheet(bookOf(FIRST, SECOND), 0))).toBe(false);
  });
});
