// The notices on the live list of conversations: an event in a chat the person is not looking at (another chat is
// open, or the page is hidden or behind another window) becomes a notice, a chime, «(n)» in the tab's title and a dot
// on its icon, and with the page hidden a notification of the system. What the list showed before the machine's own
// snapshot (the browser's cache) and what a chat stood at when first seen are no news. Opening the chat clears its
// notice; so does the chat moving past it (the question answered elsewhere, the answer read on another device).
import { useCallback, useEffect, useRef } from 'react';
import { appBasePath } from '@/platform/config';
import { noticeEventOf } from '@/next/features/notifications/notice-events';
import {
  playSound,
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
  noticesSnapshot,
  notifySnapshot,
  pushNotice,
  SAMPLE_CHAT,
  setNoticesPerson,
  useNoticeList,
  type INotice,
  type INotifySettings,
  type TNoticeSound,
} from '@/next/stores/notices';
import type { Conversation } from '@/next/protocol/v2';

/** What the service worker's click and the current interface's links ask for: open this chat (hooks/use-web-push.ts). */
const OPEN_CONVERSATION_EVENT = 'hl:open-conversation';
/** The last event a window of this browser chimed for: two windows of the app do not both chime. */
const PING_KEY = 'hx-notice-ping';

export const firstToPing = (key: string): boolean => {
  try {
    if (window.localStorage.getItem(PING_KEY) === key) return false;
    window.localStorage.setItem(PING_KEY, key);
  } catch {
    // Storage blocked: this window chimes on its own.
  }
  return true;
};

/** The sound an event makes: a permission waits now, a question or a plan waits, a turn is ready; a failure is quiet. */
export const soundOf = (kind: INotice['kind'], conv: Pick<Conversation, 'gate'>): TNoticeSound | null => {
  if (kind === 'waiting') return conv.gate?.kind === 'permission' ? 'urgent' : 'wait';
  if (kind === 'done') return 'done';
  return null;
};

const LOUDNESS: Record<TNoticeSound, number> = { urgent: 3, wait: 2, done: 1 };

/** Of two sounds the one that says more: «waits now» over «waits» over «ready». */
export const louder = (a: TNoticeSound | null, b: TNoticeSound | null): TNoticeSound | null => {
  if (a === null) return b;
  if (b === null) return a;
  return LOUDNESS[b] > LOUDNESS[a] ? b : a;
};

/** Whether this sound is on: the sounds at all and this one. */
export const soundOn = (settings: INotifySettings, sound: TNoticeSound | null): sound is TNoticeSound =>
  sound !== null && settings.sound && settings.sounds[sound];

const pageInFront = (): boolean => document.visibilityState === 'visible' && document.hasFocus();

/**
 * A notice whose chat stands at another event goes only once the chat has stood there this long: a sync that shows the
 * record for a moment as it was (another copy of it, a reconnect) does not take the notice away and bring it back.
 */
export const DROP_SETTLE_MS = 1500;

/**
 * The notices to take away now. `leaving` (kept by the caller) remembers since when each notice's chat stands elsewhere;
 * a chat back at the notice's event forgets it. `current` is each chat's event now (absent: not known yet).
 */
export const settledDrops = (
  leaving: Map<string, number>,
  notices: readonly INotice[],
  current: ReadonlyMap<string, string | null>,
  now: number,
): ReadonlySet<string> => {
  const drop = new Set<string>();
  const listed = new Set<string>();
  for (const n of notices) {
    listed.add(n.key);
    if (!current.has(n.chatId) || current.get(n.chatId) === n.key) {
      leaving.delete(n.key);
      continue;
    }
    const since = leaving.get(n.key) ?? now;
    leaving.set(n.key, since);
    if (now - since >= DROP_SETTLE_MS) drop.add(n.key);
  }
  for (const key of [...leaving.keys()]) if (!listed.has(key) || drop.has(key)) leaving.delete(key);
  return drop;
};

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
  /** Since when each notice's chat stands at another event (settledDrops). */
  const leaving = useRef(new Map<string, number>());
  const settleTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(settleTimer.current), []);
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
    let sound: TNoticeSound | null = null;
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
      // Back at an event it still shows (it only looked away for a moment): no new notice, no sound.
      if (noticesSnapshot().some((n) => n.key === event.key)) continue;
      sound = louder(sound, soundOf(event.kind, conv));
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
    const settle = () => {
      const drop = settledDrops(leaving.current, noticesSnapshot(), now, Date.now());
      if (drop.size > 0) dropNotices((n) => drop.has(n.key));
      window.clearTimeout(settleTimer.current);
      if (leaving.current.size > 0) settleTimer.current = window.setTimeout(settle, DROP_SETTLE_MS);
    };
    settle();
    fresh.forEach(pushNotice);
    const last = fresh[fresh.length - 1];
    if (!last) return;
    if (document.visibilityState === 'visible') {
      if (soundOn(settings, sound) && firstToPing(last.key)) playSound(sound);
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

  // The sample of Settings → Notifications is no news of a chat.
  const count = notices.filter((n) => n.chatId !== SAMPLE_CHAT).length;
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
    if (notice.chatId === SAMPLE_CHAT) return;
    open.current(notice.chatId);
  }, []);

  return { notices, onOpen, onDismiss: dismissNotice, onDismissAll: dismissAllNotices };
};
