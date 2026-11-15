import { describe, expect, it } from 'vitest';
import { promises } from '@/next/features/voice-live/use-voice-call';

describe('a promise of the voice', () => {
  it('is heard in «смотрю», «сделаю», «сейчас покажу», not in a plain answer', () => {
    expect(promises('Хорошо, смотрю доску.')).toBe(true);
    expect(promises('Сейчас сделаю.')).toBe(true);
    expect(promises('Почти готово, сейчас покажу')).toBe(true);
    expect(promises('Столица Франции — Париж.')).toBe(false);
    expect(promises('Пересмотрю позже')).toBe(false);
  });
});
