// Whether a dev server of the machine listens, for the cards of the feed: one read of the machine's ports serves every
// card of it for FRESH_MS, and a card that asks while it is on its way waits for the same read.
import { useEffect, useSyncExternalStore } from 'react';
import { fetchPorts } from '@/next/core/net/preview';
import { pageClock } from '@/next/core/clock';
import type { MachineId } from '@/next/protocol/v2';

export const FRESH_MS = 15_000;

interface IRead {
  /** The listening ports; null while the first read is on its way or when it failed. */
  ports: ReadonlySet<number> | null;
  at: number;
  busy: boolean;
}

const reads = new Map<MachineId, IRead>();
const listeners = new Set<() => void>();
let version = 0;

const changed = () => {
  version += 1;
  for (const notify of [...listeners]) notify();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};
const snapshot = () => version;

const read = (machine: MachineId) => {
  const was = reads.get(machine);
  if (was?.busy || (was && was.ports !== null && pageClock().now() - was.at < FRESH_MS)) return;
  reads.set(machine, { ports: was?.ports ?? null, at: was?.at ?? 0, busy: true });
  void fetchPorts(machine, []).then(
    (list) => {
      reads.set(machine, {
        ports: new Set(list.filter((p) => p.responding).map((p) => p.port)),
        at: pageClock().now(),
        busy: false,
      });
      changed();
    },
    () => {
      reads.set(machine, { ports: was?.ports ?? null, at: pageClock().now(), busy: false });
      changed();
    },
  );
};

/** Whether the port listens on the machine; null while it is not known. */
export const usePortUp = (machine: MachineId | undefined, port: number | null): boolean | null => {
  const seen = useSyncExternalStore(subscribe, snapshot, snapshot);
  useEffect(() => {
    if (machine !== undefined && port !== null) read(machine);
  }, [machine, port]);
  return portUpOf(machine, port, seen);
};

/** What the reads say of the port. `_seen` is the store's version: the React Compiler memoizes this call on its
 *  arguments, so the version is one of them, or a new read would not show. */
const portUpOf = (machine: MachineId | undefined, port: number | null, _seen: number): boolean | null => {
  if (machine === undefined || port === null) return null;
  const ports = reads.get(machine)?.ports ?? null;
  return ports === null ? null : ports.has(port);
};

/** Forgets every read (tests). */
export const resetPortUp = (): void => {
  reads.clear();
  changed();
};
