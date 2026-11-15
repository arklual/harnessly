import { describe, expect, it } from 'vitest';
import { lastLineOf, linksOf } from '@/next/features/designly/last-line';

describe('lastLineOf', () => {
  it('takes the last paragraph without its markdown', () => {
    expect(lastLineOf('Поправил шапку.\n\n- **Кнопка** стала [синей](x.html)')).toBe('Кнопка стала синей');
    expect(lastLineOf('## Готово\n\n```\ncode\n```\n')).toBe('Готово');
  });

  it('has nothing to say for an empty answer', () => {
    expect(lastLineOf('')).toBeNull();
    expect(lastLineOf('---\n\n')).toBeNull();
  });
});

describe('linksOf', () => {
  it('finds markdown and bare links of the prose, last first, without code', () => {
    const md = [
      'Готово: [Схема оплаты](https://docs.example.com/b/plan-1).',
      '```',
      'https://docs.example.com/b/in-code',
      '```',
      'Ещё тут https://docs.example.com/a/doc-2.',
    ].join('\n');
    expect(linksOf(md)).toEqual([
      { href: 'https://docs.example.com/a/doc-2', text: '' },
      { href: 'https://docs.example.com/b/plan-1', text: 'Схема оплаты' },
    ]);
  });

  it('has none in plain words', () => {
    expect(linksOf('Поправил шапку.')).toEqual([]);
  });
});
