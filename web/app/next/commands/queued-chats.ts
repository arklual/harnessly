// A new chat started while none of the person's machines answers: its first message waits in the hub
// (<platform api>/offline-chats), shows as a chat that waits for its machine on every device of the person's, and the
// hub makes the chat on the machine, with that message, once the machine is back, with every tab closed too. The page
// asks the hub what waits while anything does, and opens the chat the machine made.
//
// A runtime alone has no such queue (404): nothing waits there, and a new chat goes to the machine as before.

import { uuidv7 } from '@/next/core/ids';
import { getPage, sendBody, sendPage } from '@/next/core/net/http';
import { gatewayPath } from '@/next/core/net/machine';
import { createStore, useStore } from '@/next/core/store';
import type { NewConversation } from '@/next/commands/index';
import type { CommandBodies, ConvId, MachineId, Ms } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;
/** How often the page asks while a chat waits: the hub itself tries its machine every 5 s. */
export const QUEUED_POLL_MS = 5_000;

export const QUEUED_STATE = {
  queued: 'queued',
  sending: 'sending',
  delivered: 'delivered',
  failed: 'failed',
} as const;
export type QueuedState = (typeof QUEUED_STATE)[keyof typeof QUEUED_STATE];
const STATES: readonly string[] = Object.values(QUEUED_STATE);

export interface QueuedChat {
  id: string;
  machineId: MachineId;
  clientId: string;
  text: string;
  state: QueuedState;
  error: string | null;
  /** The chat the machine made of it; null until then. */
  convId: ConvId | null;
  createdAt: Ms;
}

/** The person's chats that wait for a machine; null until the hub said, [] where it has no queue. */
export const queuedChatsStore = createStore<readonly QueuedChat[] | null>(null);
export const useQueuedChats = () => useStore(queuedChatsStore);

/** A waiting chat in the sidebar and in the address: `queued:<item id>`, never a machine's `tab-…`. */
const PREFIX = 'queued:';
export const queuedChatId = (id: string): string => `${PREFIX}${id}`;
export const queuedItemOf = (chatId: string): string | null =>
  chatId.startsWith(PREFIX) ? chatId.slice(PREFIX.length) : null;

const readItem = (v: unknown): QueuedChat => {
  if (typeof v !== 'object' || v === null) throw new Error('item');
  const o = v as Record<string, unknown>;
  if (
    typeof o.id !== 'string' ||
    typeof o.machineId !== 'string' ||
    typeof o.clientId !== 'string' ||
    typeof o.text !== 'string' ||
    typeof o.state !== 'string' ||
    !STATES.includes(o.state) ||
    typeof o.createdAt !== 'number'
  )
    throw new Error('item');
  return {
    id: o.id,
    machineId: o.machineId,
    clientId: o.clientId,
    text: o.text,
    state: o.state as QueuedState,
    error: typeof o.error === 'string' ? o.error : null,
    convId: typeof o.convId === 'string' ? (o.convId as ConvId) : null,
    createdAt: o.createdAt,
  };
};

const readList = (v: unknown): QueuedChat[] => {
  const items = (v as { items?: unknown } | null)?.items;
  if (!Array.isArray(items)) throw new Error('items');
  return items.map(readItem);
};
const readOne = (v: unknown): QueuedChat => readItem((v as { item?: unknown } | null)?.item);

const put = (item: QueuedChat): void =>
  queuedChatsStore.set([...(queuedChatsStore.get() ?? []).filter((x) => x.id !== item.id), item]);

/** The conversation.create the hub hands the machine: one opId, which is the first message's clientId. */
export const queuedCreate = (
  req: NewConversation,
  clientId: string,
): CommandBodies['conversation.create'] => ({
  opId: clientId,
  ...req.where,
  provider: req.provider,
  model: req.model,
  ...(req.profile ? { profile: req.profile } : {}),
  ...(req.worktree ? { worktree: true } : {}),
  first: {
    clientId,
    text: req.first?.text ?? '',
    attachmentIds: [],
    ...(req.first?.refs && req.first.refs.length > 0 ? { refs: req.first.refs } : {}),
  },
});

/** Leaves the new chat with the hub for its machine; the waiting chat, or why the hub did not take it. */
export async function queueChat(
  machine: MachineId,
  req: NewConversation,
): Promise<{ ok: true; item: QueuedChat } | { ok: false; reason: string | null }> {
  const create = queuedCreate(req, uuidv7());
  const res = await sendBody(
    'POST',
    gatewayPath(`/offline-chats?machine=${encodeURIComponent(machine)}`),
    { create },
    readOne,
    TIMEOUT_MS,
  );
  if (!('ok' in res)) return { ok: false, reason: null };
  put(res.value);
  return { ok: true, item: res.value };
}

/** The person takes a waiting chat back (or puts away one that failed). */
export async function cancelQueuedChat(id: string): Promise<boolean> {
  const res = await sendPage(
    'DELETE',
    gatewayPath(`/offline-chats?id=${encodeURIComponent(id)}`),
    readOne,
    TIMEOUT_MS,
  );
  if (!('ok' in res)) return false;
  queuedChatsStore.set((queuedChatsStore.get() ?? []).filter((x) => x.id !== id));
  return true;
}

let noQueue = false;

/** Asks the hub what waits; a hub or runtime without the queue answers 404 once and is not asked again. */
export async function loadQueuedChats(): Promise<void> {
  if (noQueue) return;
  const res = await getPage(gatewayPath('/offline-chats'), readList, TIMEOUT_MS);
  if ('ok' in res) {
    queuedChatsStore.set(res.value);
    return;
  }
  if (res.r === 'failed' && res.code === 'http' && (res.status === 404 || res.status === 405)) {
    noQueue = true;
    queuedChatsStore.set([]);
  }
}

/** Something still waits for its machine: the page keeps asking. */
export const waiting = (items: readonly QueuedChat[] | null): boolean =>
  (items ?? []).some((x) => x.state === QUEUED_STATE.queued || x.state === QUEUED_STATE.sending);

/** Asks once now, then every QUEUED_POLL_MS while a chat waits; the stop ends it. */
export function watchQueuedChats(setTimer = setTimeout, clearTimer = clearTimeout): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let live = true;
  const schedule = () => {
    if (!live || timer !== undefined) return;
    timer = setTimer(() => {
      timer = undefined;
      void tick();
    }, QUEUED_POLL_MS);
  };
  const tick = async () => {
    await loadQueuedChats();
    if (waiting(queuedChatsStore.get())) schedule();
  };
  // A chat just queued here starts the asking again.
  const unsub = queuedChatsStore.subscribe(() => {
    if (waiting(queuedChatsStore.get())) schedule();
  });
  void tick();
  return () => {
    live = false;
    unsub();
    if (timer !== undefined) clearTimer(timer);
  };
}
