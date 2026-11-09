import { describe, expect, it } from 'vitest';
import { gistOf, groupNotifications, MAX_OLDER } from '@/next/features/notifications/group';
import {
  NOTIFICATION_KIND,
  type INotification,
  type TNotificationKind,
} from '@/next/features/notifications/kinds';

const item = (
  id: string,
  kind: TNotificationKind,
  chatId: string | null,
  at: number,
  unread = true,
): INotification => ({
  id,
  kind,
  icon: 'circle-check',
  title: chatId ?? id,
  text: id,
  at,
  unread,
  chatId,
});

describe('groupNotifications', () => {
  it('puts what waits first, then the machine, what broke and what is done', () => {
    const sections = groupNotifications([
      item('d', NOTIFICATION_KIND.done, 'c1', 5),
      item('f', NOTIFICATION_KIND.failed, 'c2', 4),
      item('m', NOTIFICATION_KIND.machine, null, 3),
      item('w', NOTIFICATION_KIND.waiting, 'c3', 2),
    ]);
    expect(sections.map((s) => s.kind)).toEqual(['waiting', 'machine', 'failed', 'done']);
  });

  it('makes one row of a chat: the newest on top, a few before it under it', () => {
    const [done] = groupNotifications([
      item('a', NOTIFICATION_KIND.done, 'c1', 1, false),
      item('b', NOTIFICATION_KIND.done, 'c1', 3),
      item('c', NOTIFICATION_KIND.done, 'c1', 2, false),
      item('d', NOTIFICATION_KIND.done, 'c1', 0, false),
    ]);
    expect(done?.groups).toHaveLength(1);
    const row = done?.groups[0];
    expect(row?.top.id).toBe('b');
    expect(row?.older.map((e) => e.id)).toEqual(['c', 'a'].slice(0, MAX_OLDER));
    expect(row?.all).toHaveLength(4);
    expect(row?.unread).toBe(true);
  });

  it('lists what waits longest first and what is done newest first', () => {
    const sections = groupNotifications([
      item('w1', NOTIFICATION_KIND.waiting, 'c1', 10),
      item('w2', NOTIFICATION_KIND.waiting, 'c2', 5),
      item('d1', NOTIFICATION_KIND.done, 'c3', 1),
      item('d2', NOTIFICATION_KIND.done, 'c4', 9),
    ]);
    expect(sections[0]?.groups.map((g) => g.top.id)).toEqual(['w2', 'w1']);
    expect(sections[1]?.groups.map((g) => g.top.id)).toEqual(['d2', 'd1']);
  });
});

describe('gistOf', () => {
  it('takes the first line with words, without markdown marks', () => {
    expect(gistOf('\n## **Готово**: загрузка работает\nдальше')).toBe('Готово: загрузка работает');
    expect(gistOf('- `npm test` зелёный')).toBe('npm test зелёный');
    expect(gistOf('   \n')).toBe('');
  });
});
