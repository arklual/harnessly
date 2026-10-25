import { describe, expect, it } from 'vitest';
import {
  addQuick,
  addRecent,
  clampSelection,
  filterRows,
  HISTORY_LIMIT,
  moveSelection,
  quickName,
  RECENT_SHOWN,
  ROW_KIND,
  rowText,
  slashQuery,
} from '@/next/features/composer/prompts';

const QUICK = [
  { name: 'review', text: 'Look at the diff against main' },
  { name: 'tests', text: 'Write a failing test first' },
];
const RECENT = ['Find why payment fails', 'Compare payouts with the registry', 'Make the build faster'];

describe('filterRows', () => {
  it('shows the quick prompts, then the sent texts, when nothing is typed', () => {
    const rows = filterRows(RECENT, QUICK, '', false);
    expect(rows.map((row) => row.kind)).toEqual([
      ROW_KIND.quick,
      ROW_KIND.quick,
      ROW_KIND.recent,
      ROW_KIND.recent,
      ROW_KIND.recent,
    ]);
    expect(rowText(rows[0])).toBe(QUICK[0].text);
    expect(rowText(rows[2])).toBe(RECENT[0]);
  });

  it('narrows both lists by the typed text, in any case', () => {
    const rows = filterRows(RECENT, QUICK, 'FAIL', false);
    expect(rows.map(rowText)).toEqual(['Write a failing test first', 'Find why payment fails']);
  });

  it('after «/» looks only at the names of the quick prompts', () => {
    expect(filterRows(RECENT, QUICK, 'te', true).map(rowText)).toEqual([QUICK[1].text]);
    // «failing» is in the text of a quick prompt, not in its name.
    expect(filterRows(RECENT, QUICK, 'failing', true)).toEqual([]);
    expect(filterRows(RECENT, QUICK, '', true)).toHaveLength(QUICK.length);
  });

  it('shows only the newest few sent texts', () => {
    const many = Array.from({ length: 20 }, (_, index) => `text ${index}`);
    expect(filterRows(many, [], '', false)).toHaveLength(RECENT_SHOWN);
  });
});

describe('selection', () => {
  it('goes round the list in both directions', () => {
    expect(moveSelection(0, 1, 3)).toBe(1);
    expect(moveSelection(2, 1, 3)).toBe(0);
    expect(moveSelection(0, -1, 3)).toBe(2);
    expect(moveSelection(0, 1, 0)).toBe(0);
  });

  it('lands on the last row when the list shrank under it', () => {
    expect(clampSelection(5, 3)).toBe(2);
    expect(clampSelection(1, 3)).toBe(1);
    expect(clampSelection(2, 0)).toBe(0);
  });
});

describe('the history', () => {
  it('puts a sent text first and does not repeat it', () => {
    expect(addRecent(['a', 'b', 'c'], '  b ')).toEqual(['b', 'a', 'c']);
    expect(addRecent(['a'], '   ')).toEqual(['a']);
  });

  it('keeps the last 50 texts', () => {
    const full = Array.from({ length: HISTORY_LIMIT }, (_, index) => `text ${index}`);
    const next = addRecent(full, 'new');
    expect(next).toHaveLength(HISTORY_LIMIT);
    expect(next[0]).toBe('new');
    expect(next).not.toContain(`text ${HISTORY_LIMIT - 1}`);
  });

  it('replaces a quick prompt of the same name', () => {
    const next = addQuick(QUICK, { name: 'tests', text: 'Cover it' });
    expect(next.map((item) => item.name)).toEqual(['review', 'tests']);
    expect(next[1].text).toBe('Cover it');
  });
});

describe('names and commands', () => {
  it('takes one word of 2–20 letters, digits or dashes, with or without «/»', () => {
    expect(quickName(' /Ревью ')).toBe('ревью');
    expect(quickName('a')).toBeNull();
    expect(quickName('two words')).toBeNull();
    expect(quickName('x'.repeat(21))).toBeNull();
  });

  it('reads «/» at the start of the field with no space as a command', () => {
    expect(slashQuery('/')).toBe('');
    expect(slashQuery('/rev')).toBe('rev');
    expect(slashQuery('/rev now')).toBeNull();
    expect(slashQuery('a /rev')).toBeNull();
  });
});
