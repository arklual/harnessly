import type { IFleetMachine } from '@/hooks/use-hl-machines';

/**
 * Machines whose conversations the shell draws itself, without their frame. The list comes with the gateway's
 * list of machines; the last one is kept on the device so a page opened on such a conversation knows it before that list.
 */
const STORAGE_KEY = 'hl-no-frame';

export const readNoFrameMachines = (): ReadonlySet<string> => {
  try {
    const raw = typeof window === 'undefined' ? null : window.localStorage.getItem(STORAGE_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return new Set(Array.isArray(parsed) ? parsed.filter((id): id is string => typeof id === 'string') : []);
  } catch {
    return new Set();
  }
};

export const noFrameMachinesOf = (
  machines: readonly IFleetMachine[] | undefined,
  known: ReadonlySet<string>,
): ReadonlySet<string> => {
  if (!machines) return known;
  const next = new Set(machines.filter((machine) => machine.noFrame === true).map((machine) => machine.id));
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify([...next]));
  } catch {
    // a private window keeps it for this page only
  }
  return next;
};
