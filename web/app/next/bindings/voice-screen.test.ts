import { describe, expect, it } from 'vitest';
import { paneLine, screenText } from '@/next/bindings/voice-screen';

describe('screenText', () => {
  it('says what is open, selected, said and answered, the agent’s answer last', () => {
    const text = screenText({
      tab: { id: 'f', kind: 'files', path: 'boards/roadmap.html', file: true },
      selection: 'Колонка Doing',
      lines: [
        { role: 'user', text: 'добавь колонку Done' },
        { role: 'assistant', text: 'Добавил колонку Done и перенёс три карточки.' },
      ],
    });
    expect(text).toContain('Open beside the chat: the file boards/roadmap.html.');
    expect(text).toContain('Selected there: «Колонка Doing»');
    expect(text).toContain('- Person: добавь колонку Done');
    expect(text.split('\n').at(-1)).toBe(
      "The agent's last answer: Добавил колонку Done и перенёс три карточки.",
    );
  });

  it('is empty with nothing on screen and clips long lines', () => {
    expect(screenText({ lines: [] })).toBe('');
    const long = screenText({ lines: [{ role: 'user', text: 'я'.repeat(1000) }] });
    expect(long.length).toBeLessThan(400);
    expect(paneLine(undefined)).toBeNull();
  });
});
