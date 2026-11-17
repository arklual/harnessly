import { describe, expect, it } from 'vitest';
import { readVoiceRefusal } from './voice-api';

describe('readVoiceRefusal', () => {
  it('keeps the words the machine gave beside the code', () => {
    expect(
      readVoiceRefusal({ r: 'refused', code: 'internal', message: 'Для этой модели не сохранён ключ' }),
    ).toEqual({
      code: 'internal',
      message: 'Для этой модели не сохранён ключ',
    });
  });

  it('reads a refusal without words as an empty message, so the page says its own', () => {
    expect(readVoiceRefusal({ r: 'refused', code: 'internal' })).toEqual({ code: 'internal', message: '' });
  });

  it('refuses an answer that is no refusal', () => {
    expect(() => readVoiceRefusal(null)).toThrow();
    expect(() => readVoiceRefusal({ r: 'refused', code: 'nonsense' })).toThrow();
  });
});
