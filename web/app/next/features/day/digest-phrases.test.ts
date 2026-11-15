import { describe, expect, it } from 'vitest';
import { dayPhrases } from '@/next/features/day/digest-phrases';

describe('dayPhrases', () => {
  it('cuts at the ends of sentences and anchors a phrase to the thing it links to', () => {
    const out = dayPhrases([
      'Сегодня три встречи. Главное — ',
      { t: 'ответить Роме', item: 42 },
      ' до обеда! Вечер свободен.',
    ]);
    expect(out.map((p) => p.text)).toEqual([
      'Сегодня три встречи.',
      'Главное — ответить Роме до обеда!',
      'Вечер свободен.',
    ]);
    expect(out.map((p) => p.anchor)).toEqual(['digest:0', '42', 'digest:2']);
    expect(out[1].item).toBe(42);
  });

  it('keeps every part on screen, links unsplit', () => {
    const parts = ['Утро. ', { t: 'Слот. Встреча', slot: '9' }, ' в девять.  '];
    const out = dayPhrases(parts);
    const back = out.flatMap((p) => p.parts.map((x) => (typeof x === 'string' ? x : x.t))).join('');
    expect(back).toBe('Утро. Слот. Встреча в девять.  ');
    expect(out).toHaveLength(2);
  });

  it('gives nothing for an empty line', () => {
    expect(dayPhrases(['  '])).toEqual([]);
  });
});
