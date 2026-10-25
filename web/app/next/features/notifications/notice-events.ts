// What in a chat is worth a notice, read off its record in the convs channel (the same live list the sidebar's
// statuses and the notification centre come from, no polling of its own): a question, a permission or a plan that waits,
// a finished turn, a failed one, a limit hit. Each has a key of its own, so the same event seen twice (a reload, a
// second window, a snapshot after a reconnect) is one notice, and a notice whose chat moved on can be told by its key.
import { t } from '@/next/i18n';
import type { Conversation } from '@/next/protocol/v2';
import { STATUS_LOOK, chatStatusOf } from '@/next/features/sidebar/status';
import { CHAT_STATUS } from '@/next/features/sidebar/types';
import { NOTICE_KIND, type TNoticeKind } from '@/next/stores/notices';

export interface INoticeEvent {
  key: string;
  kind: TNoticeKind;
  icon: string;
  text: string;
}

const WAITING = new Set<string>([CHAT_STATUS.question, CHAT_STATUS.perm, CHAT_STATUS.plan]);

/** The event the chat stands at now, or null when nothing in it waits for the person or is new to them. */
export const noticeEventOf = (conv: Conversation, machineOnline: boolean): INoticeEvent | null => {
  const status = chatStatusOf(conv, machineOnline);
  if (status === CHAT_STATUS.none) return null;
  const look = STATUS_LOOK[status];
  if (WAITING.has(status))
    return {
      key: `${conv.id}:${status}:${conv.state.since}`,
      kind: NOTICE_KIND.waiting,
      icon: look.icon,
      text: look.label,
    };
  if (conv.state.k === 'launch-failed')
    return {
      key: `${conv.id}:launch:${conv.state.since}`,
      kind: NOTICE_KIND.failed,
      icon: look.icon,
      text: t('next.notices.launchFailed'),
    };
  const turn = conv.lastTurn;
  if (!turn) return null;
  if (status === CHAT_STATUS.done)
    return {
      key: `${conv.id}:${turn.id}:done`,
      kind: NOTICE_KIND.done,
      icon: 'circle-check',
      text: turn.snippet.trim() || look.label,
    };
  if (status === CHAT_STATUS.failed && turn.failure === 'rate-limit')
    return {
      key: `${conv.id}:${turn.id}:limit`,
      kind: NOTICE_KIND.limit,
      icon: 'gauge',
      text: t('next.notices.limit'),
    };
  if (status === CHAT_STATUS.failed)
    return {
      key: `${conv.id}:${turn.id}:failed`,
      kind: NOTICE_KIND.failed,
      icon: look.icon,
      text: look.label,
    };
  return null;
};
