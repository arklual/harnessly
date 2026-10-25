// The page's clock and the machines' clocks. Times on the wire are the machine's unix ms; welcome.serverTime gives
// the offset, so a wait's slowAt and deadlineAt compare with the machine's now, not the browser's.

import type { MachineId, Ms } from '@/next/protocol/v2';

export interface Clock {
  now(): Ms;
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(handle: unknown): void;
}

export const systemClock: Clock = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
};

let clock: Clock = systemClock;

/** The clock of every timer of the core; tests put a fake one. */
export const pageClock = (): Clock => clock;
export function setPageClock(next: Clock): void {
  clock = next;
}

const offsets = new Map<MachineId, number>();

/** Notes the machine's time at the moment its welcome arrived. */
export function noteServerTime(machine: MachineId, serverTime: Ms, at: Ms = clock.now()): void {
  offsets.set(machine, serverTime - at);
}

/** Now by the machine's clock: what a Wait of that machine compares with. */
export function machineNow(machine: MachineId): Ms {
  return clock.now() + (offsets.get(machine) ?? 0);
}

/** A machine time as the page's time, for timers. */
export function toPageTime(machine: MachineId, at: Ms): Ms {
  return at - (offsets.get(machine) ?? 0);
}

export function resetClockOffsets(): void {
  offsets.clear();
}
