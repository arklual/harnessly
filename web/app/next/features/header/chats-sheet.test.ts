import { describe, expect, it } from 'vitest';
import { recentChats } from '@/next/features/header/chats-sheet';
import type { ISidebarChat } from '@/next/features/sidebar/types';

const chat = (id: string, title: string, lastPromptAt: number, status = 'done') =>
  ({ id, title, lastPromptAt, status }) as ISidebarChat;

describe('recentChats', () => {
  const chats = [
    chat('a', 'Отчёт по продажам', 1),
    chat('b', 'Шапка телефона', 3),
    chat('c', 'Отчёт за неделю', 2),
  ];

  it('lists the newest first', () => {
    expect(recentChats(chats, '', null).map((c) => c.id)).toEqual(['b', 'c', 'a']);
  });

  it('finds by every word of the query, in any case', () => {
    expect(recentChats(chats, 'отчёт НЕДЕЛЮ', null).map((c) => c.id)).toEqual(['c']);
  });

  it('keeps the open chat when it is older than the recent ones', () => {
    const many = Array.from({ length: 40 }, (_, i) => chat(`n${i}`, `Чат ${i}`, 100 + i));
    const list = recentChats([...many, chat('old', 'Старый', 0)], '', 'old');
    expect(list[0].id).toBe('old');
    expect(list).toHaveLength(30);
  });

  it('leaves out chats an agent delegated', () => {
    const sub = { ...chat('s', 'Подчат', 9), parentId: 'b' } as ISidebarChat;
    expect(recentChats([...chats, sub], '', null).map((c) => c.id)).toEqual(['b', 'c', 'a']);
  });

  it("without a query lists only today's chats and those with a status", () => {
    const now = new Date(2026, 10, 15, 21, 0).getTime();
    const morning = new Date(2026, 10, 15, 9, 0).getTime();
    const list = [
      chat('old', 'Вчерашний', now - 30 * 3600_000, 'none'),
      chat('busy', 'Вчерашний, но в работе', now - 30 * 3600_000, 'thinking'),
      chat('today', 'Сегодняшний', morning, 'none'),
    ];
    expect(recentChats(list, '', null, now).map((c) => c.id)).toEqual(['today', 'busy']);
    expect(recentChats(list, 'вчерашний', null, now).map((c) => c.id)).toEqual(['old', 'busy']);
  });
});
