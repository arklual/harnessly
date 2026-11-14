// The person's machines: the fleet's records, and per machine what its runtime said in welcome (epoch, features,
// limits). A machine whose runtime answers no v2 is 'outdated': its chats are not listed (spec §8).

import { createKeyedStore, createStore, sameItems, useAll, useKey, useStore } from '@/next/core/store';
import { useCallback, useRef, useSyncExternalStore } from 'react';

import {
  FEATURES,
  type Feature,
  type Limits,
  type Machine,
  type MachineId,
  type MachineLoad,
} from '@/next/protocol/v2';

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
/**
 * The load the active machine reports every 2 s, apart from its record: a reading changed the record, and
 * every reader of the record (the shell, the chat, the composer) drew anew each time with nothing new to show. Only
 * what shows the numbers reads it: the machine's card, and a critical alert that names the free disk or memory.
 */
export const machineLoadStore = createKeyedStore<MachineId, MachineLoad>();

/** The record without its load, the load into machineLoadStore: what a welcome or a snapshot of the machine brings. */
export function splitLoad(m: MachineId, machine: Machine): Machine {
  if (machine.load === undefined) return machine;
  const { load, ...rest } = machine;
  machineLoadStore.set(m, load);
  return rest;
}

export const useMachineLoad = (m: MachineId | null) => useKey(machineLoadStore, m);

/** The loads of these machines, the same array while none of theirs changed: a reading of another machine draws nothing. */
export function useMachineLoads(ids: readonly MachineId[]): readonly (MachineLoad | undefined)[] {
  const key = ids.join('\n');
  const subscribe = useCallback(
    (fn: () => void) => {
      const want = new Set(key.split('\n'));
      return machineLoadStore.subscribeAll((changed) => {
        for (const id of changed) if (want.has(id)) return fn();
      });
    },
    [key],
  );
  const last = useRef<{ key: string; loads: readonly (MachineLoad | undefined)[] } | null>(null);
  const get = () => {
    const loads = ids.map((id) => machineLoadStore.get(id));
    const was = last.current;
    if (was && was.key === key && sameItems(was.loads, loads)) return was.loads;
    last.current = { key, loads };
    return loads;
  };
  return useSyncExternalStore(subscribe, get, get);
}

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

const FEATURES_KEY = 'hx-machine-features';

const readKept = (): Record<string, unknown> => {
  try {
    const all: unknown = JSON.parse(localStorage.getItem(FEATURES_KEY) ?? '{}');
    return typeof all === 'object' && all !== null && !Array.isArray(all)
      ? (all as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
};

/** What the machine said it can in its last welcome in this browser; [] when it never said. */
export function keptFeatures(m: MachineId): Feature[] {
  const v = readKept()[m];
  return Array.isArray(v) ? v.filter((f): f is Feature => (FEATURES as readonly unknown[]).includes(f)) : [];
}

/**
 * A machine's welcome comes over its socket, seconds after the page (a busy machine, a restart for an
 * update, a slow tunnel). Until then the page drew it as able of nothing and the microphone was not offered: it came
 * and went. What the machine said last is kept here and stands in until it speaks again.
 */
export function keepFeatures(m: MachineId, features: readonly Feature[]): void {
  try {
    const all = readKept();
    const was = all[m];
    if (Array.isArray(was) && was.length === features.length && was.every((f, i) => f === features[i]))
      return;
    localStorage.setItem(FEATURES_KEY, JSON.stringify({ ...all, [m]: features }));
  } catch {
    // Blocked or full storage: the page waits for the welcome as before.
  }
}

function putMachine({ hangs, features, usage: _usage, ...m }: FleetMachine): void {
  machinesStore.patch(m.id, (prev) => {
    const { hangs: _was, ...view }: MachineView = prev
      ? { ...prev, machine: m }
      : {
          machine: m,
          runtime: 'unknown',
          epoch: null,
          features: features?.length ? features : keptFeatures(m.id),
          limits: null,
        };
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
