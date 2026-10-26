import { describe, expect, it } from 'vitest';
import { messageGroups } from '@/next/features/search/search-chats';
import type { ISidebarChat } from '@/next/features/sidebar/types';
import type { MessageHit } from '@/next/protocol/v2';

const chat = (id: string, title: string): ISidebarChat => ({
  id,
  title,
  status: 'none',
  pinned: false,
  projectId: null,
  createdAt: 0,
  lastPromptAt: 0,
  lastActivityAt: 0,
  terminal: false,
  canStopTerminal: false,
});

const hit = (
  convId: MessageHit['convId'],
  n: number,
  role: MessageHit['role'],
  snippet: string,
): MessageHit => ({
  convId,
  entryId: `c:00000000-0000-4000-8000-00000000000${n}#0`,
  turnId: `c:00000000-0000-4000-8000-00000000000${n}#0`,
  at: Date.UTC(2026, 9, 20, 12, 0),
  role,
  snippet,
});

describe('messageGroups', () => {
  const chats = [chat('tab-a', 'Deploy'), chat('tab-b', 'Billing')];

  it('groups the messages by chat, the chats in the order of their first hit, with the place each opens', () => {
    const groups = messageGroups(
      [
        hit('tab-b', 1, 'user', 'why is billing slow'),
        hit('tab-a', 2, 'agent', 'the deploy is fixed'),
        hit('tab-b', 3, 'agent', 'billing was a cold cache'),
      ],
      chats,
      Date.UTC(2026, 9, 25),
    );
    expect(groups.map((g) => [g.label, g.rows.length])).toEqual([
      ['Billing', 2],
      ['Deploy', 1],
    ]);
    const first = groups[0].rows[0];
    expect(first).toMatchObject({
      kind: 'message',
      icon: 'user',
      snippet: 'why is billing slow',
      target: { chatId: 'tab-b', entryId: 'c:00000000-0000-4000-8000-000000000001#0' },
    });
    expect(groups[0].rows[1]).toMatchObject({ icon: 'sparkles' });
    expect(new Set(groups.flatMap((g) => g.rows.map((r) => r.id))).size).toBe(3);
  });

  it('leaves out a message of a chat the list does not have', () => {
    expect(messageGroups([hit('tab-gone', 1, 'user', 'x')], chats, 0)).toEqual([]);
  });
});
