// The person's choices about project folders in the sidebar: how many chats a folder shows before «Дальше»,
// whether a closed folder still shows its chats that are at work or wait to be looked at, and whether the app offers a
// folder for a new chat. Kept as the settings' `prefs` (stores/synced-prefs.ts), so they follow the person.
import { notePrefWrite } from '@/next/core/pref-write';
import { useSyncExternalStore } from 'react';
import { CHAT_STATUS, type ISidebarChat, type TChatStatus } from './types';

export const FOLDER_PREFS_KEY = 'hx-folders';

export interface IFolderPrefs {
  /** Chats a folder shows before «Дальше». */
  rows: number;
  /** A closed folder shows its chats that are at work or wait for the person's look, opened today. */
  peek: boolean;
  /** After a few prompts the app offers a folder for a chat that has none. */
  suggest: boolean;
}

export const FOLDER_ROWS_CHOICES = [3, 5, 8, 12, 20] as const;
export const FOLDER_DEFAULTS: IFolderPrefs = { rows: 5, peek: true, suggest: true };

/** The most chats a closed folder lets through, so a busy day does not turn it into a list. */
export const PEEK_MAX = 5;

/** Reads a stored record; whatever does not fit falls back to the default of its field. */
export const parseFolderPrefs = (raw: string | null): IFolderPrefs => {
  if (!raw) return FOLDER_DEFAULTS;
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return FOLDER_DEFAULTS;
    const v = value as Partial<Record<keyof IFolderPrefs, unknown>>;
    const rows =
      typeof v.rows === 'number' && Number.isInteger(v.rows) && v.rows >= 1 && v.rows <= 50 ? v.rows : null;
    return {
      rows: rows ?? FOLDER_DEFAULTS.rows,
      peek: typeof v.peek === 'boolean' ? v.peek : FOLDER_DEFAULTS.peek,
      suggest: typeof v.suggest === 'boolean' ? v.suggest : FOLDER_DEFAULTS.suggest,
    };
  } catch {
    return FOLDER_DEFAULTS;
  }
};

const listeners = new Set<() => void>();
let cached: IFolderPrefs | null = null;

const read = (): IFolderPrefs => {
  try {
    return parseFolderPrefs(localStorage.getItem(FOLDER_PREFS_KEY));
  } catch {
    return FOLDER_DEFAULTS;
  }
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  // The person's settings brought another device's choice (stores/synced-prefs.ts).
  const onStorage = (event: StorageEvent) => {
    if (event.key !== FOLDER_PREFS_KEY) return;
    cached = null;
    listener();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('storage', onStorage);
  };
};

const snapshot = (): IFolderPrefs => (cached ??= read());

export const useFolderPrefs = (): IFolderPrefs =>
  useSyncExternalStore(subscribe, snapshot, () => FOLDER_DEFAULTS);

export const setFolderPrefs = (patch: Partial<IFolderPrefs>): void => {
  cached = { ...snapshot(), ...patch };
  try {
    localStorage.setItem(FOLDER_PREFS_KEY, JSON.stringify(cached));
  } catch {
    // Private mode or full storage: the choice holds for this page.
  }
  for (const listener of [...listeners]) listener();
  notePrefWrite(FOLDER_PREFS_KEY);
};

/** The statuses of a chat that is at work or waits for the person's look. */
const LIVE: ReadonlySet<TChatStatus> = new Set([
  CHAT_STATUS.thinking,
  CHAT_STATUS.background,
  CHAT_STATUS.delegated,
  CHAT_STATUS.question,
  CHAT_STATUS.perm,
  CHAT_STATUS.plan,
  CHAT_STATUS.done,
]);

/** Local midnight of the moment `now`. */
export const startOfDay = (now: number): number => {
  const day = new Date(now);
  day.setHours(0, 0, 0, 0);
  return day.getTime();
};

/**
 * The chats a closed folder still shows, so the person sees where work goes on: the chat open now, whatever its
 * state, and the ones at work or waiting for a look, active today (older ones stay behind the fold), newest first, at
 * most PEEK_MAX.
 */
export const peekedChats = (
  list: readonly ISidebarChat[],
  now: number,
  currentId: string | null = null,
): readonly ISidebarChat[] => {
  const since = startOfDay(now);
  const live = (chat: ISidebarChat) =>
    LIVE.has(chat.status) && Math.max(chat.lastActivityAt, chat.lastPromptAt) >= since;
  const shown = list.filter((chat) => chat.id !== currentId && live(chat)).slice(0, PEEK_MAX);
  const current = currentId === null ? undefined : list.find((chat) => chat.id === currentId);
  if (!current) return shown;
  // The open chat keeps its place among the others; when they fill the peek, it takes the last one's.
  return list.filter((chat) => chat === current || shown.slice(0, PEEK_MAX - 1).includes(chat));
};
