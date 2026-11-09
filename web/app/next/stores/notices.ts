// The notices at the top of the window: what needs the person in a chat they are not looking at (a question, a
// permission, a plan, a finished turn, a failure, a limit). One notice per chat, the newest event replacing the one
// before; it stays until the chat is opened, the person takes it away, or its chat moves past what it said. The list
// is the viewer's: localStorage per person, shared by every window of the app in this browser (a notice taken away in
// one leaves all of them), and the switches of Settings → Notifications next to it.
import { useSyncExternalStore } from 'react';

export const NOTICE_KIND = {
  waiting: 'waiting',
  done: 'done',
  failed: 'failed',
  limit: 'limit',
} as const;
export type TNoticeKind = (typeof NOTICE_KIND)[keyof typeof NOTICE_KIND];
export const NOTICE_KINDS = [
  NOTICE_KIND.waiting,
  NOTICE_KIND.done,
  NOTICE_KIND.failed,
  NOTICE_KIND.limit,
] as const satisfies readonly TNoticeKind[];

export interface INotice {
  /** The event it tells of: the chat, and what happened in it (a turn, a question); a repeat of the same key is no news. */
  key: string;
  chatId: string;
  kind: TNoticeKind;
  icon: string;
  /** The chat's title. */
  title: string;
  /** What happened, in a line: the question waits, the answer's start, why it failed. */
  text: string;
  at: number;
  /**
   * A finished turn's summary: 2–4 lines of plain text, one per line («\n» between them), on what the agent
   * did — the cheap model's, or Claude's own recap written after the turn. Absent until it comes, and when Settings
   * turn summaries off; `text` stays the answer's start either way.
   */
  summary?: string;
}

/** The chat of the sample notice Settings → Notifications shows: it has no chat to open. */
export const SAMPLE_CHAT = 'notice-sample';

/** More than this is not a pile to read any more: the oldest leave (the notification centre still lists them). */
export const MAX_NOTICES = 20;
const LIST_PREFIX = 'hx-notices:';
const SETTINGS_KEY = 'hx-notify';

const isNotice = (raw: unknown): raw is INotice => {
  if (typeof raw !== 'object' || raw === null) return false;
  const n = raw as Record<string, unknown>;
  return (
    typeof n.key === 'string' &&
    typeof n.chatId === 'string' &&
    typeof n.title === 'string' &&
    typeof n.text === 'string' &&
    typeof n.icon === 'string' &&
    typeof n.at === 'number' &&
    (n.summary === undefined || typeof n.summary === 'string') &&
    (NOTICE_KINDS as readonly unknown[]).includes(n.kind)
  );
};

/** The kept list as it was written; anything that is not a notice is dropped. */
export const parseNotices = (raw: string | null): readonly INotice[] => {
  if (!raw) return [];
  try {
    const list: unknown = JSON.parse(raw);
    return Array.isArray(list) ? list.filter(isNotice).slice(0, MAX_NOTICES) : [];
  } catch {
    return [];
  }
};

/** The list with `notice` on top: it replaces its chat's notice; the same event twice changes nothing. */
export const withNotice = (list: readonly INotice[], notice: INotice): readonly INotice[] => {
  if (list.some((n) => n.key === notice.key)) return list;
  return [notice, ...list.filter((n) => n.chatId !== notice.chatId)].slice(0, MAX_NOTICES);
};

// ---- the list

const listeners = new Set<() => void>();
let person = 'local';
let list: readonly INotice[] | null = null;

const listKey = () => LIST_PREFIX + person;
const emit = () => {
  for (const listener of [...listeners]) listener();
};

const read = (): readonly INotice[] => {
  try {
    return parseNotices(window.localStorage.getItem(listKey()));
  } catch {
    return [];
  }
};

const write = (next: readonly INotice[]) => {
  if (next === list) return;
  list = next;
  try {
    if (next.length === 0) window.localStorage.removeItem(listKey());
    else window.localStorage.setItem(listKey(), JSON.stringify(next));
  } catch {
    // Private mode or a full storage: the notices hold for this page.
  }
  emit();
};

export const noticesSnapshot = (): readonly INotice[] => (list ??= read());

/** Whose notices these are: one list per person signed in on this browser. Called while rendering, before the list
 *  is read, so it tells no one: the next read is the person's own list. */
export const setNoticesPerson = (next: string): void => {
  if (next === person) return;
  person = next;
  list = null;
};

export const pushNotice = (notice: INotice): void => write(withNotice(noticesSnapshot(), notice));

/** Takes notices away: by key, or every one `drop` says so of. */
export const dropNotices = (drop: (notice: INotice) => boolean): void => {
  const current = noticesSnapshot();
  const kept = current.filter((n) => !drop(n));
  if (kept.length !== current.length) write(kept);
};

/** Puts a finished turn's summary on its notice, if the notice is still there. */
export const setNoticeSummary = (key: string, summary: string): void => {
  const current = noticesSnapshot();
  if (!current.some((n) => n.key === key && n.summary !== summary)) return;
  write(current.map((n) => (n.key === key ? { ...n, summary } : n)));
};

export const dismissNotice = (key: string): void => dropNotices((n) => n.key === key);
export const clearChatNotices = (chatId: string): void => dropNotices((n) => n.chatId === chatId);
export const dismissAllNotices = (): void => write([]);

const subscribeList = (listener: () => void) => {
  listeners.add(listener);
  const onStorage = (event: StorageEvent) => {
    if (event.key !== listKey()) return;
    list = null;
    listener();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('storage', onStorage);
  };
};

const NO_NOTICES: readonly INotice[] = [];

export const useNoticeList = (): readonly INotice[] =>
  useSyncExternalStore(subscribeList, noticesSnapshot, () => NO_NOTICES);

// ---- the switches

/** Where the smart notice stands: a corner or the middle of an edge of the window. The middle of the bottom edge is
 *  over the field of the chat on screen (the window's bottom where there is no field). */
export const NOTICE_PLACES = [
  'top-left',
  'top-center',
  'top-right',
  'bottom-left',
  'bottom-center',
  'bottom-right',
] as const;
export type TNoticePlace = (typeof NOTICE_PLACES)[number];

/** The sounds: something waits for an answer, something waits now (a code, a permission), something is ready. */
export const NOTICE_SOUNDS = ['wait', 'urgent', 'done'] as const;
export type TNoticeSound = (typeof NOTICE_SOUNDS)[number];

/** The version of the saved switches: before 2 the default place was the top right corner, saved with everything;
 *  before 3 the smart notice could be turned off in Settings (the switch is gone: «Что показывать» says what comes). */
const NOTIFY_VERSION = 3;

const versionOf = (s: Record<string, unknown>): number => (typeof s.v === 'number' ? s.v : 1);

export interface INotifySettings {
  /** Which events make a notice. */
  kinds: Readonly<Record<TNoticeKind, boolean>>;
  /** The sounds at all; off, none of `sounds` plays. */
  sound: boolean;
  sounds: Readonly<Record<TNoticeSound, boolean>>;
  /** A notification of the system while the app's tab is in the background (the browser's permission asked once). */
  system: boolean;
  /** The smart notice in the window; off, a notice only counts in the tab's title and sounds. */
  cards: boolean;
  place: TNoticePlace;
  /** A finished turn comes with a few lines on what was done, from the cheapest model. */
  summary: boolean;
}

export const DEFAULT_NOTIFY: INotifySettings = {
  kinds: { waiting: true, done: true, failed: true, limit: true },
  sound: true,
  sounds: { wait: true, urgent: true, done: false },
  system: false,
  cards: true,
  place: 'bottom-center',
  summary: true,
} as const;

const savedPlace = (s: Record<string, unknown>): TNoticePlace => {
  const place = NOTICE_PLACES.find((one) => one === s.place);
  // The old default was saved as if chosen: it moves to the new one; a place picked by hand stays.
  if (!place || (versionOf(s) < 2 && place === 'top-right')) return DEFAULT_NOTIFY.place;
  return place;
};

/** The saved switches over the defaults: what this browser never saved is its default. */
export const parseNotify = (raw: string | null): INotifySettings => {
  if (!raw) return DEFAULT_NOTIFY;
  try {
    const saved: unknown = JSON.parse(raw);
    if (typeof saved !== 'object' || saved === null) return DEFAULT_NOTIFY;
    const s = saved as Record<string, unknown>;
    const kinds: Record<TNoticeKind, boolean> = { ...DEFAULT_NOTIFY.kinds };
    if (typeof s.kinds === 'object' && s.kinds !== null)
      for (const kind of NOTICE_KINDS) {
        const on = (s.kinds as Record<string, unknown>)[kind];
        if (typeof on === 'boolean') kinds[kind] = on;
      }
    const sounds: Record<TNoticeSound, boolean> = { ...DEFAULT_NOTIFY.sounds };
    if (typeof s.sounds === 'object' && s.sounds !== null)
      for (const sound of NOTICE_SOUNDS) {
        const on = (s.sounds as Record<string, unknown>)[sound];
        if (typeof on === 'boolean') sounds[sound] = on;
      }
    return {
      kinds,
      sound: typeof s.sound === 'boolean' ? s.sound : DEFAULT_NOTIFY.sound,
      sounds,
      system: typeof s.system === 'boolean' ? s.system : DEFAULT_NOTIFY.system,
      // Settings no longer turn the smart notice off: one turned off before comes back, or it could never be turned on.
      cards: versionOf(s) >= 3 && typeof s.cards === 'boolean' ? s.cards : DEFAULT_NOTIFY.cards,
      place: savedPlace(s),
      summary: typeof s.summary === 'boolean' ? s.summary : DEFAULT_NOTIFY.summary,
    };
  } catch {
    return DEFAULT_NOTIFY;
  }
};

const settingsListeners = new Set<() => void>();
let settings: INotifySettings | null = null;

const readSettings = (): INotifySettings => {
  try {
    return parseNotify(window.localStorage.getItem(SETTINGS_KEY));
  } catch {
    return DEFAULT_NOTIFY;
  }
};

export const notifySnapshot = (): INotifySettings => (settings ??= readSettings());

export const setNotify = (
  patch: Partial<Omit<INotifySettings, 'kinds' | 'sounds'>> & {
    kinds?: Partial<INotifySettings['kinds']>;
    sounds?: Partial<INotifySettings['sounds']>;
  },
): void => {
  const current = notifySnapshot();
  settings = {
    ...current,
    ...patch,
    kinds: { ...current.kinds, ...patch.kinds },
    sounds: { ...current.sounds, ...patch.sounds },
  };
  try {
    window.localStorage.setItem(SETTINGS_KEY, JSON.stringify({ ...settings, v: NOTIFY_VERSION }));
  } catch {
    // Private mode or a full storage: the switches hold for this page.
  }
  for (const listener of [...settingsListeners]) listener();
};

export const useNotify = (): INotifySettings =>
  useSyncExternalStore(
    (listener) => {
      settingsListeners.add(listener);
      const onStorage = (event: StorageEvent) => {
        if (event.key !== SETTINGS_KEY) return;
        settings = null;
        listener();
      };
      window.addEventListener('storage', onStorage);
      return () => {
        settingsListeners.delete(listener);
        window.removeEventListener('storage', onStorage);
      };
    },
    notifySnapshot,
    () => DEFAULT_NOTIFY,
  );
