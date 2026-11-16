// Who sits at this browser: one of the people who share the machine (PersonSettings.people), or nobody named. It is
// this browser's alone and never one of the synced prefs (stores/synced-prefs.ts): two people on one account each name
// themselves on their own devices. The sidebar, the notices and the author of what this page sends go by it
// (features/people).
import { useSyncExternalStore } from 'react';

const KEY = 'hx-whoami';

const listeners = new Set<() => void>();
let me: string | null | undefined;

const read = (): string | null => {
  try {
    const v = window.localStorage.getItem(KEY);
    return v === null || v.trim() === '' ? null : v;
  } catch {
    return null;
  }
};

/** The name this browser gave its person, null when none. */
export const whoamiSnapshot = (): string | null => (me === undefined ? (me = read()) : me);

export const setWhoami = (next: string | null): void => {
  me = next;
  try {
    if (next === null) window.localStorage.removeItem(KEY);
    else window.localStorage.setItem(KEY, next);
  } catch {
    // Private mode or a full storage: the choice holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

export const useWhoami = (): string | null =>
  useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      const onStorage = (event: StorageEvent) => {
        if (event.key !== KEY) return;
        me = undefined;
        listener();
      };
      window.addEventListener('storage', onStorage);
      return () => {
        listeners.delete(listener);
        window.removeEventListener('storage', onStorage);
      };
    },
    whoamiSnapshot,
    () => null,
  );

// The other people whose things this browser unfolded in the sidebar, by name: this browser's too.
const OPEN_KEY = 'hx-people-open';
const openListeners = new Set<() => void>();
let unfolded: ReadonlySet<string> | undefined;

const readOpen = (): ReadonlySet<string> => {
  try {
    const v: unknown = JSON.parse(window.localStorage.getItem(OPEN_KEY) ?? '[]');
    return new Set(Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);
  } catch {
    return new Set();
  }
};

export const unfoldedSnapshot = (): ReadonlySet<string> => (unfolded ??= readOpen());

export const setUnfolded = (person: string, open: boolean): void => {
  const next = new Set(unfoldedSnapshot());
  if (open) next.add(person);
  else next.delete(person);
  unfolded = next;
  try {
    window.localStorage.setItem(OPEN_KEY, JSON.stringify([...next]));
  } catch {
    // The fold holds for this page.
  }
  for (const listener of [...openListeners]) listener();
};

const NONE: ReadonlySet<string> = new Set();

export const useUnfolded = (): ReadonlySet<string> =>
  useSyncExternalStore(
    (listener) => {
      openListeners.add(listener);
      return () => openListeners.delete(listener);
    },
    unfoldedSnapshot,
    () => NONE,
  );
