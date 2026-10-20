import { useSyncExternalStore } from 'react';

const TICK_MS = 30_000;

let now = Date.now();
let timer: ReturnType<typeof setInterval> | undefined;
const listeners = new Set<() => void>();

const subscribe = (notify: () => void) => {
  listeners.add(notify);
  if (listeners.size === 1) {
    now = Date.now();
    timer = setInterval(() => {
      now = Date.now();
      listeners.forEach((listener) => listener());
    }, TICK_MS);
  }
  return () => {
    listeners.delete(notify);
    if (!listeners.size) clearInterval(timer);
  };
};

/** One shared clock for the ages of every row: they move on by themselves and all at once. */
const useMinuteClock = () =>
  useSyncExternalStore(
    subscribe,
    () => now,
    () => now,
  );

export default useMinuteClock;
