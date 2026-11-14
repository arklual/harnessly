// «Не сейчас» on a machine's card: the card stays away for SNOOZE_MS, or until what the machine says changes
// level (another cause, or a warning that became a stop of the work). Kept in this browser per person (localStorage),
// shared by every window of the app here: the cluster keeps no «read» of the person a machine's card could use, so
// another device of the same person asks again once.
import { useSyncExternalStore } from 'react';

/** How long «Не сейчас» keeps a machine's card away while what it says holds. */
export const SNOOZE_MS = 6 * 60 * 60_000;

const PREFIX = 'hx-machine-snooze:';

/** Per machine: the level put away (`metric:stop|warn`) and until when. */
export type TSnoozes = Readonly<Record<string, { level: string; until: number }>>;

/** The level of a machine's card: what runs short and whether the work stops. */
export const snoozeLevel = (metric: string, urgent: boolean): string =>
  `${metric}:${urgent ? 'stop' : 'warn'}`;

/** Whether the card of `machine` at `level` is put away at `now`. */
export const isSnoozed = (snoozes: TSnoozes, machine: string, level: string, now: number): boolean => {
  const s = snoozes[machine];
  return s !== undefined && s.level === level && s.until > now;
};

/** Whether the card of `machine` at `level` is put away, by the store's list: it holds only the snoozes that still
 *  run (the ended ones are left out when it is read again, at the end of the earliest), so no clock is needed here. */
export const isPutAway = (snoozes: TSnoozes, machine: string, level: string): boolean =>
  snoozes[machine]?.level === level;

/** The snoozes still running at `now`, from what the storage holds; anything malformed is left out. */
export const parseSnoozes = (raw: string | null, now: number): TSnoozes => {
  if (!raw) return {};
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return {};
    const out: Record<string, { level: string; until: number }> = {};
    for (const [id, s] of Object.entries(value as Record<string, unknown>)) {
      const { level, until } = (s ?? {}) as { level?: unknown; until?: unknown };
      if (typeof level === 'string' && typeof until === 'number' && until > now) out[id] = { level, until };
    }
    return out;
  } catch {
    return {};
  }
};

const EMPTY: TSnoozes = {};
const listeners = new Set<() => void>();
let person = 'local';
let current: TSnoozes | null = null;
let timer: number | undefined;

const key = () => PREFIX + person;
const emit = () => {
  for (const listener of [...listeners]) listener();
};

/** When the earliest snooze ends, the list is read again: the card comes back without a reload. */
const schedule = (snoozes: TSnoozes) => {
  window.clearTimeout(timer);
  const ends = Object.values(snoozes).map((s) => s.until);
  if (ends.length === 0) return;
  const wait = Math.max(0, Math.min(...ends) - Date.now()) + 1000;
  timer = window.setTimeout(
    () => {
      current = null;
      emit();
    },
    Math.min(wait, 2 ** 31 - 1),
  );
};

export const snoozesSnapshot = (): TSnoozes => {
  if (current) return current;
  let raw: string | null = null;
  try {
    raw = window.localStorage.getItem(key());
  } catch {
    // Private mode: nothing is kept.
  }
  const read = parseSnoozes(raw, Date.now());
  current = Object.keys(read).length === 0 ? EMPTY : read;
  schedule(current);
  return current;
};

/** Whose snoozes these are; called while rendering, it tells no one: the next read is the person's own. */
export const setSnoozePerson = (next: string): void => {
  if (next === person) return;
  person = next;
  current = null;
};

/** Puts the card of `machine` at `level` away for SNOOZE_MS. */
export const snoozeMachine = (machine: string, level: string, now: number = Date.now()): void => {
  const next = { ...snoozesSnapshot(), [machine]: { level, until: now + SNOOZE_MS } };
  current = next;
  try {
    window.localStorage.setItem(key(), JSON.stringify(next));
  } catch {
    // Private mode or a full storage: the snooze holds for this page.
  }
  schedule(next);
  emit();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  const onStorage = (event: StorageEvent) => {
    if (event.key !== key()) return;
    current = null;
    listener();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('storage', onStorage);
  };
};

export const useMachineSnoozes = (): TSnoozes =>
  useSyncExternalStore(subscribe, snoozesSnapshot, () => EMPTY);
