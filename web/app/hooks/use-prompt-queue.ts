import { create } from 'zustand';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import { createHandoffTracker, LOCAL_ID_PREFIX, type THandoffEvent } from '@/lib/hl-queue-handoff';
import type { IQueuedPrompt } from '@/types/server/hl-prompt-queue';

/**
 * The server's prompt queue as the page sees it: every tab's messages, from the status socket, so they outlive a
 * reload and show in every tab of the browser. A message added here shows at once with a local id until the runtime
 * has it: the server's item with the same client id takes its place.
 */
interface IPromptQueueStore {
  /** Every tab's queue as the runtime last told it. */
  server: Record<string, IQueuedPrompt[]>;
  /** Messages this page is adding, until the runtime answers. */
  local: Record<string, IQueuedPrompt[]>;
  /** What the composer shows: the runtime's queue, then this page's messages it does not have yet. */
  queues: Record<string, IQueuedPrompt[]>;
}

const EMPTY: IQueuedPrompt[] = [];

export const usePromptQueueStore = create<IPromptQueueStore>(() => ({ server: {}, local: {}, queues: {} }));

/**
 * The runtime's queue with this page's messages it does not have yet. A runtime that predates client ids answers
 * without them; its item then stands for a local message with the same text.
 */
export const mergeQueue = (
  server: readonly IQueuedPrompt[],
  local: readonly IQueuedPrompt[],
): readonly IQueuedPrompt[] => {
  if (local.length === 0) return server;
  const keyed = new Set(server.flatMap((item) => (item.clientId ? [item.clientId] : [])));
  const unkeyed = server.filter((item) => !item.clientId).map((item) => item.text);
  const pending = local.filter((item) => {
    if (item.clientId && keyed.has(item.clientId)) return false;
    const at = unkeyed.indexOf(item.text);
    if (at === -1) return true;
    unkeyed.splice(at, 1);
    return false;
  });
  if (pending.length === 0) return server;
  return server.length === 0 && pending.length === local.length ? local : [...server, ...pending];
};

const withTab = (all: Record<string, IQueuedPrompt[]>, tabId: string, items: readonly IQueuedPrompt[]) => {
  const next = { ...all };
  if (items.length > 0) next[tabId] = items as IQueuedPrompt[];
  else delete next[tabId];
  return next;
};

/** One tab's lists changed: only its shown queue is built again, the others keep their identity. */
const changeTab = (
  state: IPromptQueueStore,
  tabId: string,
  change: Partial<Pick<IPromptQueueStore, 'server' | 'local'>>,
): Partial<IPromptQueueStore> => {
  const server = change.server ?? state.server;
  const local = change.local ?? state.local;
  return {
    server,
    local,
    queues: withTab(state.queues, tabId, mergeQueue(server[tabId] ?? EMPTY, local[tabId] ?? EMPTY)),
  };
};

export const syncPromptQueues = (queues: Record<string, IQueuedPrompt[]>): void =>
  usePromptQueueStore.setState((state) => {
    let shown: Record<string, IQueuedPrompt[]> = {};
    for (const tabId of new Set([...Object.keys(queues), ...Object.keys(state.local)]))
      shown = withTab(shown, tabId, mergeQueue(queues[tabId] ?? EMPTY, state.local[tabId] ?? EMPTY));
    return { server: queues, queues: shown };
  });

export const setPromptQueue = (tabId: string, items: IQueuedPrompt[]): void =>
  usePromptQueueStore.setState((state) =>
    changeTab(state, tabId, { server: withTab(state.server, tabId, items) }),
  );

const setLocal = (tabId: string, change: (items: IQueuedPrompt[]) => IQueuedPrompt[]) =>
  usePromptQueueStore.setState((state) =>
    changeTab(state, tabId, { local: withTab(state.local, tabId, change(state.local[tabId] ?? EMPTY)) }),
  );

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
  | { action: 'add'; text: string; images: string[]; clientId: string; now?: boolean }
  | { action: 'remove' | 'take' | 'steer' | 'retry'; id: string };

/** What the runtime did with a new message: typed it into the running turn, or kept it for the end of the turn. */
export type TQueueDelivery = 'steered' | 'queued';

export interface IEnqueued {
  item: IQueuedPrompt | null;
  delivery: TQueueDelivery | null;
}

/** A refusal of the runtime's queue: its words for the person, and its code for the page when it gives one. */
export class QueueRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string | null,
  ) {
    super(message);
  }
}

const NOT_QUEUED = 'not-queued';

/**
 * A runtime that predates refusal codes says it only in words, in both of its languages; they are looked at only when
 * the answer has no code.
 */
const GONE_FROM_QUEUE = ['The message is no longer in the queue', 'Сообщения уже нет в очереди'];

/** The agent took the message out of the queue before a steer reached it: for ⌘↵ that is a success. */
export const isGoneFromQueue = (error: unknown): boolean => {
  if (error instanceof QueueRequestError && error.code !== null) return error.code === NOT_QUEUED;
  return error instanceof Error && GONE_FROM_QUEUE.includes(error.message);
};

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
    code?: string;
  };
  if (!response.ok)
    throw new QueueRequestError(
      data.error ?? `HTTP ${response.status}`,
      response.status,
      typeof data.code === 'string' ? data.code : null,
    );
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
  const key = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
  // The client id goes with every try of this one message: a repeat the runtime already took is not queued twice.
  const local: IQueuedPrompt = {
    id: `${LOCAL_ID_PREFIX}${key}`,
    clientId: `c-${key}`,
    text,
    images: previews,
    createdAt: Date.now(),
  };
  setLocal(tabId, (items) => [...items, local]);
  try {
    const ready = await payload();
    if (handoff) handoffTracker.own(tabId, ready.text);
    const answer = await call(machineId, tabId, {
      action: 'add',
      text: ready.text,
      images: ready.images,
      clientId: local.clientId as string,
      ...(now ? { now: true } : {}),
    }).catch((error: unknown) => {
      if (handoff) handoffTracker.disown(tabId, ready.text);
      throw error;
    });
    if (answer.item) emitHandoff(handoffTracker.answered(tabId, answer.item, answer.delivery ?? null));
    return { item: answer.item ?? null, delivery: answer.delivery ?? null };
  } finally {
    // The server's update has the real item by now; a failed add leaves nothing behind.
    setLocal(tabId, (items) => items.filter((item) => item.id !== local.id));
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
/** Asks again for a message the runtime gave up on; it waits in its place and goes out when the agent can take it. */
export const retryQueuedPrompt = (machineId: string | undefined, tabId: string, id: string) =>
  call(machineId, tabId, { action: 'retry', id });

export const isFailedPrompt = (item: IQueuedPrompt): boolean => item.status === 'failed';
