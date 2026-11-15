import { describe, expect, it } from 'vitest';
import { DELIVERY, EFFORT_DEFAULT, styleFromWords, type ICallStyle } from '@/next/features/voice-live/style';

const base: ICallStyle = { delivery: DELIVERY.narrate, effort: EFFORT_DEFAULT };

describe('styleFromWords', () => {
  it('turns results-only on and off by the person’s words', () => {
    for (const said of ['Давай помолчи, скажи просто результат', 'без комментариев', 'говори только итог']) {
      expect(styleFromWords(said, base)?.style.delivery).toBe(DELIVERY.result);
    }
    const quiet = { ...base, delivery: DELIVERY.result };
    expect(styleFromWords('рассказывай по ходу', quiet)?.style.delivery).toBe(DELIVERY.narrate);
    expect(styleFromWords('помолчи', quiet)).toBeNull();
  });

  it('steps the effort up and back to instant', () => {
    expect(styleFromWords('думай лучше', base)?.style.effort).toBe('medium');
    expect(styleFromWords('подумай хорошенько', { ...base, effort: 'high' })?.style.effort).toBe('max');
    expect(styleFromWords('думай лучше', { ...base, effort: 'max' })).toBeNull();
    expect(styleFromWords('давай быстрее', { ...base, effort: 'high' })?.style.effort).toBe(EFFORT_DEFAULT);
  });

  it('leaves ordinary requests alone', () => {
    for (const said of [
      'сделай мне план на день',
      'что на этой доске?',
      'перекрась колонки под тёмную тему',
      '',
    ]) {
      expect(styleFromWords(said, base)).toBeNull();
    }
  });
});
