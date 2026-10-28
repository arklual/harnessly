// The person's machines: the fleet's records, and per machine what its runtime said in welcome (epoch, features,
// limits). A machine whose runtime answers no v2 is 'outdated': its chats are not listed (spec §8).

import { createKeyedStore, createStore, sameItems, useAll, useKey, useStore } from '@/next/core/store';
import type { Feature, Limits, Machine, MachineId } from '@/next/protocol/v2';

export interface MachineView {
  machine: Machine;
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
  maxFileBytes: 52428800,
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
 * chat of another machine makes that one active (its channel), not this one: «здесь» of «Продолжить здесь».
 */
export const homeMachineStore = createStore<MachineId | null>(null);

export function putMachines(list: Machine[]): void {
  for (const m of list) {
    machinesStore.patch(m.id, (prev) =>
      prev
        ? { ...prev, machine: m }
        : { machine: m, runtime: 'unknown', epoch: null, features: [], limits: null },
    );
  }
  machineOrderStore.set(list.map((m) => m.id));
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
