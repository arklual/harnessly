import { describe, expect, it } from 'vitest';
import {
  clampPlace,
  columnAt,
  columnOffsets,
  columnPixels,
  fromClipboard,
  inRange,
  parseCellPlace,
  rangeOf,
  statsOf,
  stylesToCss,
  toClipboard,
} from '@/next/features/pane/sheet/sheet-model';

describe('a place in a sheet', () => {
  const sheets = ['Продажи', 'Итог всего'];
  it('names a cell, a range and a sheet', () => {
    expect(parseCellPlace('D2', sheets)).toEqual({
      sheet: null,
      range: { top: 1, left: 3, bottom: 1, right: 3 },
    });
    expect(parseCellPlace('Продажи!F9:D2', sheets)).toEqual({
      sheet: 0,
      range: { top: 1, left: 3, bottom: 8, right: 5 },
    });
    expect(parseCellPlace("'Итог всего'!$AA$1", sheets)?.sheet).toBe(1);
    expect(parseCellPlace("'Итог всего'!$AA$1", sheets)?.range.left).toBe(26);
  });
  it('refuses what names neither', () => {
    expect(parseCellPlace('Нет!D2', sheets)).toBeNull();
    expect(parseCellPlace('12.5', sheets)).toBeNull();
    expect(parseCellPlace('A0', sheets)).toBeNull();
  });
});

describe('the sheet grid', () => {
  it('sizes columns from the file and hides the hidden', () => {
    expect(columnPixels(undefined)).toBe(104);
    expect(columnPixels(0)).toBe(0);
    expect(columnPixels(10)).toBe(75);
    expect(columnPixels(1000)).toBe(520);
  });

  it('finds the column at a position', () => {
    const offsets = columnOffsets([100, 50, 100]);
    expect(offsets).toEqual([0, 100, 150, 250]);
    expect(columnAt(offsets, 0)).toBe(0);
    expect(columnAt(offsets, 120)).toBe(1);
    expect(columnAt(offsets, 9999)).toBe(2);
  });

  it('turns a selection dragged any way into one range', () => {
    const range = rangeOf({ anchor: { row: 5, column: 1 }, focus: { row: 2, column: 3 } });
    expect(range).toEqual({ top: 2, left: 1, bottom: 5, right: 3 });
    expect(inRange(range, 3, 2)).toBe(true);
    expect(inRange(range, 6, 2)).toBe(false);
    expect(clampPlace({ row: -1, column: 99 }, 10, 5)).toEqual({ row: 0, column: 4 });
  });

  it('adds up a selection', () => {
    expect(statsOf([1, 3])).toEqual({ sum: 4, average: 2, count: 2 });
    expect(statsOf([])).toBeNull();
  });

  it('carries cells through the clipboard and back', () => {
    const rows = [
      ['a', 'b\tc'],
      ['1', 'x\n"y"'],
    ];
    expect(fromClipboard(toClipboard(rows))).toEqual(rows);
    expect(fromClipboard('1\t2\r\n3\t4\r\n')).toEqual([
      ['1', '2'],
      ['3', '4'],
    ]);
  });

  it('keeps coloured cells readable on a dark window', () => {
    const base = { bold: false, italic: false, align: null };
    const css = stylesToCss([
      { ...base, color: null, fill: null },
      { ...base, bold: true, color: '000000', fill: null },
      { ...base, color: null, fill: 'ffff00' },
      { ...base, color: 'ff0000', fill: null },
    ]);
    expect(css).not.toContain('data-s="0"');
    expect(css).toContain('.hx-cells__cell[data-s="1"]{font-weight:600}');
    expect(css).toContain('background:#ffff00;color:#111111');
    expect(css).toContain('color:#ff0000');
  });
});
