import { describe, expect, it } from 'vitest';
import { pagesFound, rowAfter } from '@/next/features/designly/page-pick';

const pages = [
  { path: 'home.html', name: 'Главная' },
  { path: 'offer.html', name: 'Выдача' },
  { path: 'ds/buttons.html', name: 'Кнопки' },
];

describe('pagesFound', () => {
  it('finds by the name or the path, any case, all words', () => {
    expect(pagesFound(pages, 'выд')).toEqual([pages[1]]);
    expect(pagesFound(pages, 'DS BUT')).toEqual([pages[2]]);
    expect(pagesFound(pages, 'html')).toHaveLength(3);
    expect(pagesFound(pages, 'нет такой')).toEqual([]);
  });

  it('gives everything for an empty query', () => {
    expect(pagesFound(pages, '  ')).toEqual(pages);
  });
});

describe('rowAfter', () => {
  it('goes round the list with the arrows', () => {
    expect(rowAfter('ArrowDown', 2, 3)).toBe(0);
    expect(rowAfter('ArrowDown', -1, 3)).toBe(0);
    expect(rowAfter('ArrowUp', 0, 3)).toBe(2);
    expect(rowAfter('ArrowUp', 2, 3)).toBe(1);
  });

  it('jumps to the ends and stays on other keys', () => {
    expect(rowAfter('Home', 2, 3)).toBe(0);
    expect(rowAfter('End', 0, 3)).toBe(2);
    expect(rowAfter('a', 1, 3)).toBe(1);
  });

  it('has no row in an empty list', () => {
    expect(rowAfter('ArrowDown', 0, 0)).toBe(-1);
  });
});
