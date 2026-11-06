import { describe, expect, it } from 'vitest';
import { readSpeechKeys } from '@/next/features/providers/speech-keys';

const none = { configured: false, tail: null };

describe('the speech keys the runtime lists', () => {
  it('reads each key as saved or not, with only its tail', () => {
    const keys = readSpeechKeys({
      keys: { openai: { configured: true, tail: '…WXYZ' }, yandex: none, 'yandex-folder': none },
    });
    expect(keys.openai).toEqual({ configured: true, tail: '…WXYZ' });
    expect(keys.yandex.configured).toBe(false);
  });

  it('reads whether Codex is signed in by a plan; an older runtime does not say', () => {
    const keys = { openai: none, yandex: none, 'yandex-folder': none };
    expect(readSpeechKeys({ keys, chatgpt: true }).chatgpt).toBe(true);
    expect(readSpeechKeys({ keys }).chatgpt).toBe(false);
    expect(readSpeechKeys({ keys, chatgpt: 'yes' }).chatgpt).toBe(false);
  });

  it('refuses an answer that is not the list or whose tail is not a tail', () => {
    expect(() => readSpeechKeys({})).toThrow();
    expect(() => readSpeechKeys({ keys: { openai: none, yandex: none } })).toThrow();
    expect(() =>
      readSpeechKeys({
        keys: { openai: { configured: true, tail: 'sk-secret-key' }, yandex: none, 'yandex-folder': none },
      }),
    ).toThrow();
  });
});
