// The cluster's own copy of the person's chats: the list of a machine that is switched off still comes from
// here, and a chat of such a machine is read from here, read-only. A machine that is live answers for itself; this
// copy never overrides it.
import { batch, createStore, useStore } from '@/next/core/store';
import { gatewayPath } from '@/next/core/net/machine';
import { getPage, type PageOutcome } from '@/next/core/net/http';
import { connectionStore } from '@/next/stores/connection';
import { conversationsStore, convsCachedStore, convsLoadedStore } from '@/next/stores/conversations';
import {
  parseCentral,
  type CentralChat,
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
  /** The machines the copy is on for: only their chats join the one list (their moves are the ones known). */
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

export const useCentral = (): CentralView => useStore(centralStore);

/** A chat of a switched-off machine is shown stopped and not resumable: nothing there can run it. */
export const asOffline = (c: Conversation): Conversation => ({
  ...c,
  state: { k: 'stopped', since: c.lastActivityAt as Ms, resumable: false },
  queue: { count: 0, paused: null },
  gate: null,
});

export async function refreshCentral(): Promise<void> {
  const res = await getPage(gatewayPath('/history/conversations'), parseCentral.list, TIMEOUT_MS);
  if (!('ok' in res)) return;
  const { enabled, machines, conversations, moved = [] } = res.value;
  const off = new Set(machines.filter((m) => m.state === 'offline').map((m) => m.id));
  batch(() => {
    centralStore.set({
      enabled,
      off,
      moved: new Set(moved.map((m) => movedKey(m.machineId, m.convId))),
      names: new Map(machines.map((m) => [m.id, m.name])),
      on: new Set(machines.map((m) => m.id)),
    });
    if (!enabled) return;
    const byMachine = new Map<MachineId, Conversation[]>();
    for (const c of conversations) {
      if (!off.has(c.machineId) || connectionStore.get(c.machineId)?.k === 'live') continue;
      byMachine.set(c.machineId, [...(byMachine.get(c.machineId) ?? []), c]);
    }
    for (const [machine, list] of byMachine) {
      for (const c of list) {
        const have = conversationsStore.get(c.id);
        if (have && have.lastActivityAt > c.lastActivityAt) continue;
        conversationsStore.set(c.id, asOffline(c));
      }
      if (!convsLoadedStore.has(machine)) convsCachedStore.set(machine, true);
    }
  });
}

export function loadCentralChat(
  conv: string,
  before?: string,
  limit = 33,
): Promise<PageOutcome<CentralChat>> {
  const q = new URLSearchParams({ limit: String(limit) });
  if (before) q.set('before', before);
  return getPage(
    gatewayPath(`/history/conversations/${encodeURIComponent(conv)}?${q}`),
    parseCentral.chat,
    TIMEOUT_MS,
  );
}
