// The widgets under the chat's right column: kept in this browser under `hx-widgets` and carried to the
// person's settings by stores/synced-prefs.ts, so every computer has the same list. Writes come from the person's hand
// (add, move, fold, a note) and are few; a timer's tick or a widget's data never writes here.
import { useSyncExternalStore } from 'react';
import { notePrefWrite } from '@/next/core/pref-write';
import { NO_WIDGETS, parseWidgets, serializeWidgets, type ISideWidgets } from '@/next/features/widgets/model';

export const WIDGETS_KEY = 'hx-widgets';

const listeners = new Set<() => void>();
let current: ISideWidgets | null = null;

const read = (): ISideWidgets => {
  try {
    return parseWidgets(window.localStorage.getItem(WIDGETS_KEY));
  } catch {
    return NO_WIDGETS;
  }
};

export const widgetsSnapshot = (): ISideWidgets => (current ??= read());

/** Saves the list and tells the settings (stores/synced-prefs.ts). */
export const setWidgets = (next: ISideWidgets): void => {
  current = next;
  try {
    window.localStorage.setItem(WIDGETS_KEY, serializeWidgets(next));
  } catch {
    // A closed storage: the list holds for this page.
  }
  for (const listener of [...listeners]) listener();
  notePrefWrite(WIDGETS_KEY);
};

/** Changes the list from what it is now: `make` answers null for no change. */
export const updateWidgets = (make: (now: ISideWidgets) => ISideWidgets | null): boolean => {
  const next = make(widgetsSnapshot());
  if (next === null) return false;
  setWidgets(next);
  return true;
};

export const useWidgets = (): ISideWidgets =>
  useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      // Another tab, or the settings' copy coming in (synced-prefs.ts writes the key and sends a storage event).
      const onStorage = (event: StorageEvent) => {
        if (event.key !== WIDGETS_KEY) return;
        current = null;
        listener();
      };
      window.addEventListener('storage', onStorage);
      return () => {
        listeners.delete(listener);
        window.removeEventListener('storage', onStorage);
      };
    },
    widgetsSnapshot,
    () => NO_WIDGETS,
  );
