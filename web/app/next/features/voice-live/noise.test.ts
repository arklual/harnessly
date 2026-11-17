import { describe, expect, it } from 'vitest';
import { isNoiseText } from '@/next/features/voice-live/noise';

describe('noise phantoms', () => {
  it('drops what a recognizer makes of a noise', () => {
    for (const s of [
      'Продолжение следует...',
      'Субтитры сделал DimaTorzok',
      'Спасибо за просмотр!',
      '[Музыка]',
      'Ээ... мм',
      '...',
      'Thank you for watching.',
    ])
      expect(isNoiseText(s), s).toBe(true);
  });
  it('keeps what a person says, short ones too', () => {
    for (const s of ['да', 'нет', 'стоп', 'открой настройки', 'ну давай второй вариант', 'ok'])
      expect(isNoiseText(s), s).toBe(false);
  });
});
