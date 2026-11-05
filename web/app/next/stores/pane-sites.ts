// «Открывать в панели» (Settings → General → Links): the sites whose links open in the pane's browser. The list is
// the installation's (features/chat/pane-sites.ts) with what the person added and without what they took off, kept in
// this browser as the other switches of the look are: a new site of the installation reaches everyone who did not
// take it off.
import { useSyncExternalStore } from 'react';
import { defaultSites, siteOf } from '@/next/features/chat/pane-sites';

const KEY = 'hl:pane-sites';

export interface IPaneSitesChoice {
  added: string[];
  removed: string[];
}

const EMPTY: IPaneSitesChoice = { added: [], removed: [] };

const listeners = new Set<() => void>();
let choice: IPaneSitesChoice | null = null;
let sites: { from: IPaneSitesChoice; list: readonly string[] } | null = null;

const strings = (v: unknown): string[] =>
  Array.isArray(v) ? v.map((s) => (typeof s === 'string' ? siteOf(s) : null)).filter((s) => s !== null) : [];

const read = (): IPaneSitesChoice => {
  try {
    const raw = window.localStorage.getItem(KEY);
    if (!raw) return EMPTY;
    const parsed = JSON.parse(raw) as Partial<Record<keyof IPaneSitesChoice, unknown>>;
    return { added: strings(parsed.added), removed: strings(parsed.removed) };
  } catch {
    return EMPTY;
  }
};

const choiceSnapshot = (): IPaneSitesChoice => (choice ??= read());

const save = (next: IPaneSitesChoice): void => {
  choice = next;
  try {
    if (next.added.length === 0 && next.removed.length === 0) window.localStorage.removeItem(KEY);
    else window.localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // Private mode or a full storage: the list holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

/** The sites in effect: the installation's without the removed, then the added. */
export const paneSitesSnapshot = (): readonly string[] => {
  const from = choiceSnapshot();
  if (sites?.from === from) return sites.list;
  const removed = new Set(from.removed);
  const list = [...new Set([...defaultSites().filter((s) => !removed.has(s)), ...from.added])];
  sites = { from, list };
  return list;
};

/** Adds a site as typed; false when it is not one. */
export const addPaneSite = (text: string): boolean => {
  const site = siteOf(text);
  if (!site) return false;
  const now = choiceSnapshot();
  const removed = now.removed.filter((s) => s !== site);
  const added = defaultSites().includes(site) || now.added.includes(site) ? now.added : [...now.added, site];
  save({ added, removed });
  return true;
};

/** Takes a site off the list: one the person added is forgotten, one of the installation is remembered as taken off. */
export const removePaneSite = (site: string): void => {
  const now = choiceSnapshot();
  const added = now.added.filter((s) => s !== site);
  const removed =
    defaultSites().includes(site) && !now.removed.includes(site) ? [...now.removed, site] : now.removed;
  save({ added, removed });
};

/** The installation's list again. */
export const resetPaneSites = (): void => save(EMPTY);

/** Whether the person changed the list in this browser. */
export const paneSitesChanged = (): boolean => {
  const now = choiceSnapshot();
  return now.added.length > 0 || now.removed.length > 0;
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  const onStorage = (event: StorageEvent) => {
    if (event.key !== KEY) return;
    choice = null;
    listener();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('storage', onStorage);
  };
};

export const usePaneSites = (): readonly string[] =>
  useSyncExternalStore(subscribe, paneSitesSnapshot, paneSitesSnapshot);
