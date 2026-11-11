// The person's machines: the fleet's records, and per machine what its runtime said in welcome (epoch, features,
// limits). A machine whose runtime answers no v2 is 'outdated': its chats are not listed (spec §8).

import { createKeyedStore, createStore, sameItems, useAll, useKey, useStore } from '@/next/core/store';
import type { Feature, Limits, Machine, MachineId } from '@/next/protocol/v2';

export interface MachineView {
  machine: Machine;
  /** the hub listed the machine as on with a runtime that did not answer a moment ago; gone with its own
   *  welcome. */
  hangs?: true;
  /** v2: the runtime speaks proto 2; outdated: /api/v2/hello is missing or older; unknown: not asked yet. */
  runtime: 'unknown' | 'v2' | 'outdated';
  epoch: string | null;
  features: Feature[];
  limits: Limits | null;
}

/** What a page assumes before a machine's welcome: wire.DefaultLimits. */
export const DEFAULT_LIMITS: Limits = {
  maxQueue: 20,
  maxText: 100000,
  maxAttachments: 20,
  maxImageBytes: 10485760,
  maxFileBytes: 0,
  slowMs: 3000,
  cmdBudgetMs: 10000,
  cmdTimeoutMs: 12000,
};

export const machinesStore = createKeyedStore<MachineId, MachineView>();
/** The fleet's order; null while the list is loading. */
export const machineOrderStore = createStore<MachineId[] | null>(null);
/** The machine whose chat is open: it holds machine, convs and the open conv; the rest hold only convs. */
export const activeMachineStore = createStore<MachineId | null>(null);
/**
 * The machine the person works on: the default one at start, then the one picked in the sidebar. Opening a
 * chat of another machine makes that one active (its channel), not this one: the default machine of new chats.
 */
export const homeMachineStore = createStore<MachineId | null>(null);

/**
 * A machine of the fleet's list. From a hub its agents are what the machine last said of them, whatever it
 * does now, with its features and its agents' limits (`usage`, the platform's …/machines `limits`); `hangs`: it is on
 * but its runtime did not answer a moment ago.
 */
export type FleetMachine = Machine & { hangs?: boolean; features?: Feature[]; usage?: unknown };

function putMachine({ hangs, features, usage: _usage, ...m }: FleetMachine): void {
  machinesStore.patch(m.id, (prev) => {
    const { hangs: _was, ...view }: MachineView = prev
      ? { ...prev, machine: m }
      : { machine: m, runtime: 'unknown', epoch: null, features: features ?? [], limits: null };
    // A machine that has said welcome on this page answers for itself.
    return hangs && view.epoch === null ? { ...view, hangs: true } : view;
  });
}

export function putMachines(list: FleetMachine[]): void {
  for (const m of list) putMachine(m);
  machineOrderStore.set(list.map((m) => m.id));
}

/**
 * A later reading of the fleet's list, on a page that is already open. A machine the page did not know is put as at
 * the start. One it knows keeps what its own socket said (runtime, agents, health, load, welcome) and takes what only
 * the list knows: its name and host, whose it is, and its reach while no socket of the page is live to say it (`live`).
 * A machine the list no longer has leaves the order; its record is the caller's to drop, with its socket.
 */
export function mergeMachines(list: FleetMachine[], live: (m: MachineId) => boolean = () => false): void {
  const fresh: FleetMachine[] = [];
  for (const m of list) {
    const prev = machinesStore.get(m.id);
    if (!prev) {
      fresh.push(m);
      continue;
    }
    const was = prev.machine;
    const reach = live(m.id)
      ? { reach: was.reach, lastSeenAt: was.lastSeenAt }
      : { reach: m.reach, lastSeenAt: m.lastSeenAt };
    const next: Machine = {
      ...was,
      name: m.name,
      host: m.host,
      ...reach,
      ...(m.orgName === undefined ? {} : { orgName: m.orgName }),
      ...(m.mine === undefined ? {} : { mine: m.mine }),
    };
    const same =
      next.name === was.name &&
      next.host === was.host &&
      next.reach === was.reach &&
      next.lastSeenAt === was.lastSeenAt &&
      next.orgName === was.orgName &&
      next.mine === was.mine;
    if (!same) machinesStore.set(m.id, { ...prev, machine: next });
  }
  for (const m of fresh) putMachine(m);
  const order = list.map((m) => m.id);
  const had = machineOrderStore.get();
  if (!had || had.length !== order.length || had.some((id, i) => id !== order[i]))
    machineOrderStore.set(order);
}

export const limitsOf = (m: MachineId): Limits => machinesStore.get(m)?.limits ?? DEFAULT_LIMITS;

export const hasFeature = (m: MachineId, f: Feature): boolean =>
  machinesStore.get(m)?.features.includes(f) ?? false;

export const useMachine = (m: MachineId | null) => useKey(machinesStore, m);
export const useActiveMachine = () => useStore(activeMachineStore);
export const useHomeMachine = () => useStore(homeMachineStore);

/** The machines in fleet order; null while loading. */
export function useMachines(): MachineView[] | null {
  const order = useStore(machineOrderStore);
  const views = useAll(
    machinesStore,
    (s) => (order ? order.map((id) => s.get(id)).filter((v): v is MachineView => v !== undefined) : null),
    (a, b) => (a === null || b === null ? a === b : sameItems(a, b)),
  );
  return views;
}
