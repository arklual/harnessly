// The cluster's own copy of the person's chats: the list of a machine that is switched off still comes from
// here, and a chat of such a machine is read from here, read-only. A machine that is live answers for itself; this
// copy never overrides it.
import { batch, createStore, useStore } from '@/next/core/store';
import { gatewayPath } from '@/next/core/net/machine';
import { getPage, getPageIfChanged, type PageOutcome } from '@/next/core/net/http';
import { connectionStore } from '@/next/stores/connection';
import { convsCachedStore, convsLoadedStore } from '@/next/stores/conversations';
import { FROM_CENTRAL, mergeConversation, veilConversation } from '@/next/stores/conversation-merge';
import {
  parseCentral,
  type CentralChat,
  type CentralList,
  type Conversation,
  type MachineId,
  type Ms,
} from '@/next/protocol/v2';

const TIMEOUT_MS = 15_000;

export interface CentralView {
  /** The copy is on for this person. */
  enabled: boolean;
  /** Machines the cluster sees switched off. */
  off: ReadonlySet<MachineId>;
  /** The chats continued on another machine, by `movedKey`: the one list shows where they went on, not them. */
  moved: ReadonlySet<string>;
  /** The person's machines by the cluster's names, for a chat of a machine the page has no record of. */
  names: ReadonlyMap<MachineId, string>;
  /** The person's own machines the copy is on for: only their chats join the one list (their moves are the ones
   *  known). A machine the person reaches as an administrator is never here (#57). */
  on: ReadonlySet<MachineId>;
}

const NONE: CentralView = {
  enabled: false,
  off: new Set(),
  moved: new Set(),
  names: new Map(),
  on: new Set(),
};

export const movedKey = (machine: MachineId, conv: string): string => `${machine}/${conv}`;
export const centralStore = createStore<CentralView>(NONE);

/** Whether the machine is off by the cluster's word and the page has no live link to it. */
export function isMachineOff(m: MachineId | null): boolean {
  if (m === null) return false;
  const c = centralStore.get();
  return c.enabled && c.off.has(m) && connectionStore.get(m)?.k !== 'live';
}

/**
 * HRN-879, spec v10: whether the cluster keeps this machine's chats, so their history (the feed, older pages, a turn's
 * steps) is read from it whether the machine is on, off, starting or hung. The machine answers only for what the copy
 * does not have yet.
 */
export function keepsHistory(m: MachineId | null): boolean {
  if (m === null) return false;
  const c = centralStore.get();
  return c.enabled && c.on.has(m);
}

export const useCentral = (): CentralView => useStore(centralStore);

/** A chat of a switched-off machine is shown stopped and not resumable: nothing there can run it. */
export const asOffline = (c: Conversation): Conversation => ({
  ...c,
  state: { k: 'stopped', since: c.lastActivityAt as Ms, resumable: false },
  queue: { count: 0, paused: null },
  gate: null,
});

const looksOffline = (c: Conversation): boolean =>
  c.state.k === 'stopped' &&
  !c.state.resumable &&
  c.queue.count === 0 &&
  c.queue.paused === null &&
  c.gate === null;

/** The list as the cluster last gave it, with its entity tag: an answer that nothing changed carries no body. */
let held: { tag: string; value: CentralList } | null = null;

/** The number of the last request for the list: only its answer is applied. */
let asked = 0;
/** A merge anew was asked by a request whose answer was not applied; the next applied answer owes it. */
let remergeOwed = false;

/** Forgets the held list: the next request asks for the whole one. */
export function forgetCentralList(): void {
  held = null;
  remergeOwed = false;
}

/** A new session starts from nothing: the copy is on again only by the cluster's word to it. */
export function resetCentral(): void {
  forgetCentralList();
  centralStore.set(NONE);
}

/**
 * Asks the cluster for the list, naming the one held. A list that has not changed is not applied again, unless
 * `remerge`: what the list is merged with on the page (the links) has changed, so the held one is merged anew.
 *
 * Only the answer to the last request is applied: one that comes after a later request was made describes an older
 * moment (the machines were live then, a chat had not moved yet) and is dropped, whatever it says.
 */
export async function refreshCentral(remerge = false): Promise<void> {
  asked += 1;
  const mine = asked;
  if (remerge) remergeOwed = true;
  const res = await getPageIfChanged(
    gatewayPath('/history/conversations'),
    parseCentral.list,
    TIMEOUT_MS,
    held?.tag ?? null,
  );
  if (mine !== asked) return;
  if ('same' in res) {
    const owed = remergeOwed;
    remergeOwed = false;
    if (owed && held) applyCentral(held.value);
    return;
  }
  if (!('ok' in res)) return;
  // A copy that was on does not go off within a session by one answer: the cluster answered so for a moment
  // it could not list the person's machines, and the page dropped to one machine's list, projects and favorites and
  // came back with the next answer. The answer is not kept, so the next ask is a full one.
  if (!res.value.enabled && centralStore.get().enabled) return;
  remergeOwed = false;
  held = res.tag ? { tag: res.tag, value: res.value } : null;
  applyCentral(res.value);
}

function applyCentral(list: CentralList): void {
  const { enabled, machines, conversations, moved = [], others = [] } = list;
  // #57: the machines the person administers are named and stated, but only the person's own are `on`.
  const named = [...machines, ...others];
  const off = new Set(named.filter((m) => m.state === 'offline').map((m) => m.id));
  batch(() => {
    centralStore.set({
      enabled,
      off,
      moved: new Set(moved.map((m) => movedKey(m.machineId, m.convId))),
      names: new Map(named.map((m) => [m.id, m.name])),
      on: new Set(machines.map((m) => m.id)),
    });
    if (!enabled) return;
    const byMachine = new Map<MachineId, Conversation[]>();
    for (const c of conversations) {
      if (!drawsFromCopy(c.machineId, off)) continue;
      byMachine.set(c.machineId, [...(byMachine.get(c.machineId) ?? []), c]);
    }
    for (const [machine, list] of byMachine) {
      // Stopped only where nothing can run them; a machine that is starting or live keeps the copy's look until its list.
      const stopped = off.has(machine) && connectionStore.get(machine)?.k !== 'live';
      for (const c of list) {
        // The copy only draws the chat until the machine speaks: by the rule of the merge any word of the machine
        // replaces it, and the copy does not replace what the machine said unless it knows of later activity. It is
        // kept at rev 0, since the rev the cluster holds is not one the machine's next word is compared with.
        const shown = mergeConversation({ ...c, rev: 0 }, FROM_CENTRAL);
        // With nothing to run it the chat is drawn stopped, whichever record stood; that look is the page's, so the
        // machine's list replaces it when the machine is back, at whatever rev.
        if (stopped && shown.machineId === machine)
          veilConversation(c.id, (have) => (looksOffline(have) ? have : asOffline(have)));
      }
      if (!convsLoadedStore.has(machine)) convsCachedStore.set(machine, true);
    }
  });
}

/** Whether a machine's chats are drawn from the copy: it is off by the cluster's word, or its own list has not come
 *  on this page yet (a machine that is starting, hung or only just switched off, which the cluster names offline
 *  45 s later). A live link's list is the machine's own word and replaces the copy as it comes. */
const drawsFromCopy = (m: MachineId, off: ReadonlySet<MachineId>): boolean => {
  if (connectionStore.get(m)?.k === 'live' && convsLoadedStore.has(m)) return false;
  return off.has(m) || !convsLoadedStore.has(m);
};

export function loadCentralChat(
  conv: string,
  before?: string,
  limit = 33,
): Promise<PageOutcome<CentralChat>> {
  const q = new URLSearchParams({ limit: String(limit) });
  if (before) q.set('before', before);
  const read = () =>
    getPage(
      gatewayPath(`/history/conversations/${encodeURIComponent(conv)}?${q}`),
      parseCentral.chat,
      TIMEOUT_MS,
    );
  if (before || limit !== 33) return read();
  // The chat's newest page is asked by its feed and by its machine channel at once: one request serves both.
  const have = newestInFlight.get(conv);
  if (have) return have;
  const p = read().finally(() => newestInFlight.delete(conv));
  newestInFlight.set(conv, p);
  return p;
}

const newestInFlight = new Map<string, Promise<PageOutcome<CentralChat>>>();
