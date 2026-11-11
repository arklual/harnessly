import { describe, expect, it } from 'vitest';
import { analysisQuestion, parseAnalysis } from '@/next/features/review/app/thread-analysis';

const notes = [
  { who: 'Борис', body: 'Линейный поиск — может, map?' },
  { who: 'Олег', body: 'Записей единицы, оставлю.' },
];
const words = {
  askOne: 'Разбери комментарий {who} (№{n}).',
  askAll: 'Разбери обсуждение.',
  formatOne: 'Ответь блоком:',
  formatAll: 'Ответь блоком:',
};
const block = (json: string) => `Вот.\n\`\`\`review-analysis\n${json}\n\`\`\``;

it('asks about one comment or the whole discussion', () => {
  const one = analysisQuestion(notes, 0, 'registry.go:30', words);
  expect(one).toContain('Разбери комментарий Борис (№1).');
  expect(one).toContain('2. Олег: Записей единицы, оставлю.');
  expect(one).toContain('```review-analysis\n{"why"');
  expect(analysisQuestion(notes, null, 'x', words)).toContain('{"points"');
});

describe('parseAnalysis', () => {
  it('reads one comment', () => {
    expect(parseAnalysis(block('{"why":" Прав. ","replies":["Да","", 3]}'), notes[0] ?? null)).toEqual({
      points: [{ who: 'Борис', why: 'Прав.' }],
      sum: null,
      replies: ['Да'],
    });
  });
  it('reads a discussion and drops broken points', () => {
    const got = parseAnalysis(
      block(
        '{"points":[{"who":"Борис","why":"Прав"},{"who":"Олег"},null],"sum":"Оставить","replies":["Ок"]}',
      ),
      null,
    );
    expect(got).toEqual({ points: [{ who: 'Борис', why: 'Прав' }], sum: 'Оставить', replies: ['Ок'] });
  });
  it('is null without a block, with broken JSON or with nothing in it', () => {
    expect(parseAnalysis('просто текст', null)).toBeNull();
    expect(parseAnalysis(block('{oops'), null)).toBeNull();
    expect(parseAnalysis(block('{}'), null)).toBeNull();
  });
});
