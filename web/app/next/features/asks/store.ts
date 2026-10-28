// The asks of a machine, read once for every part of the page that shows them (the sidebar's «People», the cards in
// the chats): one poll per machine while anything subscribes, quicker while an ask waits. An answer given here puts
// the machine's answer into the list at once.
import { useCallback, useEffect, useSyncExternalStore } from 'react';
import { isAskRefusal, loadAsks, type IAsk } from '@/next/features/asks/api';
import type { MachineId } from '@/next/protocol/v2';

/** While an ask waits the list is read this often; otherwise only now and then (an agent may ask any time). */
const WAITING_POLL_MS = 4_000;
const IDLE_POLL_MS = 20_000;

/** null while the first read has not come back. */
type TAsks = readonly IAsk[] | null;

interface IEntry {
  asks: TAsks;
  listeners: Set<() => void>;
  timer: ReturnType<typeof setTimeout> | null;
  inFlight: boolean;
}

const entries = new Map<MachineId, IEntry>();

const entryOf = (machine: MachineId): IEntry => {
  let e = entries.get(machine);
  if (!e) {
    e = { asks: null, listeners: new Set(), timer: null, inFlight: false };
    entries.set(machine, e);
  }
  return e;
};

const emit = (e: IEntry) => e.listeners.forEach((fn) => fn());

const schedule = (machine: MachineId, e: IEntry) => {
  if (e.timer) clearTimeout(e.timer);
  if (e.listeners.size === 0) {
    e.timer = null;
    return;
  }
  const waiting = e.asks?.some((a) => a.status === 'waiting') ?? false;
  e.timer = setTimeout(() => void refreshAsks(machine), waiting ? WAITING_POLL_MS : IDLE_POLL_MS);
};

/** Reads the machine's asks again now. */
export const refreshAsks = async (machine: MachineId): Promise<void> => {
  const e = entryOf(machine);
  if (e.inFlight) return;
  e.inFlight = true;
  const res = await loadAsks(machine);
  e.inFlight = false;
  if ('ok' in res && !isAskRefusal(res.value)) {
    e.asks = res.value;
    emit(e);
  }
  schedule(machine, e);
};

/** Puts one ask as the machine answered it into the list. */
export const putAsk = (machine: MachineId, ask: IAsk) => {
  const e = entryOf(machine);
  const list = e.asks ?? [];
  const at = list.findIndex((a) => a.id === ask.id);
  e.asks = at < 0 ? [ask, ...list] : list.map((a) => (a.id === ask.id ? ask : a));
  emit(e);
  schedule(machine, e);
};

const NONE: TAsks = null;

/** The machine's asks, newest first; null while they load (and with no machine). */
export const useAsks = (machine: MachineId | null): TAsks => {
  const subscribe = useCallback(
    (fn: () => void) => {
      if (!machine) return () => {};
      const e = entryOf(machine);
      e.listeners.add(fn);
      return () => {
        e.listeners.delete(fn);
        if (e.listeners.size === 0 && e.timer) {
          clearTimeout(e.timer);
          e.timer = null;
        }
      };
    },
    [machine],
  );
  const asks = useSyncExternalStore(
    subscribe,
    () => (machine ? entryOf(machine).asks : NONE),
    () => NONE,
  );
  useEffect(() => {
    if (machine) void refreshAsks(machine);
  }, [machine]);
  return asks;
};
