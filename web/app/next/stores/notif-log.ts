// The notification centre's log of what happened in the chats: a finished turn (with the start of the answer), a
// failed one (with why). The chats' statuses say only what stands now and lose it once the chat is opened; the log
// keeps it as history, read or not, until the person takes it away. Kept in this browser per person (localStorage, as
// the notices are), the newest first, a short list. What waits for the person is not logged: it is live, from the chats.
import { useSyncExternalStore } from 'react';

export const LOG_KIND = { done: 'done', failed: 'failed' } as const;
export type TLogKind = (typeof LOG_KIND)[keyof typeof LOG_KIND];

export interface ILogEntry {
  /** The event's own key (notice-events.ts): the same turn seen twice is one entry. */
  key: string;
  chatId: string;
  kind: TLogKind;
  icon: string;
  /** The chat's title when it happened. */
  title: string;
  /** The gist: the start of the answer, or why it failed. */
  text: string;
  at: number;
  read: boolean;
}

export const MAX_LOG = 60;
const PREFIX = 'hx-notif-log:';
const GONE_PREFIX = 'hx-notif-gone:';
/** Keys taken away are remembered this long, so a chat that still stands at the same event does not bring it back. */
const MAX_GONE = 200;

const isEntry = (raw: unknown): raw is ILogEntry => {
  if (typeof raw !== 'object' || raw === null) return false;
  const e = raw as Record<string, unknown>;
  return (
    typeof e.key === 'string' &&
    typeof e.chatId === 'string' &&
    (e.kind === LOG_KIND.done || e.kind === LOG_KIND.failed) &&
    typeof e.icon === 'string' &&
    typeof e.title === 'string' &&
    typeof e.text === 'string' &&
    typeof e.at === 'number' &&
    typeof e.read === 'boolean'
  );
};

export const parseLog = (raw: string | null): readonly ILogEntry[] => {
  if (!raw) return [];
  try {
    const list: unknown = JSON.parse(raw);
    return Array.isArray(list) ? list.filter(isEntry).slice(0, MAX_LOG) : [];
  } catch {
    return [];
  }
};

const parseGone = (raw: string | null): readonly string[] => {
  if (!raw) return [];
  try {
    const list: unknown = JSON.parse(raw);
    return Array.isArray(list)
      ? list.filter((k): k is string => typeof k === 'string').slice(0, MAX_GONE)
      : [];
  } catch {
    return [];
  }
};

/**
 * The log with the chats' events of now put in: a new event goes on top (unread), one already there keeps its place;
 * an entry whose chat stands at another event now (opened, answered, gone on) is read. Unchanged, it is the same list.
 */
export const mergeLog = (
  log: readonly ILogEntry[],
  events: readonly Omit<ILogEntry, 'read'>[],
  current: ReadonlyMap<string, string | null>,
  gone: ReadonlySet<string>,
): readonly ILogEntry[] => {
  const have = new Set(log.map((e) => e.key));
  const fresh = events
    .filter((e) => !have.has(e.key) && !gone.has(e.key))
    .map((e) => ({ ...e, read: false }));
  // A gist that came later for the same event (Claude's recap after the answer's start) takes its place.
  const gist = new Map(events.map((e) => [e.key, e.text]));
  let changed = fresh.length > 0;
  const kept = log.map((e) => {
    const text = gist.get(e.key);
    const next = text !== undefined && text !== e.text ? { ...e, text } : e;
    const read = next.read || (current.has(e.chatId) && current.get(e.chatId) !== e.key);
    if (next === e && read === e.read) return e;
    changed = true;
    return { ...next, read };
  });
  if (!changed) return log;
  return [...fresh.sort((a, b) => b.at - a.at), ...kept].slice(0, MAX_LOG);
};

// ---- the store

const listeners = new Set<() => void>();
let person = 'local';
let list: readonly ILogEntry[] | null = null;
let gone: ReadonlySet<string> | null = null;

const read = <T>(key: string, parse: (raw: string | null) => T, fallback: T): T => {
  try {
    return parse(window.localStorage.getItem(key));
  } catch {
    return fallback;
  }
};
const save = (key: string, value: unknown) => {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Private mode or a full storage: the log holds for this page.
  }
};

export const logSnapshot = (): readonly ILogEntry[] => (list ??= read(PREFIX + person, parseLog, []));
const goneSnapshot = (): ReadonlySet<string> =>
  (gone ??= new Set(read(GONE_PREFIX + person, parseGone, [] as readonly string[])));

/** Whose log: called while rendering, before it is read (as setNoticesPerson). */
export const setLogPerson = (next: string): void => {
  if (next === person) return;
  person = next;
  list = null;
  gone = null;
};

const write = (next: readonly ILogEntry[]) => {
  if (next === list) return;
  list = next;
  save(PREFIX + person, next);
  for (const listener of [...listeners]) listener();
};

/** Puts the chats' events of now into the log (see mergeLog). */
export const syncLog = (
  events: readonly Omit<ILogEntry, 'read'>[],
  current: ReadonlyMap<string, string | null>,
): void => write(mergeLog(logSnapshot(), events, current, goneSnapshot()));

export const markLogRead = (keys: readonly string[]): void => {
  const set = new Set(keys);
  if (!logSnapshot().some((e) => set.has(e.key) && !e.read)) return;
  write(logSnapshot().map((e) => (set.has(e.key) && !e.read ? { ...e, read: true } : e)));
};

/** Takes entries away for good: the same event of a chat does not come back. */
export const dropLog = (keys: readonly string[]): void => {
  const set = new Set(keys);
  gone = new Set([...keys, ...goneSnapshot()].slice(0, MAX_GONE));
  save(GONE_PREFIX + person, [...gone]);
  write(logSnapshot().filter((e) => !set.has(e.key)));
};

const NONE: readonly ILogEntry[] = [];

export const useLog = (): readonly ILogEntry[] =>
  useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      // Another window of the app read or took away an entry: the same log here.
      const onStorage = (event: StorageEvent) => {
        if (event.key !== PREFIX + person && event.key !== GONE_PREFIX + person) return;
        list = null;
        gone = null;
        listener();
      };
      window.addEventListener('storage', onStorage);
      return () => {
        listeners.delete(listener);
        window.removeEventListener('storage', onStorage);
      };
    },
    logSnapshot,
    () => NONE,
  );
