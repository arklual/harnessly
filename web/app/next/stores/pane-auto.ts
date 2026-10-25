// «Открывать то, над чем работает агент» (Settings → General → Look): whether the pane opens by itself the design the
// agent edits, the document or board it publishes. This browser's, as the theme and the tabs over the window are; the
// key is the one Legacy's pane keeps the same switch under, so both interfaces of a browser agree.
import { useSyncExternalStore } from 'react';

const KEY = 'hl:pane-auto-open';

const listeners = new Set<() => void>();
let on: boolean | null = null;

const read = (): boolean => {
  try {
    return window.localStorage.getItem(KEY) !== '0';
  } catch {
    return true;
  }
};

/** On unless the person switched it off in this browser. */
export const paneAutoSnapshot = (): boolean => (on ??= read());

export const setPaneAuto = (next: boolean): void => {
  on = next;
  try {
    if (next) window.localStorage.removeItem(KEY);
    else window.localStorage.setItem(KEY, '0');
  } catch {
    // Private mode or a full storage: the switch holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

export const usePaneAuto = (): boolean =>
  useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      const onStorage = (event: StorageEvent) => {
        if (event.key !== KEY) return;
        on = null;
        listener();
      };
      window.addEventListener('storage', onStorage);
      return () => {
        listeners.delete(listener);
        window.removeEventListener('storage', onStorage);
      };
    },
    paneAutoSnapshot,
    () => true,
  );
