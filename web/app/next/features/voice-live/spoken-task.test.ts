import { describe, expect, it } from 'vitest';
import { spokenTask } from '@/next/features/voice-live/spoken';
import { saidSince } from '@/next/features/voice-live/use-voice-call';

describe('a spoken task', () => {
  it('brings the agent the person’s own words, the voice’s reading only as a note', () => {
    const text = spokenTask({
      text: 'Перерисуй интерфейс доски',
      said: 'Оставь комментарий на доске про кнопку',
    });
    expect(text.startsWith('Оставь комментарий на доске про кнопку')).toBe(true);
    expect(text).toContain('Голос понял так: Перерисуй интерфейс доски');
    expect(spokenTask({ text: 'Сделай план', said: '' })).toBe('Сделай план');
    expect(spokenTask({ text: 'Сделай план', said: 'Сделай план' })).toBe('Сделай план');
  });
  it('takes the person’s lines since the last task', () => {
    const lines = [
      { role: 'user', text: 'Покрась кнопку' },
      { role: 'assistant', text: 'Передал' },
      { role: 'user', text: 'Оставь комментарий' },
      { role: 'user', text: 'про кнопку' },
    ];
    expect(saidSince(lines, 2)).toBe('Оставь комментарий про кнопку');
    expect(saidSince(lines, 0)).toBe('Покрась кнопку Оставь комментарий про кнопку');
  });
});
