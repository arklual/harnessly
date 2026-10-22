import { create } from 'zustand';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import { createHandoffTracker, LOCAL_ID_PREFIX, type THandoffEvent } from '@/lib/hl-queue-handoff';
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

const handoffTracker = createHandoffTracker();
const handoffListeners = new Map<string, Set<(event: THandoffEvent) => void>>();

const emitHandoff = (events: THandoffEvent[]) => {
  for (const event of events) handoffListeners.get(event.tabId)?.forEach((listener) => listener(event));
};

/**
 * Calls back when a message this page queued leaves the queue for the agent (and again if the hand-over fell
 * through and the message came back), so the timeline can show it as a pending row instead of letting it vanish.
 */
export const onQueueHandoff = (tabId: string, listener: (event: THandoffEvent) => void): (() => void) => {
  const listeners = handoffListeners.get(tabId) ?? new Set();
  listeners.add(listener);
  handoffListeners.set(tabId, listeners);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) handoffListeners.delete(tabId);
  };
};

usePromptQueueStore.subscribe((state, previous) => {
  if (state.queues === previous.queues) return;
  for (const tabId of new Set([...Object.keys(state.queues), ...Object.keys(previous.queues)])) {
    if (state.queues[tabId] !== previous.queues[tabId])
      emitHandoff(
        handoffTracker.observe(tabId, previous.queues[tabId] ?? EMPTY, state.queues[tabId] ?? EMPTY),
      );
  }
});

export const usePromptQueue = (tabId: string | undefined): IQueuedPrompt[] =>
  usePromptQueueStore((state) => (tabId ? (state.queues[tabId] ?? EMPTY) : EMPTY));

type TQueueAction =
  | { action: 'add'; text: string; images: string[]; now?: boolean }
  | { action: 'remove' | 'take' | 'steer'; id: string };

/** What the runtime did with a new message: typed it into the running turn, or kept it for the end of the turn. */
export type TQueueDelivery = 'steered' | 'queued';

export interface IEnqueued {
  item: IQueuedPrompt | null;
  delivery: TQueueDelivery | null;
}

/** The runtime's words for a message that is no longer queued (the agent took it meanwhile), in both of its languages. */
const GONE_FROM_QUEUE = ['The message is no longer in the queue', 'Сообщения уже нет в очереди'];

/** The agent took the message out of the queue before a steer reached it: for ⌘↵ that is a success. */
export const isGoneFromQueue = (error: unknown): boolean =>
  error instanceof Error && GONE_FROM_QUEUE.includes(error.message);

const call = async (
  machineId: string | undefined,
  tabId: string,
  body: TQueueAction,
): Promise<{ item?: IQueuedPrompt; delivery?: TQueueDelivery }> => {
  const response = await appFetch(scopedAppPath('/api/platform/prompt-queue', machineId), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ tabId, ...body }),
  });
  const data = (await response.json().catch(() => ({}))) as {
    item?: IQueuedPrompt;
    delivery?: TQueueDelivery;
    error?: string;
  };
  if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data;
};

export interface IEnqueueOptions {
  /**
   * Whether the message gets a pending row in the timeline once the runtime takes it out of the queue. A launch's
   * first messages already show in the launch preview, so they say `false`.
   */
  handoff?: boolean;
  /** Into the agent's running turn now. Without it the message waits in the queue for the turn to end. */
  now?: boolean;
}

/**
 * Queues a message: it shows above the composer before the server answers. `payload` finishes what takes time — the
 * uploads — and gives what the server stores. Gives the queued item the server made and what became of it: with `now`
 * the runtime types the message into the running turn when it can, without it the message waits for the turn's end.
 */
export const enqueuePrompt = async (
  machineId: string | undefined,
  tabId: string,
  text: string,
  previews: string[],
  payload: () => Promise<{ text: string; images: string[] }>,
  { handoff = true, now = false }: IEnqueueOptions = {},
): Promise<IEnqueued> => {
  const local: IQueuedPrompt = {
    id: `${LOCAL_ID_PREFIX}${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`,
    text,
    images: previews,
    createdAt: Date.now(),
  };
  usePromptQueueStore.setState((state) => ({
    queues: { ...state.queues, [tabId]: [...(state.queues[tabId] ?? []), local] },
  }));
  try {
    const ready = await payload();
    if (handoff) handoffTracker.own(tabId, ready.text);
    const answer = await call(machineId, tabId, {
      action: 'add',
      text: ready.text,
      images: ready.images,
      ...(now ? { now: true } : {}),
    }).catch((error: unknown) => {
      if (handoff) handoffTracker.disown(tabId, ready.text);
      throw error;
    });
    if (answer.item) emitHandoff(handoffTracker.answered(tabId, answer.item, answer.delivery ?? null));
    return { item: answer.item ?? null, delivery: answer.delivery ?? null };
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

/** A removal or take-back is the person's own: the message leaving the queue then is not a hand-over to the agent. */
const dismissing = async <T>(tabId: string, id: string, task: () => Promise<T>): Promise<T> => {
  handoffTracker.dismiss(tabId, id);
  try {
    return await task();
  } catch (error) {
    handoffTracker.undismiss(tabId, id);
    throw error;
  }
};

export const removeQueuedPrompt = (machineId: string | undefined, tabId: string, id: string) =>
  dismissing(tabId, id, () => call(machineId, tabId, { action: 'remove', id }));
export const takeQueuedPromptBack = async (
  machineId: string | undefined,
  tabId: string,
  id: string,
): Promise<IQueuedPrompt | null> =>
  (await dismissing(tabId, id, () => call(machineId, tabId, { action: 'take', id }))).item ?? null;
export const steerQueuedPrompt = (machineId: string | undefined, tabId: string, id: string) =>
  call(machineId, tabId, { action: 'steer', id });
