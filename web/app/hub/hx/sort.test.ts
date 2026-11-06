import { describe, expect, it } from 'vitest';

import { nextSort, readSort, searchRows, sortRows, withParams } from './sort';

const rows = [
  { n: 'бета', c: 5 },
  { n: 'альфа', c: 9 },
  { n: 'гамма', c: 5 },
];
const get = { n: (r: (typeof rows)[number]) => r.n, c: (r: (typeof rows)[number]) => r.c };

describe('sort', () => {
  it('sorts numbers and keeps equal rows in order', () => {
    expect(sortRows(rows, { key: 'c', dir: 'desc' }, get).map((r) => r.n)).toEqual([
      'альфа',
      'бета',
      'гамма',
    ]);
    expect(sortRows(rows, { key: 'c', dir: 'asc' }, get).map((r) => r.n)).toEqual(['бета', 'гамма', 'альфа']);
  });
  it('sorts texts the Russian way', () => {
    expect(sortRows(rows, { key: 'n', dir: 'asc' }, get).map((r) => r.n)).toEqual(['альфа', 'бета', 'гамма']);
  });
  it('turns a column around on the second click', () => {
    const a = nextSort({ key: null, dir: 'desc' }, 'c');
    expect(a).toEqual({ key: 'c', dir: 'desc' });
    expect(nextSort(a, 'c')).toEqual({ key: 'c', dir: 'asc' });
  });
  it('reads the address and ignores an unknown column', () => {
    const fb = { key: 'c', dir: 'desc' } as const;
    expect(readSort('?sort=n&dir=asc', ['n', 'c'], fb)).toEqual({ key: 'n', dir: 'asc' });
    expect(readSort('?sort=zzz', ['n', 'c'], fb)).toEqual(fb);
  });
  it('searches in any field', () => {
    expect(searchRows(rows, ' ГАМ ', (r) => [r.n])).toHaveLength(1);
    expect(searchRows(rows, '', (r) => [r.n])).toHaveLength(3);
  });
  it('sets and removes parameters', () => {
    expect(withParams('?a=1&b=2', { a: null, c: '3' })).toBe('?b=2&c=3');
    expect(withParams('?a=1', { a: null })).toBe('');
  });
});
