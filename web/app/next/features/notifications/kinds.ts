// What the notification centre lists: the kinds of rows and one row's event.
export const NOTIFICATION_KIND = {
  machine: 'machine',
  waiting: 'waiting',
  done: 'done',
  failed: 'failed',
} as const;
export type TNotificationKind = (typeof NOTIFICATION_KIND)[keyof typeof NOTIFICATION_KIND];

export interface INotification {
  id: string;
  kind: TNotificationKind;
  icon: string;
  /** The chat's title, or the machine's name. */
  title: string;
  /** The gist: the start of the answer, the question, why it failed. */
  text: string;
  at: number;
  unread: boolean;
  /** The chat a row opens; null for a machine's row, which opens the machine's card. */
  chatId: string | null;
  /** Where it was answered («из уведомления»), for what the person did. */
  note?: string;
}
