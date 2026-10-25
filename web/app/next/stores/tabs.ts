// The tabs over the main window: every place the main area can show — a chat, Designly or one of its
// designs, Settings, Apps — is a tab, the way a browser keeps its pages. The address is the active tab's place: going
// anywhere moves the active tab there, a new tab is opened on purpose (⌘-click, the middle button, «+»). Each tab keeps
// what it is cheap to keep: its place, the scroll of its page and the little a view asks to keep (useTabState). The
// list is this browser's: localStorage, read once, written a moment after a change; private mode only forgets it.
import { useCallback, useEffect, useState, useSyncExternalStore } from 'react';
import { placeKeyOf } from '@/next/routes/routes';

export interface ITab {
  id: string;
  /** The place inside the app, as routePath() writes it: `/`, `/c/tab-1`, `/designly/afisha`. */
  path: string;
  /** The last name the place had (a chat's title, a design's), to draw before the lists come. */
  title: string | null;
  /** The scroll of the page's scrollers, by their selector, as the tab was left. */
  scroll: Readonly<Record<string, number>> | null;
  /** What views asked to keep (useTabState), by their key. */
  view: Readonly<Record<string, unknown>> | null;
}

export interface ITabsState {
  tabs: readonly ITab[];
  active: string;
}

export const HOME_PATH = '/';
/** The localStorage keys: the list, and whether the strip is on (Settings → General). */
export const TABS_KEY = 'hx-tabs';
export const TOP_TABS_KEY = 'hx-top-tabs';
const VERSION = 1;
/** More than this is not a working set any more; the oldest are left out of what is kept. */
export const MAX_TABS = 40;
const PERSIST_MS = 300;

let seq = 0;
/** A tab's id: unique on the page and across reloads (the kept tabs keep theirs). */
export const newTabId = (): string => `t${Date.now().toString(36)}${(++seq).toString(36)}`;

export const createTab = (path: string, id: string = newTabId()): ITab => ({
  id,
  path,
  title: null,
  scroll: null,
  view: null,
});

export const activeTab = (state: ITabsState): ITab =>
  state.tabs.find((tab) => tab.id === state.active) ?? state.tabs[0];

const homeState = (path: string = HOME_PATH): ITabsState => {
  const tab = createTab(path);
  return { tabs: [tab], active: tab.id };
};

/**
 * One tab per place: of the tabs with the same place key (placeKeyOf) the first stays. When the active tab was one of
 * those that went, its kept twin is active.
 */
export const dedupeTabs = (state: ITabsState): ITabsState => {
  const kept = new Map<string, ITab>();
  let active = state.active;
  for (const tab of state.tabs) {
    const key = placeKeyOf(tab.path);
    const first = kept.get(key);
    if (!first) kept.set(key, tab);
    else if (tab.id === state.active) active = first.id;
  }
  if (kept.size === state.tabs.length) return state;
  return { tabs: [...kept.values()], active };
};

const isTab = (raw: unknown): raw is ITab => {
  if (typeof raw !== 'object' || raw === null) return false;
  const tab = raw as Record<string, unknown>;
  return typeof tab.id === 'string' && typeof tab.path === 'string' && tab.path.startsWith('/');
};

/** The kept list as it was written, or null when there is none or it is not ours. */
export const parseSaved = (raw: string | null): ITabsState | null => {
  if (!raw) return null;
  try {
    const data = JSON.parse(raw) as { v?: unknown; tabs?: unknown; active?: unknown };
    if (data.v !== VERSION || !Array.isArray(data.tabs)) return null;
    const tabs = data.tabs.filter(isTab).map((tab) => ({
      id: tab.id,
      path: tab.path,
      title: typeof tab.title === 'string' ? tab.title : null,
      scroll: typeof tab.scroll === 'object' && tab.scroll !== null ? tab.scroll : null,
      view: typeof tab.view === 'object' && tab.view !== null ? tab.view : null,
    }));
    if (tabs.length === 0) return null;
    const active = tabs.some((tab) => tab.id === data.active) ? String(data.active) : tabs[0].id;
    return dedupeTabs({ tabs, active });
  } catch {
    return null;
  }
};

export const serialize = (state: ITabsState): string =>
  JSON.stringify({ v: VERSION, tabs: state.tabs.slice(-MAX_TABS), active: state.active });

export interface IInit {
  state: ITabsState;
  /** Where to go: the page came in on a bare `/` and the person's active tab is somewhere else. */
  go: string | null;
}

/**
 * The tabs for a page that opens on `path`. The kept list comes back; an address of its own (a link, a reload)
 * is the tab that has it, or a new one after the active tab; a bare `/` (the app opened anew) returns to where the
 * person was, unless `restore` is off (an address of the current interface is on its way to a chat).
 */
export const initTabs = (saved: ITabsState | null, path: string, restore = true): IInit => {
  if (!saved) return { state: homeState(path), go: null };
  const current = activeTab(saved);
  const key = placeKeyOf(path);
  if (placeKeyOf(current.path) === key) return { state: patchTab(saved, current.id, { path }), go: null };
  if (path === HOME_PATH && restore) return { state: saved, go: current.path };
  const same = saved.tabs.find((tab) => placeKeyOf(tab.path) === key);
  if (same) return { state: { ...patchTab(saved, same.id, { path }), active: same.id }, go: null };
  return { state: openTab(saved, path), go: null };
};

export interface IOpenOptions {
  /** A tab opened by ⌘-click or the middle button stays behind the active one, as a browser opens it. */
  background?: boolean;
  /** Right after the active tab (a link opened from it), or at the end («+», ⌘T). */
  at?: 'next' | 'end';
  id?: string;
}

export const openTab = (state: ITabsState, path: string, options: IOpenOptions = {}): ITabsState => {
  const tab = createTab(path, options.id);
  const at = options.at ?? 'next';
  const index = at === 'end' ? state.tabs.length : state.tabs.findIndex((x) => x.id === state.active) + 1;
  const tabs = [...state.tabs.slice(0, index), tab, ...state.tabs.slice(index)];
  return { tabs, active: options.background ? state.active : tab.id };
};

/** What opening a place from the menus does (see planOpen). */
export const OPEN = { focus: 'focus', create: 'create', keep: 'keep' } as const;
export type TOpen = (typeof OPEN)[keyof typeof OPEN];

export interface IOpenPlan {
  state: ITabsState;
  /** focus: a tab that had the place was activated; create: a new tab after the active one; keep: nothing changed. */
  did: TOpen;
}

/**
 * Opening a place from the sidebar, a search result or a notification never replaces what a tab shows: the tab that
 * already has the place is switched to, otherwise a new tab opens right after the active one. `background` (⌘-click)
 * leaves the active tab active in both cases.
 */
export const planOpen = (
  state: ITabsState,
  path: string,
  options: { background?: boolean; id?: string } = {},
): IOpenPlan => {
  const key = placeKeyOf(path);
  const same = state.tabs.find((tab) => placeKeyOf(tab.path) === key);
  if (same) {
    // The same place in another state (a board, a section) shows what was asked.
    const shown = patchTab(state, same.id, { path });
    if (options.background || same.id === state.active) return { state: shown, did: OPEN.keep };
    return { state: activateTab(shown, same.id), did: OPEN.focus };
  }
  return {
    state: openTab(state, path, { background: options.background, id: options.id }),
    did: OPEN.create,
  };
};

/** The active tab goes to `path` (any navigation that is not a new tab); a new place starts at its top. */
export const navigateTab = (state: ITabsState, path: string): ITabsState => {
  const current = activeTab(state);
  if (current.path === path) return state;
  const key = placeKeyOf(path);
  // The same place in another state of its own: the tab stays, with its title and its scroll.
  if (placeKeyOf(current.path) === key) return patchTab(state, current.id, { path });
  // A place another tab has: that tab is switched to, the active one stays as it was.
  const other = state.tabs.find((tab) => tab.id !== current.id && placeKeyOf(tab.path) === key);
  if (other) return activateTab(patchTab(state, other.id, { path }), other.id);
  return patchTab(state, current.id, { path, title: null, scroll: null });
};

export const activateTab = (state: ITabsState, id: string): ITabsState =>
  state.active === id || !state.tabs.some((tab) => tab.id === id) ? state : { ...state, active: id };

/**
 * Closes a tab. The active one hands over to its right neighbour, or the left one at the end, as a browser does;
 * the last one leaves a tab on the start page.
 */
export const closeTab = (state: ITabsState, id: string): ITabsState => {
  const index = state.tabs.findIndex((tab) => tab.id === id);
  if (index < 0) return state;
  const tabs = state.tabs.filter((tab) => tab.id !== id);
  if (tabs.length === 0) return homeState();
  if (state.active !== id) return { ...state, tabs };
  const next = tabs[Math.min(index, tabs.length - 1)];
  return { tabs, active: next.id };
};

/** Every tab but this one goes. */
export const closeOtherTabs = (state: ITabsState, id: string): ITabsState => {
  const kept = state.tabs.find((tab) => tab.id === id);
  return kept ? { tabs: [kept], active: kept.id } : state;
};

/** Every tab to the right of this one goes. */
export const closeRightTabs = (state: ITabsState, id: string): ITabsState => {
  const at = state.tabs.findIndex((tab) => tab.id === id);
  if (at < 0 || at === state.tabs.length - 1) return state;
  const tabs = state.tabs.slice(0, at + 1);
  return { tabs, active: tabs.some((tab) => tab.id === state.active) ? state.active : id };
};

export const moveTab = (state: ITabsState, id: string, index: number): ITabsState => {
  const from = state.tabs.findIndex((tab) => tab.id === id);
  const to = Math.max(0, Math.min(state.tabs.length - 1, index));
  if (from < 0 || from === to) return state;
  const tabs = [...state.tabs];
  const [tab] = tabs.splice(from, 1);
  tabs.splice(to, 0, tab);
  return { ...state, tabs };
};

export const patchTab = (state: ITabsState, id: string, patch: Partial<Omit<ITab, 'id'>>): ITabsState => {
  const at = state.tabs.findIndex((tab) => tab.id === id);
  if (at < 0) return state;
  const tab = state.tabs[at];
  const changed = (Object.keys(patch) as (keyof typeof patch)[]).some((key) => tab[key] !== patch[key]);
  if (!changed) return state;
  const tabs = [...state.tabs];
  tabs[at] = { ...tab, ...patch };
  return { ...state, tabs };
};

/** The tab `step` away from the active one, round the ends (Ctrl+Tab, Ctrl+Shift+Tab). */
export const stepTab = (state: ITabsState, step: number): ITabsState => {
  const at = state.tabs.findIndex((tab) => tab.id === state.active);
  const n = state.tabs.length;
  return activateTab(state, state.tabs[(((at + step) % n) + n) % n].id);
};

/** ⌘1…⌘8 the tab at that place, ⌘9 the last one, as in a browser. */
export const nthTab = (state: ITabsState, digit: number): ITabsState => {
  const tab = digit === 9 ? state.tabs[state.tabs.length - 1] : state.tabs[digit - 1];
  return tab ? activateTab(state, tab.id) : state;
};

// ── The store ──

const storage = {
  read(key: string): string | null {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  write(key: string, value: string) {
    try {
      localStorage.setItem(key, value);
    } catch {
      // Private mode or a full storage: the tabs work for this page and are forgotten on reload.
    }
  },
};

const listeners = new Set<() => void>();
let state: ITabsState | null = null;
let started = false;
let persistTimer: ReturnType<typeof setTimeout> | null = null;

const persistNow = () => {
  persistTimer = null;
  if (state) storage.write(TABS_KEY, serialize(state));
};

const commit = (next: ITabsState) => {
  if (next === state) return;
  state = next;
  if (persistTimer === null) persistTimer = setTimeout(persistNow, PERSIST_MS);
  for (const listener of [...listeners]) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/** The tabs now; before the shell has started them, the one the address is. */
export const tabsSnapshot = (): ITabsState => (state ??= homeState());

/** Starts the tabs for the page's address, once; tells where to go when the person is returned to their tab. */
export const startTabs = (path: string, restore: boolean): string | null => {
  if (started) return null;
  started = true;
  const init = initTabs(parseSaved(storage.read(TABS_KEY)), path, restore);
  state = init.state;
  for (const listener of [...listeners]) listener();
  return init.go;
};

export const tabs = {
  open: (path: string, options?: IOpenOptions) => commit(openTab(tabsSnapshot(), path, options)),
  navigate: (path: string) => commit(navigateTab(tabsSnapshot(), path)),
  activate: (id: string) => commit(activateTab(tabsSnapshot(), id)),
  close: (id: string) => commit(closeTab(tabsSnapshot(), id)),
  closeOthers: (id: string) => commit(closeOtherTabs(tabsSnapshot(), id)),
  closeRight: (id: string) => commit(closeRightTabs(tabsSnapshot(), id)),
  move: (id: string, index: number) => commit(moveTab(tabsSnapshot(), id, index)),
  patch: (id: string, patch: Partial<Omit<ITab, 'id'>>) => commit(patchTab(tabsSnapshot(), id, patch)),
  step: (step: number) => commit(stepTab(tabsSnapshot(), step)),
  nth: (digit: number) => commit(nthTab(tabsSnapshot(), digit)),
  /** Writes the list at once (the page is going away). */
  flush: persistNow,
};

/** Forgets the page's tabs (tests). */
export const resetTabs = () => {
  if (persistTimer !== null) clearTimeout(persistTimer);
  persistTimer = null;
  state = null;
  started = false;
};

export const useTabs = (): ITabsState => useSyncExternalStore(subscribe, tabsSnapshot, tabsSnapshot);

/**
 * A piece of a view's state that belongs to the tab: switching tabs gives each its own, a reload brings it back.
 * The value must be JSON. `key` names the place it is of (a design's id), so another place in the same tab starts
 * from `initial`.
 */
export const useTabState = <T>(key: string, initial: T): [T, (next: T | ((current: T) => T)) => void] => {
  const snapshot = useTabs();
  // The first value only, as with useState: a new [] each render would be a new value each render.
  const [first] = useState(initial);
  const tab = activeTab(snapshot);
  const value = (tab.view?.[key] as T | undefined) ?? first;
  const set = useCallback(
    (next: T | ((current: T) => T)) => {
      const now = activeTab(tabsSnapshot());
      const current = (now.view?.[key] as T | undefined) ?? first;
      const value = typeof next === 'function' ? (next as (current: T) => T)(current) : next;
      tabs.patch(now.id, { view: { ...now.view, [key]: value } });
    },
    [key, first],
  );
  return [value, set];
};

/**
 * Names the active tab while it shows `path` (a design's title, once its library has come). The tab may take the path
 * after the title is known (the shell moves it in an effect of its own, which runs after this one): the tab that is due
 * a name is watched, not read once.
 */
export const useTabTitle = (path: string, title: string | null) => {
  const dueOf = () => {
    const current = activeTab(tabsSnapshot());
    return title && current.path === path && current.title !== title ? current.id : null;
  };
  const due = useSyncExternalStore(subscribe, dueOf, dueOf);
  useEffect(() => {
    if (due !== null && title) tabs.patch(due, { title });
  }, [due, title]);
};

// ── Whether the strip is on (Settings → General; this browser's, as the theme is) ──

const prefListeners = new Set<() => void>();
let topTabs: boolean | null = null;

const topTabsSnapshot = (): boolean => (topTabs ??= storage.read(TOP_TABS_KEY) !== '0');

export const setTopTabs = (on: boolean) => {
  topTabs = on;
  storage.write(TOP_TABS_KEY, on ? '1' : '0');
  for (const listener of [...prefListeners]) listener();
};

export const useTopTabs = (): boolean =>
  useSyncExternalStore(
    (listener) => {
      prefListeners.add(listener);
      return () => {
        prefListeners.delete(listener);
      };
    },
    topTabsSnapshot,
    () => true,
  );
