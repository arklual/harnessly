import { describe, expect, it } from 'vitest';
import { dropEcho } from '@/next/features/voice-live/echo';

describe('dropEcho', () => {
  it('cuts the voice’s own words out of the person’s line (15.11)', () => {
    const voice = [
      'Так, чё думаешь?',
      'Оба очень важны.',
      'А в плане команды, что ты имеешь в виду под ними?',
    ];
    const heard = 'Привет Да, да Так, чё думаешь Оба очень важны А смотри, ну у нас две задачи';
    expect(dropEcho(heard, voice)).toBe('Привет Да, да А смотри, ну у нас две задачи');
  });
  it('keeps the person’s words when the voice said nothing like them', () => {
    expect(dropEcho('Сделай план на день', ['Передал агенту.'])).toBe('Сделай план на день');
    expect(dropEcho('да', ['Да, конечно, сделаю.'])).toBe('да');
  });
});
