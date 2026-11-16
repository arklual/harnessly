// an output cut by its size says so in KB, and one cut by its lines in lines — not one phrase for both.
import { describe, expect, it } from 'vitest';
import { cutBySize, size } from '@/next/i18n/format';
import { pinLocale, plural } from '@/next/i18n';

describe('a cut output', () => {
  it('by size, in KB or MB', () => {
    pinLocale('ru');
    expect(size(4096)).toBe('4 КБ');
    expect(size(10)).toBe('1 КБ');
    expect(size(1.5 * 1024 * 1024)).toBe('1,5 МБ');
    expect(cutBySize(4096, 122_880)).toBe('Первые 4 КБ из 120 КБ');
  });

  it('by lines, with the lines counted', () => {
    pinLocale('ru');
    expect(plural(19, 'next.tool.shownLines', { shown: 4 })).toBe('Показано 4 из 19 строк');
    expect(plural(21, 'next.tool.shownLines', { shown: 4 })).toBe('Показано 4 из 21 строки');
    pinLocale('en');
    expect(cutBySize(4096, 8192)).toBe('First 4 KB of 8 KB');
  });
});
