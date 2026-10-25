import { useSyncExternalStore } from 'react';

/** The desktop sidebar folded to a rail; per device. */
const RAIL_KEY = 'hx-sidebar-collapsed';

const listeners = new Set<() => void>();
let folded: boolean | null = null;

const read = (): boolean => {
  try {
    return localStorage.getItem(RAIL_KEY) === '1';
  } catch {
    return false;
  }
};

const write = (value: boolean) => {
  try {
    localStorage.setItem(RAIL_KEY, value ? '1' : '0');
  } catch {
    // Private mode or full storage: the rail still folds for this page.
  }
};

const snapshot = (): boolean => (folded ??= read());

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/**
 * Folds the sidebar to its rail and opens it again. The state lives here and not in the shell: the toggle then
 * re-renders only what draws the rail, not the whole shell with the chat in it, so the click's frame stays short.
 */
export const toggleRail = () => {
  folded = !snapshot();
  write(folded);
  for (const listener of [...listeners]) listener();
};

export const useRail = (): boolean => useSyncExternalStore(subscribe, snapshot, () => false);
