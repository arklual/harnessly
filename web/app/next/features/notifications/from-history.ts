// What was done from the smart notice, as rows of the notification centre: read, under what is done.
import { NOTIFICATION_KIND, type INotification } from '@/next/features/notifications/notification-center';
import type { IHistoryEntry } from '@/next/stores/notice-history';

export const notificationsFromHistory = (history: readonly IHistoryEntry[]): readonly INotification[] =>
  history.map((entry) => ({
    id: `history:${entry.key}`,
    kind: NOTIFICATION_KIND.done,
    icon: entry.icon,
    title: entry.title,
    text: entry.text,
    at: entry.at,
    unread: false,
    chatId: entry.chatId,
  }));
