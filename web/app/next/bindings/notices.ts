// The notices on the live list of conversations: an event in a chat the person is not looking at (another chat is
// open, or the page is hidden or behind another window) becomes a notice, a chime, «(n)» in the tab's title and a dot
// on its icon, and with the page hidden a notification of the system. What the list showed before the machine's own
// snapshot (the browser's cache) and what a chat stood at when first seen are no news. Opening the chat clears its
// notice; so does the chat moving past it (the question answered elsewhere, the answer read on another device).
import { useCallback, useEffect, useRef } from 'react';
import { appBasePath } from '@/platform/config';
import { noticeEventOf } from '@/next/features/notifications/notice-events';
import {
  playChime,
  setIconDot,
  showSystem,
  titleWithCount,
  unlockSound,
} from '@/next/features/notifications/ping';
import { t } from '@/next/i18n';
import { routePath, ROUTE } from '@/next/routes/routes';
import { convsLoadedStore, useConversationList } from '@/next/stores/conversations';
import {
  clearChatNotices,
  dismissAllNotices,
  dismissNotice,
  dropNotices,
  notifySnapshot,
  pushNotice,
  setNoticesPerson,
  useNoticeList,
  type INotice,
} from '@/next/stores/notices';

/** What the service worker's click and the current interface's links ask for: open this chat (hooks/use-web-push.ts). */
const OPEN_CONVERSATION_EVENT = 'hl:open-conversation';
/** The last event a window of this browser chimed for: two windows of the app do not both chime. */
const PING_KEY = 'hx-notice-ping';

const firstToPing = (key: string): boolean => {
  try {
    if (window.localStorage.getItem(PING_KEY) === key) return false;
    window.localStorage.setItem(PING_KEY, key);
  } catch {
    // Storage blocked: this window chimes on its own.
  }
  return true;
};

const pageInFront = (): boolean => document.visibilityState === 'visible' && document.hasFocus();

export interface INoticesOptions {
  /** Whose notices: one list per person on this browser. */
  person: string;
  /** The chat on screen, if any. */
  currentId: string | null;
  onOpenChat: (id: string) => void;
}

export const useNotices = ({ person, currentId, onOpenChat }: INoticesOptions) => {
  setNoticesPerson(person);
  const notices = useNoticeList();
  const convs = useConversationList(null);
  /** Each chat's event at the last look; absent: not looked at yet, so what it stands at is not news. */
  const seen = useRef(new Map<string, string | null>());
  const current = useRef(currentId);
  const open = useRef(onOpenChat);
  // Before the effects below, which read them.
  useEffect(() => {
    current.current = currentId;
    open.current = onOpenChat;
  });

  useEffect(() => unlockSound(), []);

  useEffect(() => {
    if (!convs) return;
    const settings = notifySnapshot();
    const now = new Map<string, string | null>();
    const fresh: INotice[] = [];
    for (const conv of convs) {
      // The list the browser kept is not what happened: only the machine's own snapshot and what follows it are.
      if (!convsLoadedStore.has(conv.machineId)) continue;
      const event = noticeEventOf(conv, true);
      const before = seen.current.get(conv.id);
      now.set(conv.id, event?.key ?? null);
      seen.current.set(conv.id, event?.key ?? null);
      if (before === undefined || !event || event.key === before) continue;
      if (!settings.kinds[event.kind]) continue;
      if (conv.id === current.current && pageInFront()) continue;
      fresh.push({
        key: event.key,
        chatId: conv.id,
        kind: event.kind,
        icon: event.icon,
        title: conv.title || t('next.notifications.untitled'),
        text: event.text,
        at: Date.now(),
      });
    }
    dropNotices((n) => now.has(n.chatId) && now.get(n.chatId) !== n.key);
    fresh.forEach(pushNotice);
    const last = fresh[fresh.length - 1];
    if (!last) return;
    if (document.visibilityState === 'visible') {
      if (settings.sound && firstToPing(last.key)) playChime();
      return;
    }
    if (!settings.system) return;
    for (const notice of fresh)
      void showSystem({
        key: notice.key,
        title: notice.title,
        text: notice.text,
        url: `${appBasePath().replace(/\/+$/, '')}${routePath({ kind: ROUTE.chat, id: notice.chatId })}`,
        onClick: () => open.current(notice.chatId),
      });
  }, [convs]);

  // The chat in front of the person is read: its notices go, now and whenever the page comes back to the front.
  useEffect(() => {
    if (!currentId) return;
    const clear = () => {
      if (pageInFront()) clearChatNotices(currentId);
    };
    clear();
    window.addEventListener('focus', clear);
    document.addEventListener('visibilitychange', clear);
    return () => {
      window.removeEventListener('focus', clear);
      document.removeEventListener('visibilitychange', clear);
    };
  }, [currentId, notices]);

  const count = notices.length;
  useEffect(() => {
    document.title = titleWithCount(document.title, count);
    setIconDot(count > 0);
  }, [count]);
  useEffect(
    () => () => {
      document.title = titleWithCount(document.title, 0);
      setIconDot(false);
    },
    [],
  );

  // A click on a push of the runtime (the service worker tells the page) opens its chat here.
  useEffect(() => {
    const onOpen = (event: Event) => {
      const id = (event as CustomEvent<unknown>).detail;
      if (typeof id === 'string' && id.startsWith('tab-')) open.current(id);
    };
    window.addEventListener(OPEN_CONVERSATION_EVENT, onOpen);
    return () => window.removeEventListener(OPEN_CONVERSATION_EVENT, onOpen);
  }, []);

  const onOpen = useCallback((notice: INotice) => {
    clearChatNotices(notice.chatId);
    open.current(notice.chatId);
  }, []);

  return { notices, onOpen, onDismiss: dismissNotice, onDismissAll: dismissAllNotices };
};
