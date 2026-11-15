import { describe, expect, it } from 'vitest';
import { isBye, isWake } from '@/next/features/voice-live/wake';

describe('wake phrase', () => {
  it('is heard only as the whole phrase', () => {
    expect(isWake('привет хар несли')).toBe(true);
    expect(isWake('привет')).toBe(false);
    expect(isWake('хар несли')).toBe(false);
  });
});

describe('bye', () => {
  it('ends on a short line closed by «пока» or «спасибо, всё»', () => {
    for (const s of ['Пока!', 'Всё, пока.', 'Спасибо, всё.', 'Ну всё, спасибо', 'ладно спасибо всё'])
      expect(isBye(s)).toBe(true);
  });
  it('does not end on «пока» inside a request or a long line', () => {
    for (const s of [
      'Пока ты это делаешь, открой доску',
      'Пока не надо',
      'спасибо',
      '',
      'сделай заголовок крупнее и проверь отступы, пока',
    ])
      expect(isBye(s)).toBe(false);
  });
});
