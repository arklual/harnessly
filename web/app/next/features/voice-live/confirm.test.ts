import { describe, expect, it } from 'vitest';
import { ANSWER, answerOf, needsYes } from '@/next/features/voice-live/confirm';

describe('needsYes', () => {
  it.each([
    'Удали прошлую страницу',
    'удалить чат',
    'Отправь документ всем участникам',
    'опубликуй доску',
    'Останови все чаты',
    'выкати в прод',
    'сделай релиз',
    'смержи ветку',
    'delete the page',
    'deploy it',
  ])('holds «%s»', (task) => expect(needsYes(task)).toBe(true));

  it.each([
    'Сделай заголовок крупнее',
    'Поменяй цвет кнопки на зелёный',
    'добавь блок с ценой',
    'make it bigger',
  ])('lets «%s» go', (task) => expect(needsYes(task)).toBe(false));
});

describe('answerOf', () => {
  it('reads yes, no and anything else', () => {
    expect(answerOf('Да')).toBe(ANSWER.yes);
    expect(answerOf('да, удаляй')).toBe(ANSWER.yes);
    expect(answerOf('нет')).toBe(ANSWER.no);
    expect(answerOf('Не надо.')).toBe(ANSWER.no);
    expect(answerOf('дай подумать')).toBe(ANSWER.other);
    expect(answerOf('давайте лучше заголовок')).toBe(ANSWER.other);
  });
});
