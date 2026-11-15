import { describe, expect, it } from 'vitest';
import {
  clearSpaceLine,
  gistOf,
  questionOf,
  showSpaceLine,
  spaceLineNow,
} from '@/next/features/space-field/line';

describe('gistOf: one line of an answer', () => {
  it('takes the first sentence, without markdown', () => {
    expect(gistOf('**Готово.** Добавил три карточки в колонку «Сделано».\n\nДальше — детали.')).toBe(
      'Готово. Добавил три карточки в колонку «Сделано».',
    );
  });
  it('cuts a long one at a word', () => {
    const g = gistOf('слово '.repeat(60), 40);
    expect(g.endsWith('…')).toBe(true);
    expect(g.length).toBeLessThanOrEqual(41);
  });
  it('drops a list marker and a link', () => {
    expect(gistOf('- Открыл [доску](https://views.example.test/b/x) и поправил подписи')).toBe(
      'Открыл доску и поправил подписи',
    );
  });
});

describe('questionOf: the agent asks', () => {
  it('finds the closing question', () => {
    expect(questionOf('Нашёл две колонки «План».\n\nОбъединить их в одну?')).toBe('Объединить их в одну?');
  });
  it('is null for a statement', () => {
    expect(questionOf('Готово, поправил.')).toBeNull();
  });
});

describe('the line', () => {
  it('a newer line is not cleared by an older one', () => {
    showSpaceLine({ text: 'a' });
    const first = spaceLineNow();
    showSpaceLine({ text: 'b' });
    clearSpaceLine(first);
    expect(spaceLineNow()?.text).toBe('b');
    clearSpaceLine();
    expect(spaceLineNow()).toBeNull();
  });
});
