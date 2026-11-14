// The one-time codes the agents' commands of a machine wait for (harnessly secret run --code): read every two seconds
// while the page is visible and an ask waits or the list moves (such a code lives half a minute), slower while quiet. Every place that shows them (the chat's column
// «Ждут код», later the island of notifications) reads them here and answers through features/safe/api.ts.
import { useCallback, useMemo, useState, useSyncExternalStore } from 'react';
import { calmPoll } from '@/next/core/net/calm-poll';
import { isSafeRefusal, loadCodeAsks, type ICodeAsk } from '@/next/features/safe/api';
import type { MachineId } from '@/next/protocol/v2';

/** The step while asks come and go, or one waits (a code lives half a minute). */
export const CODES_POLL_MS = 2000;
/** The longest step while there is none and nothing changes: the conversations moving bring it back. */
export const CODES_QUIET_MS = 15_000;

export interface ICodeAsksRead {
  /** The asks waiting for the person, oldest first. */
  asks: readonly ICodeAsk[];
  /** The machine's clock minus this page's: `until` is the machine's. */
  skew: number;
  /** Takes an ask off at once (answered or cancelled here), before the next read says so. */
  drop: (id: string) => void;
}

const same = (a: readonly ICodeAsk[], b: readonly ICodeAsk[]) =>
  a.length === b.length && a.every((x, i) => x.id === b[i]?.id && x.until === b[i]?.until);

const NO_ASKS: readonly ICodeAsk[] = [];

interface IRead {
  machine: MachineId;
  asks: readonly ICodeAsk[];
  skew: number;
}

/** One poll per machine for every place that shows the asks (the chat's column and the smart notice). */
const polls = new Map<MachineId, { read: IRead | null; listeners: Set<() => void>; stop: () => void }>();

const watch = (m: MachineId, fn: () => void): (() => void) => {
  let p = polls.get(m);
  if (!p) {
    const entry: { read: IRead | null; listeners: Set<() => void>; stop: () => void } = {
      read: null,
      listeners: new Set(),
      stop: () => undefined,
    };
    entry.stop = calmPoll({
      base: CODES_POLL_MS,
      max: CODES_QUIET_MS,
      look: async () => {
        const res = await loadCodeAsks(m);
        if (!('ok' in res) || isSafeRefusal(res.value)) return false;
        const { codes, now } = res.value;
        const skew = now - Date.now();
        const was = entry.read;
        const changed = !(was && same(was.asks, codes) && Math.abs(was.skew - skew) < 1000);
        if (changed) {
          entry.read = { machine: m, asks: codes, skew };
          for (const l of entry.listeners) l();
        }
        // An ask that waits keeps the base step: its code lives half a minute.
        return changed || codes.length > 0;
      },
    });
    polls.set(m, entry);
    p = entry;
  }
  const entry = p;
  entry.listeners.add(fn);
  return () => {
    entry.listeners.delete(fn);
    if (entry.listeners.size === 0) {
      entry.stop();
      polls.delete(m);
    }
  };
};

export const useCodeAsks = (machine: MachineId | null): ICodeAsksRead => {
  const subscribe = useCallback(
    (fn: () => void) => (machine ? watch(machine, fn) : () => undefined),
    [machine],
  );
  const read = useSyncExternalStore(
    subscribe,
    () => (machine ? (polls.get(machine)?.read ?? null) : null),
    () => null,
  );
  const [dropped, setDropped] = useState<readonly string[]>([]);
  // The ids dropped here only hide asks; an ask the machine no longer lists is gone from `read` anyway.

  // The same list until the read or the drops change: the smart notice recomputes its items on a new one.
  const asks = useMemo(
    () =>
      read !== null && read.machine === machine ? read.asks.filter((a) => !dropped.includes(a.id)) : NO_ASKS,
    [read, machine, dropped],
  );
  return {
    asks,
    skew: read?.skew ?? 0,
    drop: (id) => setDropped((was) => (was.includes(id) ? was : [...was, id])),
  };
};
