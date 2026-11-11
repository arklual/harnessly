// HRN-879, spec v10 («машина нужна только для живого»): a message written to a machine that is off waits in the
// cluster, not only in this browser. The page hands the cluster's gateway the very send command it sends the machine
// itself (same clientId, same opId); the gateway keeps it, shows it to the person's other devices, and gives it to the
// machine as soon as the machine answers — with every tab closed too. Whoever comes first — the gateway or this page's
// outbox — the machine takes one opId once, so the agent gets the message once.
//
// A hub or runtime without the gateway's queue answers 404: the page's own outbox (IndexedDB) is then all there is,
// as before.

import { pageClock } from '@/next/core/clock';
import { getPage, sendBody, sendPage } from '@/next/core/net/http';
import { gatewayPath } from '@/next/core/net/machine';
import { isMachineOff, keepsHistory } from '@/next/stores/central';
import { messagesStore, putLocal, type MessageView } from '@/next/stores/messages';
import type { ClientId, ConvId, MachineId, MessageKind, MessageMode, MessageRef } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

/** The send command as runCommand sends it (with its opId): the gateway keeps it to the byte the machine reads. */
export interface ParkedSend {
  opId: string;
  clientId: ClientId;
  conversationId: ConvId;
  text: string;
  attachmentIds: string[];
  mode: MessageMode;
  kind: MessageKind;
  refs?: MessageRef[];
}

export interface ParkedItem {
  id: string;
  state: 'queued' | 'sending' | 'delivered' | 'failed';
  createdAt: number;
  send: ParkedSend | null;
}

/** The page's send command of a message, as submit() sends it. */
export const sendOf = (m: MessageView & { conversationId: ConvId }): ParkedSend => ({
  opId: m.clientId,
  clientId: m.clientId,
  conversationId: m.conversationId,
  text: m.text,
  attachmentIds: [],
  mode: m.mode,
  kind: m.kind,
  ...(m.refs && m.refs.length > 0 ? { refs: m.refs } : {}),
});

const queuePath = (machine: MachineId, query: string) =>
  gatewayPath(`/offline-prompts?machine=${encodeURIComponent(machine)}${query}`);

const STATES = new Set(['queued', 'sending', 'delivered', 'failed']);

const readSend = (v: unknown): ParkedSend | null => {
  if (typeof v !== 'object' || v === null) return null;
  const s = v as Record<string, unknown>;
  if (typeof s.clientId !== 'string' || typeof s.conversationId !== 'string' || typeof s.text !== 'string')
    return null;
  if (typeof s.mode !== 'string' || typeof s.kind !== 'string') return null;
  return {
    opId: s.clientId,
    clientId: s.clientId as ClientId,
    conversationId: s.conversationId as ConvId,
    text: s.text,
    attachmentIds: [],
    mode: s.mode as MessageMode,
    kind: s.kind as MessageKind,
    ...(Array.isArray(s.refs) && s.refs.length > 0 ? { refs: s.refs as MessageRef[] } : {}),
  };
};

const readItem = (v: unknown): ParkedItem => {
  if (typeof v !== 'object' || v === null) throw new Error('item');
  const o = v as Record<string, unknown>;
  if (typeof o.id !== 'string' || typeof o.state !== 'string' || !STATES.has(o.state))
    throw new Error('item');
  return {
    id: o.id,
    state: o.state as ParkedItem['state'],
    createdAt: typeof o.createdAt === 'number' ? o.createdAt : 0,
    send: readSend(o.send),
  };
};

const readAdded = (v: unknown): ParkedItem => readItem((v as { item?: unknown } | null)?.item);
const readList = (v: unknown): ParkedItem[] => {
  const items = (v as { items?: unknown } | null)?.items;
  if (!Array.isArray(items)) throw new Error('items');
  return items.map(readItem);
};

/** Machines whose gateway has no queue (a hub, a runtime alone): asked once per page. */
const noQueue = new Set<MachineId>();
const parked = new Set<ClientId>();
/** The parks on their way, and each machine's last one: a machine's messages are handed over one after another. */
const parking = new Map<ClientId, Promise<boolean>>();
const lines = new Map<MachineId, Promise<unknown>>();
const unparking = new Map<ClientId, Promise<Unparked>>();
/** Grows when the page forgets what it parked (tests): a retry of before that stops. */
let epoch = 0;

const parkable = (m: MessageView | undefined): m is MessageView & { conversationId: ConvId } =>
  !!m &&
  !!m.conversationId &&
  m.rev === 0 &&
  m.state.k === 'submitting' &&
  !m.cancelling &&
  m.attachments.length === 0 &&
  !noQueue.has(m.machineId) &&
  keepsHistory(m.machineId) &&
  isMachineOff(m.machineId);

/**
 * Hands a message to the cluster's queue when its machine is off. The page keeps its own copy and keeps sending it
 * too: the gateway's copy is what outlives this tab and shows on another device. The cluster keeps a machine's messages
 * in the order they came to it, so they are handed over one at a time, in the order of the calls.
 */
export function parkInCluster(clientId: ClientId): Promise<boolean> {
  const going = parking.get(clientId);
  if (going) return going;
  const m = messagesStore.get(clientId);
  if (parked.has(clientId) || !parkable(m)) return Promise.resolve(parked.has(clientId));
  const before = lines.get(m.machineId) ?? Promise.resolve();
  const run = before
    .then(() => park(clientId))
    .catch(() => false)
    .finally(() => parking.delete(clientId));
  parking.set(clientId, run);
  lines.set(m.machineId, run);
  return run;
}

async function park(clientId: ClientId): Promise<boolean> {
  // Taken back, or shown by the runtime, while the one before it was handed over: nothing to keep.
  const m = messagesStore.get(clientId);
  if (parked.has(clientId) || !parkable(m)) return parked.has(clientId);
  const send = sendOf(m);
  const res = await sendBody(
    'POST',
    queuePath(m.machineId, ''),
    { tabId: send.conversationId, text: send.text, send },
    readAdded,
    TIMEOUT_MS,
  );
  if ('ok' in res) {
    parked.add(clientId);
    return true;
  }
  if (res.r === 'failed' && res.code === 'http' && (res.status === 404 || res.status === 405))
    noQueue.add(m.machineId);
  return false;
}

/**
 * What became of the cluster's copy of a message the person took back: none — the cluster keeps no copy; cancelled —
 * it waited there and is gone, the cluster will not deliver it; delivered — the cluster gave it to the machine, which
 * is now the one to cancel it; unknown — the cluster did not say (it cannot be asked, or an older one that only says
 * «not now»), so the copy may still be there.
 */
export type Unparked = 'none' | 'cancelled' | 'delivered' | 'unknown';

/** How long the page waits before it asks the cluster again to drop a copy: 1 s, 2 s … then every 30 s. */
export const UNPARK_STEPS_MS = [1000, 2000, 4000, 8000, 15000, 30000] as const;
/** An older cluster answers 409 for a copy it will never drop (delivered, or not there): asked this many times. */
export const UNPARK_CONFLICTS = 10;

const readRemoved = (v: unknown): Unparked =>
  (v as { outcome?: unknown } | null)?.outcome === 'delivered' ? 'delivered' : 'cancelled';

/**
 * The person took the message back: the cluster's copy goes too. A park still on its way is waited for first — its copy
 * would otherwise outlive the cancel — and the delete is asked again until the cluster answers for it: done (2xx), or
 * no such copy (404). While the cluster is handing the copy to the machine it answers 409, and is asked again.
 *
 * `ask`: the page does not know whether the cluster keeps a copy (the message is not of this page's making), so it
 * asks all the same.
 */
export function unparkInCluster(m: MessageView, ask = false): Promise<Unparked> {
  const going = unparking.get(m.clientId);
  if (going) return going;
  const run = unpark(m, ask)
    .catch((): Unparked => 'unknown')
    .finally(() => unparking.delete(m.clientId));
  unparking.set(m.clientId, run);
  return run;
}

async function unpark(m: MessageView, ask: boolean): Promise<Unparked> {
  const born = epoch;
  await parking.get(m.clientId);
  if (!parked.has(m.clientId) && !(ask && !noQueue.has(m.machineId) && keepsHistory(m.machineId)))
    return 'none';
  let conflicts = 0;
  for (let n = 0; ; n += 1) {
    const res = await sendPage(
      'DELETE',
      queuePath(m.machineId, `&client=${encodeURIComponent(m.clientId)}`),
      readRemoved,
      TIMEOUT_MS,
    );
    if (born !== epoch) return 'unknown';
    if ('ok' in res) {
      parked.delete(m.clientId);
      return res.value;
    }
    if (res.r !== 'failed') return 'unknown';
    if (res.code === 'http') {
      if (res.status === 404) {
        parked.delete(m.clientId);
        return 'none';
      }
      if (res.status === 409) {
        conflicts += 1;
        if (conflicts >= UNPARK_CONFLICTS) return 'unknown';
      } else if (res.status < 500 && res.status !== 408 && res.status !== 429) {
        // No rights, or a request the cluster does not take: the same question gets the same answer.
        return 'unknown';
      }
    }
    const wait = UNPARK_STEPS_MS[Math.min(n, UNPARK_STEPS_MS.length - 1)];
    await new Promise<void>((resolve) => pageClock().setTimeout(resolve, wait));
    if (born !== epoch) return 'unknown';
  }
}

/**
 * The messages this chat has waiting in the cluster that this page has not got (written on another device, or
 * before the tab was closed): they join the page's own as messages on their way, with the same clientId. Returns the
 * ones taken, for the outbox to start on.
 */
export async function adoptParked(
  machine: MachineId,
  conv: ConvId,
  view: (send: ParkedSend) => MessageView,
): Promise<ClientId[]> {
  if (noQueue.has(machine) || !keepsHistory(machine)) return [];
  const res = await getPage(queuePath(machine, `&tab=${encodeURIComponent(conv)}`), readList, TIMEOUT_MS);
  if (!('ok' in res)) {
    if (res.r === 'failed' && res.code === 'http' && (res.status === 404 || res.status === 405))
      noQueue.add(machine);
    return [];
  }
  const taken: ClientId[] = [];
  for (const item of res.value) {
    const send = item.send;
    if (!send || send.conversationId !== conv || (item.state !== 'queued' && item.state !== 'sending'))
      continue;
    parked.add(send.clientId);
    if (messagesStore.get(send.clientId)) continue;
    putLocal(view(send));
    taken.push(send.clientId);
  }
  return taken;
}

/** For tests: the page forgets what it parked. */
export function resetClusterQueue(): void {
  epoch += 1;
  noQueue.clear();
  parked.clear();
  parking.clear();
  lines.clear();
  unparking.clear();
}
