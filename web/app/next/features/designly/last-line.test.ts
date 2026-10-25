import { describe, expect, it } from 'vitest';
import { lastLineOf } from '@/next/features/designly/last-line';

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
