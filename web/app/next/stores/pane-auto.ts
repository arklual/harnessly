// «Открывать то, над чем работает агент» (Settings → General → Look): whether the pane opens by itself the design the
// agent edits, the document or board it publishes, its browser, a file it hands over, and which of them. This
// browser's, kept in the person's settings (synced-prefs.ts). The key is the one Legacy's pane keeps the same switch
// under, so both interfaces of a browser agree: '0' is off, absent is everything on, and a JSON record of the kinds is
// on with some kinds off (Legacy reads anything but '0' as on).
import { notePrefWrite } from '@/next/core/pref-write';
import { useSyncExternalStore } from 'react';
import { PANE_AUTO_KINDS, type TPaneAutoKind, type TPaneAutoKinds } from '@/next/features/pane/intents';

const KEY = 'hl:pane-auto-open';

export const ALL_PANE_AUTO_KINDS: TPaneAutoKinds = {
  design: true,
  artifacts: true,
  browser: true,
  files: true,
};

export interface IPaneAuto {
  on: boolean;
  kinds: TPaneAutoKinds;
}

const ALL_ON: IPaneAuto = { on: true, kinds: ALL_PANE_AUTO_KINDS };

/** The stored value as the switches; a broken record is everything on. */
export const parsePaneAuto = (raw: string | null): IPaneAuto => {
  if (raw === '0') return { on: false, kinds: ALL_PANE_AUTO_KINDS };
  if (!raw || raw[0] !== '{') return ALL_ON;
  try {
    const saved: unknown = JSON.parse(raw);
    if (typeof saved !== 'object' || saved === null) return ALL_ON;
    const kinds: Record<TPaneAutoKind, boolean> = { ...ALL_PANE_AUTO_KINDS };
    for (const kind of PANE_AUTO_KINDS) {
      const v = (saved as Record<string, unknown>)[kind];
      if (typeof v === 'boolean') kinds[kind] = v;
    }
    return { on: true, kinds };
  } catch {
    return ALL_ON;
  }
};

/** What to store: null (remove) for everything on. */
export const formatPaneAuto = (value: IPaneAuto): string | null => {
  if (!value.on) return '0';
  if (PANE_AUTO_KINDS.every((kind) => value.kinds[kind])) return null;
  return JSON.stringify(value.kinds);
};

const listeners = new Set<() => void>();
let current: IPaneAuto | null = null;

const read = (): IPaneAuto => {
  try {
    return parsePaneAuto(window.localStorage.getItem(KEY));
  } catch {
    return ALL_ON;
  }
};

const write = (next: IPaneAuto): void => {
  current = next;
  try {
    const raw = formatPaneAuto(next);
    if (raw === null) window.localStorage.removeItem(KEY);
    else window.localStorage.setItem(KEY, raw);
  } catch {
    // Private mode or a full storage: the switch holds for this page.
  }
  for (const listener of [...listeners]) listener();
  notePrefWrite(KEY);
};

export const paneAutoStateSnapshot = (): IPaneAuto => (current ??= read());

/** On unless the person switched it off in this browser. */
export const paneAutoSnapshot = (): boolean => paneAutoStateSnapshot().on;

export const setPaneAuto = (next: boolean): void => write({ ...paneAutoStateSnapshot(), on: next });

export const setPaneAutoKind = (kind: TPaneAutoKind, on: boolean): void => {
  const state = paneAutoStateSnapshot();
  write({ on: state.on, kinds: { ...state.kinds, [kind]: on } });
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  const onStorage = (event: StorageEvent) => {
    if (event.key !== KEY) return;
    current = null;
    listener();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('storage', onStorage);
  };
};

export const usePaneAuto = (): boolean => useSyncExternalStore(subscribe, paneAutoSnapshot, () => true);

export const usePaneAutoState = (): IPaneAuto =>
  useSyncExternalStore(subscribe, paneAutoStateSnapshot, () => ALL_ON);
