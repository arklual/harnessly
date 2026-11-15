import { describe, expect, it } from 'vitest';
import { byeLine } from '@/next/features/voice-live/wake-end';

const u = (text: string, done = true) => ({ role: 'user', text, done });
const a = (text: string) => ({ role: 'assistant', text, done: true });

describe('byeLine', () => {
  it('finds the person’s goodbye after `from`', () => {
    const lines = [u('открой доску'), a('Открыла.'), u('Спасибо, всё.')];
    expect(byeLine(lines, 0)).toBe(2);
    expect(byeLine(lines, 3)).toBe(-1);
  });
  it('waits for the line to be finished', () => {
    expect(byeLine([u('пока', false)], 0)).toBe(-1);
  });
  it('ignores the voice’s own goodbye heard back by the microphone', () => {
    expect(byeLine([a('Хорошо, спасибо, всё готово.'), u('Спасибо, всё')], 0)).toBe(-1);
    expect(byeLine([a('Хорошо, спасибо, всё готово. Пока!')], 0)).toBe(-1);
  });
});
