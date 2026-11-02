import { strFromU8, strToU8, unzipSync, zipSync } from 'fflate';
import { describe, expect, it } from 'vitest';
import {
  addressOf,
  cellOfInput,
  columnIndex,
  columnName,
  displayOf,
  inputOf,
  loadSheet,
  parseDelimited,
  patchSheetXml,
  readDelimited,
  readXlsx,
  setCell,
  writeDelimited,
  writeXlsx,
} from '@/next/features/pane/sheet/sheet-core';
import { formatValue, isDateFormat } from '@/next/features/pane/sheet/sheet-format';

const SHEET = `<?xml version="1.0"?><worksheet xmlns="x"><dimension ref="A1:B2"/><sheetViews><sheetView><pane ySplit="1" topLeftCell="A2" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="1" width="20"/></cols><sheetData><row r="1" spans="1:2"><c r="A1" t="s"><v>0</v></c><c r="B1" s="1"><v>1234.5</v></c></row><row r="3"><c r="A3"><f>B1*2</f><v>2469</v></c></row></sheetData><drawing r:id="rId9"/></worksheet>`;

const book = () =>
  zipSync({
    'xl/workbook.xml': strToU8(
      '<workbook><sheets><sheet name="Лист &amp; 1" sheetId="1" r:id="rId1"/><sheet name="Скрытый" sheetId="2" state="hidden" r:id="rId2"/></sheets></workbook>',
    ),
    'xl/_rels/workbook.xml.rels': strToU8(
      '<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/><Relationship Id="rId3" Type="x/calcChain" Target="calcChain.xml"/></Relationships>',
    ),
    '[Content_Types].xml': strToU8('<Types><Override PartName="/xl/calcChain.xml" ContentType="y"/></Types>'),
    'xl/calcChain.xml': strToU8('<calcChain/>'),
    'xl/worksheets/sheet1.xml': strToU8(SHEET),
    'xl/worksheets/sheet2.xml': strToU8('<worksheet><sheetData/></worksheet>'),
    'xl/sharedStrings.xml': strToU8('<sst><si><t>Привет &lt;мир&gt;</t></si></sst>'),
    'xl/styles.xml': strToU8(
      '<styleSheet><fonts><font/><font><b/><color rgb="FFFF0000"/></font></fonts><fills><fill><patternFill patternType="none"/></fill></fills><cellXfs><xf numFmtId="0" fontId="0" fillId="0"/><xf numFmtId="4" fontId="1" fillId="0"/></cellXfs></styleSheet>',
    ),
    'xl/media/chart.bin': new Uint8Array([1, 2, 3, 250]),
  });

describe('a delimited file written back', () => {
  it('keeps its line ends and how it stopped', () => {
    const windows = readDelimited(strToU8('a;b\r\n1;2\r\n'), 'x.csv');
    expect(strFromU8(writeDelimited(windows))).toBe('a;b\r\n1;2\r\n');
    const bare = readDelimited(strToU8('a,b\n1,2'), 'x.csv');
    expect(strFromU8(writeDelimited(bare))).toBe('a,b\n1,2');
  });
});

describe('reading a spreadsheet', () => {
  it('names columns both ways', () => {
    expect(columnName(0)).toBe('A');
    expect(columnName(26)).toBe('AA');
    expect(columnIndex('AB')).toBe(27);
    expect(addressOf(2, 1)).toBe('B3');
  });

  it('reads sheets, skips the hidden, and knows shared strings, styles, widths and the frozen row', () => {
    const b = readXlsx(book());
    expect(b.sheets.map((s) => s.name)).toEqual(['Лист & 1']);
    const sheet = loadSheet(b, 0);
    expect(sheet.frozenRows).toBe(1);
    expect(sheet.widths.get(0)).toBe(20);
    expect(displayOf(b, sheet.rows[0][0])).toBe('Привет <мир>');
    expect(displayOf(b, sheet.rows[0][1])).toBe('1 234,50');
    expect(b.styles[1]).toMatchObject({ bold: true, color: 'ff0000' });
    expect(inputOf(sheet.rows[2][0])).toBe('=B1*2');
  });

  it('formats numbers and dates as the file says', () => {
    expect(formatValue(0.256, '0.0%', false)).toBe('25,6%');
    expect(formatValue(1234567.891, '#,##0.00', false)).toBe('1 234 567,89');
    expect(formatValue(45993, 'dd.mm.yyyy', false)).toBe('02.12.2025');
    expect(formatValue(0.5, 'h:mm', false)).toBe('12:00');
    expect(formatValue(1.5, 'General', false)).toBe('1,5');
    expect(isDateFormat('#,##0.00')).toBe(false);
    expect(isDateFormat('yyyy-mm-dd')).toBe(true);
    expect(isDateFormat('"мес." 0')).toBe(false);
  });

  it('takes what was typed for what it is', () => {
    expect(cellOfInput('12,5', 0)).toMatchObject({ kind: 'n', value: '12.5' });
    expect(cellOfInput('=SUM(A1:A2)', 3)).toMatchObject({ formula: 'SUM(A1:A2)', style: 3 });
    expect(cellOfInput('привет', 0)).toMatchObject({ kind: 's', value: 'привет' });
    expect(cellOfInput('', 0)).toBeNull();
    expect(cellOfInput('', 2)).toMatchObject({ value: '', style: 2 });
  });
});

describe('writing a spreadsheet', () => {
  it('changes only the edited cells and leaves the rest of the XML as it was', () => {
    const patched = patchSheetXml(SHEET, new Map([['B1', { kind: 'n', value: '7', style: 1 }]]));
    expect(patched).toContain('<c r="B1" s="1"><v>7</v></c>');
    expect(patched).toContain('<c r="A1" t="s"><v>0</v></c>');
    expect(patched).toContain('<row r="3"><c r="A3"><f>B1*2</f><v>2469</v></c></row>');
    expect(patched).toContain('<drawing r:id="rId9"/>');
    expect(patched).not.toContain('spans=');
    expect(patched).not.toContain('<dimension');
  });

  it('puts a new cell in column order and a new row in row order', () => {
    const patched = patchSheetXml(
      SHEET,
      new Map([
        ['C3', { kind: 's', value: 'a<b', style: 0 }],
        ['A2', { kind: 'n', value: '1', style: 0 }],
        ['D9', { kind: 'n', value: '2', style: 0 }],
      ]),
    );
    const order = [...patched.matchAll(/<row r="(\d+)"/g)].map((m) => m[1]);
    expect(order).toEqual(['1', '2', '3', '9']);
    expect(patched).toContain(
      '<c r="A3"><f>B1*2</f><v>2469</v></c><c r="C3" t="inlineStr"><is><t xml:space="preserve">a&lt;b</t></is></c>',
    );
  });

  it('removes a cleared cell and writes into an empty sheet', () => {
    expect(patchSheetXml(SHEET, new Map([['A1', null]]))).not.toContain('r="A1"');
    expect(
      patchSheetXml(
        '<worksheet><sheetData/></worksheet>',
        new Map([['A1', { kind: 'n', value: '1', style: 0 }]]),
      ),
    ).toBe('<worksheet><sheetData><row r="1"><c r="A1"><v>1</v></c></row></sheetData></worksheet>');
  });

  it('round-trips the file: other parts untouched, calc chain gone, recount asked', () => {
    const b = readXlsx(book());
    setCell(b, 0, 0, 1, { kind: 'n', value: '9', style: 1 });
    const again = unzipSync(writeXlsx(b));
    expect([...again['xl/media/chart.bin']]).toEqual([1, 2, 3, 250]);
    expect(again['xl/calcChain.xml']).toBeUndefined();
    expect(strFromU8(again['[Content_Types].xml'])).not.toContain('calcChain');
    expect(strFromU8(again['xl/_rels/workbook.xml.rels'])).not.toContain('calcChain');
    expect(strFromU8(again['xl/workbook.xml'])).toContain('fullCalcOnLoad="1"');
    expect(strFromU8(again['xl/worksheets/sheet2.xml'])).toBe('<worksheet><sheetData/></worksheet>');
    const reread = readXlsx(writeXlsx(b));
    expect(displayOf(reread, loadSheet(reread, 0).rows[0][1])).toBe('9,00');
  });
});

describe('a CSV', () => {
  it('reads quotes, new lines inside a field and both line ends', () => {
    expect(parseDelimited('a,"b ""c"", d"\r\n1,"x\ny"\n', ',')).toEqual([
      ['a', 'b "c", d'],
      ['1', 'x\ny'],
    ]);
  });

  it('guesses the separator and writes the file back as it was', () => {
    const source = 'имя;сумма\nАня;007\n"Б;В";1.50\n';
    const b = readDelimited(strToU8(source), 'a.csv');
    expect(b.delimiter).toBe(';');
    expect(strFromU8(writeDelimited(b))).toBe(source);
  });

  it('keeps a BOM and reads the Russian Excel encoding', () => {
    const withBom = new Uint8Array([0xef, 0xbb, 0xbf, ...strToU8('a,b\n')]);
    expect([...writeDelimited(readDelimited(withBom, 'a.csv'))].slice(0, 3)).toEqual([0xef, 0xbb, 0xbf]);
    const cp1251 = new Uint8Array([0xcf, 0xf0, 0xe8, 0xe2, 0xe5, 0xf2]);
    const b = readDelimited(cp1251, 'a.csv');
    expect(b.sheets[0].rows[0][0]?.value).toBe('Привет');
    expect([...writeDelimited(b)]).toEqual([...cp1251]);
  });
});
