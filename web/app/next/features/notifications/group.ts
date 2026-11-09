// The notification centre's list, laid out by meaning and by chat: what waits for the person first (the longest
// waiting on top), then a machine in trouble, then what broke and what is ready (the newest on top). Inside a section a
// chat is one row: its newest event, and the ones before it under it.
import {
  NOTIFICATION_KIND,
  type INotification,
  type TNotificationKind,
} from '@/next/features/notifications/kinds';

export const SECTION_ORDER: readonly TNotificationKind[] = [
  NOTIFICATION_KIND.waiting,
  NOTIFICATION_KIND.machine,
  NOTIFICATION_KIND.failed,
  NOTIFICATION_KIND.done,
];

/** The events before the newest a row shows under it; the rest are in the chat. */
export const MAX_OLDER = 2;

export interface INotificationGroup {
  /** The row's key: the section and the chat (or the event, for what has no chat). */
  key: string;
  kind: TNotificationKind;
  chatId: string | null;
  title: string;
  /** The newest event. */
  top: INotification;
  /** The events before it, newest first, at most MAX_OLDER. */
  older: readonly INotification[];
  /** Every event of the row, for «Прочитано» and «Убрать». */
  all: readonly INotification[];
  unread: boolean;
}

export interface INotificationSection {
  kind: TNotificationKind;
  groups: readonly INotificationGroup[];
}

export const groupNotifications = (items: readonly INotification[]): readonly INotificationSection[] =>
  SECTION_ORDER.flatMap((kind) => {
    const byRow = new Map<string, INotification[]>();
    for (const item of items) {
      if (item.kind !== kind) continue;
      const key = `${kind}:${item.chatId ?? item.id}`;
      const row = byRow.get(key);
      if (row) row.push(item);
      else byRow.set(key, [item]);
    }
    if (byRow.size === 0) return [];
    const groups = [...byRow.entries()].map(([key, events]): INotificationGroup => {
      const sorted = [...events].sort((a, b) => b.at - a.at);
      const top = sorted[0] as INotification;
      return {
        key,
        kind,
        chatId: top.chatId,
        title: top.title,
        top,
        older: sorted.slice(1, 1 + MAX_OLDER),
        all: sorted,
        unread: sorted.some((e) => e.unread),
      };
    });
    // What waits: the one waiting longest first. Everything else: the newest first.
    groups.sort((a, b) => (kind === NOTIFICATION_KIND.waiting ? a.top.at - b.top.at : b.top.at - a.top.at));
    return [{ kind, groups }];
  });

const MARKDOWN = /^[\s>#*\-+|]+|\*\*|`/g;

/** The gist of an answer: its first line with words in it, without markdown's marks. */
export const gistOf = (text: string): string => {
  for (const line of text.split('\n')) {
    const clean = line.replace(MARKDOWN, '').trim();
    if (clean) return clean;
  }
  return '';
};
