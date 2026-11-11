// The notification centre on the stores. What waits for the person is live, from the chats' statuses; a finished or
// failed turn goes into the centre's log (stores/notif-log.ts) with its gist — the start of the answer, why it failed —
// and stays there as history, read or not, until taken away; what was done from the smart notice comes from its
// history. A waiting row reads its question or permission from the chat's channel while the centre is open and answers
// through the same commands as the chat's own cards and the smart notice.
import { useCallback, useEffect, useMemo, type ReactNode } from 'react';
import { settled } from '@/next/bindings/outcome';
import { answerQuestion, decidePermission, markSeen } from '@/next/commands';
import { useConversationChannel } from '@/next/core/realtime/hub';
import { gistOf } from '@/next/features/notifications/group';
import { noticeEventOf } from '@/next/features/notifications/notice-events';
import { notificationsFromMachines, type IMachineNotice } from '@/next/features/notifications/from-machines';
import { NOTIFICATION_KIND, type INotification } from '@/next/features/notifications/notification-center';
import WaitRow, { type TWaitDetail } from '@/next/features/notifications/wait-row';
import { STATUS_LOOK, chatStatusOf } from '@/next/features/sidebar/status';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';
import { t } from '@/next/i18n';
import type { Conversation } from '@/next/protocol/v2';
import { openGates, useGates } from '@/next/stores/conversation';
import { convsLoadedStore, useConversationList } from '@/next/stores/conversations';
import { SOURCE, originOf } from '@/next/stores/conversation-merge';
import { readUpToOf } from '@/next/bindings/read-while-visible';
import { addHistory, dropHistory, useHistory } from '@/next/stores/notice-history';
import { NOTICE_KIND } from '@/next/stores/notices';
import {
  LOG_KIND,
  dropLog,
  markLogRead,
  setLogPerson,
  syncLog,
  useLog,
  type ILogEntry,
} from '@/next/stores/notif-log';

const WAITING_STATUS = [CHAT_STATUS.question, CHAT_STATUS.perm, CHAT_STATUS.plan] as const;
type TWaitingStatus = (typeof WAITING_STATUS)[number];
const waits = (status: string): status is TWaitingStatus =>
  (WAITING_STATUS as readonly string[]).includes(status);
const LOG_ID = 'log:';
const HISTORY_ID = 'history:';

/** The log's event of a chat as it stands now, or null: a finished turn with its gist, a failed one with why. */
export const logEventOf = (conv: Conversation): Omit<ILogEntry, 'read'> | null => {
  const event = noticeEventOf(conv, true);
  if (!event || event.kind === NOTICE_KIND.waiting) return null;
  const turn = conv.lastTurn;
  const at = turn?.endedAt ?? conv.state.since;
  const title = conv.title || t('next.notifications.untitled');
  if (event.kind === NOTICE_KIND.done) {
    // Claude's own «while you were away» recap of this turn, when it wrote one: what was done, not how it began.
    const recap = conv.recap && turn && conv.recap.at >= turn.startedAt ? conv.recap.text.trim() : '';
    return {
      key: event.key,
      chatId: conv.id,
      kind: LOG_KIND.done,
      icon: event.icon,
      title,
      text: recap || gistOf(turn?.snippet ?? '') || event.text,
      at,
    };
  }
  const why = event.kind === NOTICE_KIND.failed && turn ? gistOf(turn.snippet) : '';
  return {
    key: event.key,
    chatId: conv.id,
    kind: LOG_KIND.failed,
    icon: event.icon,
    title,
    text: why ? `${t('next.nc.turnFailed')}: ${why}` : event.text,
    at,
  };
};

/**
 * What a record may tell the log. A new event is logged only from the machine's own list and what follows it: the list
 * the browser kept is not what happened. That the chat moved on (read on another tab or device, answered) is taken from
 * the hub's copy too, so a row goes read with its chat while the machine is still being asked or does not answer.
 */
export const logSourceOf = (
  conv: Conversation,
  machineListed: boolean,
): { logs: boolean; reads: boolean } => ({
  logs: machineListed,
  reads: machineListed || originOf(conv).src !== SOURCE.cache,
});

interface IWaitBindingProps {
  conv: Conversation;
  item: INotification;
  open: boolean;
  onToggle: () => void;
  onOpenChat: () => void;
}

/** One waiting row: the chat's channel while it is on screen, its question or permission, the answer from here. */
const WaitBinding = ({ conv, item, open, onToggle, onOpenChat }: IWaitBindingProps) => {
  const status = chatStatusOf(conv, true);
  const needsGate = status === CHAT_STATUS.question || status === CHAT_STATUS.perm;
  useConversationChannel(needsGate ? conv.machineId : null, needsGate ? conv.id : null);
  const gates = useGates(needsGate ? conv.id : null);
  const gate = useMemo(() => openGates(gates), [gates]);
  const question = gate.questions[0] ?? null;
  const permission = gate.permissions[0] ?? null;

  const detail = useMemo((): TWaitDetail => {
    if (status === CHAT_STATUS.plan) return { k: 'plain', text: t('next.nc.waitsPlan') };
    if (status === CHAT_STATUS.question) {
      if (!question) return { k: 'loading' };
      const [only, ...more] = question.items;
      // Several questions or a choice of several are answered in the chat, where they have room.
      if (!only) return { k: 'plain', text: t('next.nc.waitsAnswer') };
      if (more.length > 0 || only.multi)
        return { k: 'plain', text: only.question || t('next.nc.manyQuestions') };
      return { k: 'question', text: only.question, options: only.options.map((o) => o.label) };
    }
    if (!permission) return { k: 'loading' };
    if (permission.kind !== 'tool') return { k: 'plain', text: t('next.nc.waitsPlan') };
    // Only a command shown in full is allowed here; anything else is decided in the chat, with all it says.
    if (!permission.command) return { k: 'plain', text: permission.tool ?? t('next.nc.waitsPermission') };
    return { k: 'permission', command: permission.command };
  }, [status, question, permission]);

  const remember = (text: string, icon: string) =>
    addHistory({
      key: `${item.id}:${Date.now()}`,
      chatId: conv.id,
      icon,
      title: item.title,
      text,
      at: Date.now(),
    });

  const onChoose = async (index: number) => {
    if (!question) return;
    await settled(answerQuestion(conv.machineId, conv.id, question.id, [{ i: 0, choices: [index] }]));
    remember(t('next.nc.history.answer', { text: question.items[0]?.options[index]?.label ?? '' }), 'check');
  };
  const onText = async (text: string) => {
    if (!question) return;
    await settled(answerQuestion(conv.machineId, conv.id, question.id, [{ i: 0, choices: [], text }]));
    remember(t('next.nc.history.answer', { text }), 'check');
  };
  const onDecide = async (allow: boolean) => {
    if (!permission) return;
    const option = permission.options.find((o) => o.scope === (allow ? 'once' : 'deny'));
    if (!option) return;
    await settled(decidePermission(conv.machineId, conv.id, permission.id, option.id));
    remember(t(allow ? 'next.nc.history.allowed' : 'next.nc.history.denied'), allow ? 'check' : 'ban');
  };

  return (
    <WaitRow
      detail={detail}
      open={open}
      onToggle={onToggle}
      onOpenChat={onOpenChat}
      onChoose={question ? onChoose : undefined}
      onText={question ? onText : undefined}
      onDecide={permission ? onDecide : undefined}
    />
  );
};

export interface INotificationCenterData {
  items: readonly INotification[] | null;
  onRead: (items: readonly INotification[]) => void;
  onRemove: (items: readonly INotification[]) => void;
  onReadAll: () => Promise<void>;
  renderWait: (item: INotification, open: boolean, toggle: () => void) => ReactNode;
}

/**
 * The centre's rows and actions. `chats` is the sidebar's list (null while it loads): the chats the person sees, so
 * the centre lists none that the sidebar leaves out.
 */
export const useNotificationCenter = ({
  person,
  chats,
  machineNotices,
  onOpenChat,
}: {
  person: string;
  chats: readonly ISidebarChat[] | null;
  machineNotices: readonly IMachineNotice[];
  onOpenChat: (id: string) => void;
}): INotificationCenterData => {
  setLogPerson(person);
  const log = useLog();
  const history = useHistory();
  const convs = useConversationList(null);
  const byId = useMemo(() => new Map<string, Conversation>((convs ?? []).map((c) => [c.id, c])), [convs]);
  const shown = useMemo(() => (chats === null ? null : new Set(chats.map((c) => c.id))), [chats]);

  // What the chats stand at now goes into the log; what they moved past is read there.
  useEffect(() => {
    if (!convs || !shown) return;
    const events: Omit<ILogEntry, 'read'>[] = [];
    const current = new Map<string, string | null>();
    for (const conv of convs) {
      if (!shown.has(conv.id)) continue;
      const fresh = logSourceOf(conv, convsLoadedStore.has(conv.machineId));
      if (fresh.reads) current.set(conv.id, noticeEventOf(conv, true)?.key ?? null);
      const logged = fresh.logs ? logEventOf(conv) : null;
      if (logged) events.push(logged);
    }
    syncLog(events, current);
  }, [convs, shown]);

  const items = useMemo((): readonly INotification[] | null => {
    if (chats === null) return null;
    const titleOf = (id: string, was: string) => chats.find((c) => c.id === id)?.title || was;
    const waiting = chats.flatMap((chat): INotification[] => {
      const status = chat.status;
      if (!waits(status)) return [];
      const conv = byId.get(chat.id);
      return [
        {
          id: `wait:${chat.id}:${status}`,
          kind: NOTIFICATION_KIND.waiting,
          icon: STATUS_LOOK[status].icon,
          title: chat.title || t('next.notifications.untitled'),
          text: STATUS_LOOK[status].label,
          at: conv?.state.since ?? chat.lastActivityAt,
          unread: true,
          chatId: chat.id,
        },
      ];
    });
    const logged = log
      .filter((e) => shown?.has(e.chatId))
      .map((e): INotification => ({
        id: LOG_ID + e.key,
        kind: e.kind === LOG_KIND.done ? NOTIFICATION_KIND.done : NOTIFICATION_KIND.failed,
        icon: e.icon,
        title: titleOf(e.chatId, e.title),
        text: e.text,
        at: e.at,
        unread: !e.read,
        chatId: e.chatId,
      }));
    const done = history.map((e): INotification => ({
      id: HISTORY_ID + e.key,
      kind: NOTIFICATION_KIND.done,
      icon: e.icon,
      title: e.chatId ? titleOf(e.chatId, e.title) : e.title,
      text: e.text,
      at: e.at,
      unread: false,
      chatId: e.chatId,
    }));
    return [...waiting, ...notificationsFromMachines(machineNotices), ...logged, ...done];
  }, [chats, byId, log, shown, history, machineNotices]);

  /** The chats of unread rows are read on their machines too: the sidebar's mark goes with the centre's. */
  const seeChats = useCallback(
    (list: readonly INotification[]) => {
      const chatsToSee = new Set(list.filter((i) => i.unread && i.chatId).map((i) => i.chatId as string));
      for (const id of chatsToSee) {
        const conv = byId.get(id);
        // Up to its last turn by the machine's clock: the browser's may be ahead and read the next answer before it comes.
        const upTo = conv ? readUpToOf(conv) : null;
        if (conv && upTo !== null) void markSeen(conv.machineId, conv.id, upTo);
      }
    },
    [byId],
  );

  const logKeys = (list: readonly INotification[]) =>
    list.filter((i) => i.id.startsWith(LOG_ID)).map((i) => i.id.slice(LOG_ID.length));

  const onRead = useCallback(
    (list: readonly INotification[]) => {
      markLogRead(logKeys(list));
      seeChats(list);
    },
    [seeChats],
  );

  const onRemove = useCallback(
    (list: readonly INotification[]) => {
      dropLog(logKeys(list));
      dropHistory(list.filter((i) => i.id.startsWith(HISTORY_ID)).map((i) => i.id.slice(HISTORY_ID.length)));
      seeChats(list);
    },
    [seeChats],
  );

  const onReadAll = useCallback(async () => {
    if (!items) return;
    onRead(items.filter((i) => i.kind === NOTIFICATION_KIND.done || i.kind === NOTIFICATION_KIND.failed));
  }, [items, onRead]);

  const renderWait = useCallback(
    (item: INotification, open: boolean, toggle: () => void) => {
      const conv = item.chatId ? byId.get(item.chatId) : undefined;
      if (!conv) return null;
      return (
        <WaitBinding
          key={item.id}
          conv={conv}
          item={item}
          open={open}
          onToggle={toggle}
          onOpenChat={() => onOpenChat(conv.id)}
        />
      );
    },
    [byId, onOpenChat],
  );

  return { items, onRead, onRemove, onReadAll, renderWait };
};

/** The row of the centre that was opened: its event is read in the log at once (the chat follows when it opens). */
export const readOpened = (item: INotification): void => {
  if (item.id.startsWith(LOG_ID)) markLogRead([item.id.slice(LOG_ID.length)]);
};
