import { create } from 'zustand';
import { tabLaunchOf, type ITabLaunch } from '@/lib/hl-launch-phase';

/**
 * Which tabs have an agent launch under way, from the status socket: kept apart from the tab store because
 * a launch is not a state of the agent but of the moment — the composer waits on it, the queue note shows it.
 */
interface IAgentLaunchStore {
  launches: Record<string, ITabLaunch>;
  /**
   * Tabs the status socket has told about. A runtime that has just started syncs before it has scanned its tabs, so a
   * sync that lacks a tab says nothing about it: a launch may be under way there unseen.
   */
  known: Record<string, true>;
}

interface ILaunchFields {
  launchingSince?: number | null;
  launchFailed?: boolean;
}

export const useAgentLaunchStore = create<IAgentLaunchStore>(() => ({ launches: {}, known: {} }));

export const syncAgentLaunches = (tabs: Record<string, ILaunchFields>): void =>
  useAgentLaunchStore.setState({
    known: Object.fromEntries(Object.keys(tabs).map((tabId) => [tabId, true as const])),
    launches: Object.fromEntries(
      Object.entries(tabs).flatMap(([tabId, entry]) => {
        const launch = tabLaunchOf(entry);
        return launch ? [[tabId, launch]] : [];
      }),
    ),
  });

export const setAgentLaunch = (tabId: string, fields: ILaunchFields & { cliState?: unknown }): void =>
  useAgentLaunchStore.setState((state) => {
    const launch = tabLaunchOf(fields);
    const removed = fields.cliState === null;
    const current = state.launches[tabId];
    const known = removed ? state.known[tabId] === undefined : state.known[tabId] === true;
    if (!launch && !current && known) return state;
    if (launch && current && launch.since === current.since && launch.failed === current.failed && known)
      return state;
    const launches = { ...state.launches };
    if (launch) launches[tabId] = launch;
    else delete launches[tabId];
    const knownTabs = { ...state.known };
    if (removed) delete knownTabs[tabId];
    else knownTabs[tabId] = true;
    return { launches, known: knownTabs };
  });

export const useTabLaunch = (tabId: string | undefined): ITabLaunch | null =>
  useAgentLaunchStore((state) => (tabId ? (state.launches[tabId] ?? null) : null));

/** The status socket has told this tab's launch state: until then a message may not be typed into its pane. */
export const useLaunchKnown = (tabId: string | undefined): boolean =>
  useAgentLaunchStore((state) => (tabId ? state.known[tabId] === true : false));
