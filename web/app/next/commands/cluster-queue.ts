// HRN-879, spec v10 («машина нужна только для живого»): a message written to a machine that is off waits in the
// cluster, not only in this browser. The page hands the cluster's gateway the very send command it sends the machine
// itself (same clientId, same opId); the gateway keeps it, shows it to the person's other devices, and gives it to the
// machine as soon as the machine answers — with every tab closed too. Whoever comes first — the gateway or this page's
// outbox — the machine takes one opId once, so the agent gets the message once.
//
// A hub or runtime without the gateway's queue answers 404: the page's own outbox (IndexedDB) is then all there is,
// as before.

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
const parking = new Set<ClientId>();

const parkable = (m: MessageView | undefined): m is MessageView & { conversationId: ConvId } =>
  !!m &&
  !!m.conversationId &&
  m.rev === 0 &&
  m.state.k === 'submitting' &&
  m.attachments.length === 0 &&
  !noQueue.has(m.machineId) &&
  keepsHistory(m.machineId) &&
  isMachineOff(m.machineId);

/**
 * Hands a message to the cluster's queue when its machine is off. The page keeps its own copy and keeps sending it
 * too: the gateway's copy is what outlives this tab and shows on another device.
 */
export async function parkInCluster(clientId: ClientId): Promise<boolean> {
  const m = messagesStore.get(clientId);
  if (parked.has(clientId) || parking.has(clientId) || !parkable(m)) return parked.has(clientId);
  parking.add(clientId);
  try {
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
  } finally {
    parking.delete(clientId);
  }
}

/** The person took the message back: the cluster's copy goes too, unless it is already on its way to the machine. */
export async function unparkInCluster(m: MessageView): Promise<void> {
  if (!parked.has(m.clientId)) return;
  parked.delete(m.clientId);
  await sendPage(
    'DELETE',
    queuePath(m.machineId, `&client=${encodeURIComponent(m.clientId)}`),
    () => null,
    TIMEOUT_MS,
  );
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
  noQueue.clear();
  parked.clear();
  parking.clear();
}
