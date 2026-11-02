import { describe, expect, it } from 'vitest';
import { startMessage } from '@/next/features/composer/voice-socket';

describe('the start of a dictation', () => {
  it('names the model the person picked and carries their words', () => {
    const msg = JSON.parse(
      startMessage('ru', { engine: 'openai', words: [{ word: 'Sinopia', heard: ['синопия'] }] }),
    ) as Record<string, unknown>;
    expect(msg).toEqual({
      type: 'start',
      lang: 'ru',
      engine: 'openai',
      dict: [{ word: 'Sinopia', heard: ['синопия'] }],
    });
  });
});
