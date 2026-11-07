// Debug mode of the apps: the person's own switch (this browser), which opens the debug section of every app's inspector
// (calls, errors of the view, storage, reload), not only a dev install's. Kept in localStorage: a viewer's convenience.
import { useSyncExternalStore } from 'react';

const KEY = 'hx-apps-debug';
const listeners = new Set<() => void>();

const read = (): boolean => {
  try {
    return localStorage.getItem(KEY) === 'on';
  } catch {
    return false;
  }
};

let on: boolean | null = null;

export const appsDebug = (): boolean => {
  on ??= read();
  return on;
};

export const setAppsDebug = (next: boolean): void => {
  on = next;
  try {
    if (next) localStorage.setItem(KEY, 'on');
    else localStorage.removeItem(KEY);
  } catch {
    // Without storage the switch holds for this page only.
  }
  listeners.forEach((l) => l());
};

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

export const useAppsDebug = (): boolean => useSyncExternalStore(subscribe, appsDebug, () => false);
