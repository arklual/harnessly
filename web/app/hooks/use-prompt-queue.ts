import { create } from 'zustand';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import type { IQueuedPrompt } from '@/types/server/hl-prompt-queue';

/**
 * The server's prompt queue as the page sees it: every tab's waiting messages, from the status socket. A
 * message added here shows at once with a local id; the server's update that follows replaces the tab's list.
 */
interface IPromptQueueStore {
  queues: Record<string, IQueuedPrompt[]>;
}

const EMPTY: IQueuedPrompt[] = [];

export const usePromptQueueStore = create<IPromptQueueStore>(() => ({ queues: {} }));

export const syncPromptQueues = (queues: Record<string, IQueuedPrompt[]>): void =>
  usePromptQueueStore.setState({ queues });

export const setPromptQueue = (tabId: string, items: IQueuedPrompt[]): void =>
  usePromptQueueStore.setState((state) => {
    const queues = { ...state.queues };
    if (items.length > 0) queues[tabId] = items;
    else delete queues[tabId];
    return { queues };
  });

export const usePromptQueue = (tabId: string | undefined): IQueuedPrompt[] =>
  usePromptQueueStore((state) => (tabId ? (state.queues[tabId] ?? EMPTY) : EMPTY));

type TQueueAction =
  { action: 'add'; text: string; images: string[] } | { action: 'remove' | 'take' | 'steer'; id: string };

const call = async (
  machineId: string | undefined,
  tabId: string,
  body: TQueueAction,
): Promise<{ item?: IQueuedPrompt }> => {
  const response = await appFetch(scopedAppPath('/api/platform/prompt-queue', machineId), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ tabId, ...body }),
  });
  const data = (await response.json().catch(() => ({}))) as { item?: IQueuedPrompt; error?: string };
  if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data;
};

/**
 * Queues a message: it shows above the composer before the server answers. `payload` finishes what takes time — the
 * uploads — and gives what the server stores. Gives the queued item the server made.
 */
export const enqueuePrompt = async (
  machineId: string | undefined,
  tabId: string,
  text: string,
  previews: string[],
  payload: () => Promise<{ text: string; images: string[] }>,
): Promise<IQueuedPrompt | null> => {
  const local: IQueuedPrompt = {
    id: `local-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`,
    text,
    images: previews,
    createdAt: Date.now(),
  };
  usePromptQueueStore.setState((state) => ({
    queues: { ...state.queues, [tabId]: [...(state.queues[tabId] ?? []), local] },
  }));
  try {
    const ready = await payload();
    return (
      (await call(machineId, tabId, { action: 'add', text: ready.text, images: ready.images })).item ?? null
    );
  } finally {
    // The server's update has the real item by now; a failed add leaves nothing behind.
    usePromptQueueStore.setState((state) => {
      const rest = (state.queues[tabId] ?? []).filter((item) => item.id !== local.id);
      const queues = { ...state.queues };
      if (rest.length > 0) queues[tabId] = rest;
      else delete queues[tabId];
      return { queues };
    });
  }
};

export const removeQueuedPrompt = (machineId: string | undefined, tabId: string, id: string) =>
  call(machineId, tabId, { action: 'remove', id });
export const takeQueuedPromptBack = async (
  machineId: string | undefined,
  tabId: string,
  id: string,
): Promise<IQueuedPrompt | null> => (await call(machineId, tabId, { action: 'take', id })).item ?? null;
export const steerQueuedPrompt = (machineId: string | undefined, tabId: string, id: string) =>
  call(machineId, tabId, { action: 'steer', id });
