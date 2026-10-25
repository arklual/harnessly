import { describe, expect, it } from 'vitest';
import { partsOf } from '@/next/features/search/highlight';

describe('partsOf', () => {
  it('lights every word of the query, ignoring case and ё', () => {
    expect(partsOf('Ещё один Релиз', 'еще релиз')).toEqual([
      { text: 'Ещё', hit: true },
      { text: ' один ', hit: false },
      { text: 'Релиз', hit: true },
    ]);
  });

  it('takes the query literally', () => {
    expect(partsOf('a+b (c)', '(c)')).toEqual([
      { text: 'a+b ', hit: false },
      { text: '(c)', hit: true },
    ]);
    expect(partsOf('text', '  ')).toEqual([{ text: 'text', hit: false }]);
  });
});
