import { describe, expect, it } from 'vitest';
import { chatLines, chatOf } from '@/next/features/app-views/host-handlers';
import type { ConvId } from '@/next/protocol/v2';
import type { FeedItem } from '@/next/features/chat/types';

const turn = (key: string, prompt: string | null, answer: string | null) =>
  ({
    kind: 'turn',
    key,
    prompt: prompt === null ? null : { t: 'user', text: prompt },
    answer: answer === null ? null : { t: 'say', md: answer },
  }) as unknown as FeedItem;

const outgoing = (text: string) =>
  ({ kind: 'outgoing', key: text, message: { text } }) as unknown as FeedItem;

describe('chatLines', () => {
  it('reads the person and the agent as text, in order', () => {
    const items = [turn('1', 'Привет', 'Здравствуйте'), turn('2', 'Ещё', 'Готово')];
    expect(chatLines(items, 10)).toEqual([
      { role: 'user', text: 'Привет' },
      { role: 'assistant', text: 'Здравствуйте' },
      { role: 'user', text: 'Ещё' },
      { role: 'assistant', text: 'Готово' },
    ]);
  });

  it('gives the latest lines only', () => {
    const items = [turn('1', 'a', 'b'), turn('2', 'c', 'd')];
    expect(chatLines(items, 3).map((l) => l.text)).toEqual(['b', 'c', 'd']);
  });

  it('leaves out what is not text: a turn without prompt or answer, other feed items, empty words', () => {
    const items = [
      turn('1', null, null),
      { kind: 'switch', key: 's' } as unknown as FeedItem,
      turn('2', '  ', 'x'),
      outgoing('идёт'),
    ];
    expect(chatLines(items, 10)).toEqual([
      { role: 'assistant', text: 'x' },
      { role: 'user', text: 'идёт' },
    ]);
  });

  it('is empty before the feed is read', () => {
    expect(chatLines(null, 5)).toEqual([]);
  });
});

describe('chatOf: an app works with a chat only where it is on', () => {
  it('gives the chat where the app is on', () => {
    expect(chatOf('tab-1' as ConvId, true)).toBe('tab-1');
  });

  it('refuses a chat the app is not on in with permission_denied', () => {
    expect(() => chatOf('tab-1' as ConvId, false)).toThrowError(
      expect.objectContaining({ code: 'permission_denied' }),
    );
  });

  it('says there is no chat when there is none', () => {
    expect(() => chatOf(null, true)).toThrowError(expect.objectContaining({ code: 'host_unavailable' }));
  });
});
