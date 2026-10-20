import { create } from 'zustand';

/**
 * Tabs whose Claude was started without the platform's hooks (a bare `claude`), from the status socket:
 * their status is read from the screen and the transcript, so it lags, and the composer says why.
 */
interface IUnhookedStore {
  tabs: Record<string, true>;
}

interface IHooksFields {
  hooksMissing?: boolean;
  cliState?: unknown;
}

export const useUnhookedStore = create<IUnhookedStore>(() => ({ tabs: {} }));

export const syncUnhooked = (tabs: Record<string, IHooksFields>): void =>
  useUnhookedStore.setState({
    tabs: Object.fromEntries(
      Object.entries(tabs).flatMap(([tabId, entry]) => (entry.hooksMissing ? [[tabId, true as const]] : [])),
    ),
  });

export const setUnhooked = (tabId: string, fields: IHooksFields): void =>
  useUnhookedStore.setState((state) => {
    const missing = fields.cliState !== null && fields.hooksMissing === true;
    if (missing === (state.tabs[tabId] === true)) return state;
    const tabs = { ...state.tabs };
    if (missing) tabs[tabId] = true;
    else delete tabs[tabId];
    return { tabs };
  });

export const useAgentUnhooked = (tabId: string | undefined): boolean =>
  useUnhookedStore((state) => (tabId ? state.tabs[tabId] === true : false));
