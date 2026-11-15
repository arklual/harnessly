import { describe, expect, it } from 'vitest';
import { messageGroups, orderChats, recentChats, searchChats } from '@/next/features/search/search-chats';
import { pinLocale } from '@/next/i18n';
import { formatAgo, formatWhen } from '@/next/i18n/format';
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

describe('the dates of the found messages (HRN-1447)', () => {
  it('are in the page language, as the rest of it, not the browser one', () => {
    pinLocale('ru');
    const a = chat('tab-a', 'Альфа');
    const at = Date.UTC(2026, 9, 20, 12, 0);
    const now = at + 3 * 24 * 3_600_000;
    const groups = messageGroups([hit('tab-a' as MessageHit['convId'], 1, 'user', 'слово')], [a], now);
    expect(groups[0]?.rows[0]?.meta).toBe(formatAgo(at, now));
    expect(groups[0]?.rows[0]?.meta).not.toMatch(/[A-Za-z]/);
    pinLocale(null);
  });
});

// A Sunday evening by the device's clock: the week before it is Mon 9 – Sat 14 Nov.
const NOW = new Date(2026, 10, 15, 18, 0).getTime();
const used = (id: string, title: string, at: Date, parentId?: string): ISidebarChat => ({
  ...chat(id, title),
  lastPromptAt: at.getTime(),
  ...(parentId ? { parentId } : {}),
});

describe('the «when» of a search row (HRN-1508)', () => {
  it('says minutes and hours today, «вчера», the weekday, the date, the year when it is another', () => {
    pinLocale('ru');
    expect(formatWhen(NOW - 12 * 60_000, NOW)).toBe('12 мин');
    expect(formatWhen(NOW - 3 * 3_600_000, NOW)).toBe('3 ч');
    expect(formatWhen(new Date(2026, 10, 14, 23, 0).getTime(), NOW)).toBe('вчера');
    expect(formatWhen(new Date(2026, 10, 11, 10, 0).getTime(), NOW)).toBe('ср');
    expect(formatWhen(new Date(2026, 10, 5, 10, 0).getTime(), NOW)).toBe('5 нояб.');
    expect(formatWhen(new Date(2026, 9, 12, 10, 0).getTime(), NOW)).toBe('12 окт.');
    expect(formatWhen(new Date(2025, 9, 12, 10, 0).getTime(), NOW)).toBe('12 окт. 2025');
    pinLocale(null);
  });
});

describe('the chats of the search, by when they were used (HRN-1508)', () => {
  const chats = [
    used('old', 'Аналитика v2', new Date(2026, 9, 2)),
    used('today', 'Деплой', new Date(2026, 10, 15, 16)),
    used('sub', 'Аналитика подчат', new Date(2026, 10, 15, 17), 'today'),
    used('week', 'Аналитика', new Date(2026, 10, 11)),
    used('month', 'Аналитика доски', new Date(2026, 10, 3)),
    used('yesterday', 'Вёрстка', new Date(2026, 10, 14, 9)),
    used('week2', 'Аналитика ревью', new Date(2026, 10, 12)),
  ];

  it('orders by the last use', () => {
    expect(orderChats(chats, NOW).map((c) => c.id)).toEqual([
      'sub',
      'today',
      'yesterday',
      'week2',
      'week',
      'month',
      'old',
    ]);
  });

  it('lifts a title named just as the words inside its day group, parts the rows by days, never finds a subchat', () => {
    pinLocale('ru');
    const [group] = searchChats(chats, 'аналитика', undefined, NOW);
    expect(group?.rows.map((r) => [r.id, r.section, r.meta])).toEqual([
      ['week', 'На этой неделе', 'ср'],
      ['week2', 'На этой неделе', 'чт'],
      ['month', 'В этом месяце', '3 нояб.'],
      ['old', 'Раньше', '2 окт.'],
    ]);
    pinLocale(null);
  });

  it('leaves the delegated chats and their messages out', () => {
    expect(recentChats(chats, true, NOW).map((c) => c.id)).not.toContain('sub');
    expect(recentChats(chats, false, NOW).every((r) => r.section === undefined)).toBe(true);
    expect(messageGroups([hit('sub' as MessageHit['convId'], 1, 'user', 'x')], chats, NOW)).toEqual([]);
  });
});
