import { useSyncExternalStore } from 'react';

/**
 * The machine the person picked for new tasks. It outlives a reload: the choice is kept in this browser, and every
 * place that starts a conversation (the composer, "Open in chat" of a design) asks it first.
 */
const KEY = 'hl:new-task-machine';
const listeners = new Set<() => void>();

const read = (): string | null => {
  try {
    return localStorage.getItem(KEY);
  } catch {
    return null;
  }
};

let chosen: string | null = typeof window === 'undefined' ? null : read();

export const newTaskMachine = (): string | null => chosen;

export const setNewTaskMachine = (machineId: string | null): void => {
  if (machineId === chosen) return;
  chosen = machineId;
  try {
    if (machineId) localStorage.setItem(KEY, machineId);
    else localStorage.removeItem(KEY);
  } catch {
    // Storage is blocked: the pick lasts until the page is closed.
  }
  listeners.forEach((notify) => notify());
};

const subscribe = (notify: () => void) => {
  listeners.add(notify);
  // Another window of the same browser picked a machine.
  const onStorage = (event: StorageEvent) => {
    if (event.key !== KEY) return;
    chosen = event.newValue;
    notify();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(notify);
    window.removeEventListener('storage', onStorage);
  };
};

export const useNewTaskMachine = (): string | null =>
  useSyncExternalStore(
    subscribe,
    () => chosen,
    () => null,
  );

/**
 * Where a conversation about a design starts: the machine picked for new tasks when it holds a copy of the project
 * (the library is synced between machines), otherwise the machine the copy on screen came from.
 */
export const designChatMachine = (project: {
  machineId: string | undefined;
  machineIds?: readonly (string | undefined)[];
}): string | undefined => {
  const picked = newTaskMachine();
  if (picked && project.machineIds?.includes(picked)) return picked;
  return project.machineId;
};

/** The latest conversation about a design on the machine its chats start on; null when there is none yet. */
export const latestDesignChat = <
  T extends { machineId: string | undefined; agent: { tabId: string } },
>(project: {
  machineId: string | undefined;
  machineIds?: readonly (string | undefined)[];
  chats: readonly T[];
}): T | null => {
  const machine = designChatMachine(project);
  return project.chats.find((chat) => chat.machineId === machine) ?? null;
};
