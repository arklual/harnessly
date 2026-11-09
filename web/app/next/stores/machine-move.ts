// The move of each machine's data into the hub (features/settings/machine-move.ts), asked of the machine while it is
// on: every 15 s while it moves, every 5 minutes once it is over or stopped. One request per machine whoever shows it
// («Машины», the chat's bar).
import { useEffect } from 'react';
import { createKeyedStore, useKey } from '@/next/core/store';
import {
  loadMachineMove,
  moveDone,
  moveStopped,
  retryMachineMove,
  type IMachineMove,
} from '@/next/features/settings/machine-move';
import type { MachineId } from '@/next/protocol/v2';

const MOVING_MS = 15_000;
const QUIET_MS = 5 * 60_000;

/** null: the machine moves nothing (or does not say); absent: not asked yet. */
export const machineMoveStore = createKeyedStore<MachineId, IMachineMove | null>();

const watchers = new Map<MachineId, { n: number; timer: ReturnType<typeof setTimeout> | null }>();

const ask = async (machine: MachineId) => {
  const w = watchers.get(machine);
  if (!w) return;
  const move = await loadMachineMove(machine);
  machineMoveStore.set(machine, move);
  const again = watchers.get(machine);
  if (!again || again.n === 0) return;
  const quiet = move === null || moveDone(move) || moveStopped(move);
  again.timer = setTimeout(() => void ask(machine), quiet ? QUIET_MS : MOVING_MS);
};

/** The machine's move, asked while `on` (the machine answers) and something on the page shows it. */
export const useMachineMove = (machine: MachineId | null, on: boolean): IMachineMove | null | undefined => {
  useEffect(() => {
    if (machine === null || !on) return;
    const w = watchers.get(machine) ?? { n: 0, timer: null };
    w.n++;
    watchers.set(machine, w);
    if (w.n === 1) void ask(machine);
    return () => {
      w.n--;
      if (w.n === 0) {
        if (w.timer !== null) clearTimeout(w.timer);
        watchers.delete(machine);
      }
    };
  }, [machine, on]);
  return useKey(machineMoveStore, machine);
};

/** «Повторить»: the move as the machine has it after; false when the machine did not take it. */
export const retryMove = async (machine: MachineId): Promise<boolean> => {
  const move = await retryMachineMove(machine);
  if (move === null) return false;
  machineMoveStore.set(machine, move);
  const w = watchers.get(machine);
  if (w) {
    if (w.timer !== null) clearTimeout(w.timer);
    w.timer = setTimeout(() => void ask(machine), MOVING_MS);
  }
  return true;
};
