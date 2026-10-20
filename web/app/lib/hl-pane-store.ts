import { useCallback, useSyncExternalStore } from 'react';
import {
  OPEN_PANE_TAB_EVENT,
  loadPaneTabs,
  newPaneState,
  openTab,
  savePaneTabs,
  type IPaneState,
  type IPaneTabRequest,
  type TPaneTabKind,
} from '@/lib/hl-pane-tabs';

type TPaneStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

/** Tabs of the right panel by conversation; loaded from storage on first use, saved on every change. */
export const createPaneStore = (storage: () => TPaneStorage | null) => {
  const states = new Map<string, IPaneState>();
  const listeners = new Set<() => void>();
  const get = (key: string): IPaneState => {
    let state = states.get(key);
    if (!state) {
      state = loadPaneTabs(storage(), key);
      states.set(key, state);
    }
    return state;
  };
  return {
    get,
    update: (key: string, change: (state: IPaneState) => IPaneState): void => {
      const previous = get(key);
      const next = change(previous);
      if (next === previous) return;
      states.set(key, next);
      savePaneTabs(storage(), key, next);
      for (const listener of listeners) listener();
    },
    subscribe: (listener: () => void): (() => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
};

const browserStorage = (): TPaneStorage | null => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

const store = createPaneStore(browserStorage);
const SERVER_STATE = newPaneState();

export { OPEN_PANE_TAB_EVENT };

export const updatePane = store.update;

export const applyPaneRequest = (key: string, { kind, target = null, title = null }: IPaneTabRequest): void =>
  store.update(key, (state) => openTab(state, kind, target, { title }));

/** The tabs of a conversation's panel as they are now, for the agent's `panel status`. */
export const readPane = (key: string): IPaneState => store.get(key);

export const usePane = (
  key: string | null,
): [IPaneState, (change: (state: IPaneState) => IPaneState) => void] => {
  const state = useSyncExternalStore(
    store.subscribe,
    () => (key ? store.get(key) : SERVER_STATE),
    () => SERVER_STATE,
  );
  const update = useCallback(
    (change: (state: IPaneState) => IPaneState) => {
      if (key) store.update(key, change);
    },
    [key],
  );
  return [state, update];
};

/** The conversation a panel belongs to: the agent's tab, or the chat (`chat:new` until its first message names it). */
export const paneKeyOf = (
  mode: 'agent' | 'chat',
  tabId: string | null,
  chatId: string | null,
): string | null => (mode === 'agent' ? tabId : `chat:${chatId ?? 'new'}`);

/**
 * Opens (or brings forward) a tab in the right panel of the conversation on screen and shows the panel. The entry point
 * for the side chat, the gallery and agent commands.
 */
export const openPaneTab = (
  kind: TPaneTabKind,
  target: string | null = null,
  title: string | null = null,
): void => {
  window.dispatchEvent(
    new CustomEvent<IPaneTabRequest>(OPEN_PANE_TAB_EVENT, { detail: { kind, target, title } }),
  );
};
