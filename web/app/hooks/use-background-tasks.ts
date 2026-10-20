import { useSyncExternalStore } from 'react';
import type { IBackgroundTask } from '@/types/server/hl-background';

const EMPTY: IBackgroundTask[] = [];

let byTab: Record<string, IBackgroundTask[]> = {};
const listeners = new Set<() => void>();

const notify = () => listeners.forEach((listener) => listener());

/** Everything the status socket knows on (re)connect; tabs missing from it run nothing in the background. */
export const syncBackgroundTasks = (tabs: Record<string, IBackgroundTask[]>) => {
  byTab = tabs;
  notify();
};

export const setBackgroundTasks = (tabId: string, tasks: IBackgroundTask[]) => {
  const next = { ...byTab };
  if (tasks.length) next[tabId] = tasks;
  else delete next[tabId];
  byTab = next;
  notify();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

/** Background shells, monitors, agents and wakeups of one agent tab, pushed by the status socket. */
const useBackgroundTasks = (tabId: string | null | undefined): IBackgroundTask[] =>
  useSyncExternalStore(
    subscribe,
    () => (tabId ? (byTab[tabId] ?? EMPTY) : EMPTY),
    () => EMPTY,
  );

export const tabIdOfSession = (sessionName: string) => sessionName.match(/-(tab-[\w-]+)$/)?.[1] ?? null;

export default useBackgroundTasks;
