/**
 * Which machine of a fleet the page's runtime calls go to: the one its address names (`?machine=`, as a machine's
 * frame is opened), else the machine whose conversation the shell draws itself without a frame (the view machine),
 * else none — the home machine. HTTP calls and sockets ask this one place.
 */

/** The machine of the page's address; a frame has one, the shell's own page has none. */
export const addressMachineId = (): string | undefined => {
  if (typeof window === 'undefined') return undefined;
  return new URLSearchParams(window.location.search).get('machine') || undefined;
};

/**
 * The machine whose conversation the shell draws itself, without a frame: the shell's page has no `machine`
 * in its address, so its runtime calls — the conversation, its terminal, panel, statuses — go to this machine instead
 * of the home one, as they would inside that machine's frame. The shell's own lists, pins and settings ask the home
 * machine explicitly (`homeAppPath`, `homeFetch`). Unset means the home machine.
 */
let viewMachine: string | undefined;
const viewListeners = new Set<(machineId: string | undefined) => void>();

export const viewMachineId = (): string | undefined => viewMachine;

export const setViewMachine = (machineId: string | undefined): void => {
  if (machineId === viewMachine) return;
  viewMachine = machineId;
  for (const listener of viewListeners) listener(machineId);
};

/** Called after the view machine changed; sockets reconnect to it and stores load its data. */
export const onViewMachineChange = (listener: (machineId: string | undefined) => void): (() => void) => {
  viewListeners.add(listener);
  return () => viewListeners.delete(listener);
};

export const currentMachineId = (): string | undefined => addressMachineId() ?? viewMachine;
