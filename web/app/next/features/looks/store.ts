// The person's looks in this browser: the prefs key hx-looks, which next/stores/synced-prefs.ts carries to the person's
// settings and back (every device the same; this copy makes the first paint right without waiting for the network).
import { useSyncExternalStore } from 'react';
import { notePrefWrite } from '@/next/core/pref-write';
import {
  NO_LOOKS,
  isEmpty,
  readLooks,
  scopeLook,
  withLook,
  type ILook,
  type ILooks,
  type TLookScope,
} from '@/next/features/looks/types';

export const LOOKS_KEY = 'hx-looks';

const listeners = new Set<() => void>();
let cache: { raw: string | null; looks: ILooks } | null = null;

const readRaw = (): string | null => {
  try {
    return window.localStorage.getItem(LOOKS_KEY);
  } catch {
    return null;
  }
};

export const getLooks = (): ILooks => {
  if (typeof window === 'undefined') return NO_LOOKS;
  const raw = readRaw();
  if (cache?.raw !== raw) cache = { raw, looks: readLooks(raw) };
  return cache.looks;
};

const emit = () => listeners.forEach((fn) => fn());

let listening = false;
const listen = () => {
  if (listening || typeof window === 'undefined') return;
  listening = true;
  // Another tab, or the settings bringing another device's looks (synced-prefs writes and tells by a storage event).
  window.addEventListener('storage', (event) => {
    if (event.key === LOOKS_KEY || event.key === null) emit();
  });
};

export const subscribeLooks = (fn: () => void): (() => void) => {
  listen();
  listeners.add(fn);
  return () => listeners.delete(fn);
};

export const useLooks = (): ILooks => useSyncExternalStore(subscribeLooks, getLooks, () => NO_LOOKS);

/** The settings are written once a change rests: a dot dragged across the picker is one write, not hundreds. */
const SYNC_REST_MS = 600;
let syncTimer: ReturnType<typeof setTimeout> | null = null;

/** Keeps the whole set: in this browser at once, then (when the change rests) in the person's settings. */
export const setLooks = (looks: ILooks): void => {
  try {
    if (isEmpty(looks)) window.localStorage.removeItem(LOOKS_KEY);
    else window.localStorage.setItem(LOOKS_KEY, JSON.stringify(looks));
  } catch {
    // Storage closed: the look lasts for this page.
  }
  cache = null;
  emit();
  if (syncTimer) clearTimeout(syncTimer);
  syncTimer = setTimeout(() => {
    syncTimer = null;
    notePrefWrite(LOOKS_KEY);
  }, SYNC_REST_MS);
};

/** Puts a look on a scope (null takes the scope's own away); answers the scope's look before, for an Undo. */
export const putLook = (scope: TLookScope, look: ILook | null): ILook | null => {
  const now = getLooks();
  const before = scopeLook(now, scope);
  setLooks(withLook(now, scope, look));
  return before;
};

/** Where the editor of a look is open (the sheet of a folder or a chat, look-sheet.tsx); null: closed. */
let editing: TLookScope | null = null;
const editListeners = new Set<() => void>();
export const openLookEditor = (scope: TLookScope | null): void => {
  editing = scope;
  editListeners.forEach((fn) => fn());
};
export const useLookEditing = (): TLookScope | null =>
  useSyncExternalStore(
    (fn) => {
      editListeners.add(fn);
      return () => editListeners.delete(fn);
    },
    () => editing,
    () => null,
  );
