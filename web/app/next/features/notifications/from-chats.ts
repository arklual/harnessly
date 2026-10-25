import { t } from '@/next/i18n';
import {
  NOTIFICATION_KIND,
  type INotification,
  type TNotificationKind,
} from '@/next/features/notifications/notification-center';
import { STATUS_LOOK } from '@/next/features/sidebar/status';
import { CHAT_STATUS, type ISidebarChat, type TChatStatus } from '@/next/features/sidebar/types';

const KIND_OF: Partial<Record<TChatStatus, TNotificationKind>> = {
  [CHAT_STATUS.question]: NOTIFICATION_KIND.waiting,
  [CHAT_STATUS.perm]: NOTIFICATION_KIND.waiting,
  [CHAT_STATUS.plan]: NOTIFICATION_KIND.waiting,
  [CHAT_STATUS.done]: NOTIFICATION_KIND.done,
  [CHAT_STATUS.failed]: NOTIFICATION_KIND.failed,
};

/**
 * Until the runtime keeps a notification log, the centre is what the chats' statuses say: a question or permission
 * waits, a finished unread turn is done, a broken one failed. Each is unread while its chat shows the status.
 */
export const notificationsFromChats = (
  chats: readonly ISidebarChat[] | null,
): readonly INotification[] | null => {
  if (chats === null) return null;
  return chats
    .flatMap((chat): INotification[] => {
      const kind = KIND_OF[chat.status];
      if (!kind || chat.status === CHAT_STATUS.none) return [];
      const look = STATUS_LOOK[chat.status];
      return [
        {
          id: `${chat.id}:${chat.status}`,
          kind,
          icon: look.icon,
          title: chat.title || t('next.notifications.untitled'),
          text: look.label,
          at: chat.lastActivityAt,
          unread: true,
          chatId: chat.id,
        },
      ];
    })
    .sort((a, b) => b.at - a.at);
};
