import { describe, expect, it } from 'vitest';
import { parseAnnotationBlock } from '@/lib/hl-annotation-format';

describe('a sent space block (HRN-1452)', () => {
  it('reads back as the board and the selection, not «element»', () => {
    const block = [
      '<annotation n="1" kind="space">',
      'open: board «Plan 07.11.26» https://a.example.com/b/plan',
      'slug: plan',
      'selection: Рилсы',
      'rule: человек работает в этом месте.',
      '</annotation>',
    ].join('\n');
    expect(parseAnnotationBlock(block)?.label).toBe('Plan 07.11.26 · Рилсы');
    expect(parseAnnotationBlock(block.replace('selection: Рилсы\n', ''))?.label).toBe('Plan 07.11.26');
  });
});
