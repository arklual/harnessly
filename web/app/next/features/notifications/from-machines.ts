import { NOTIFICATION_KIND, type INotification } from '@/next/features/notifications/notification-center';
import type { IMachineAlert } from '@/next/features/sidebar/types';

/** A machine about to fail its work, for the notification centre: since when, and what its banner says. */
export interface IMachineNotice {
  id: string;
  name: string;
  alert: IMachineAlert;
  since: number;
}

/** A machine's row is unread while it is critical and leaves once it is not: the runtime keeps no log of it. */
export const notificationsFromMachines = (notices: readonly IMachineNotice[]): readonly INotification[] =>
  notices.map((n) => ({
    id: `machine:${n.alert.key}`,
    kind: NOTIFICATION_KIND.machine,
    icon: n.alert.icon,
    title: n.name,
    text: n.alert.text,
    at: n.since,
    unread: true,
    chatId: null,
  }));
