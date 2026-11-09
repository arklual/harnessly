// What the person did from the smart notice: an answer, a decision, a code. The notification centre lists it under
// what happened, so the person sees what was answered and where. Kept in this browser per person (localStorage, as the
// notices are), the newest first, a short list.
import { useSyncExternalStore } from 'react';

export interface IHistoryEntry {
  key: string;
  chatId: string | null;
  icon: string;
  /** The chat's title. */
  title: string;
  /** What was done: «Ответ: …», «Разрешено», «Код введён». */
  text: string;
  at: number;
}

export const MAX_HISTORY = 30;
const PREFIX = 'hx-notice-history:';

const isEntry = (raw: unknown): raw is IHistoryEntry => {
  if (typeof raw !== 'object' || raw === null) return false;
  const e = raw as Record<string, unknown>;
  return (
    typeof e.key === 'string' &&
    (typeof e.chatId === 'string' || e.chatId === null) &&
    typeof e.icon === 'string' &&
    typeof e.title === 'string' &&
    typeof e.text === 'string' &&
    typeof e.at === 'number'
  );
};

export const parseHistory = (raw: string | null): readonly IHistoryEntry[] => {
  if (!raw) return [];
  try {
    const list: unknown = JSON.parse(raw);
    return Array.isArray(list) ? list.filter(isEntry).slice(0, MAX_HISTORY) : [];
  } catch {
    return [];
  }
};

const listeners = new Set<() => void>();
let person = 'local';
let list: readonly IHistoryEntry[] | null = null;
const key = () => PREFIX + person;

const read = (): readonly IHistoryEntry[] => {
  try {
    return parseHistory(window.localStorage.getItem(key()));
  } catch {
    return [];
  }
};

export const historySnapshot = (): readonly IHistoryEntry[] => (list ??= read());

/** Whose history: called while rendering, before it is read (as setNoticesPerson). */
export const setHistoryPerson = (next: string): void => {
  if (next === person) return;
  person = next;
  list = null;
};

export const addHistory = (entry: IHistoryEntry): void => {
  list = [entry, ...historySnapshot().filter((e) => e.key !== entry.key)].slice(0, MAX_HISTORY);
  try {
    window.localStorage.setItem(key(), JSON.stringify(list));
  } catch {
    // Private mode or a full storage: the history holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

const NONE: readonly IHistoryEntry[] = [];

export const useHistory = (): readonly IHistoryEntry[] =>
  useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      const onStorage = (event: StorageEvent) => {
        if (event.key !== key()) return;
        list = null;
        listener();
      };
      window.addEventListener('storage', onStorage);
      return () => {
        listeners.delete(listener);
        window.removeEventListener('storage', onStorage);
      };
    },
    historySnapshot,
    () => NONE,
  );
